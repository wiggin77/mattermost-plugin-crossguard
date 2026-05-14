package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/testreorder"
)

// TestIntegrationCrossChannelIsolation verifies that a gap on one
// channel does not stall delivery on another channel. The sequencer
// maintains per-(connName, channelID) cursors, so a missing seq on
// channel A should not delay seqs arriving on channel B.
//
// The test injects loss only on every other publish using
// ModeDelayOneInN with a long hold, simulating channel A being
// stuck waiting for a never-arriving predecessor. Channel B's
// envelopes should dispatch promptly while channel A waits.
func TestIntegrationCrossChannelIsolation(t *testing.T) {
	// Use passthrough at the wrapper level; we'll simulate per-channel
	// loss by tracking which channel we're publishing to and pretending
	// a particular seq on channel A is dropped.
	cfg := testreorder.Config{Mode: testreorder.ModePassthrough}
	h := newIntegrationHarness(t, "crossguard.test.crosschan", "low-to-high", cfg, 200*time.Millisecond)

	// Send 5 posts on channel-A, leaving a gap at seq=3.
	for i := 1; i <= 5; i++ {
		if i == 3 {
			// Skip seq=3 by manually advancing the per-(conn,channel)
			// counter so the next publish goes out with seq=4. This
			// reproduces a transport drop where seq=3 was lost.
			h.sender.seqMu.Lock()
			h.sender.seqs[h.sender.connName+"\x00ch-A"]++
			h.sender.seqMu.Unlock()
			continue
		}
		h.sender.SendSyncMsg(t, "ch-A", makePostWithChannel("a-"+strconv.Itoa(i), "ch-A", "body-A-"+strconv.Itoa(i)))
	}

	// Send 4 posts on channel-B, all in order.
	for i := 1; i <= 4; i++ {
		h.sender.SendSyncMsg(t, "ch-B", makePostWithChannel("b-"+strconv.Itoa(i), "ch-B", "body-B-"+strconv.Itoa(i)))
	}

	// Channel B should dispatch promptly: 4 envelopes. Channel A's
	// post p-a-1 and p-a-2 dispatch in order, then the receiver holds
	// p-a-4, p-a-5 behind the missing seq=3.
	require.Eventually(t, func() bool {
		got := extractPostIDs(h.receiver.Snapshot())
		// channel B (4) + channel A's first two (a-1, a-2) = 6
		return len(got) >= 6 &&
			contains(got, "b-1") && contains(got, "b-2") && contains(got, "b-3") && contains(got, "b-4") &&
			contains(got, "a-1") && contains(got, "a-2")
	}, 2*time.Second, 10*time.Millisecond,
		"channel B should dispatch independent of channel A's gap")

	// Channel A's later seqs are still held.
	got := extractPostIDs(h.receiver.Snapshot())
	assert.NotContains(t, got, "a-4", "channel A held behind missing seq=3")
	assert.NotContains(t, got, "a-5")

	// Force gap-timeout on channel A. The held envelopes should now
	// dispatch (best-effort drain past missing seqs).
	time.Sleep(250 * time.Millisecond)
	h.receiver.DrainGapTimeouts()

	got = extractPostIDs(h.receiver.Snapshot())
	assert.Contains(t, got, "a-4", "after gap timeout, channel A's held envelopes dispatch")
	assert.Contains(t, got, "a-5")
}

func contains(s map[string]struct{}, id string) bool {
	_, ok := s[id]
	return ok
}
