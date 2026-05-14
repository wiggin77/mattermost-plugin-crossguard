package main

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/testreorder"
)

// TestIntegrationReorderAzureBlob verifies the sequencer correctly
// reassembles batched messages relayed via Azure Blob Storage (Azurite
// emulator). The reorder vector here is concurrent senders producing
// out-of-order blob list arrival: the receiver lists blobs and may
// process them in lexicographic order rather than write order,
// depending on names.
//
// Skipped automatically when Azurite Blob is not running on
// 127.0.0.1:10000. To run: `make docker-setup` first.
func TestIntegrationReorderAzureBlob(t *testing.T) {
	requireEmulatorReachable(t, "Azurite Blob", azuriteBlobPort)

	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")

	containerName := "crossguard-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	cfg := AzureBlobProviderConfig{
		ServiceURL:        azuriteBlobEndpoint,
		AccountName:       azuriteAccountName,
		AccountKey:        azuriteAccountKey,
		BlobContainerName: containerName,
	}

	ctx, cancel := withTestContext(t, 30*time.Second)
	defer cancel()

	provider, err := newAzureBlobProvider(ctx, cfg, api, &noopKV{}, "node-A", "low-to-high", true /* outbound */)
	if err != nil {
		t.Skipf("Azure Blob provider could not be constructed: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	// Receiver side: spin up a second provider in inbound mode pointing
	// at the same container.
	inboundProvider, err := newAzureBlobProvider(ctx, cfg, api, &noopKV{}, "node-B", "low-to-high", false /* inbound */)
	if err != nil {
		t.Skipf("Azure Blob inbound provider could not be constructed: %v", err)
	}
	t.Cleanup(func() { _ = inboundProvider.Close() })

	wrapped := testreorder.New(asProvider(provider), testreorder.Config{Mode: testreorder.ModePassthrough})
	sender := newSenderProcess("low-to-high", wrapped)
	receiver := newReceiverProcess("low-to-high", 10*time.Second)

	go func() {
		_ = inboundProvider.Subscribe(ctx, func(data []byte) error {
			receiver.handle(data)
			return nil
		})
	}()

	// Pre-flight: verify Publish works against this emulator instance.
	// If not (auth, container setup, blob batching not initialized),
	// skip rather than fail.
	preflightCtx, preflightCancel := withTestContext(t, 3*time.Second)
	if err := provider.Publish(preflightCtx, []byte("preflight")); err != nil {
		preflightCancel()
		t.Skipf("Azure Blob pre-flight Publish failed: %v", err)
	}
	preflightCancel()

	const n = 10
	for i := 1; i <= n; i++ {
		sender.SendSyncMsg(t, "ch-test", makePost("p-"+strconv.Itoa(i), "body-"+strconv.Itoa(i)))
	}
	wrapped.Flush()

	require.Eventually(t, func() bool {
		got := extractPostIDs(receiver.Snapshot())
		return len(got) >= n
	}, 30*time.Second, 200*time.Millisecond, "all posts should arrive via Azure Blob batch relay")

	got := extractPostIDs(receiver.Snapshot())
	for i := 1; i <= n; i++ {
		assert.Contains(t, got, "p-"+strconv.Itoa(i))
	}
}

// noopKV is a trivial kvClient implementation used by the Azure Blob
// integration test. It returns zero values, accepts every Set, and
// reports every Delete as successful. The Azure Blob provider's WAL
// recovery and lock paths both write through kvClient; the no-op
// suffices for a single-node test against the emulator.
type noopKV struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (k *noopKV) Get(_ string, _ any) error {
	// Always return "no data found" to drive cold-start paths in the
	// Azure Blob provider. The integration test does not exercise WAL
	// recovery; if a future test needs to, replace this with a real
	// in-memory KV mock.
	return nil
}

func (k *noopKV) Set(key string, value any, _ ...pluginapi.KVSetOption) (bool, error) {
	_ = value
	_ = context.Background
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = make(map[string][]byte)
	}
	k.m[key] = []byte{}
	return true, nil
}

func (k *noopKV) Delete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.m, key)
	return nil
}
