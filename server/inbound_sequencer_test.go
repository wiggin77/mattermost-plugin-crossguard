package main

import (
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/wire"
)

// syncEnv builds a minimal sync_msg envelope for the sequencer tests.
// All envelopes in a given test share the same channel ID unless
// otherwise specified, so the per-channel cursor is the focus.
func syncEnv(channelID, epoch string, seq uint64) *TransportEnvelope {
	return &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    "low-to-high",
		Timestamp:   "2026-05-13T10:00:00Z",
		Epoch:       epoch,
		Sequence:    seq,
		TeamName:    "team-a",
		ChannelName: "channel-a",
		SyncMsg: &wire.SyncMsg{
			Id:        "sm-test",
			ChannelId: channelID,
		},
	}
}

// newTestSequencer constructs a sequencer with permissive caps for
// state-machine tests. Buffer caps are tightened individually in the
// overflow tests below. The byte estimator returns a fixed 100 bytes
// per envelope so byte-overflow tests are deterministic.
func newTestSequencer(t *testing.T) (*inboundSequencer, *plugintest.API) {
	t.Helper()
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
	seq := newInboundSequencer(api, 30*time.Second, 200, 50*1024*1024)
	seq.bytesPerEnvelope = func(*TransportEnvelope) int { return 100 }
	return seq, api
}

// expectLog asserts that at least one log call carried the named
// `error_code` value among its key/value pairs. Used to verify
// audit-code emissions.
func expectLog(t *testing.T, api *plugintest.API, method string, code int) {
	t.Helper()
	for _, call := range api.Calls {
		if call.Method != method {
			continue
		}
		for i := 1; i+1 < len(call.Arguments); i += 2 {
			if k, ok := call.Arguments[i].(string); ok && k == "error_code" {
				if v, ok := call.Arguments[i+1].(int); ok && v == code {
					return
				}
			}
		}
	}
	t.Errorf("expected %s log call with error_code=%d, none found among %d %s calls",
		method, code, countCalls(api, method), method)
}

func countCalls(api *plugintest.API, method string) int {
	n := 0
	for _, call := range api.Calls {
		if call.Method == method {
			n++
		}
	}
	return n
}

// ----------------------------------------------------------------------
// State machine tests
// ----------------------------------------------------------------------

func TestSequencerInOrder(t *testing.T) {
	seq, api := newTestSequencer(t)

	for i := uint64(1); i <= 3; i++ {
		got := seq.Admit("low-to-high", syncEnv("ch1", "epochA", i))
		require.Len(t, got, 1, "in-order seq %d should dispatch immediately", i)
		assert.Equal(t, i, got[0].Sequence)
	}
	expectLog(t, api, "LogDebug", 15300) // InboundSeqInOrder
}

func TestSequencerSingleGapFilled(t *testing.T) {
	seq, api := newTestSequencer(t)

	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 1)), 1)
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 3)), "seq=3 should be buffered while waiting for 2")
	expectLog(t, api, "LogWarn", 15301) // InboundSeqGapDetected

	got := seq.Admit("c", syncEnv("ch1", "epochA", 2))
	require.Len(t, got, 2, "filling the gap should drain 2 and 3")
	assert.Equal(t, uint64(2), got[0].Sequence)
	assert.Equal(t, uint64(3), got[1].Sequence)
	expectLog(t, api, "LogInfo", 15302) // InboundSeqGapFilled
}

func TestSequencerMultiGapDrain(t *testing.T) {
	seq, _ := newTestSequencer(t)

	// In-order arrivals: 1.
	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 1)), 1)
	// Out-of-order: 4 buffers (waiting for 2).
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 4)))
	// 2 arrives: dispatches alone because 3 still missing.
	got := seq.Admit("c", syncEnv("ch1", "epochA", 2))
	require.Len(t, got, 1)
	assert.Equal(t, uint64(2), got[0].Sequence)
	// 3 arrives: drains [3, 4] (next contiguous run).
	got = seq.Admit("c", syncEnv("ch1", "epochA", 3))
	require.Len(t, got, 2)
	assert.Equal(t, uint64(3), got[0].Sequence)
	assert.Equal(t, uint64(4), got[1].Sequence)
	// 5 dispatches in order.
	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 5)), 1)
}

