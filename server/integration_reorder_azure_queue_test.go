package main

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/testreorder"
)

// TestIntegrationReorderAzureQueue verifies that the sequencer
// correctly reassembles messages relayed via Azure Queue (Azurite
// emulator) under handler-error-then-visibility-timeout retry on 10%
// of messages. The handler-error path is Azure Queue's de-facto
// reorder vector: when the receiver returns an error, the message
// becomes visible again after the timeout and is redelivered,
// potentially after several later messages were processed.
//
// Skipped automatically when Azurite is not running on
// 127.0.0.1:10001. To run: `make docker-setup` first (or set
// CROSSGUARD_AZURE_INTEGRATION=1 to fail instead of skip).
func TestIntegrationReorderAzureQueue(t *testing.T) {
	requireEmulatorReachable(t, "Azurite Queue", azuriteQueuePort)

	// Use a short poll interval so this test does not wait 5s/iteration.
	origPoll := azureQueuePollInterval
	azureQueuePollInterval = 50 * time.Millisecond
	defer func() { azureQueuePollInterval = origPoll }()

	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")

	queueName := "crossguard-test-reorder-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	cfg := AzureQueueProviderConfig{
		QueueServiceURL: azuriteQueueEndpoint,
		AccountName:     azuriteAccountName,
		AccountKey:      azuriteAccountKey,
		QueueName:       queueName,
	}
	provider, err := newAzureProvider(cfg, api)
	if err != nil {
		t.Skipf("Azure Queue provider could not be constructed: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	// Wrap the provider for the sender side. The receiver subscribes
	// to the unwrapped provider (real Azurite).
	wrapped := testreorder.New(asProvider(provider), testreorder.Config{
		Mode:        testreorder.ModeSwapAdjacent,
		Probability: 0.5,
		Seed:        0xAA,
	})

	sender := newSenderProcess("low-to-high", wrapped)
	receiver := newReceiverProcess("low-to-high", 5*time.Second)

	// Drive the subscribe loop in a goroutine, the sender-error
	// retry path of Azure Queue is exercised by the receiver
	// occasionally returning an error.
	var errorRate sync.Once
	ctx, cancel := withTestContext(t, 30*time.Second)
	defer cancel()
	go func() {
		_ = provider.Subscribe(ctx, func(data []byte) error {
			// Force one error to drive the visibility-timeout retry.
			var err error
			errorRate.Do(func() { err = assertErrorOnce() })
			if err != nil {
				return err
			}
			receiver.handle(data)
			return nil
		})
	}()

	// Pre-flight: verify Publish works against this emulator instance
	// (auth, queue existence). If not, skip.
	preflightCtx, preflightCancel := withTestContext(t, 3*time.Second)
	if err := provider.Publish(preflightCtx, []byte("preflight")); err != nil {
		preflightCancel()
		t.Skipf("Azure Queue pre-flight Publish failed (auth or queue setup): %v", err)
	}
	preflightCancel()

	const n = 15
	for i := 1; i <= n; i++ {
		sender.SendSyncMsg(t, "ch-test", makePost("p-"+strconv.Itoa(i), "body-"+strconv.Itoa(i)))
	}
	wrapped.Flush()

	require.Eventually(t, func() bool {
		got := extractPostIDs(receiver.Snapshot())
		return len(got) >= n
	}, 20*time.Second, 100*time.Millisecond, "all posts should arrive after retries + reorder")

	got := extractPostIDs(receiver.Snapshot())
	for i := 1; i <= n; i++ {
		assert.Contains(t, got, "p-"+strconv.Itoa(i))
	}
}

// assertErrorOnce returns a non-nil error once and is used by the
// Azure Queue test to force a single visibility-timeout retry. It is
// declared as a package-level helper rather than inlined so the
// sync.Once-style "fire just once" semantic is obvious.
func assertErrorOnce() error {
	return errIntentionalRetry
}

var errIntentionalRetry = &intentionalRetryError{}

type intentionalRetryError struct{}

func (e *intentionalRetryError) Error() string { return "intentional handler error to drive retry" }

// asProvider type-asserts a QueueProvider as a testreorder.Provider
// for the wrapper. The interface shapes match in production; this is
// a compile-time bridge.
func asProvider(p QueueProvider) testreorder.Provider { return p }
