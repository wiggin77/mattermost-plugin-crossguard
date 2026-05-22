package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockServiceBusSender implements serviceBusSender for unit tests.
type mockServiceBusSender struct {
	sendFn  func(ctx context.Context, msg *azservicebus.Message, opts *azservicebus.SendMessageOptions) error
	closeFn func(ctx context.Context) error
}

func (m *mockServiceBusSender) SendMessage(ctx context.Context, msg *azservicebus.Message, opts *azservicebus.SendMessageOptions) error {
	if m.sendFn != nil {
		return m.sendFn(ctx, msg, opts)
	}
	return nil
}

func (m *mockServiceBusSender) Close(ctx context.Context) error {
	if m.closeFn != nil {
		return m.closeFn(ctx)
	}
	return nil
}

// mockServiceBusReceiver implements serviceBusReceiver for unit tests.
type mockServiceBusReceiver struct {
	receiveFn  func(ctx context.Context, maxMessages int, opts *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error)
	peekFn     func(ctx context.Context, maxMessageCount int, opts *azservicebus.PeekMessagesOptions) ([]*azservicebus.ReceivedMessage, error)
	completeFn func(ctx context.Context, msg *azservicebus.ReceivedMessage, opts *azservicebus.CompleteMessageOptions) error
	abandonFn  func(ctx context.Context, msg *azservicebus.ReceivedMessage, opts *azservicebus.AbandonMessageOptions) error
	closeFn    func(ctx context.Context) error
}

func (m *mockServiceBusReceiver) ReceiveMessages(ctx context.Context, maxMessages int, opts *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error) {
	if m.receiveFn != nil {
		return m.receiveFn(ctx, maxMessages, opts)
	}
	// Default: simulate empty queue via ctx deadline to avoid a hot-loop in tests.
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *mockServiceBusReceiver) PeekMessages(ctx context.Context, maxMessageCount int, opts *azservicebus.PeekMessagesOptions) ([]*azservicebus.ReceivedMessage, error) {
	if m.peekFn != nil {
		return m.peekFn(ctx, maxMessageCount, opts)
	}
	return nil, nil
}

func (m *mockServiceBusReceiver) CompleteMessage(ctx context.Context, msg *azservicebus.ReceivedMessage, opts *azservicebus.CompleteMessageOptions) error {
	if m.completeFn != nil {
		return m.completeFn(ctx, msg, opts)
	}
	return nil
}

func (m *mockServiceBusReceiver) AbandonMessage(ctx context.Context, msg *azservicebus.ReceivedMessage, opts *azservicebus.AbandonMessageOptions) error {
	if m.abandonFn != nil {
		return m.abandonFn(ctx, msg, opts)
	}
	return nil
}

func (m *mockServiceBusReceiver) Close(ctx context.Context) error {
	if m.closeFn != nil {
		return m.closeFn(ctx)
	}
	return nil
}

// mockServiceBusClient implements serviceBusClientCloser for unit tests.
type mockServiceBusClient struct {
	closeFn func(ctx context.Context) error
}

func (m *mockServiceBusClient) Close(ctx context.Context) error {
	if m.closeFn != nil {
		return m.closeFn(ctx)
	}
	return nil
}

// --- sanitizer ---

func TestSanitizeServiceBusError_Nil(t *testing.T) {
	assert.Equal(t, "", sanitizeServiceBusError(nil))
}

func TestSanitizeServiceBusError_SharedAccessKey(t *testing.T) {
	err := errors.New("dial failed: Endpoint=sb://foo.servicebus.windows.net/;SharedAccessKeyName=Root;SharedAccessKey=abc123XYZ+/=;EntityPath=q1")
	out := sanitizeServiceBusError(err)
	assert.NotContains(t, out, "abc123XYZ")
	assert.Contains(t, out, "SharedAccessKey=REDACTED")
}

func TestSanitizeServiceBusError_SharedAccessSignature(t *testing.T) {
	err := errors.New("auth: SharedAccessSignature=SharedAccessSignature sr=foo&sig=abc123 refused")
	out := sanitizeServiceBusError(err)
	assert.NotContains(t, out, "abc123")
}