func TestSequencerReactionBeforePost(t *testing.T) {
	// Models the motivating regression: the sender emits a post at
	// seq=N then a reaction at seq=N+1; the transport reorders so the
	// reaction arrives first. The sequencer holds the reaction until
	// the post arrives, then dispatches both.
	seq, _ := newTestSequencer(t)

	reactionEnv := syncEnv("ch1", "epochA", 2)
	reactionEnv.SyncMsg.Reactions = []*wire.Reaction{{
		UserId: "u1", PostId: "p1", EmojiName: "thumbsup",
	}}
	postEnv := syncEnv("ch1", "epochA", 1)
	postEnv.SyncMsg.Post = &wire.Post{Id: "p1", Message: "hello"}

	require.Empty(t, seq.Admit("c", reactionEnv), "reaction at seq=2 buffered while post at seq=1 not yet seen")

	got := seq.Admit("c", postEnv)
	require.Len(t, got, 2)
	assert.Equal(t, "p1", got[0].SyncMsg.Post.Id, "post dispatched first")
	assert.Equal(t, "thumbsup", got[1].SyncMsg.Reactions[0].EmojiName, "reaction dispatched after post")
}

func TestSequencerGapTimeout(t *testing.T) {
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
	// Short timeout so the test runs fast.
	seq := newInboundSequencer(api, 5*time.Millisecond, 200, 50*1024*1024)
	seq.bytesPerEnvelope = func(*TransportEnvelope) int { return 100 }

	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 1)), 1)
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 3)))

	time.Sleep(20 * time.Millisecond)
	released := seq.TickGapDeadlines(time.Now())
	require.Len(t, released, 1, "seq=3 should be released after timeout")
	assert.Equal(t, uint64(3), released[0].Sequence)
	expectLog(t, api, "LogError", 15303) // InboundSeqGapTimeout
}

func TestSequencerPersistentGap(t *testing.T) {
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
	seq := newInboundSequencer(api, 5*time.Millisecond, 200, 50*1024*1024)
	seq.bytesPerEnvelope = func(*TransportEnvelope) int { return 100 }

	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 1)), 1)
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 5)))

	time.Sleep(20 * time.Millisecond)
	released := seq.TickGapDeadlines(time.Now())
	require.Len(t, released, 1)
	assert.Equal(t, uint64(5), released[0].Sequence)
	// Audit log should reflect a 3-wide gap (seqs 2, 3, 4).
	expectLog(t, api, "LogError", 15303)
}

func TestSequencerDuplicateBelowCursor(t *testing.T) {
	seq, api := newTestSequencer(t)

	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 1)), 1)
	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 2)), 1)
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 2)),
		"seq=2 already dispatched should be discarded")
	expectLog(t, api, "LogWarn", 15305) // InboundSeqDuplicate
}

func TestSequencerDuplicateBuffered(t *testing.T) {
	seq, api := newTestSequencer(t)

	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 1)), 1)
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 3)))
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 3)),
		"duplicate of buffered seq is discarded")
	expectLog(t, api, "LogWarn", 15305)
}

func TestSequencerEpochResetEmptyBuffer(t *testing.T) {
	seq, api := newTestSequencer(t)

	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 1)), 1)
	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 2)), 1)
	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 3)), 1)

	got := seq.Admit("c", syncEnv("ch1", "epochB", 1))
	require.Len(t, got, 1, "new epoch starts fresh from seq=1")
	assert.Equal(t, "epochB", got[0].Epoch)
	expectLog(t, api, "LogInfo", 15306) // InboundSeqEpochReset
}

