package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/testreorder"
)

// TestIntegrationReorderNATS verifies that the sequencer correctly
// reassembles a stream of posts after the sender-side transport
// injects 50% adjacent-pair reordering. This is the most common
// real-world reordering vector (queue-group race between two
// workers, message-redelivery overlap, etc).
//
// The test publishes 20 posts, asserts the receiver dispatched all
// 20 by post.Id (idempotent set), and asserts the sequence dispatched
// is monotonically non-decreasing per the sequencer's design contract.
func TestIntegrationReorderNATS(t *testing.T) {
	cfg := testreorder.Config{
		Mode:        testreorder.ModeSwapAdjacent,
		Probability: 0.5,
		Seed:        0xC0FFEE,
	}
	h := newIntegrationHarness(t, "crossguard.test.reorder", "low-to-high", cfg, 30*time.Second)

	const n = 20
	for i := 1; i <= n; i++ {
		h.sender.SendSyncMsg(t, "ch-test", makePost("p-"+strconv.Itoa(i), "body-"+strconv.Itoa(i)))
	}

	// Flush any payloads still held back inside the wrapper (the
	// last swap-buffer entry, if any). This also gives the embedded
	// NATS server time to drain to the subscriber.
	h.wrapped.Flush()

	h.WaitForDispatchCount(t, n, 5*time.Second)

	dispatched := h.receiver.Snapshot()
	require.Len(t, dispatched, n, "all posts should be dispatched despite reordering")

	// Assert every post id is present.
	got := extractPostIDs(dispatched)
	for i := 1; i <= n; i++ {
		assert.Contains(t, got, "p-"+strconv.Itoa(i))
	}

	// Sequence numbers dispatched must be non-decreasing per the
	// sequencer's monotonicity contract.
	var prev uint64
	for i, env := range dispatched {
		assert.GreaterOrEqual(t, env.Sequence, prev,
			"dispatched seq at index %d not monotonic (%d before %d)", i, prev, env.Sequence)
		prev = env.Sequence
	}
}
