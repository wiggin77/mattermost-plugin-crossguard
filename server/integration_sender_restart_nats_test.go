package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/testreorder"
)

// TestIntegrationSenderRestartNATS is the end-to-end regression test
// for the production poc1->poc2 stall bug. The shape:
//
//  1. Sender publishes N posts under epoch E1, seqs 1..N.
//  2. Sender "restarts" (new epoch E2, counter wiped).
//  3. Sender publishes one more post under epoch E2, seq=1.
//
// The receiver must dispatch the post-restart envelope immediately
// without waiting for the 30-second gap timeout. This is what the
// in-cluster tests (TestSequencerCheckpointLoadThenEpochChange,
// TestOutboundInboundRoundTripAcrossSenderRestart) cover at the
// unit level; this test pins the same shape end-to-end over a real
// embedded NATS transport.
func TestIntegrationSenderRestartNATS(t *testing.T) {
	cfg := testreorder.Config{Mode: testreorder.ModePassthrough}
	h := newIntegrationHarness(t, "crossguard.test.restart", "low-to-high", cfg, 30*time.Second)

	// Phase 1: send 5 posts under epoch E1.
	const preRestart = 5
	for i := 1; i <= preRestart; i++ {
		h.sender.SendSyncMsg(t, "ch-test", makePost("pre-"+strconv.Itoa(i), "pre-body-"+strconv.Itoa(i)))
	}
	h.WaitForDispatchCount(t, preRestart, 2*time.Second)

	// Verify all five dispatched cleanly.
	dispatched := h.receiver.Snapshot()
	require.Len(t, dispatched, preRestart)
	preEpoch := dispatched[0].Epoch
	for _, env := range dispatched {
		assert.Equal(t, preEpoch, env.Epoch, "all pre-restart envelopes share epoch E1")
	}

	// Phase 2: sender restart. New epoch, fresh counter.
	h.sender.Restart()
	require.NotEqual(t, preEpoch, h.sender.epoch, "Restart must mint a new epoch")

	// Phase 3: send one more post post-restart. Receiver must dispatch
	// this within a short window, NOT after 30s.
	start := time.Now()
	h.sender.SendSyncMsg(t, "ch-test", makePost("post-1", "post-body"))

	// Allow generous slack in CI but assert it's well under 30s.
	deadline := start.Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := extractPostIDs(h.receiver.Snapshot())
		if _, ok := got["post-1"]; ok {
			elapsed := time.Since(start)
			t.Logf("post-restart envelope dispatched in %s", elapsed)
			assert.Less(t, elapsed, 30*time.Second, "regression: post-restart envelope must not block on gap timeout")
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("post-restart envelope never dispatched; dispatched=%v",
		extractPostIDs(h.receiver.Snapshot()))
}