func TestSequencerEpochResetNonOneBaseline(t *testing.T) {
	// A new epoch's first envelope can carry any Sequence value; the
	// receiver adopts whatever it sees as the cursor baseline rather
	// than insisting on seq=1. This covers the case where a sender's
	// counter strategy persists across epochs (e.g., KV-backed) and
	// the new epoch resumes at seq=N instead of seq=1, and it also
	// avoids a 30-second gap-fill wait on the very first envelope
	// after a sender restart.
	seq, api := newTestSequencer(t)

	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 1)), 1)

	// New epoch first envelope arrives at seq=20 (e.g., sender counter
	// did not reset on restart). Should dispatch immediately.
	got := seq.Admit("c", syncEnv("ch1", "epochB", 20))
	require.Len(t, got, 1, "first seq of new epoch dispatches in-order regardless of value")
	assert.Equal(t, "epochB", got[0].Epoch)
	assert.Equal(t, uint64(20), got[0].Sequence)

	// Subsequent envelope in the same epoch must be monotonic from the
	// adopted baseline. seq=21 dispatches in-order.
	got = seq.Admit("c", syncEnv("ch1", "epochB", 21))
	require.Len(t, got, 1)
	assert.Equal(t, uint64(21), got[0].Sequence)

	// seq=19 in the new epoch is below the baseline and counts as a
	// duplicate (or out-of-order arrival from an indistinguishable
	// pre-cursor seq).
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochB", 19)),
		"seq below the adopted baseline is treated as duplicate")
	expectLog(t, api, "LogInfo", 15306) // InboundSeqEpochReset
}

func TestSequencerEpochResetNonEmptyBuffer(t *testing.T) {
	seq, api := newTestSequencer(t)

	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 1)), 1)
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 3)))
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 4)))

	// New epoch arrives. Old-epoch buffered envelopes (seqs 3, 4) are
	// dispatched best-effort before the cursor resets; the new epoch's
	// seq=1 follows.
	got := seq.Admit("c", syncEnv("ch1", "epochB", 1))
	require.Len(t, got, 3, "old-epoch envelopes dispatch before new-epoch reset")
	assert.Equal(t, "epochA", got[0].Epoch)
	assert.Equal(t, uint64(3), got[0].Sequence)
	assert.Equal(t, "epochA", got[1].Epoch)
	assert.Equal(t, uint64(4), got[1].Sequence)
	assert.Equal(t, "epochB", got[2].Epoch)
	assert.Equal(t, uint64(1), got[2].Sequence)
	expectLog(t, api, "LogWarn", 15306) // InboundSeqEpochReset (dispatching old)
	expectLog(t, api, "LogInfo", 15306) // InboundSeqEpochReset (cursor reset)
}

func TestSequencerCrossChannelIsolation(t *testing.T) {
	seq, _ := newTestSequencer(t)

	// A's gap should not affect B.
	require.Len(t, seq.Admit("c", syncEnv("chA", "epochA", 1)), 1)
	require.Empty(t, seq.Admit("c", syncEnv("chA", "epochA", 3)),
		"chA gap")

	// Channel B is independent and should dispatch in order.
	for i := uint64(1); i <= 3; i++ {
		got := seq.Admit("c", syncEnv("chB", "epochA", i))
		require.Len(t, got, 1, "chB seq %d should dispatch immediately", i)
	}
}

