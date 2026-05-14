package main

import (
	"math/rand"
	"testing"
	"testing/quick"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/wire"
)

// TestSequencerInvariants is a property-based test that asserts the
// correctness statements the sequencer's design guarantees against
// randomly generated reorder sequences:
//
//  1. No loss without audit. Every delivered sequence number either
//     appears in the dispatched output or triggers an audit log entry
//     that identifies it (InboundSeqGapTimeout covers a range, while
//     InboundSeqBufferOverflow and InboundSeqDuplicate include the seq
//     directly). The union of dispatched + audited covers every
//     delivered seq, so the receiver is never silently lossy.
//
//  2. Dispatched seqs are non-decreasing. The cursor only moves
//     forward; the sequencer never returns to a seq it has already
//     passed.
//
// The test generates a random permutation of seqs 1..N for varying N,
// optionally drops a fraction to simulate transport loss, and runs each
// permutation through the sequencer with a short gap-timeout to force
// the timeout path. The failing seed is printed for replay.
func TestSequencerInvariants(t *testing.T) {
	// testing/quick supports MaxCountScale via -quickchecks; defaults to 100.
	cfg := &quick.Config{
		MaxCount: 50,
		Rand:     rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // tests don't need crypto randomness
	}

	property := func(seedInt32 int32, sizeRaw uint8, dropPctRaw uint8) bool {
		// Constrain the inputs to a useful range. sizeRaw chosen modulo
		// to keep tests fast yet diverse; dropPctRaw modulo 30 caps loss
		// at 29% so we don't generate trivially-all-lost runs.
		n := int(sizeRaw)%40 + 4 // 4..43 envelopes
		dropPct := int(dropPctRaw) % 30

		seed := int64(seedInt32)
		return runOnePermutation(t, seed, n, dropPct)
	}

	if err := quick.Check(property, cfg); err != nil {
		t.Fatal(err)
	}
}

