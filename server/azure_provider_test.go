package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAzureCloseWithoutSubscribe(t *testing.T) {
	p := &azureProvider{}
	assert.NoError(t, p.Close())
}

func TestBuildQueueURL(t *testing.T) {
	tests := []struct {
		name       string
		serviceURL string
		queueName  string
		want       string
	}{
		{"no trailing slash", "https://acct.queue.core.windows.net", "q1", "https://acct.queue.core.windows.net/q1"},
		{"with trailing slash", "https://acct.queue.core.windows.net/", "q1", "https://acct.queue.core.windows.net/q1"},
		{"with multiple trailing slashes", "https://acct.queue.core.windows.net///", "q1", "https://acct.queue.core.windows.net/q1"},
		{"empty service URL", "", "q1", "/q1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildQueueURL(tt.serviceURL, tt.queueName))
		})
	}
}

func TestBuildBlobContainerURL(t *testing.T) {
	tests := []struct {
		name       string
		serviceURL string
		container  string
		want       string
	}{
		{"no trailing slash", "https://acct.blob.core.windows.net", "c1", "https://acct.blob.core.windows.net/c1"},
		{"with trailing slash", "https://acct.blob.core.windows.net/", "c1", "https://acct.blob.core.windows.net/c1"},
		{"empty service URL", "", "c1", "/c1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildBlobContainerURL(tt.serviceURL, tt.container))
		})
	}
}

func TestAzureUploadFileWithoutBlobClient(t *testing.T) {
	p := &azureProvider{containerClient: nil}
	err := p.UploadFile(context.TODO(), "key", []byte("data"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blob container not configured")
}

func TestAzureWatchFilesWithoutBlobClient(t *testing.T) {
	p := &azureProvider{containerClient: nil}
	err := p.WatchFiles(context.TODO(), nil)
	assert.NoError(t, err)
}

func TestExtractBlobHeaders(t *testing.T) {
	t.Run("nil metadata returns empty map", func(t *testing.T) {
		headers := extractBlobHeaders(nil)
		assert.Empty(t, headers)
	})

	t.Run("missing crossguard_headers key returns empty map", func(t *testing.T) {
		v := "something"
		headers := extractBlobHeaders(map[string]*string{"other": &v})
		assert.Empty(t, headers)
	})

	t.Run("nil value for key returns empty map", func(t *testing.T) {
		headers := extractBlobHeaders(map[string]*string{"crossguard_headers": nil})
		assert.Empty(t, headers)
	})

	t.Run("invalid base64 returns empty map", func(t *testing.T) {
		v := "not-valid-base64!!!"
		headers := extractBlobHeaders(map[string]*string{"crossguard_headers": &v})
		assert.Empty(t, headers)
	})

	t.Run("invalid JSON returns empty map", func(t *testing.T) {
		v := base64.StdEncoding.EncodeToString([]byte("not json"))
		headers := extractBlobHeaders(map[string]*string{"crossguard_headers": &v})
		assert.Empty(t, headers)
	})

	t.Run("valid metadata round-trips correctly", func(t *testing.T) {
		original := map[string]string{
			"X-Post-Id":   "post123",
			"X-Conn-Name": "my-conn",
			"X-Filename":  "report.pdf",
		}
		meta := azureBlobMetadata{Headers: original}
		metaJSON, err := json.Marshal(meta)
		require.NoError(t, err)

		encoded := base64.StdEncoding.EncodeToString(metaJSON)
		headers := extractBlobHeaders(map[string]*string{"crossguard_headers": &encoded})

		assert.Equal(t, original, headers)
	})
}

func TestOptStr(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		assert.Nil(t, optStr(""))
	})

	t.Run("non-empty string returns pointer", func(t *testing.T) {
		result := optStr("hello")
		require.NotNil(t, result)
		assert.Equal(t, "hello", *result)
	})
}

func TestNewNopCloser(t *testing.T) {
	data := []byte("test data")
	rc := newNopCloser(data)

	buf := make([]byte, len(data))
	n, err := rc.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf)

	assert.NoError(t, rc.Close())
}