func TestSequencerBufferOverflowByCount(t *testing.T) {
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
	seq := newInboundSequencer(api, 30*time.Second, 3, 50*1024*1024)
	seq.bytesPerEnvelope = func(*TransportEnvelope) int { return 100 }

	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 1)), 1)
	// Buffer seqs 3, 4, 5 (3 envelopes, at cap, all waiting for 2).
	for _, s := range []uint64{3, 4, 5} {
		require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", s)))
	}
	// Adding seq 6 overflows. Best-effort policy: dispatch every held
	// envelope (3, 4, 5, 6) in seq order so the receiver can salvage
	// what it can. Only the missing predecessor seq=2 is lost.
	got := seq.Admit("c", syncEnv("ch1", "epochA", 6))
	require.Len(t, got, 4, "overflow drains the full buffer in seq order")
	assert.Equal(t, uint64(3), got[0].Sequence)
	assert.Equal(t, uint64(4), got[1].Sequence)
	assert.Equal(t, uint64(5), got[2].Sequence)
	assert.Equal(t, uint64(6), got[3].Sequence)
	expectLog(t, api, "LogError", 15304) // InboundSeqBufferOverflow

	// Late seq=2 is below cursor (advanced to 7) so it is discarded.
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 2)))
}

func TestSequencerBufferOverflowByBytes(t *testing.T) {
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
	// Allow 10 envelopes by count but only 200 bytes (2 envelopes by
	// byte size at 100 each). seqs 3 and 4 fit; seq 5 triggers overflow.
	seq := newInboundSequencer(api, 30*time.Second, 10, 200)
	seq.bytesPerEnvelope = func(*TransportEnvelope) int { return 100 }

	require.Len(t, seq.Admit("c", syncEnv("ch1", "epochA", 1)), 1)
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 3)))
	require.Empty(t, seq.Admit("c", syncEnv("ch1", "epochA", 4)))
	// Adding seq 5 takes the buffer to 300 bytes > 200 cap. Best-effort
	// dispatch: 3, 4, 5 all flow to the receiver in seq order.
	got := seq.Admit("c", syncEnv("ch1", "epochA", 5))
	require.Len(t, got, 3)
	assert.Equal(t, uint64(3), got[0].Sequence)
	assert.Equal(t, uint64(4), got[1].Sequence)
	assert.Equal(t, uint64(5), got[2].Sequence)
	expectLog(t, api, "LogError", 15304)
}

func TestSequencerMissingEpochField(t *testing.T) {
	seq, api := newTestSequencer(t)

	env := syncEnv("ch1", "", 1)
	got := seq.Admit("c", env)
	require.Len(t, got, 1, "missing epoch passes through")
	expectLog(t, api, "LogWarn", 15308) // InboundSeqMissingFields
}

func TestSequencerMissingSequenceField(t *testing.T) {
	seq, api := newTestSequencer(t)

	env := syncEnv("ch1", "epochA", 0)
	got := seq.Admit("c", env)
	require.Len(t, got, 1, "missing sequence passes through")
	expectLog(t, api, "LogWarn", 15308)
}

func TestSequencerNonSyncMsgPassthrough(t *testing.T) {
	seq, _ := newTestSequencer(t)

	testEnv := &TransportEnvelope{
		Version:  1,
		Type:     TransportTypeTest,
		ConnName: "low-to-high",
		Epoch:    "epochA",
		TestID:   "t1",
	}
	got := seq.Admit("c", testEnv)
	require.Len(t, got, 1, "test envelope passes through")
	assert.Equal(t, "t1", got[0].TestID)
}