// runOnePermutation generates a random permutation of seqs 1..n,
// optionally drops a percentage, replays through the sequencer with a
// short gap-timeout, and verifies both invariants. Returns false on
// invariant violation; the seed printed in the t.Log line is enough to
// replay deterministically.
func runOnePermutation(t *testing.T, seed int64, n, dropPct int) bool {
	r := rand.New(rand.NewSource(seed)) //nolint:gosec // tests don't need crypto randomness

	// Generate the permutation of 1..n.
	seqs := make([]uint64, 0, n)
	for i := 1; i <= n; i++ {
		seqs = append(seqs, uint64(i)) //nolint:gosec // i in [1, 43]
	}
	r.Shuffle(len(seqs), func(i, j int) { seqs[i], seqs[j] = seqs[j], seqs[i] })

	// Apply transport loss: drop dropPct% of envelopes. Record what got
	// dropped so invariant 1 (no loss without audit) can verify.
	delivered := make([]uint64, 0, len(seqs))
	dropped := make(map[uint64]struct{})
	for _, s := range seqs {
		if r.Intn(100) < dropPct {
			dropped[s] = struct{}{}
			continue
		}
		delivered = append(delivered, s)
	}

	// Run through a real sequencer. Use a very short gap-timeout so the
	// timeout path can fire mid-run; use generous buffer caps so the
	// overflow path is unlikely on small N.
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
	const gapTO = 5 * time.Millisecond
	seq := newInboundSequencer(api, gapTO, 1000, 10*1024*1024)
	seq.bytesPerEnvelope = func(*TransportEnvelope) int { return 100 }

	const channelID = "ch-prop"
	const epoch = "abcdef0123456789abcdef0123"

	dispatched := make([]uint64, 0, n)
	for _, s := range delivered {
		env := propEnv(channelID, epoch, s)
		for _, e := range seq.Admit("conn-prop", env) {
			dispatched = append(dispatched, e.Sequence)
		}
		// Periodically sleep just enough that the gap timer can fire and
		// the TickGapDeadlines call below has work to do. Without this,
		// every test run completes in microseconds and gap timeouts
		// never trigger; with it, some runs do.
		if r.Intn(5) == 0 {
			time.Sleep(gapTO + time.Millisecond)
			for _, e := range seq.TickGapDeadlines(time.Now()) {
				dispatched = append(dispatched, e.Sequence)
			}
		}
	}

	// Final drain pass to flush anything still held.
	time.Sleep(gapTO + 2*time.Millisecond)
	for _, e := range seq.TickGapDeadlines(time.Now()) {
		dispatched = append(dispatched, e.Sequence)
	}

	// Invariant 1: every delivered seq appears in dispatched OR was
	// audited away (duplicate suppression, buffer-overflow drop, or
	// gap-timeout range). Transport-layer drops (in `dropped`) need not
	// be accounted for since the receiver never saw them.
	disp := make(map[uint64]struct{}, len(dispatched))
	for _, s := range dispatched {
		disp[s] = struct{}{}
	}
	audited := collectAuditedSeqs(api)
	for _, s := range delivered {
		if _, ok := disp[s]; ok {
			continue
		}
		if _, ok := audited[s]; ok {
			continue
		}
		t.Logf("invariant violation: delivered seq %d neither dispatched nor audited\n  seed=%d  n=%d  dropPct=%d\n  delivered=%v\n  dropped=%v\n  dispatched=%v\n  audited=%v",
			s, seed, n, dropPct, delivered, sortedKeys(dropped), dispatched, sortedKeys(audited))
		return false
	}

	// Invariant 2: dispatched seqs are monotonic. Strictly, the
	// sequencer may emit a buffered batch on overflow or gap-timeout in
	// sorted order even when nextExpected has not advanced past every
	// intermediate value. Within each call to Admit/Tick, the returned
	// slice is in seq order. Across calls, the cursor only moves
	// forward, so the overall dispatched sequence is non-decreasing
	// modulo the fact that overflow paths can emit later-seq envelopes
	// after the cursor has advanced past gaps. We assert non-decreasing,
	// which is what the design actually promises.
	for i := 1; i < len(dispatched); i++ {
		if dispatched[i] < dispatched[i-1] {
			// A backward jump is allowed only when the previous batch
			// was a buffer-overflow drain that advanced the cursor and
			// then the current envelope is a late arrival for a seq
			// that was inside the dropped range. Track whether that's
			// the case by looking at the audit log; here we use the
			// simpler assertion that dispatched is non-decreasing,
			// since the design (read the Admit logic carefully)
			// guarantees this for the in-order and gap-fill paths and
			// for the overflow/timeout drain paths individually.
			t.Logf("invariant violation: dispatched not non-decreasing at index %d (%d < %d)\n  seed=%d  n=%d  dropPct=%d\n  delivered=%v\n  dispatched=%v",
				i, dispatched[i], dispatched[i-1], seed, n, dropPct, delivered, dispatched)
			return false
		}
	}

	return true
}

// propEnv builds a sync_msg envelope for the property test. Distinct
// from syncEnv so the property test owns its own minimum envelope
// shape independent of state-machine test fixtures.
func propEnv(channelID, epoch string, seq uint64) *TransportEnvelope {
	return &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    "conn-prop",
		Timestamp:   "2026-05-13T10:00:00Z",
		Epoch:       epoch,
		Sequence:    seq,
		TeamName:    "team",
		ChannelName: "channel",
		SyncMsg: &wire.SyncMsg{
			Id:        "sm-prop",
			ChannelId: channelID,
		},
	}
}

// collectAuditedSeqs walks the API mock's recorded log calls and
// returns the set of sequence numbers the sequencer has audited as
// dropped or out-of-band. It recognizes three audit shapes:
//
//   - InboundSeqDuplicate: a single "seq" K/V on the call.
//   - InboundSeqGapTimeout / InboundSeqBufferOverflow: "missing_from"
//     and "missing_to" K/V pairs that name an inclusive range.
//
// Any other audit code is ignored; only these three actually represent
// "delivered but not dispatched" outcomes.
func collectAuditedSeqs(api *plugintest.API) map[uint64]struct{} {
	out := make(map[uint64]struct{})
	for _, call := range api.Calls {
		if call.Method != "LogWarn" && call.Method != "LogError" {
			continue
		}
		code, seq, missingFrom, missingTo, hasCode, hasSeq, hasMF, hasMT := parseLogArgs(call.Arguments)
		if !hasCode {
			continue
		}
		switch code {
		case errcode.InboundSeqDuplicate:
			if hasSeq {
				out[seq] = struct{}{}
			}
		case errcode.InboundSeqGapTimeout, errcode.InboundSeqBufferOverflow:
			if hasMF && hasMT {
				for v := missingFrom; v <= missingTo; v++ {
					out[v] = struct{}{}
				}
			}
		}
	}
	return out
}

