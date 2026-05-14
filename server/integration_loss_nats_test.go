package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/testreorder"
)

// TestIntegrationLossNATS asserts the sequencer correctly audits and
// continues after transport-layer message loss. The wrapper drops 5%
// of publishes; the receiver should:
//   - Dispatch every delivered envelope (no silent extra loss).
//   - Emit at least one InboundSeqGapTimeout audit covering the lost
//     range when the gap deadline expires.
func TestIntegrationLossNATS(t *testing.T) {
	cfg := testreorder.Config{
		Mode:        testreorder.ModeLoss,
		Probability: 0.05,
		Seed:        0xB16B00B5,
	}
	// Use a short gap timeout so the test does not have to wait 30s.
	gapTO := 200 * time.Millisecond
	h := newIntegrationHarness(t, "crossguard.test.loss", "low-to-high", cfg, gapTO)

	const n = 60
	for i := 1; i <= n; i++ {
		h.sender.SendSyncMsg(t, "ch-test", makePost("p-"+strconv.Itoa(i), "body-"+strconv.Itoa(i)))
	}

	// Give the embedded NATS server time to deliver the survivors.
	time.Sleep(50 * time.Millisecond)

	// Force the gap-timeout path: any seq held in the reorder buffer
	// waiting for a dropped predecessor will be released here.
	h.receiver.DrainGapTimeouts()

	// Some seqs may still be pending after the first drain if a tail
	// gap hadn't yet been opened; sleep a tick and drain once more.
	time.Sleep(gapTO + 50*time.Millisecond)
	h.receiver.DrainGapTimeouts()

	dispatched := h.receiver.Snapshot()
	// We expect some loss; assert at least half delivered (loose
	// bound to avoid flake under unlucky RNG runs of a 5% rate).
	require.GreaterOrEqual(t, len(dispatched), n/2,
		"at least half of envelopes should reach the receiver despite 5%% loss")
	require.Less(t, len(dispatched), n,
		"5%% loss config should drop some envelopes (none would suggest the wrapper is silently passing through)")

	// At least one gap-timeout audit must have fired.
	api := h.receiver.api
	found := false
	for _, call := range api.Calls {
		if call.Method != "LogError" {
			continue
		}
		for i := 1; i+1 < len(call.Arguments); i += 2 {
			k, _ := call.Arguments[i].(string)
			v, _ := call.Arguments[i+1].(int)
			if k == "error_code" && v == errcode.InboundSeqGapTimeout {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	assert.True(t, found, "expected at least one InboundSeqGapTimeout audit")
}