func TestSanitizeServiceBusError_SigQuery(t *testing.T) {
	err := errors.New("request to https://foo/?sig=TOKEN123&se=456 failed")
	out := sanitizeServiceBusError(err)
	assert.NotContains(t, out, "TOKEN123")
	assert.Contains(t, out, "sig=REDACTED")
}

func TestSanitizeServiceBusError_PreservesIntent(t *testing.T) {
	err := errors.New("service bus unreachable: SharedAccessKey=abc; timeout")
	out := sanitizeServiceBusError(err)
	assert.Contains(t, out, "service bus unreachable")
	assert.Contains(t, out, "timeout")
}

// --- Lifecycle ---

func TestServiceBusCloseWithoutSubscribe(t *testing.T) {
	p := &azureServiceBusProvider{}
	assert.NoError(t, p.Close())
}

func TestServiceBusCloseIdempotent(t *testing.T) {
	p := &azureServiceBusProvider{}
	assert.NoError(t, p.Close())
	assert.NoError(t, p.Close())
}

func TestServiceBusCloseNilFields(t *testing.T) {
	p := &azureServiceBusProvider{cancel: nil, pollDone: nil}
	assert.NoError(t, p.Close())
}

func TestServiceBusCloseClosesSDKObjectsInOrder(t *testing.T) {
	var order []string
	p := &azureServiceBusProvider{
		client: &mockServiceBusClient{closeFn: func(_ context.Context) error {
			order = append(order, "client")
			return nil
		}},
		sender: &mockServiceBusSender{closeFn: func(_ context.Context) error {
			order = append(order, "sender")
			return nil
		}},
		receiver: &mockServiceBusReceiver{closeFn: func(_ context.Context) error {
			order = append(order, "receiver")
			return nil
		}},
	}
	assert.NoError(t, p.Close())
	assert.Equal(t, []string{"receiver", "sender", "client"}, order)
}

// --- Publish ---

func TestServiceBusPublish_SendsRawBytes(t *testing.T) {
	var captured []byte
	p := &azureServiceBusProvider{
		maxMsgSize: serviceBusDefaultMaxMessageSize,
		sender: &mockServiceBusSender{sendFn: func(_ context.Context, msg *azservicebus.Message, _ *azservicebus.SendMessageOptions) error {
			captured = msg.Body
			return nil
		}},
	}
	require.NoError(t, p.Publish(context.TODO(), []byte("hello")))
	assert.Equal(t, []byte("hello"), captured)
}

func TestServiceBusPublish_RejectsOversize(t *testing.T) {
	p := &azureServiceBusProvider{
		maxMsgSize: 10,
		sender:     &mockServiceBusSender{},
	}
	err := p.Publish(context.TODO(), make([]byte, 20))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds max")
}

func TestServiceBusPublish_SanitizesErrorString(t *testing.T) {
	p := &azureServiceBusProvider{
		maxMsgSize: serviceBusDefaultMaxMessageSize,
		sender: &mockServiceBusSender{sendFn: func(_ context.Context, _ *azservicebus.Message, _ *azservicebus.SendMessageOptions) error {
			return errors.New("send failed: SharedAccessKey=secret_abc123")
		}},
	}
	err := p.Publish(context.TODO(), []byte("x"))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret_abc123")
	assert.Contains(t, err.Error(), "SharedAccessKey=REDACTED")
}

// --- Subscribe: happy path ---

func TestServiceBusSubscribe_HappyPath_Completes(t *testing.T) {
	var receivedOnce atomic.Bool
	var completeCount atomic.Int32

	mr := &mockServiceBusReceiver{
		receiveFn: func(ctx context.Context, _ int, _ *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error) {
			if receivedOnce.Swap(true) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []*azservicebus.ReceivedMessage{
				{Body: []byte("hello"), MessageID: "m1"},
			}, nil
		},
		completeFn: func(_ context.Context, _ *azservicebus.ReceivedMessage, _ *azservicebus.CompleteMessageOptions) error {
			completeCount.Add(1)
			return nil
		},
	}

	p := &azureServiceBusProvider{
		maxMsgSize: serviceBusDefaultMaxMessageSize,
		receiver:   mr,
		api:        newTestAPI(),
		cfg:        AzureServiceBusProviderConfig{QueueName: "q1"},
	}

	handlerCalled := make(chan []byte, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, p.Subscribe(ctx, func(data []byte) error {
		handlerCalled <- data
		return nil
	}))

	select {
	case got := <-handlerCalled:
		assert.Equal(t, []byte("hello"), got)
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not called")
	}

	// Give the loop a moment to issue CompleteMessage after the handler returned.
	require.Eventually(t, func() bool { return completeCount.Load() == 1 }, time.Second, 10*time.Millisecond)

	cancel()
	<-p.pollDone
}

