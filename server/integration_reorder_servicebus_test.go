package main

import (
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/testreorder"
)

// TestIntegrationReorderServiceBus verifies the sequencer correctly
// reassembles messages relayed via Azure Service Bus (emulator) when
// the receiver abandons 10% of messages. AbandonMessage releases the
// peek-lock without acking, so the broker redelivers later, which is
// Service Bus's primary reorder vector.
//
// Skipped automatically when the Service Bus emulator is not running
// on 127.0.0.1:5672. To run: `make docker-setup` first.
func TestIntegrationReorderServiceBus(t *testing.T) {
	requireEmulatorReachable(t, "Service Bus emulator", servicebusAMQPPort)

	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")

	queueName := "crossguard-test-sb-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	cfg := AzureServiceBusProviderConfig{
		ConnectionString: "Endpoint=sb://127.0.0.1;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true",
		QueueName:        queueName,
	}
	provider, err := newAzureServiceBusProvider(cfg, api)
	if err != nil {
		// Emulator may be reachable on TCP but the queue may not be
		// pre-created. Skip with a clear message; the docker harness
		// owns queue lifecycle.
		t.Skipf("Service Bus provider could not be constructed (queue %q may not exist): %v",
			queueName, err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	wrapped := testreorder.New(asProvider(provider), testreorder.Config{Mode: testreorder.ModePassthrough})
	sender := newSenderProcess("low-to-high", wrapped)
	receiver := newReceiverProcess("low-to-high", 5*time.Second)

	var abandonCount atomic.Int64
	ctx, cancel := withTestContext(t, 30*time.Second)
	defer cancel()
	go func() {
		_ = provider.Subscribe(ctx, func(data []byte) error {
			// Abandon roughly 10% of messages by returning an error.
			// We use a counter rather than RNG to keep the test
			// deterministic.
			if abandonCount.Add(1)%10 == 0 {
				return errIntentionalRetry
			}
			receiver.handle(data)
			return nil
		})
	}()

	// Pre-flight: a fresh emulator queue must exist before we drive the
	// bulk send. If the docker harness hasn't created it, skip rather
	// than fail (the queue creation is a docker-up responsibility).
	preflightCtx, preflightCancel := withTestContext(t, 3*time.Second)
	if err := provider.Publish(preflightCtx, []byte("preflight")); err != nil {
		preflightCancel()
		t.Skipf("Service Bus pre-flight Publish failed (queue %q likely missing): %v", queueName, err)
	}
	preflightCancel()

	const n = 20
	for i := 1; i <= n; i++ {
		sender.SendSyncMsg(t, "ch-test", makePost("p-"+strconv.Itoa(i), "body-"+strconv.Itoa(i)))
	}

	require.Eventually(t, func() bool {
		got := extractPostIDs(receiver.Snapshot())
		return len(got) >= n
	}, 30*time.Second, 100*time.Millisecond, "all posts should arrive after abandon retries")

	got := extractPostIDs(receiver.Snapshot())
	for i := 1; i <= n; i++ {
		assert.Contains(t, got, "p-"+strconv.Itoa(i))
	}
}