// Compile-time interface conformance checks.
var (
	_ QueueProvider = (*azureProvider)(nil)
	_ QueueProvider = (*natsProvider)(nil)
)

func TestProviderMaxMessageSize(t *testing.T) {
	t.Run("azure returns 48000", func(t *testing.T) {
		p := &azureProvider{}
		assert.Equal(t, 48000, p.MaxMessageSize())
	})

	t.Run("nats returns 0 (no limit)", func(t *testing.T) {
		p := &natsProvider{}
		assert.Equal(t, 0, p.MaxMessageSize())
	})
}

func TestAzureCloseIdempotent(t *testing.T) {
	p := &azureProvider{}
	assert.NoError(t, p.Close())
	assert.NoError(t, p.Close())
}

func TestAzureCloseNilFields(t *testing.T) {
	p := &azureProvider{
		cancel:   nil,
		pollDone: nil,
	}
	assert.NoError(t, p.Close())
}

func TestExtractBlobHeaders_EmptyHeadersMap(t *testing.T) {
	emptyHeaders := map[string]string{}
	meta := azureBlobMetadata{Headers: emptyHeaders}
	metaJSON, err := json.Marshal(meta)
	require.NoError(t, err)
	encoded := base64.StdEncoding.EncodeToString(metaJSON)
	headers := extractBlobHeaders(map[string]*string{"crossguard_headers": &encoded})
	assert.Empty(t, headers)
}

// mockAzureQueue implements azureQueuer for unit tests.
type mockAzureQueue struct {
	enqueueFn func(ctx context.Context, content string, o *azqueue.EnqueueMessageOptions) (azqueue.EnqueueMessagesResponse, error)
	dequeueFn func(ctx context.Context, o *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error)
	deleteFn  func(ctx context.Context, messageID string, popReceipt string, o *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error)
}

func (m *mockAzureQueue) EnqueueMessage(ctx context.Context, content string, o *azqueue.EnqueueMessageOptions) (azqueue.EnqueueMessagesResponse, error) {
	if m.enqueueFn != nil {
		return m.enqueueFn(ctx, content, o)
	}
	return azqueue.EnqueueMessagesResponse{}, nil
}

func (m *mockAzureQueue) DequeueMessages(ctx context.Context, o *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
	if m.dequeueFn != nil {
		return m.dequeueFn(ctx, o)
	}
	return azqueue.DequeueMessagesResponse{}, nil
}

func (m *mockAzureQueue) DeleteMessage(ctx context.Context, messageID string, popReceipt string, o *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error) {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, messageID, popReceipt, o)
	}
	return azqueue.DeleteMessageResponse{}, nil
}

func newTestAPI() *plugintest.API {
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
	return api
}

func TestAzurePublish_EncodesBase64(t *testing.T) {
	var captured string
	mq := &mockAzureQueue{
		enqueueFn: func(_ context.Context, content string, _ *azqueue.EnqueueMessageOptions) (azqueue.EnqueueMessagesResponse, error) {
			captured = content
			return azqueue.EnqueueMessagesResponse{}, nil
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
	}

	original := []byte("hello world")
	err := p.Publish(context.Background(), original)
	require.NoError(t, err)

	expected := base64.StdEncoding.EncodeToString(original)
	assert.Equal(t, expected, captured)

	decoded, err := base64.StdEncoding.DecodeString(captured)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)
}