func TestSequencerCheckpointLoadThenEpochChange(t *testing.T) {
	// The poc1/poc2 production bug, captured as a regression test.
	// Setup: a previous sender session committed cursor (epochA, next=20)
	// to KV. After a sender restart the receiver loads that checkpoint,
	// then the first new-epoch envelope arrives. Pre-fix behavior was to
	// reset to (epochB, next=1), buffer the envelope at seq=20, wait for
	// the gap timeout to fire (30 s default), and only then dispatch.
	// Fixed behavior: adopt the incoming envelope's Sequence as the new
	// baseline and dispatch immediately.
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
	seq := newInboundSequencer(api, 30*time.Second, 200, 50*1024*1024)
	seq.bytesPerEnvelope = func(*TransportEnvelope) int { return 100 }
	seq.loadCursor = func(connName, channelID string) (string, uint64, error) {
		if connName == "c" && channelID == "ch1" {
			return "epochA", 20, nil
		}
		return "", 0, nil
	}

	// First envelope of the new epoch arrives. The sender's persisted
	// counter might have continued from the old epoch (seq=20) or might
	// have reset to seq=1; either way the receiver must dispatch
	// immediately, not wait for a gap timeout.
	got := seq.Admit("c", syncEnv("ch1", "epochB", 20))
	require.Len(t, got, 1, "checkpointed cursor + new epoch must dispatch immediately, no gap wait")
	assert.Equal(t, "epochB", got[0].Epoch)
	assert.Equal(t, uint64(20), got[0].Sequence)

	// Subsequent envelope under the same new epoch is monotonic from
	// the adopted baseline.
	got = seq.Admit("c", syncEnv("ch1", "epochB", 21))
	require.Len(t, got, 1)
	assert.Equal(t, uint64(21), got[0].Sequence)

	expectLog(t, api, "LogInfo", 15401) // InboundSeqCheckpointLoaded
	expectLog(t, api, "LogInfo", 15306) // InboundSeqEpochReset
}

func TestSequencerCheckpointRoundTrip(t *testing.T) {
	// Simulate a leadership handoff: original sequencer runs, takes a
	// snapshot, a fresh sequencer loads from that snapshot via the
	// cursorLoader hook and resumes without re-dispatching the
	// already-processed seqs.
	api1 := &plugintest.API{}
	registerLogMocks(api1, "LogInfo", "LogWarn", "LogError", "LogDebug")
	original := newInboundSequencer(api1, 30*time.Second, 200, 50*1024*1024)
	original.bytesPerEnvelope = func(*TransportEnvelope) int { return 100 }

	for i := uint64(1); i <= 5; i++ {
		require.Len(t, original.Admit("c", syncEnv("ch1", "epochA", i)), 1)
	}
	snap := original.Snapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, "c", snap[0].ConnName)
	assert.Equal(t, "ch1", snap[0].ChannelID)
	assert.Equal(t, "epochA", snap[0].Epoch)
	assert.Equal(t, uint64(6), snap[0].NextExpected)

	api2 := &plugintest.API{}
	registerLogMocks(api2, "LogInfo", "LogWarn", "LogError", "LogDebug")
	resumed := newInboundSequencer(api2, 30*time.Second, 200, 50*1024*1024)
	resumed.bytesPerEnvelope = func(*TransportEnvelope) int { return 100 }
	resumed.loadCursor = func(connName, channelID string) (string, uint64, error) {
		for _, e := range snap {
			if e.ConnName == connName && e.ChannelID == channelID {
				return e.Epoch, e.NextExpected, nil
			}
		}
		return "", 0, nil
	}

	// Late redelivery of seq=5 (already dispatched on the previous
	// active node) should be suppressed as a duplicate.
	require.Empty(t, resumed.Admit("c", syncEnv("ch1", "epochA", 5)),
		"redelivered seq below loaded cursor is duplicate")
	// Fresh seq=6 should dispatch.
	got := resumed.Admit("c", syncEnv("ch1", "epochA", 6))
	require.Len(t, got, 1)
	assert.Equal(t, uint64(6), got[0].Sequence)
}

func TestSequencerTestEpochChangeDetection(t *testing.T) {
	seq, api := newTestSequencer(t)

	// First test envelope establishes the baseline epoch.
	seq.Admit("c", &TransportEnvelope{
		Version: 1, Type: TransportTypeTest, Epoch: "epochA", TestID: "t1",
	})
	// Second test envelope with a different epoch should audit a
	// restart event.
	seq.Admit("c", &TransportEnvelope{
		Version: 1, Type: TransportTypeTest, Epoch: "epochB", TestID: "t2",
	})
	expectLog(t, api, "LogInfo", 15310) // InboundSeqTestEpochChange
}