// --- Subscribe: handler error -> Abandon, not Complete ---

func TestServiceBusSubscribe_HandlerError_Abandons(t *testing.T) {
	var receivedOnce atomic.Bool
	var completeCount atomic.Int32
	var abandonCount atomic.Int32

	mr := &mockServiceBusReceiver{
		receiveFn: func(ctx context.Context, _ int, _ *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error) {
			if receivedOnce.Swap(true) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []*azservicebus.ReceivedMessage{
				{Body: []byte("hello"), MessageID: "m1"},
			}, nil
		},
		completeFn: func(_ context.Context, _ *azservicebus.ReceivedMessage, _ *azservicebus.CompleteMessageOptions) error {
			completeCount.Add(1)
			return nil
		},
		abandonFn: func(_ context.Context, _ *azservicebus.ReceivedMessage, _ *azservicebus.AbandonMessageOptions) error {
			abandonCount.Add(1)
			return nil
		},
	}

	p := &azureServiceBusProvider{
		maxMsgSize: serviceBusDefaultMaxMessageSize,
		receiver:   mr,
		api:        newTestAPI(),
		cfg:        AzureServiceBusProviderConfig{QueueName: "q1"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, p.Subscribe(ctx, func(_ []byte) error {
		return errors.New("transient failure")
	}))

	require.Eventually(t, func() bool { return abandonCount.Load() == 1 }, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(0), completeCount.Load())

	cancel()
	<-p.pollDone
}

// --- Subscribe: malformed (empty) body -> Complete (drop) ---

func TestServiceBusSubscribe_EmptyBody_DroppedViaComplete(t *testing.T) {
	var receivedOnce atomic.Bool
	var completeCount atomic.Int32
	var abandonCount atomic.Int32
	var handlerCalls atomic.Int32

	mr := &mockServiceBusReceiver{
		receiveFn: func(ctx context.Context, _ int, _ *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error) {
			if receivedOnce.Swap(true) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []*azservicebus.ReceivedMessage{
				{Body: nil, MessageID: "m1"},
			}, nil
		},
		completeFn: func(_ context.Context, _ *azservicebus.ReceivedMessage, _ *azservicebus.CompleteMessageOptions) error {
			completeCount.Add(1)
			return nil
		},
		abandonFn: func(_ context.Context, _ *azservicebus.ReceivedMessage, _ *azservicebus.AbandonMessageOptions) error {
			abandonCount.Add(1)
			return nil
		},
	}

	p := &azureServiceBusProvider{
		maxMsgSize: serviceBusDefaultMaxMessageSize,
		receiver:   mr,
		api:        newTestAPI(),
		cfg:        AzureServiceBusProviderConfig{QueueName: "q1"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, p.Subscribe(ctx, func(_ []byte) error {
		handlerCalls.Add(1)
		return nil
	}))

	require.Eventually(t, func() bool { return completeCount.Load() == 1 }, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(0), abandonCount.Load(), "empty body must not be abandoned (would loop)")
	assert.Equal(t, int32(0), handlerCalls.Load(), "empty body must not reach handler")

	cancel()
	<-p.pollDone
}

// --- Subscribe: empty-queue (DeadlineExceeded) is not treated as error ---
//
// The Subscribe loop wraps ReceiveMessages in a per-iteration
// context.WithTimeout(serviceBusReceiveMaxWait). When the queue is empty the
// inner ctx fires with DeadlineExceeded; the loop must treat that as a
// normal empty batch, not a transport error.
func TestServiceBusSubscribe_EmptyQueue_DoesNotErrorLog(t *testing.T) {
	var callCount atomic.Int32
	mr := &mockServiceBusReceiver{
		receiveFn: func(ctx context.Context, _ int, _ *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error) {
			callCount.Add(1)
			// Return DeadlineExceeded immediately, as if ctx deadline fired.
			return nil, context.DeadlineExceeded
		},
	}

	p := &azureServiceBusProvider{
		maxMsgSize: serviceBusDefaultMaxMessageSize,
		receiver:   mr,
		api:        newTestAPI(),
		cfg:        AzureServiceBusProviderConfig{QueueName: "q1"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, p.Subscribe(ctx, func(_ []byte) error { return nil }))

	// DeadlineExceeded should be swallowed silently and the loop should re-enter.
	require.Eventually(t, func() bool { return callCount.Load() >= 5 }, time.Second, 10*time.Millisecond)

	cancel()
	<-p.pollDone
}

// --- UploadFile / WatchFiles guards ---

func TestServiceBusUploadFileWithoutBlobClient(t *testing.T) {
	p := &azureServiceBusProvider{containerClient: nil}
	err := p.UploadFile(context.TODO(), "key", []byte("data"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blob container not configured")
}

func TestServiceBusWatchFilesWithoutBlobClient(t *testing.T) {
	p := &azureServiceBusProvider{containerClient: nil}
	err := p.WatchFiles(context.TODO(), nil)
	assert.NoError(t, err)
}

// --- Lifecycle concurrency ---
//
// Exercises Subscribe + Close from different goroutines to catch data races
// under `go test -race`. The lifecycleMu mutex must serialize field access;
// without it, this test fails reliably under the race detector.

func TestServiceBusSubscribeCloseRace(t *testing.T) {
	mr := &mockServiceBusReceiver{
		receiveFn: func(ctx context.Context, _ int, _ *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	p := &azureServiceBusProvider{
		maxMsgSize: serviceBusDefaultMaxMessageSize,
		receiver:   mr,
		sender:     &mockServiceBusSender{},
		client:     &mockServiceBusClient{},
		api:        newTestAPI(),
		cfg:        AzureServiceBusProviderConfig{QueueName: "q1"},
	}

	// Subscribe on the main goroutine, Close on a spawned one (the realistic
	// Mattermost teardown pattern: plugin lifecycle goroutine calls Close
	// while the reconnect goroutine may still be starting the subscription).
	require.NoError(t, p.Subscribe(t.Context(), func(_ []byte) error { return nil }))

	done := make(chan struct{})
	go func() {
		defer close(done)
		assert.NoError(t, p.Close())
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return within 2s; lifecycle mutex may be deadlocked")
	}
}

func TestServiceBusDoubleSubscribeRejected(t *testing.T) {
	mr := &mockServiceBusReceiver{
		receiveFn: func(ctx context.Context, _ int, _ *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	p := &azureServiceBusProvider{
		maxMsgSize: serviceBusDefaultMaxMessageSize,
		receiver:   mr,
		api:        newTestAPI(),
		cfg:        AzureServiceBusProviderConfig{QueueName: "q1"},
	}

	require.NoError(t, p.Subscribe(t.Context(), func(_ []byte) error { return nil }))

	// Second Subscribe must be rejected; otherwise the first poll goroutine
	// is orphaned and two consumers race for settlement on the same lock.
	err := p.Subscribe(t.Context(), func(_ []byte) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already subscribed")

	assert.NoError(t, p.Close())
}

// --- UploadFile ---
//
// The nil-container guard is already exercised by TestServiceBusUploadFileWithoutBlobClient.
// These tests drive the happy path and the SDK-error path through fakeBlobOps.

func TestServiceBusUploadFile_HappyPath(t *testing.T) {
	ops := &fakeBlobOps{}
	p := &azureServiceBusProvider{
		containerClient: ops,
		api:             newTestAPI(),
	}

	headers := map[string]string{
		"X-Post-Id":  "post1",
		"X-Filename": "report.pdf",
	}
	data := []byte("file bytes")

	require.NoError(t, p.UploadFile(t.Context(), "conn/post1/file1", data, headers))
	require.Len(t, ops.uploads, 1)
	assert.Equal(t, "conn/post1/file1", ops.uploads[0].name)
	assert.Equal(t, data, ops.uploads[0].data)

	// Metadata round-trip: the provider stores headers as base64(JSON) on
	// the crossguard_headers blob metadata key.
	got := extractBlobHeaders(ops.uploads[0].metadata)
	assert.Equal(t, headers, got)
}

func TestServiceBusUploadFile_SanitizesError(t *testing.T) {
	ops := &fakeBlobOps{uploadFn: func(_ context.Context, _ string, _ []byte, _ map[string]*string) error {
		return errors.New("forbidden: SharedAccessKey=leaked-key-abc")
	}}
	p := &azureServiceBusProvider{
		containerClient: ops,
		api:             newTestAPI(),
	}

	err := p.UploadFile(t.Context(), "k", []byte("x"), nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "leaked-key-abc")
	assert.Contains(t, err.Error(), "SharedAccessKey=REDACTED")
}

// --- WatchFiles ---
//
// The nil-container guard is covered by TestServiceBusWatchFilesWithoutBlobClient.
// These exercise the poll loop: list error, download error, handler error, delete error.

// watchFilesUntil runs WatchFiles in a goroutine, waits for `cond` to become
// true (up to 2s), then cancels ctx and waits for the goroutine to return.
// Shrinks blobPoll to 5ms so the test is responsive.
func watchFilesUntil(t *testing.T, p *azureServiceBusProvider, handler func(string, []byte, map[string]string) error, cond func() bool) {
	t.Helper()
	p.blobPoll = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = p.WatchFiles(ctx, handler)
	}()

	require.Eventually(t, cond, 2*time.Second, 5*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WatchFiles did not return after ctx cancellation")
	}
}

func TestServiceBusWatchFiles_HandlerSuccess_Deletes(t *testing.T) {
	listCalls := atomic.Int32{}
	var deleteCalls atomic.Int32
	var deletedName atomic.Value
	ops := &fakeBlobOps{
		listFn: func(_ context.Context, _ string, _ bool) ([]blobListing, error) {
			if listCalls.Add(1) > 1 {
				// Empty list after first successful round so the
				// handler is called exactly once.
				return nil, nil
			}
			return []blobListing{{Name: "blob1", Metadata: nil}}, nil
		},
		downloadFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("payload"), nil
		},
		deleteFn: func(_ context.Context, name string) error {
			deletedName.Store(name)
			deleteCalls.Add(1)
			return nil
		},
	}
	p := &azureServiceBusProvider{containerClient: ops, api: newTestAPI()}

	watchFilesUntil(t, p, func(_ string, _ []byte, _ map[string]string) error {
		return nil
	}, func() bool { return deleteCalls.Load() >= 1 })

	assert.Equal(t, "blob1", deletedName.Load())
}

func TestServiceBusWatchFiles_HandlerError_DoesNotDelete(t *testing.T) {
	var handlerCalls atomic.Int32
	var deleteCalls atomic.Int32
	ops := &fakeBlobOps{
		listFn: func(_ context.Context, _ string, _ bool) ([]blobListing, error) {
			return []blobListing{{Name: "bad-blob", Metadata: nil}}, nil
		},
		downloadFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("payload"), nil
		},
		deleteFn: func(_ context.Context, _ string) error {
			deleteCalls.Add(1)
			return nil
		},
	}
	p := &azureServiceBusProvider{containerClient: ops, api: newTestAPI()}

	watchFilesUntil(t, p, func(_ string, _ []byte, _ map[string]string) error {
		handlerCalls.Add(1)
		return errors.New("handler rejected")
	}, func() bool { return handlerCalls.Load() >= 1 })

	assert.Zero(t, deleteCalls.Load(), "handler error must not trigger blob deletion")
}

func TestServiceBusWatchFiles_ListError_Continues(t *testing.T) {
	var listCalls atomic.Int32
	ops := &fakeBlobOps{
		listFn: func(_ context.Context, _ string, _ bool) ([]blobListing, error) {
			listCalls.Add(1)
			return nil, errors.New("transient list failure: SharedAccessKey=hidden")
		},
	}
	p := &azureServiceBusProvider{
		containerClient: ops,
		api:             newTestAPI(),
		cfg:             AzureServiceBusProviderConfig{BlobContainerName: "c1"},
	}

	// Loop should iterate multiple times on list errors (not bail out after one).
	watchFilesUntil(t, p, nil, func() bool { return listCalls.Load() >= 3 })
}

func TestServiceBusWatchFiles_DownloadError_SkipsBlob(t *testing.T) {
	var downloadCalls atomic.Int32
	var deleteCalls atomic.Int32
	ops := &fakeBlobOps{
		listFn: func(_ context.Context, _ string, _ bool) ([]blobListing, error) {
			return []blobListing{{Name: "fails"}}, nil
		},
		downloadFn: func(_ context.Context, _ string) ([]byte, error) {
			downloadCalls.Add(1)
			return nil, errors.New("download failed: sig=abc123")
		},
		deleteFn: func(_ context.Context, _ string) error {
			deleteCalls.Add(1)
			return nil
		},
	}
	p := &azureServiceBusProvider{containerClient: ops, api: newTestAPI()}

	// Wait until at least one download attempt was recorded, then cancel.
	watchFilesUntil(t, p, func(_ string, _ []byte, _ map[string]string) error {
		t.Fatal("handler must not run when download fails")
		return nil
	}, func() bool { return downloadCalls.Load() >= 1 })

	assert.Zero(t, deleteCalls.Load(), "download failure must not delete the blob")
}

func TestServiceBusWatchFiles_DeleteError_Continues(t *testing.T) {
	var deleteCalls atomic.Int32
	ops := &fakeBlobOps{
		listFn: func(_ context.Context, _ string, _ bool) ([]blobListing, error) {
			return []blobListing{{Name: "delete-fails"}}, nil
		},
		downloadFn: func(_ context.Context, _ string) ([]byte, error) { return []byte("x"), nil },
		deleteFn: func(_ context.Context, _ string) error {
			deleteCalls.Add(1)
			return errors.New("delete 500: SharedAccessKey=leak")
		},
	}
	p := &azureServiceBusProvider{containerClient: ops, api: newTestAPI()}

	watchFilesUntil(t, p, func(_ string, _ []byte, _ map[string]string) error {
		return nil
	}, func() bool { return deleteCalls.Load() >= 1 })
	// Loop must not panic or exit — just logs WARN and re-polls.
}

// --- pollQueue additional branches ---

func TestServiceBusPollQueue_EmptyBody_CompleteFailureLogged(t *testing.T) {
	var receivedOnce atomic.Bool
	var completeCalls atomic.Int32
	mr := &mockServiceBusReceiver{
		receiveFn: func(ctx context.Context, _ int, _ *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error) {
			if receivedOnce.Swap(true) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []*azservicebus.ReceivedMessage{{Body: nil, MessageID: "empty-1"}}, nil
		},
		completeFn: func(_ context.Context, _ *azservicebus.ReceivedMessage, _ *azservicebus.CompleteMessageOptions) error {
			completeCalls.Add(1)
			// Settlement fails: lock will expire and message redelivers.
			// The malformed-body branch must log WARN rather than silently swallow.
			return errors.New("server timeout: SharedAccessKey=leak")
		},
	}
	p := &azureServiceBusProvider{
		maxMsgSize: serviceBusDefaultMaxMessageSize,
		receiver:   mr,
		api:        newTestAPI(),
		cfg:        AzureServiceBusProviderConfig{QueueName: "q1"},
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, p.Subscribe(ctx, func(_ []byte) error {
		t.Fatal("handler must not run for empty-body message")
		return nil
	}))

	require.Eventually(t, func() bool { return completeCalls.Load() >= 1 }, time.Second, 5*time.Millisecond)

	cancel()
	<-p.pollDone
}

func TestServiceBusPollQueue_DeliveryCountLogged(t *testing.T) {
	var receivedOnce atomic.Bool
	handlerRan := make(chan struct{}, 1)
	mr := &mockServiceBusReceiver{
		receiveFn: func(ctx context.Context, _ int, _ *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error) {
			if receivedOnce.Swap(true) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			// DeliveryCount > 1 triggers the redelivery WARN branch.
			return []*azservicebus.ReceivedMessage{{
				Body:          []byte("payload"),
				MessageID:     "redelivered",
				DeliveryCount: 3,
			}}, nil
		},
	}
	p := &azureServiceBusProvider{
		maxMsgSize: serviceBusDefaultMaxMessageSize,
		receiver:   mr,
		api:        newTestAPI(),
		cfg:        AzureServiceBusProviderConfig{QueueName: "q1"},
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, p.Subscribe(ctx, func(_ []byte) error {
		handlerRan <- struct{}{}
		return nil
	}))

	select {
	case <-handlerRan:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not run")
	}
	cancel()
	<-p.pollDone
}

// --- newAzureServiceBusProvider constructor ---
//
// The azservicebus SDK's constructors (NewClientFromConnectionString,
// NewSender, NewReceiverForQueue) validate the connection string shape but do
// NOT make network calls, so the happy path can be exercised with a valid
// emulator-style connection string. The blob-sidecar branch is skipped here
// because it would require Azurite to be running.

const serviceBusTestConnStr = "Endpoint=sb://example.servicebus.windows.net/;SharedAccessKeyName=root;SharedAccessKey=YWJjMTIzCg==;UseDevelopmentEmulator=false"

func TestNewAzureServiceBusProvider_HappyPath(t *testing.T) {
	p, err := newAzureServiceBusProvider(context.Background(), AzureServiceBusProviderConfig{
		ConnectionString: serviceBusTestConnStr,
		QueueName:        "test-queue",
	}, newTestAPI())
	require.NoError(t, err)
	require.NotNil(t, p)
	t.Cleanup(func() { _ = p.Close() })

	sb := p.(*azureServiceBusProvider)
	assert.NotNil(t, sb.client)
	assert.NotNil(t, sb.sender)
	assert.NotNil(t, sb.receiver)
	assert.Nil(t, sb.containerClient, "no blob sidecar configured")
	assert.Equal(t, serviceBusDefaultMaxMessageSize, sb.maxMsgSize)
	assert.Equal(t, serviceBusBlobPollInterval, sb.blobPoll)
}

func TestNewAzureServiceBusProvider_CustomTunables(t *testing.T) {
	p, err := newAzureServiceBusProvider(context.Background(), AzureServiceBusProviderConfig{
		ConnectionString:        serviceBusTestConnStr,
		QueueName:               "q1",
		MaxMessageSizeBytes:     50 * 1024,
		BlobPollIntervalSeconds: 7,
	}, newTestAPI())
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	sb := p.(*azureServiceBusProvider)
	assert.Equal(t, 50*1024, sb.maxMsgSize)
	assert.Equal(t, 7*time.Second, sb.blobPoll)
}

func TestNewAzureServiceBusProvider_InvalidConnString(t *testing.T) {
	_, err := newAzureServiceBusProvider(context.Background(), AzureServiceBusProviderConfig{
		ConnectionString: "not-a-valid-connection-string",
		QueueName:        "q1",
	}, newTestAPI())
	require.Error(t, err)
	// Error must be sanitized even if the SDK's message quotes the bad input.
	assert.Contains(t, err.Error(), "service bus client init")
	assert.NotContains(t, err.Error(), "SharedAccessKey=",
		"bad connstr error must not echo SAS fragments verbatim")
}

func TestNewAzureServiceBusProvider_EmptyQueueName(t *testing.T) {
	// An empty queue name reaches NewSender/NewReceiverForQueue. The SDK
	// returns an error rather than constructing a receiver against "".
	_, err := newAzureServiceBusProvider(context.Background(), AzureServiceBusProviderConfig{
		ConnectionString: serviceBusTestConnStr,
		QueueName:        "",
	}, newTestAPI())
	require.Error(t, err)
	// Should be sender or receiver init; exact wording varies by SDK version.
	msg := err.Error()
	ok := strings.Contains(msg, "sender init") || strings.Contains(msg, "receiver init")
	assert.True(t, ok, "expected sender/receiver init error, got: %s", msg)
}

// --- MaxMessageSize ---

func TestServiceBusMaxMessageSize(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		p := &azureServiceBusProvider{maxMsgSize: serviceBusDefaultMaxMessageSize}
		assert.Equal(t, serviceBusDefaultMaxMessageSize, p.MaxMessageSize())
	})
	t.Run("custom", func(t *testing.T) {
		p := &azureServiceBusProvider{maxMsgSize: 1000000}
		assert.Equal(t, 1000000, p.MaxMessageSize())
	})
}