func TestAzurePublish_EnqueueError(t *testing.T) {
	mq := &mockAzureQueue{
		enqueueFn: func(_ context.Context, _ string, _ *azqueue.EnqueueMessageOptions) (azqueue.EnqueueMessagesResponse, error) {
			return azqueue.EnqueueMessagesResponse{}, fmt.Errorf("connection refused")
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
	}

	err := p.Publish(context.Background(), []byte("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to enqueue message")
	assert.Contains(t, err.Error(), "connection refused")
}

func TestAzurePollQueue_ContextCancellation(t *testing.T) {
	mq := &mockAzureQueue{
		dequeueFn: func(ctx context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			return azqueue.DequeueMessagesResponse{}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	cancel()
	select {
	case <-done:
		// pollQueue exited as expected.
	case <-time.After(5 * time.Second):
		t.Fatal("pollQueue did not exit after context cancellation")
	}
}

func TestAzurePollQueue_DequeueError_Continues(t *testing.T) {
	var callCount int64

	ctx, cancel := context.WithCancel(context.Background())

	mq := &mockAzureQueue{
		dequeueFn: func(_ context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			count := atomic.AddInt64(&callCount, 1)
			if count >= 2 {
				cancel()
			}
			return azqueue.DequeueMessagesResponse{}, fmt.Errorf("transient error")
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	select {
	case <-done:
		assert.GreaterOrEqual(t, atomic.LoadInt64(&callCount), int64(2))
	case <-time.After(15 * time.Second):
		t.Fatal("pollQueue did not exit in time")
	}
}

func TestAzurePollQueue_MalformedBase64_DeletesMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var deletedID string
	var deletedReceipt string
	callNum := int64(0)

	mq := &mockAzureQueue{
		dequeueFn: func(_ context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			n := atomic.AddInt64(&callNum, 1)
			if n == 1 {
				return azqueue.DequeueMessagesResponse{
					Messages: []*azqueue.DequeuedMessage{
						{
							MessageText: new("!!!not-base64!!!"),
							MessageID:   new("msg-1"),
							PopReceipt:  new("receipt-1"),
						},
					},
				}, nil
			}
			cancel()
			return azqueue.DequeueMessagesResponse{}, nil
		},
		deleteFn: func(_ context.Context, messageID string, popReceipt string, _ *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error) {
			deletedID = messageID
			deletedReceipt = popReceipt
			return azqueue.DeleteMessageResponse{}, nil
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	select {
	case <-done:
		assert.Equal(t, "msg-1", deletedID)
		assert.Equal(t, "receipt-1", deletedReceipt)
	case <-time.After(10 * time.Second):
		t.Fatal("pollQueue did not exit in time")
	}
}

func TestAzurePollQueue_HandlerError_DoesNotDelete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var deleteCallCount int64
	callNum := int64(0)
	payload := base64.StdEncoding.EncodeToString([]byte("valid payload"))

	mq := &mockAzureQueue{
		dequeueFn: func(_ context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			n := atomic.AddInt64(&callNum, 1)
			if n == 1 {
				return azqueue.DequeueMessagesResponse{
					Messages: []*azqueue.DequeuedMessage{
						{
							MessageText: &payload,
							MessageID:   new("msg-2"),
							PopReceipt:  new("receipt-2"),
						},
					},
				}, nil
			}
			cancel()
			return azqueue.DequeueMessagesResponse{}, nil
		},
		deleteFn: func(_ context.Context, _ string, _ string, _ *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error) {
			atomic.AddInt64(&deleteCallCount, 1)
			return azqueue.DeleteMessageResponse{}, nil
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
		handler: func(data []byte) error {
			return fmt.Errorf("handler failure")
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	select {
	case <-done:
		assert.Equal(t, int64(0), atomic.LoadInt64(&deleteCallCount))
	case <-time.After(10 * time.Second):
		t.Fatal("pollQueue did not exit in time")
	}
}

func TestAzurePollQueue_HandlerSuccess_DeletesMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var deletedID string
	callNum := int64(0)
	payload := base64.StdEncoding.EncodeToString([]byte("good payload"))

	mq := &mockAzureQueue{
		dequeueFn: func(_ context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			n := atomic.AddInt64(&callNum, 1)
			if n == 1 {
				return azqueue.DequeueMessagesResponse{
					Messages: []*azqueue.DequeuedMessage{
						{
							MessageText: &payload,
							MessageID:   new("msg-3"),
							PopReceipt:  new("receipt-3"),
						},
					},
				}, nil
			}
			cancel()
			return azqueue.DequeueMessagesResponse{}, nil
		},
		deleteFn: func(_ context.Context, messageID string, _ string, _ *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error) {
			deletedID = messageID
			return azqueue.DeleteMessageResponse{}, nil
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
		handler: func(data []byte) error {
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	select {
	case <-done:
		assert.Equal(t, "msg-3", deletedID)
	case <-time.After(10 * time.Second):
		t.Fatal("pollQueue did not exit in time")
	}
}

func TestAzurePollQueue_NilFields_Skipped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var deleteCallCount int64
	callNum := int64(0)

	mq := &mockAzureQueue{
		dequeueFn: func(_ context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			n := atomic.AddInt64(&callNum, 1)
			if n == 1 {
				return azqueue.DequeueMessagesResponse{
					Messages: []*azqueue.DequeuedMessage{
						{
							MessageText: nil,
							MessageID:   new("m1"),
							PopReceipt:  new("r1"),
						},
						{
							MessageText: new("text"),
							MessageID:   nil,
							PopReceipt:  new("r2"),
						},
						{
							MessageText: new("text"),
							MessageID:   new("m3"),
							PopReceipt:  nil,
						},
					},
				}, nil
			}
			cancel()
			return azqueue.DequeueMessagesResponse{}, nil
		},
		deleteFn: func(_ context.Context, _ string, _ string, _ *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error) {
			atomic.AddInt64(&deleteCallCount, 1)
			return azqueue.DeleteMessageResponse{}, nil
		},
	}

	handlerCalled := int64(0)
	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
		handler: func(data []byte) error {
			atomic.AddInt64(&handlerCalled, 1)
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	select {
	case <-done:
		assert.Equal(t, int64(0), atomic.LoadInt64(&handlerCalled), "handler should not be called for nil-field messages")
		assert.Equal(t, int64(0), atomic.LoadInt64(&deleteCallCount), "delete should not be called for nil-field messages")
	case <-time.After(10 * time.Second):
		t.Fatal("pollQueue did not exit in time")
	}
}

func TestAzureSubscribe_InitializesFields(t *testing.T) {
	mq := &mockAzureQueue{
		dequeueFn: func(ctx context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			<-ctx.Done()
			return azqueue.DequeueMessagesResponse{}, ctx.Err()
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
	}

	handler := func(data []byte) error { return nil }
	err := p.Subscribe(context.Background(), handler)
	require.NoError(t, err)

	assert.NotNil(t, p.cancel, "cancel should be set after Subscribe")
	assert.NotNil(t, p.handler, "handler should be set after Subscribe")
	assert.NotNil(t, p.pollDone, "pollDone should be set after Subscribe")

	require.NoError(t, p.Close())
}

func TestAzureClose_WaitsPollDone(t *testing.T) {
	mq := &mockAzureQueue{
		dequeueFn: func(ctx context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			<-ctx.Done()
			return azqueue.DequeueMessagesResponse{}, ctx.Err()
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
	}

	handler := func(data []byte) error { return nil }
	err := p.Subscribe(context.Background(), handler)
	require.NoError(t, err)

	closeDone := make(chan struct{})
	go func() {
		defer close(closeDone)
		_ = p.Close()
	}()

	select {
	case <-closeDone:
		// Close returned, meaning it waited for pollDone.
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return in time, may not be waiting for pollDone")
	}
}

func TestAzureClose_CancelsContext(t *testing.T) {
	var cancelCalled int64

	ctx, originalCancel := context.WithCancel(context.Background())

	p := &azureProvider{
		api: newTestAPI(),
		cancel: func() {
			atomic.AddInt64(&cancelCalled, 1)
			originalCancel()
		},
		pollDone: make(chan struct{}),
	}

	// Simulate pollDone being already closed (goroutine finished).
	close(p.pollDone)

	err := p.Close()
	require.NoError(t, err)
	assert.Equal(t, int64(1), atomic.LoadInt64(&cancelCalled), "cancel function should be called exactly once")

	select {
	case <-ctx.Done():
		// Context was cancelled as expected.
	default:
		t.Fatal("underlying context was not cancelled")
	}
}

// --- Tests for azureProvider.UploadFile and WatchFiles via fakeBlobOps ---

func TestAzureProvider_UploadFile_WithOps(t *testing.T) {
	t.Run("happy path encodes metadata", func(t *testing.T) {
		ops := &fakeBlobOps{}
		api := &plugintest.API{}
		stubLogs(api)
		p := &azureProvider{
			containerClient: ops,
			api:             api,
			cfg:             AzureQueueProviderConfig{BlobContainerName: "c1"},
		}
		headers := map[string]string{headerPostID: "p1"}
		require.NoError(t, p.UploadFile(context.Background(), "k", []byte("data"), headers))
		require.Len(t, ops.uploads, 1)
		assert.Equal(t, "k", ops.uploads[0].name)
		assert.NotNil(t, ops.uploads[0].metadata[blobMetadataHeadersKey])
	})

	t.Run("upload error wrapped", func(t *testing.T) {
		ops := &fakeBlobOps{
			uploadFn: func(ctx context.Context, name string, data []byte, metadata map[string]*string) error {
				return fmt.Errorf("boom")
			},
		}
		api := &plugintest.API{}
		stubLogs(api)
		p := &azureProvider{containerClient: ops, api: api}
		err := p.UploadFile(context.Background(), "k", []byte("d"), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to upload blob")
	})
}

func TestAzureProvider_WatchFiles_WithOps(t *testing.T) {
	prev := azureBlobPollInterval
	azureBlobPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { azureBlobPollInterval = prev })

	newProvider := func(ops azureBlobOps) *azureProvider {
		api := &plugintest.API{}
		stubLogs(api)
		return &azureProvider{containerClient: ops, api: api, cfg: AzureQueueProviderConfig{BlobContainerName: "c1"}}
	}

	t.Run("ctx cancel before first tick returns nil", func(t *testing.T) {
		p := newProvider(&fakeBlobOps{})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		assert.NoError(t, p.WatchFiles(ctx, func(k string, d []byte, h map[string]string) error { return nil }))
	})

	t.Run("list error continues until cancel", func(t *testing.T) {
		var calls atomic.Int32
		ops := &fakeBlobOps{
			listFn: func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
				calls.Add(1)
				return nil, fmt.Errorf("list boom")
			},
		}
		p := newProvider(ops)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- p.WatchFiles(ctx, func(k string, d []byte, h map[string]string) error { return nil })
		}()
		time.Sleep(30 * time.Millisecond)
		cancel()
		<-done
		assert.GreaterOrEqual(t, calls.Load(), int32(1))
	})

	t.Run("happy path downloads and deletes", func(t *testing.T) {
		var served atomic.Bool
		ops := &fakeBlobOps{
			listFn: func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
				if served.CompareAndSwap(false, true) {
					return []blobListing{{Name: "a.bin"}}, nil
				}
				return nil, nil
			},
			downloadFn: func(ctx context.Context, name string) ([]byte, error) {
				return []byte("filedata"), nil
			},
		}
		p := newProvider(ops)
		gotKey := make(chan string, 1)
		handler := func(key string, data []byte, headers map[string]string) error {
			select {
			case gotKey <- key:
			default:
			}
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- p.WatchFiles(ctx, handler) }()
		select {
		case k := <-gotKey:
			assert.Equal(t, "a.bin", k)
		case <-time.After(time.Second):
			t.Fatal("handler not called")
		}
		cancel()
		<-done
		ops.mu.Lock()
		assert.Contains(t, ops.deletes, "a.bin")
		ops.mu.Unlock()
	})

	t.Run("download error skips handler", func(t *testing.T) {
		ops := &fakeBlobOps{
			listFn: func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
				return []blobListing{{Name: "a.bin"}}, nil
			},
			downloadFn: func(ctx context.Context, name string) ([]byte, error) {
				return nil, fmt.Errorf("bad")
			},
		}
		p := newProvider(ops)
		handlerCalled := make(chan struct{}, 1)
		handler := func(key string, data []byte, headers map[string]string) error {
			handlerCalled <- struct{}{}
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- p.WatchFiles(ctx, handler) }()
		time.Sleep(30 * time.Millisecond)
		cancel()
		<-done
		select {
		case <-handlerCalled:
			t.Fatal("handler should not have been called")
		default:
		}
	})

	t.Run("handler error skips delete", func(t *testing.T) {
		ops := &fakeBlobOps{
			listFn: func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
				return []blobListing{{Name: "a.bin"}}, nil
			},
			downloadFn: func(ctx context.Context, name string) ([]byte, error) { return []byte("d"), nil },
		}
		p := newProvider(ops)
		handler := func(key string, data []byte, headers map[string]string) error {
			return fmt.Errorf("h")
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- p.WatchFiles(ctx, handler) }()
		time.Sleep(30 * time.Millisecond)
		cancel()
		<-done
		ops.mu.Lock()
		assert.Empty(t, ops.deletes)
		ops.mu.Unlock()
	})

	t.Run("delete error logged and continues", func(t *testing.T) {
		ops := &fakeBlobOps{
			listFn: func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
				return []blobListing{{Name: "a.bin"}}, nil
			},
			downloadFn: func(ctx context.Context, name string) ([]byte, error) { return []byte("d"), nil },
			deleteFn:   func(ctx context.Context, name string) error { return fmt.Errorf("del") },
		}
		p := newProvider(ops)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- p.WatchFiles(ctx, func(k string, d []byte, h map[string]string) error { return nil })
		}()
		time.Sleep(30 * time.Millisecond)
		cancel()
		<-done
	})
}

func TestNewAzureProvider_InvalidCredential(t *testing.T) {
	api := &plugintest.API{}
	cfg := AzureQueueProviderConfig{
		AccountName:     "acct",
		AccountKey:      "not-base64-!!!",
		QueueServiceURL: "https://acct.queue.core.windows.net",
		QueueName:       "q1",
	}
	_, err := newAzureProvider(context.Background(), cfg, api)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shared key credential")
}

func TestTestAzureQueueConnection_InvalidCredential(t *testing.T) {
	cfg := AzureQueueProviderConfig{
		AccountName:     "acct",
		AccountKey:      "not-base64-!!!",
		QueueServiceURL: "https://acct.queue.core.windows.net",
		QueueName:       "q1",
	}
	err := testAzureQueueConnection(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shared key credential")
}

// ----- Service Principal coverage (ported from upstream) -----

func validSPQueueCfg() AzureQueueProviderConfig {
	return AzureQueueProviderConfig{
		QueueServiceURL: "https://acct.queue.core.windows.net",
		QueueName:       "q1",
		AuthMode:        AzureAuthServicePrincipal,
		AzureCloud:      AzureCloudPublic,
		TenantID:        "11111111-2222-3333-4444-555555555555",
		ClientID:        "client-uuid",
		ClientSecret:    "topsec",
	}
}

func TestNewAzureProvider_SPMode_HappyPath(t *testing.T) {
	api := &plugintest.API{}
	defer api.AssertExpectations(t)
	// In SP mode the constructor must emit the audit log line; no Create()
	// call against Azure (skipped in SP mode).
	api.On("LogInfo", "Azure auth (service-principal): credential constructed",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return().Once()

	p, err := newAzureProvider(t.Context(), validSPQueueCfg(), api)
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestNewAzureProvider_SPMode_BadCloud(t *testing.T) {
	cfg := validSPQueueCfg()
	cfg.AzureCloud = "atlantis"
	_, err := newAzureProvider(t.Context(), cfg, &plugintest.API{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "azure-queue config")
	assert.Contains(t, err.Error(), "unknown azure_cloud")
}

func TestNewAzureProvider_SPMode_BadTenant(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogError", "Azure Queue: service principal credential failed",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return().Once()
	defer api.AssertExpectations(t)

	cfg := validSPQueueCfg()
	cfg.TenantID = "" // azidentity rejects empty tenant
	_, err := newAzureProvider(t.Context(), cfg, api)
	require.Error(t, err)
}

func TestNewAzureProvider_UnknownAuthMode(t *testing.T) {
	cfg := validSPQueueCfg()
	cfg.AuthMode = "weird-mode"
	_, err := newAzureProvider(t.Context(), cfg, &plugintest.API{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth_mode")
}

// ----- newAzureProviderBlobSidecar coverage -----

func TestNewAzureProviderBlobSidecar_SPMode_NoAutoCreate(t *testing.T) {
	// SP mode must NOT call CreateContainer. We can't easily mock the
	// container.Client constructor; instead exercise the function with a
	// valid SP cfg and verify it returns a non-nil ops adapter without
	// invoking any API method (no LogWarn, no LogError, because the
	// constructor doesn't reach the create-container path).
	api := &plugintest.API{}
	defer api.AssertExpectations(t)

	cfg := AzureQueueProviderConfig{
		BlobServiceURL:    "https://acct.blob.core.windows.net",
		BlobContainerName: "c1",
		TenantID:          "11111111-2222-3333-4444-555555555555",
		ClientID:          "client-uuid",
		ClientSecret:      "topsec",
	}
	ops, err := newAzureProviderBlobSidecar(t.Context(), cfg, AzureAuthServicePrincipal, cloud.AzurePublic, api)
	require.NoError(t, err)
	require.NotNil(t, ops)
}

func TestNewAzureProviderBlobSidecar_SPMode_BadTenant(t *testing.T) {
	cfg := AzureQueueProviderConfig{
		BlobServiceURL:    "https://acct.blob.core.windows.net",
		BlobContainerName: "c1",
		TenantID:          "", // azidentity rejects
		ClientID:          "client-uuid",
		ClientSecret:      "topsec",
	}
	_, err := newAzureProviderBlobSidecar(t.Context(), cfg, AzureAuthServicePrincipal, cloud.AzurePublic, &plugintest.API{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "azure-queue blob sidecar SP credential")
}

func TestNewAzureProviderBlobSidecar_SharedKey_BadAccountKey(t *testing.T) {
	cfg := AzureQueueProviderConfig{
		BlobServiceURL:    "https://acct.blob.core.windows.net",
		BlobContainerName: "c1",
		AccountName:       "acct",
		AccountKey:        "not-valid-base64-!!!",
	}
	_, err := newAzureProviderBlobSidecar(t.Context(), cfg, AzureAuthSharedKey, cloud.AzurePublic, &plugintest.API{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "azure-queue blob sidecar shared key")
}

func TestNewAzureProviderBlobSidecar_UnknownAuthMode(t *testing.T) {
	cfg := AzureQueueProviderConfig{
		BlobServiceURL:    "https://acct.blob.core.windows.net",
		BlobContainerName: "c1",
	}
	_, err := newAzureProviderBlobSidecar(t.Context(), cfg, "weird-mode", cloud.AzurePublic, &plugintest.API{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "azure-queue blob sidecar: unknown auth_mode")
}

// ----- testAzureQueueConnection SP branch coverage -----

func TestTestAzureQueueConnection_BadCloud(t *testing.T) {
	cfg := validSPQueueCfg()
	cfg.AzureCloud = "atlantis"
	err := testAzureQueueConnection(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "azure-queue config")
	assert.Contains(t, err.Error(), "unknown azure_cloud")
}

func TestTestAzureQueueConnection_SPMode_BadTenant(t *testing.T) {
	cfg := validSPQueueCfg()
	cfg.TenantID = ""
	err := testAzureQueueConnection(cfg)
	require.Error(t, err)
	// Falls through buildClientSecretCredential, which wraps with the
	// "azidentity NewClientSecretCredential" prefix.
	assert.Contains(t, err.Error(), "azidentity NewClientSecretCredential")
}

func TestTestAzureQueueConnection_UnknownAuthMode(t *testing.T) {
	cfg := validSPQueueCfg()
	cfg.AuthMode = "weird-mode"
	err := testAzureQueueConnection(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth_mode")
}