func parseLogArgs(args []any) (code int, seq, missingFrom, missingTo uint64, hasCode, hasSeq, hasMF, hasMT bool) {
	// args[0] is the log message; key/value pairs begin at index 1.
	for i := 1; i+1 < len(args); i += 2 {
		k, ok := args[i].(string)
		if !ok {
			continue
		}
		switch k {
		case "error_code":
			if v, ok := args[i+1].(int); ok {
				code = v
				hasCode = true
			}
		case "seq":
			if v, ok := args[i+1].(uint64); ok {
				seq = v
				hasSeq = true
			}
		case "missing_from":
			if v, ok := args[i+1].(uint64); ok {
				missingFrom = v
				hasMF = true
			}
		case "missing_to":
			if v, ok := args[i+1].(uint64); ok {
				missingTo = v
				hasMT = true
			}
		}
	}
	return code, seq, missingFrom, missingTo, hasCode, hasSeq, hasMF, hasMT
}

func sortedKeys(m map[uint64]struct{}) []uint64 {
	out := make([]uint64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Tiny manual sort to avoid a slices import dependency in a test file.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// TestSequencerInvariantsKnownFailingShapes runs hand-picked patterns
// that have historically exposed sequencer bugs, on top of the
// quick-generated shapes above. Acts as the regression net: even when
// the random generator is unlucky, these specific shapes always run.
func TestSequencerInvariantsKnownFailingShapes(t *testing.T) {
	cases := []struct {
		name      string
		delivered []uint64
		dropped   []uint64
	}{
		{
			name:      "perfect order",
			delivered: []uint64{1, 2, 3, 4, 5},
		},
		{
			name:      "reversed",
			delivered: []uint64{5, 4, 3, 2, 1},
		},
		{
			name:      "adjacent swap",
			delivered: []uint64{2, 1, 4, 3, 6, 5},
		},
		{
			name:      "gap at start",
			delivered: []uint64{3, 4, 5},
			dropped:   []uint64{1, 2},
		},
		{
			name:      "gap in middle then fills",
			delivered: []uint64{1, 2, 5, 3, 4},
		},
		{
			name:      "lone island then gap timeout",
			delivered: []uint64{5},
			dropped:   []uint64{1, 2, 3, 4},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := &plugintest.API{}
			registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
			const gapTO = 5 * time.Millisecond
			seq := newInboundSequencer(api, gapTO, 1000, 10*1024*1024)
			seq.bytesPerEnvelope = func(*TransportEnvelope) int { return 100 }

			dispatched := make([]uint64, 0, len(tc.delivered))
			for _, s := range tc.delivered {
				for _, e := range seq.Admit("conn", propEnv("ch", "epoch01aaaaaaaaaaaaaaaaaaa", s)) {
					dispatched = append(dispatched, e.Sequence)
				}
			}
			// Force any held-but-timed-out envelopes out.
			time.Sleep(gapTO + 2*time.Millisecond)
			for _, e := range seq.TickGapDeadlines(time.Now()) {
				dispatched = append(dispatched, e.Sequence)
			}

			// Invariant 1: every delivered seq dispatched.
			disp := make(map[uint64]struct{}, len(dispatched))
			for _, s := range dispatched {
				disp[s] = struct{}{}
			}
			for _, s := range tc.delivered {
				require.Contains(t, disp, s, "delivered seq %d not dispatched in case %q", s, tc.name)
			}

			// Invariant 2: monotonic non-decreasing.
			for i := 1; i < len(dispatched); i++ {
				require.GreaterOrEqual(t, dispatched[i], dispatched[i-1],
					"dispatched not monotonic at index %d in case %q (dispatched=%v)",
					i, tc.name, dispatched)
			}
		})
	}
}
