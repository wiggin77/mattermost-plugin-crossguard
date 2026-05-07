package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBlobOps is a test double implementing azureBlobOps with function
// pointer overrides and recorded state for assertions.
type fakeBlobOps struct {
	mu          sync.Mutex
	createFn    func(ctx context.Context) error
	uploadFn    func(ctx context.Context, name string, data []byte, metadata map[string]*string) error
	downloadFn  func(ctx context.Context, name string) ([]byte, error)
	deleteFn    func(ctx context.Context, name string) error
	listFn      func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error)
	uploads     []fakeUpload
	deletes     []string
	downloads   []string
	createCount int
}

type fakeUpload struct {
	name     string
	data     []byte
	metadata map[string]*string
}

func (f *fakeBlobOps) CreateContainer(ctx context.Context) error {
	f.mu.Lock()
	f.createCount++
	f.mu.Unlock()
	if f.createFn != nil {
		return f.createFn(ctx)
	}
	return nil
}

func (f *fakeBlobOps) UploadBlob(ctx context.Context, name string, data []byte, metadata map[string]*string) error {
	cp := make([]byte, len(data))
	copy(cp, data)
	f.mu.Lock()
	f.uploads = append(f.uploads, fakeUpload{name: name, data: cp, metadata: metadata})
	f.mu.Unlock()
	if f.uploadFn != nil {
		return f.uploadFn(ctx, name, data, metadata)
	}
	return nil
}

func (f *fakeBlobOps) DownloadBlob(ctx context.Context, name string) ([]byte, error) {
	f.mu.Lock()
	f.downloads = append(f.downloads, name)
	f.mu.Unlock()
	if f.downloadFn != nil {
		return f.downloadFn(ctx, name)
	}
	return nil, nil
}

func (f *fakeBlobOps) DeleteBlob(ctx context.Context, name string) error {
	f.mu.Lock()
	f.deletes = append(f.deletes, name)
	f.mu.Unlock()
	if f.deleteFn != nil {
		return f.deleteFn(ctx, name)
	}
	return nil
}

func (f *fakeBlobOps) ListBlobs(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
	if f.listFn != nil {
		return f.listFn(ctx, prefix, includeMetadata)
	}
	return nil, nil
}

// fakeKV implements kvClient for tests.
type fakeKV struct {
	mu      sync.Mutex
	getFn   func(key string, o any) error
	setFn   func(key string, value any, options ...pluginapi.KVSetOption) (bool, error)
	delFn   func(key string) error
	deletes []string
}

func (f *fakeKV) Get(key string, o any) error {
	if f.getFn != nil {
		return f.getFn(key, o)
	}
	return nil
}

func (f *fakeKV) Set(key string, value any, options ...pluginapi.KVSetOption) (bool, error) {
	if f.setFn != nil {
		return f.setFn(key, value, options...)
	}
	return true, nil
}

func (f *fakeKV) Delete(key string) error {
	f.mu.Lock()
	f.deletes = append(f.deletes, key)
	f.mu.Unlock()
	if f.delFn != nil {
		return f.delFn(key)
	}
	return nil
}

// newTestBlobProvider returns a minimal azureBlobProvider with a fakeBlobOps
// container client, suitable for tests that do not require real Azure SDK calls.
func newTestBlobProvider(t *testing.T) (*azureBlobProvider, *plugintest.API, *fakeKV) {
	t.Helper()
	a, api, kv, _ := newTestBlobProviderWithOps(t)
	return a, api, kv
}

func newTestBlobProviderWithOps(t *testing.T) (*azureBlobProvider, *plugintest.API, *fakeKV, *fakeBlobOps) {
	t.Helper()
	api := &plugintest.API{}
	stubLogs(api)
	kv := &fakeKV{}
	ops := &fakeBlobOps{}
	a := &azureBlobProvider{
		containerClient: ops,
		api:             api,
		kv:              kv,
		cfg:             AzureBlobProviderConfig{BlobContainerName: "c1"},
		nodeID:          "node-1",
		connName:        "high",
		walDir:          filepath.Join(t.TempDir(), "wal"),
	}
	require.NoError(t, os.MkdirAll(a.walDir, 0o750))
	return a, api, kv, ops
}

func TestAzureBlobProvider_InterfaceConformance(t *testing.T) {
	var _ QueueProvider = (*azureBlobProvider)(nil)
}

func TestBlobLockKey_HashesStably(t *testing.T) {
	k1 := blobLockKey("messages/high/node-1-42.jsonl")
	k2 := blobLockKey("messages/high/node-1-42.jsonl")
	assert.Equal(t, k1, k2)
	assert.True(t, strings.HasPrefix(k1, blobLockKeyPrefix))

	k3 := blobLockKey("messages/high/other.jsonl")
	assert.NotEqual(t, k1, k3)
}

func TestBlobLockKey_UnderKVLengthLimit(t *testing.T) {
	long := strings.Repeat("x", 2000)
	k := blobLockKey("messages/conn/" + long + ".jsonl")
	// pluginapi KV keys must be <= 150 chars; our hashed key should be far below.
	assert.LessOrEqual(t, len(k), 64)
}

func TestAzureBlobProvider_MaxMessageSize(t *testing.T) {
	a := &azureBlobProvider{}
	assert.Equal(t, 0, a.MaxMessageSize())
}

func TestAzureBlobProvider_OpenWALFileLocked(t *testing.T) {
	a, _, _ := newTestBlobProvider(t)
	a.walMu.Lock()
	require.NoError(t, a.openWALFileLocked())
	a.walMu.Unlock()

	require.NotNil(t, a.walFile)
	assert.FileExists(t, a.walPath)
	assert.True(t, strings.HasSuffix(a.walPath, walFileExt))
	assert.True(t, strings.HasPrefix(filepath.Base(a.walPath), "node-1-"))

	_ = a.walFile.Close()
}

func TestAzureBlobProvider_OpenWALFileLocked_Error(t *testing.T) {
	a, _, _ := newTestBlobProvider(t)
	// Point walDir at a nonexistent path so OpenFile fails.
	a.walDir = filepath.Join(t.TempDir(), "does", "not", "exist")

	a.walMu.Lock()
	err := a.openWALFileLocked()
	a.walMu.Unlock()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open WAL file")
}

func TestAzureBlobProvider_CountWALFiles(t *testing.T) {
	a, _, _ := newTestBlobProvider(t)
	assert.Equal(t, 0, a.countWALFiles())

	// Create two .jsonl files and one unrelated file.
	require.NoError(t, os.WriteFile(filepath.Join(a.walDir, "a.jsonl"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(a.walDir, "b.jsonl"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(a.walDir, "companion.files.json"), []byte("[]"), 0o600))
	assert.Equal(t, 2, a.countWALFiles())

	// Missing dir returns 0, not error.
	a.walDir = filepath.Join(t.TempDir(), "missing")
	assert.Equal(t, 0, a.countWALFiles())
}

func TestAzureBlobProvider_Publish(t *testing.T) {
	t.Run("writes data plus newline", func(t *testing.T) {
		a, _, _ := newTestBlobProvider(t)
		require.NoError(t, a.Publish(t.Context(), []byte("hello")))
		require.NoError(t, a.Publish(t.Context(), []byte("world")))

		// File should exist with both lines.
		require.NotEmpty(t, a.walPath)
		data, err := os.ReadFile(a.walPath)
		require.NoError(t, err)
		assert.Equal(t, "hello\nworld\n", string(data))

		_ = a.walFile.Close()
	})

	t.Run("does not mutate caller buffer", func(t *testing.T) {
		a, _, _ := newTestBlobProvider(t)
		data := make([]byte, 5, 64)
		copy(data, "hello")
		require.NoError(t, a.Publish(t.Context(), data))
		// Capacity byte at index 5 must not have been clobbered by an in-place append.
		buf := data[:6:6] //nolint:gosec // intentional cap inclusion to read just-past-len
		assert.Equal(t, byte(0), buf[5])
		_ = a.walFile.Close()
	})

	t.Run("backpressure at walMaxFiles", func(t *testing.T) {
		a, _, _ := newTestBlobProvider(t)
		for i := range walMaxFiles {
			path := filepath.Join(a.walDir, "stale-"+string(rune('a'+i%26))+string(rune('a'+i/26))+".jsonl")
			require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
		}
		err := a.Publish(t.Context(), []byte("x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "WAL backpressure")
	})

	t.Run("open error propagates", func(t *testing.T) {
		a, _, _ := newTestBlobProvider(t)
		// Remove walDir to cause open to fail.
		require.NoError(t, os.RemoveAll(a.walDir))
		err := a.Publish(t.Context(), []byte("x"))
		require.Error(t, err)
	})

	t.Run("write error propagates", func(t *testing.T) {
		a, _, _ := newTestBlobProvider(t)
		a.walMu.Lock()
		require.NoError(t, a.openWALFileLocked())
		// Close behind Publish's back so Write fails with "file already closed".
		require.NoError(t, a.walFile.Close())
		a.walMu.Unlock()

		err := a.Publish(t.Context(), []byte("x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "WAL")
	})
}

func TestAzureBlobProvider_TryAcquireBlobLock(t *testing.T) {
	t.Run("fresh lock success", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		kv.getFn = func(key string, o any) error { return nil }
		kv.setFn = func(key string, value any, options ...pluginapi.KVSetOption) (bool, error) {
			return true, nil
		}
		assert.True(t, a.tryAcquireBlobLock("messages/high/a.jsonl"))
	})

	t.Run("get error returns false", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		kv.getFn = func(key string, o any) error { return errors.New("boom") }
		assert.False(t, a.tryAcquireBlobLock("messages/high/a.jsonl"))
	})

	t.Run("set error returns false", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		kv.setFn = func(key string, value any, options ...pluginapi.KVSetOption) (bool, error) {
			return false, errors.New("cas failed")
		}
		assert.False(t, a.tryAcquireBlobLock("messages/high/a.jsonl"))
	})

	t.Run("existing fresh lock returns false", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		raw, _ := json.Marshal(blobLock{Node: "other", Acquired: time.Now().UnixMilli()})
		kv.getFn = func(key string, o any) error {
			// The production code passes a *[]byte - set it.
			if p, ok := o.(*[]byte); ok {
				*p = raw
			}
			return nil
		}
		assert.False(t, a.tryAcquireBlobLock("messages/high/a.jsonl"))
	})

	t.Run("stale lock reclaim success", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		stale := blobLock{Node: "other", Acquired: time.Now().Add(-2 * blobLockMaxAge).UnixMilli()}
		raw, _ := json.Marshal(stale)
		kv.getFn = func(key string, o any) error {
			if p, ok := o.(*[]byte); ok {
				*p = raw
			}
			return nil
		}
		kv.setFn = func(key string, value any, options ...pluginapi.KVSetOption) (bool, error) {
			return true, nil
		}
		assert.True(t, a.tryAcquireBlobLock("messages/high/a.jsonl"))
	})

	t.Run("stale lock reclaim set error", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		stale := blobLock{Node: "other", Acquired: time.Now().Add(-2 * blobLockMaxAge).UnixMilli()}
		raw, _ := json.Marshal(stale)
		kv.getFn = func(key string, o any) error {
			if p, ok := o.(*[]byte); ok {
				*p = raw
			}
			return nil
		}
		kv.setFn = func(key string, value any, options ...pluginapi.KVSetOption) (bool, error) {
			return false, errors.New("cas failed")
		}
		assert.False(t, a.tryAcquireBlobLock("messages/high/a.jsonl"))
	})

	t.Run("corrupt json attempts reclaim", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		kv.getFn = func(key string, o any) error {
			if p, ok := o.(*[]byte); ok {
				*p = []byte("not json")
			}
			return nil
		}
		setCalled := false
		kv.setFn = func(key string, value any, options ...pluginapi.KVSetOption) (bool, error) {
			setCalled = true
			return true, nil
		}
		assert.True(t, a.tryAcquireBlobLock("messages/high/a.jsonl"))
		assert.True(t, setCalled)
	})
}

func TestAzureBlobProvider_ReleaseBlobLock(t *testing.T) {
	t.Run("delete success", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		a.releaseBlobLock("messages/high/a.jsonl")
		assert.Len(t, kv.deletes, 1)
		assert.True(t, strings.HasPrefix(kv.deletes[0], blobLockKeyPrefix))
	})

	t.Run("delete error is logged but does not panic", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		kv.delFn = func(key string) error { return errors.New("gone") }
		assert.NotPanics(t, func() {
			a.releaseBlobLock("messages/high/a.jsonl")
		})
	})
}

func TestAzureBlobProvider_Flush_EarlyReturn(t *testing.T) {
	a, _, _ := newTestBlobProvider(t)
	// No WAL file, no pending: should return cleanly without touching containerClient.
	a.flush(t.Context())
	assert.Nil(t, a.walFile)
}

func TestAzureBlobProvider_Close_NilFields(t *testing.T) {
	a := &azureBlobProvider{}
	assert.NoError(t, a.Close())
	// Idempotent.
	assert.NoError(t, a.Close())
}

func TestAzureBlobProvider_RecoverWALOnStartup_NoRoot(t *testing.T) {
	a, _, _ := newTestBlobProvider(t)
	// Force walDir root that does not exist - should be a no-op.
	// We can't override walDirRoot easily, so just ensure no panic when the
	// current walDir is missing after we move it.
	require.NoError(t, os.RemoveAll(a.walDir))
	assert.NotPanics(t, func() {
		a.recoverWALOnStartup(t.Context())
	})
}

// --- Phase 3: tests driving azureBlobOps via fakeBlobOps ---

func TestAzureBlobProvider_UploadWALFile(t *testing.T) {
	t.Run("happy path uploads with messages prefix", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		p := filepath.Join(a.walDir, "batch.jsonl")
		require.NoError(t, os.WriteFile(p, []byte("line1\nline2\n"), 0o600))

		require.NoError(t, a.uploadWALFile(t.Context(), p))
		require.Len(t, ops.uploads, 1)
		assert.Equal(t, "messages/high/batch.jsonl", ops.uploads[0].name)
		assert.Equal(t, "line1\nline2\n", string(ops.uploads[0].data))
	})

	t.Run("empty file is no-op", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		p := filepath.Join(a.walDir, "empty.jsonl")
		require.NoError(t, os.WriteFile(p, nil, 0o600))

		require.NoError(t, a.uploadWALFile(t.Context(), p))
		assert.Empty(t, ops.uploads)
	})

	t.Run("missing file returns read error", func(t *testing.T) {
		a, _, _, _ := newTestBlobProviderWithOps(t)
		err := a.uploadWALFile(t.Context(), filepath.Join(a.walDir, "missing.jsonl"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read WAL")
	})

	t.Run("upload error wrapped", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		ops.uploadFn = func(ctx context.Context, name string, data []byte, metadata map[string]*string) error {
			return errors.New("boom")
		}
		p := filepath.Join(a.walDir, "b.jsonl")
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))

		err := a.uploadWALFile(t.Context(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "upload blob")
	})
}

func TestAzureBlobProvider_Flush_WithUpload(t *testing.T) {
	t.Run("rotates WAL, uploads, deletes on success", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		require.NoError(t, a.Publish(t.Context(), []byte("hello")))
		walPath := a.walPath

		a.flush(t.Context())

		require.Len(t, ops.uploads, 1)
		assert.True(t, strings.HasPrefix(ops.uploads[0].name, "messages/high/"))
		_, err := os.Stat(walPath)
		assert.True(t, os.IsNotExist(err), "WAL file should be removed on success")
		assert.Nil(t, a.walFile)
		assert.Empty(t, a.walPath)
	})

	t.Run("upload failure leaves WAL on disk", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		ops.uploadFn = func(ctx context.Context, name string, data []byte, metadata map[string]*string) error {
			return errors.New("boom")
		}
		require.NoError(t, a.Publish(t.Context(), []byte("hello")))
		walPath := a.walPath

		a.flush(t.Context())

		_, err := os.Stat(walPath)
		assert.NoError(t, err, "WAL file must persist for recovery")
	})
}

func TestAzureBlobProvider_UploadFile(t *testing.T) {
	t.Run("encodes headers in metadata", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		headers := map[string]string{headerPostID: "p1", headerConnName: "high", headerFilename: "a.pdf"}

		require.NoError(t, a.UploadFile(t.Context(), "p1/f1", []byte("data"), headers))
		require.Len(t, ops.uploads, 1)
		assert.Equal(t, "files/p1/f1", ops.uploads[0].name)
		assert.NotNil(t, ops.uploads[0].metadata[blobMetadataHeadersKey])
	})

	t.Run("upload error wrapped", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		ops.uploadFn = func(ctx context.Context, name string, data []byte, metadata map[string]*string) error {
			return errors.New("boom")
		}
		err := a.UploadFile(t.Context(), "k", []byte("d"), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to upload blob")
	})
}

func TestAzureBlobProvider_ProcessBlob(t *testing.T) {
	acquireLock := func(kv *fakeKV) {
		store := map[string][]byte{}
		kv.getFn = func(key string, o any) error {
			if p, ok := o.(*[]byte); ok {
				*p = append((*p)[:0], store[key]...)
			}
			return nil
		}
		kv.setFn = func(key string, value any, options ...pluginapi.KVSetOption) (bool, error) {
			if b, err := json.Marshal(value); err == nil {
				store[key] = b
			}
			return true, nil
		}
		kv.delFn = func(key string) error {
			delete(store, key)
			return nil
		}
	}

	t.Run("lock not acquired skips download", func(t *testing.T) {
		a, _, kv, ops := newTestBlobProviderWithOps(t)
		kv.getFn = func(key string, o any) error { return errors.New("kv down") }
		a.handler = func(data []byte) error { return nil }

		a.processBlob(t.Context(), "messages/high/a.jsonl")
		assert.Empty(t, ops.downloads)
	})

	t.Run("download error releases lock", func(t *testing.T) {
		a, _, kv, ops := newTestBlobProviderWithOps(t)
		acquireLock(kv)
		ops.downloadFn = func(ctx context.Context, name string) ([]byte, error) {
			return nil, errors.New("bad")
		}
		a.handler = func(data []byte) error { return nil }

		a.processBlob(t.Context(), "messages/high/a.jsonl")
		assert.NotEmpty(t, kv.deletes)
		assert.Empty(t, ops.deletes)
	})

	t.Run("handler error skips delete", func(t *testing.T) {
		a, _, kv, ops := newTestBlobProviderWithOps(t)
		acquireLock(kv)
		ops.downloadFn = func(ctx context.Context, name string) ([]byte, error) {
			return []byte("line1\nline2"), nil
		}
		a.handler = func(data []byte) error { return errors.New("handler") }

		a.processBlob(t.Context(), "messages/high/a.jsonl")
		assert.Empty(t, ops.deletes)
		assert.NotEmpty(t, kv.deletes, "lock released on failure")
	})

	t.Run("happy path deletes blob and releases lock", func(t *testing.T) {
		a, _, kv, ops := newTestBlobProviderWithOps(t)
		acquireLock(kv)
		ops.downloadFn = func(ctx context.Context, name string) ([]byte, error) {
			return []byte("line1\n\nline2\n"), nil
		}
		lines := 0
		a.handler = func(data []byte) error { lines++; return nil }

		a.processBlob(t.Context(), "messages/high/a.jsonl")
		assert.Equal(t, 2, lines, "empty lines skipped")
		assert.Equal(t, []string{"messages/high/a.jsonl"}, ops.deletes)
		assert.NotEmpty(t, kv.deletes)
	})

	t.Run("delete error releases lock", func(t *testing.T) {
		a, _, kv, ops := newTestBlobProviderWithOps(t)
		acquireLock(kv)
		ops.downloadFn = func(ctx context.Context, name string) ([]byte, error) { return []byte("x"), nil }
		ops.deleteFn = func(ctx context.Context, name string) error { return errors.New("del") }
		a.handler = func(data []byte) error { return nil }

		a.processBlob(t.Context(), "messages/high/a.jsonl")
		assert.NotEmpty(t, kv.deletes)
	})

	t.Run("already processed blob delete succeeds", func(t *testing.T) {
		a, _, kv, ops := newTestBlobProviderWithOps(t)
		blobName := "messages/high/a.jsonl"
		store := map[string][]byte{}
		// Seed the processed marker so isBlobProcessed returns true.
		store[blobProcessedKey(blobName)] = []byte{1}
		kv.getFn = func(key string, o any) error {
			if p, ok := o.(*[]byte); ok {
				*p = append((*p)[:0], store[key]...)
			}
			return nil
		}
		kv.setFn = func(key string, value any, options ...pluginapi.KVSetOption) (bool, error) {
			if b, err2 := json.Marshal(value); err2 == nil {
				store[key] = b
			}
			return true, nil
		}
		kv.delFn = func(key string) error {
			delete(store, key)
			return nil
		}
		a.handler = func(data []byte) error { return nil }

		a.processBlob(t.Context(), blobName)

		// Blob should be deleted without download.
		assert.Empty(t, ops.downloads, "should skip download for already-processed blob")
		assert.Equal(t, []string{blobName}, ops.deletes, "should delete the blob")
	})

	t.Run("already processed blob delete fails", func(t *testing.T) {
		a, _, kv, ops := newTestBlobProviderWithOps(t)
		blobName := "messages/high/a.jsonl"
		store := map[string][]byte{}
		store[blobProcessedKey(blobName)] = []byte{1}
		kv.getFn = func(key string, o any) error {
			if p, ok := o.(*[]byte); ok {
				*p = append((*p)[:0], store[key]...)
			}
			return nil
		}
		kv.setFn = func(key string, value any, options ...pluginapi.KVSetOption) (bool, error) {
			if b, err2 := json.Marshal(value); err2 == nil {
				store[key] = b
			}
			return true, nil
		}
		kv.delFn = func(key string) error {
			delete(store, key)
			return nil
		}
		ops.deleteFn = func(ctx context.Context, name string) error { return errors.New("unavailable") }
		a.handler = func(data []byte) error { return nil }

		a.processBlob(t.Context(), blobName)

		// Download still skipped, delete attempted but failed.
		assert.Empty(t, ops.downloads)
		assert.NotEmpty(t, ops.deletes, "delete was attempted")
		// Processed marker should NOT have been cleared since delete failed.
		assert.Contains(t, store, blobProcessedKey(blobName), "marker kept on delete failure")
	})
}

func TestAzureBlobProvider_PollBlobs(t *testing.T) {
	prev := azureBlobBatchPollInterval
	azureBlobBatchPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { azureBlobBatchPollInterval = prev })

	t.Run("ctx cancel returns cleanly", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		a.handler = func(data []byte) error { return nil }
		ops.listFn = func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
			return nil, nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { a.pollBlobs(ctx); close(done) }()
		time.Sleep(20 * time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("pollBlobs did not return after cancel")
		}
	})

	t.Run("list error logged, loop continues until cancel", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		a.handler = func(data []byte) error { return nil }
		var calls atomic.Int32
		ops.listFn = func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
			calls.Add(1)
			return nil, errors.New("list boom")
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { a.pollBlobs(ctx); close(done) }()
		time.Sleep(30 * time.Millisecond)
		cancel()
		<-done
		assert.GreaterOrEqual(t, calls.Load(), int32(1))
	})

	t.Run("processes listed blobs", func(t *testing.T) {
		a, _, kv, ops := newTestBlobProviderWithOps(t)
		kv.getFn = func(key string, o any) error { return nil }
		kv.setFn = func(key string, value any, options ...pluginapi.KVSetOption) (bool, error) {
			return true, nil
		}
		var handled atomic.Int32
		a.handler = func(data []byte) error { handled.Add(1); return nil }

		var listed atomic.Bool
		ops.listFn = func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
			if listed.CompareAndSwap(false, true) {
				return []blobListing{{Name: "messages/high/a.jsonl"}}, nil
			}
			return nil, nil
		}
		ops.downloadFn = func(ctx context.Context, name string) ([]byte, error) {
			return []byte("one\n"), nil
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { a.pollBlobs(ctx); close(done) }()
		assert.Eventually(t, func() bool { return handled.Load() >= 1 }, time.Second, 5*time.Millisecond)
		cancel()
		<-done
	})
}

func TestAzureBlobProvider_WatchFiles(t *testing.T) {
	prev := azureBlobBatchPollInterval
	azureBlobBatchPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { azureBlobBatchPollInterval = prev })

	acquireLock := func(kv *fakeKV) {
		kv.getFn = func(key string, o any) error { return nil }
		kv.setFn = func(key string, value any, options ...pluginapi.KVSetOption) (bool, error) {
			return true, nil
		}
	}

	t.Run("double call returns error", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		// listFn blocks so the first WatchFiles stays alive long enough for
		// the second call to observe the watching flag.
		ops.listFn = func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- a.WatchFiles(ctx, func(string, []byte, map[string]string) error { return nil }) }()
		// Give the goroutine time to set the watching flag.
		time.Sleep(20 * time.Millisecond)
		err := a.WatchFiles(ctx, func(string, []byte, map[string]string) error { return nil })
		require.Error(t, err)
		assert.Contains(t, err.Error(), "WatchFiles called twice")
		cancel()
		<-done
	})

	t.Run("ctx cancel before tick returns nil", func(t *testing.T) {
		a, _, _, _ := newTestBlobProviderWithOps(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := a.WatchFiles(ctx, func(key string, data []byte, headers map[string]string) error { return nil })
		assert.NoError(t, err)
	})

	t.Run("happy path strips prefix and deletes blob", func(t *testing.T) {
		a, _, kv, ops := newTestBlobProviderWithOps(t)
		acquireLock(kv)
		var served atomic.Bool
		ops.listFn = func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
			if served.CompareAndSwap(false, true) {
				return []blobListing{{Name: "files/p1/f1"}}, nil
			}
			return nil, nil
		}
		ops.downloadFn = func(ctx context.Context, name string) ([]byte, error) { return []byte("d"), nil }

		gotKey := make(chan string, 1)
		handler := func(key string, data []byte, headers map[string]string) error {
			select {
			case gotKey <- key:
			default:
			}
			return nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- a.WatchFiles(ctx, handler) }()
		select {
		case k := <-gotKey:
			assert.Equal(t, "p1/f1", k)
		case <-time.After(time.Second):
			t.Fatal("handler not called")
		}
		cancel()
		<-done
		ops.mu.Lock()
		assert.Contains(t, ops.deletes, "files/p1/f1")
		ops.mu.Unlock()
	})

	t.Run("download error skips handler, releases lock", func(t *testing.T) {
		a, _, kv, ops := newTestBlobProviderWithOps(t)
		acquireLock(kv)
		ops.listFn = func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
			return []blobListing{{Name: "files/p1/f1"}}, nil
		}
		ops.downloadFn = func(ctx context.Context, name string) ([]byte, error) { return nil, errors.New("bad") }
		handlerCalled := false
		handler := func(key string, data []byte, headers map[string]string) error {
			handlerCalled = true
			return nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- a.WatchFiles(ctx, handler) }()
		time.Sleep(30 * time.Millisecond)
		cancel()
		<-done
		assert.False(t, handlerCalled)
		assert.Empty(t, ops.deletes)
	})

	t.Run("handler error skips delete", func(t *testing.T) {
		a, _, kv, ops := newTestBlobProviderWithOps(t)
		acquireLock(kv)
		ops.listFn = func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
			return []blobListing{{Name: "files/p1/f1"}}, nil
		}
		ops.downloadFn = func(ctx context.Context, name string) ([]byte, error) { return []byte("d"), nil }
		handler := func(key string, data []byte, headers map[string]string) error {
			return errors.New("h")
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- a.WatchFiles(ctx, handler) }()
		time.Sleep(30 * time.Millisecond)
		cancel()
		<-done
		assert.Empty(t, ops.deletes)
	})

	t.Run("list error logged, loop continues", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		var calls atomic.Int32
		ops.listFn = func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
			calls.Add(1)
			return nil, errors.New("boom")
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			done <- a.WatchFiles(ctx, func(k string, d []byte, h map[string]string) error { return nil })
		}()
		time.Sleep(30 * time.Millisecond)
		cancel()
		<-done
		assert.GreaterOrEqual(t, calls.Load(), int32(1))
	})
}

func TestAzureBlobProvider_Subscribe_And_Close(t *testing.T) {
	prev := azureBlobBatchPollInterval
	azureBlobBatchPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { azureBlobBatchPollInterval = prev })

	a, _, _, ops := newTestBlobProviderWithOps(t)
	ops.listFn = func(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
		return nil, nil
	}
	require.NoError(t, a.Subscribe(t.Context(), func(data []byte) error { return nil }))
	time.Sleep(15 * time.Millisecond)
	assert.NoError(t, a.Close())
	assert.NoError(t, a.Close(), "Close is idempotent")
}

func TestAzureBlobProvider_StartFlushLoop(t *testing.T) {
	a, _, _, ops := newTestBlobProviderWithOps(t)
	a.flushInterval = 10 * time.Millisecond
	require.NoError(t, a.Publish(t.Context(), []byte("hello")))

	loopCtx, loopCancel := context.WithCancel(t.Context())
	a.outCancel = loopCancel
	a.startFlushLoop(loopCtx)
	assert.Eventually(t, func() bool {
		ops.mu.Lock()
		defer ops.mu.Unlock()
		return len(ops.uploads) >= 1
	}, time.Second, 5*time.Millisecond)

	assert.NoError(t, a.Close())
}

func TestAzureBlobProvider_RecoverDirectory(t *testing.T) {
	t.Run("mixed contents uploads jsonl only", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		dir := t.TempDir()
		walName := "conn-1-0.jsonl"
		require.NoError(t, os.WriteFile(filepath.Join(dir, walName), []byte("x"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "misc.txt"), []byte("x"), 0o600))

		a.recoverDirectory(t.Context(), dir, false)
		require.Len(t, ops.uploads, 1)
		assert.Contains(t, ops.uploads[0].name, "messages/high/"+walName)
	})

	t.Run("upload error keeps wal file", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		ops.uploadFn = func(ctx context.Context, name string, data []byte, metadata map[string]*string) error {
			return errors.New("boom")
		}
		dir := t.TempDir()
		p := filepath.Join(dir, "conn-1-0.jsonl")
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))

		a.recoverDirectory(t.Context(), dir, true)
		_, err := os.Stat(p)
		assert.NoError(t, err, "file kept for later retry on upload failure")
	})

	t.Run("scan error logs and returns", func(t *testing.T) {
		a, _, _, _ := newTestBlobProviderWithOps(t)
		assert.NotPanics(t, func() {
			a.recoverDirectory(t.Context(), filepath.Join(t.TempDir(), "missing"), true)
		})
	})

	t.Run("unrecognized jsonl filename is skipped", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		dir := t.TempDir()
		// File ends in .jsonl but does not match walFileNameRe pattern.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bad name!.jsonl"), []byte("x"), 0o600))

		a.recoverDirectory(t.Context(), dir, false)
		assert.Empty(t, ops.uploads, "unrecognized .jsonl should not be uploaded")
	})

	t.Run("current directory skips WAL with recent timestamp", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		// Set recoveryStartMs so that WAL with timestamp 500 is before it but 2000 is at/after.
		a.recoveryStartMs = 1000
		dir := t.TempDir()
		// Old WAL (timestamp 500, before recovery start) should be uploaded.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "conn-500-0.jsonl"), []byte("old"), 0o600))
		// Recent WAL (timestamp 2000, at/after recovery start) should be skipped.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "conn-2000-0.jsonl"), []byte("new"), 0o600))

		a.recoverDirectory(t.Context(), dir, true)
		require.Len(t, ops.uploads, 1, "only the old WAL should be uploaded")
		assert.Contains(t, ops.uploads[0].name, "conn-500-0.jsonl")
	})

	t.Run("context cancellation stops iteration", func(t *testing.T) {
		a, _, _, ops := newTestBlobProviderWithOps(t)
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "conn-1-0.jsonl"), []byte("x"), 0o600))

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // cancel immediately
		a.recoverDirectory(ctx, dir, false)
		assert.Empty(t, ops.uploads, "cancelled context should prevent uploads")
	})

	t.Run("non-current directory is removed after recovery", func(t *testing.T) {
		a, _, _, _ := newTestBlobProviderWithOps(t)
		dir := filepath.Join(t.TempDir(), "old-node")
		require.NoError(t, os.MkdirAll(dir, 0o750))
		// Empty directory (no WAL files); should be removed.
		a.recoverDirectory(t.Context(), dir, false)
		_, err := os.Stat(dir)
		assert.True(t, os.IsNotExist(err), "empty old-node directory should be removed")
	})
}

func TestTestAzureBlobConnectionOps(t *testing.T) {
	t.Run("create error not-exists is wrapped", func(t *testing.T) {
		ops := &fakeBlobOps{createFn: func(ctx context.Context) error { return errors.New("permission") }}
		err := testAzureBlobConnectionOps(t.Context(), ops)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create container")
	})

	t.Run("already exists error continues", func(t *testing.T) {
		ops := &fakeBlobOps{
			createFn: func(ctx context.Context) error { return errors.New(azureErrContainerAlreadyExists) },
		}
		assert.NoError(t, testAzureBlobConnectionOps(t.Context(), ops))
	})

	t.Run("upload error wrapped", func(t *testing.T) {
		ops := &fakeBlobOps{
			uploadFn: func(ctx context.Context, name string, data []byte, metadata map[string]*string) error {
				return errors.New("u")
			},
		}
		err := testAzureBlobConnectionOps(t.Context(), ops)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to upload test blob")
	})

	t.Run("delete error wrapped", func(t *testing.T) {
		ops := &fakeBlobOps{
			deleteFn: func(ctx context.Context, name string) error { return errors.New("d") },
		}
		err := testAzureBlobConnectionOps(t.Context(), ops)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete test blob")
	})

	t.Run("happy path", func(t *testing.T) {
		assert.NoError(t, testAzureBlobConnectionOps(t.Context(), &fakeBlobOps{}))
	})
}

// TestAzureBlobProvider_RecoverWALOnStartup_WithData seeds the shared walDirRoot
// under os.TempDir with a unique old nodeID so the provider's recoverWALOnStartup
// iteration exercises the happy-path: discover a stale conn dir, upload the WAL
// file via the fake ops, and remove the now-empty parent.
func TestAzureBlobProvider_RecoverWALOnStartup_WithData(t *testing.T) {
	a, _, _, ops := newTestBlobProviderWithOps(t)
	// Unique old nodeID to avoid stepping on other tests.
	oldNodeID := "old-node-recover-" + t.Name()
	a.connName = "recoverconn-" + t.Name()

	root := filepath.Join(os.TempDir(), walDirRoot)
	require.NoError(t, os.MkdirAll(root, 0o750))
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(root, oldNodeID)) })

	staleConnDir := filepath.Join(root, oldNodeID, a.connName)
	require.NoError(t, os.MkdirAll(staleConnDir, 0o750))
	walName := "batch-1-0.jsonl"
	walPath := filepath.Join(staleConnDir, walName)
	require.NoError(t, os.WriteFile(walPath, []byte("line1\n"), 0o600))

	// Also seed an unrelated subdir entry that should be ignored (not a dir with matching connName).
	unrelatedDir := filepath.Join(root, oldNodeID, "other-conn-unused-"+t.Name())
	require.NoError(t, os.MkdirAll(unrelatedDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(unrelatedDir, "ignored.jsonl"), []byte("x"), 0o600))

	// Also seed a non-directory entry at root level to exercise the !IsDir skip branch.
	rootFile := filepath.Join(root, "not-a-dir-"+t.Name())
	require.NoError(t, os.WriteFile(rootFile, []byte("x"), 0o600))
	t.Cleanup(func() { _ = os.Remove(rootFile) })

	a.recoverWALOnStartup(t.Context())

	// The stale WAL file should be uploaded under messages/<connName>/batch.jsonl.
	ops.mu.Lock()
	defer ops.mu.Unlock()
	var found bool
	for _, up := range ops.uploads {
		if strings.HasSuffix(up.name, a.connName+"/"+walName) {
			found = true
			break
		}
	}
	assert.True(t, found, "expected recovered WAL upload, got uploads=%v", ops.uploads)
}

// TestAzureBlobProvider_RecoverWALOnStartup_NotOurConn seeds an old node dir
// whose subdirs do not match this provider's connName; recoverWALOnStartup
// should skip without uploading anything.
func TestAzureBlobProvider_RecoverWALOnStartup_NotOurConn(t *testing.T) {
	a, _, _, ops := newTestBlobProviderWithOps(t)
	oldNodeID := "old-node-skip-" + t.Name()
	a.connName = "myconn-" + t.Name()

	root := filepath.Join(os.TempDir(), walDirRoot)
	require.NoError(t, os.MkdirAll(root, 0o750))
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(root, oldNodeID)) })

	otherConnDir := filepath.Join(root, oldNodeID, "some-other-conn")
	require.NoError(t, os.MkdirAll(otherConnDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(otherConnDir, "x.jsonl"), []byte("x"), 0o600))

	a.recoverWALOnStartup(t.Context())

	ops.mu.Lock()
	defer ops.mu.Unlock()
	assert.Empty(t, ops.uploads, "should not touch files for unrelated connName")
}

// TestAzureBlobProvider_Flush_WALCloseError exercises the branch where walFile.Close
// returns an error: the WAL file must be abandoned (walFile=nil, walPath="") and
// LogError called with "WAL close failed".
func TestAzureBlobProvider_Flush_WALCloseError(t *testing.T) {
	a, _, _, ops := newTestBlobProviderWithOps(t)
	// Open a WAL file then close it behind the provider's back so the provider's
	// Close call returns "already closed".
	a.walMu.Lock()
	require.NoError(t, a.openWALFileLocked())
	savedPath := a.walPath
	require.NoError(t, a.walFile.Close()) // sneaky close - next Close errors
	a.walMu.Unlock()

	a.flush(t.Context())

	a.walMu.Lock()
	assert.Nil(t, a.walFile)
	assert.Empty(t, a.walPath)
	a.walMu.Unlock()

	ops.mu.Lock()
	defer ops.mu.Unlock()
	assert.Empty(t, ops.uploads, "no upload after close failure")
	assert.FileExists(t, savedPath)
}

// TestAzureBlobProvider_Flush_UploadErrorLeavesWAL makes flush upload fail and
// asserts the WAL file is NOT deleted (so recovery can pick it up on restart).
func TestAzureBlobProvider_Flush_UploadErrorLeavesWAL(t *testing.T) {
	a, _, _, ops := newTestBlobProviderWithOps(t)
	ops.uploadFn = func(ctx context.Context, name string, data []byte, metadata map[string]*string) error {
		return errors.New("upload boom")
	}
	require.NoError(t, a.Publish(t.Context(), []byte("msg")))
	savedPath := a.walPath

	a.flush(t.Context())
	assert.FileExists(t, savedPath, "WAL file must survive upload failure")
}

// TestAzureBlobProvider_Flush_EmptyBatch covers the early-return branch where
// both walFile and pendingFiles are empty.
func TestAzureBlobProvider_Flush_EmptyBatch(t *testing.T) {
	a, _, _, ops := newTestBlobProviderWithOps(t)
	a.flush(t.Context())
	ops.mu.Lock()
	defer ops.mu.Unlock()
	assert.Empty(t, ops.uploads)
}

// TestNewAzureBlobProviderFromOps covers the testable seam used by both the
// public SDK constructor and tests.
func TestNewAzureBlobProviderFromOps(t *testing.T) {
	t.Run("inbound provider does not start flush loop", func(t *testing.T) {
		api := &plugintest.API{}
		stubLogs(api)
		ops := &fakeBlobOps{}
		kv := &fakeKV{}
		cfg := AzureBlobProviderConfig{BlobContainerName: "c1"}
		a, err := newAzureBlobProviderFromOps(t.Context(), cfg, api, kv,
			"node-new-"+t.Name(), "conn-new-"+t.Name(),
			false, ops)
		require.NoError(t, err)
		require.NotNil(t, a)
		assert.Nil(t, a.outCtx, "inbound must not start outbound context")
		// walDir was created.
		_, err = os.Stat(a.walDir)
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(a.walDir) })
	})

	t.Run("outbound provider starts flush loop and runs recovery", func(t *testing.T) {
		api := &plugintest.API{}
		stubLogs(api)
		ops := &fakeBlobOps{}
		kv := &fakeKV{}
		cfg := AzureBlobProviderConfig{BlobContainerName: "c1", FlushIntervalSeconds: 0}
		a, err := newAzureBlobProviderFromOps(t.Context(), cfg, api, kv,
			"node-out-"+t.Name(), "conn-out-"+t.Name(),
			true, ops)
		require.NoError(t, err)
		require.NotNil(t, a.outCtx, "outbound must have outbound context")
		// Close cleanly.
		assert.NoError(t, a.Close())
		t.Cleanup(func() { _ = os.RemoveAll(a.walDir) })
	})

	t.Run("MkdirAll failure returns error", func(t *testing.T) {
		api := &plugintest.API{}
		stubLogs(api)
		// Point walDirRoot to a file-path so MkdirAll fails. We can't change
		// walDirRoot (const), but we can create a file at nodeID path under it.
		root := filepath.Join(os.TempDir(), walDirRoot)
		require.NoError(t, os.MkdirAll(root, 0o750))
		badNode := "node-file-mkdirfail-unique"
		badNodePath := filepath.Join(root, badNode)
		_ = os.RemoveAll(badNodePath)
		require.NoError(t, os.WriteFile(badNodePath, []byte("x"), 0o600))
		t.Cleanup(func() { _ = os.Remove(badNodePath) })

		_, err := newAzureBlobProviderFromOps(t.Context(), AzureBlobProviderConfig{},
			api, &fakeKV{}, badNode, "conn-mkdir",
			false, &fakeBlobOps{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "WAL directory")
	})
}

func TestNewAzureBlobProvider_InvalidURL(t *testing.T) {
	api := &plugintest.API{}
	kv := &fakeKV{}
	cfg := AzureBlobProviderConfig{
		AccountName:       "acct",
		AccountKey:        "dGVzdC1rZXk=",
		ServiceURL:        "://bad-url",
		BlobContainerName: "c1",
	}
	_, err := newAzureBlobProvider(t.Context(), cfg, api, kv, "node", "conn", false)
	require.Error(t, err)
}

func TestNewAzureBlobProvider_InvalidCredential(t *testing.T) {
	api := &plugintest.API{}
	kv := &fakeKV{}
	cfg := AzureBlobProviderConfig{
		AccountName:       "acct",
		AccountKey:        "not-base64-!!!",
		ServiceURL:        "https://acct.blob.core.windows.net",
		BlobContainerName: "c1",
	}
	_, err := newAzureBlobProvider(t.Context(), cfg, api, kv, "node", "conn", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shared key credential")
}

func TestTestAzureBlobConnection_InvalidCredential(t *testing.T) {
	cfg := AzureBlobProviderConfig{
		AccountName:       "acct",
		AccountKey:        "not-base64-!!!",
		ServiceURL:        "https://acct.blob.core.windows.net",
		BlobContainerName: "c1",
	}
	err := testAzureBlobConnection(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shared key credential")
}

// TestContainerClientAdapter exercises the thin adapter that wraps the Azure
// SDK container client. Tests use an unreachable local URL so every call fails
// fast without needing a live service, which is enough for coverage of the
// adapter glue code (each method still invokes the SDK surface once).
func TestContainerClientAdapter(t *testing.T) {
	validKey := "dGVzdC1rZXk=" // base64 "test-key"
	cfg := AzureBlobProviderConfig{
		AccountName:       "acct",
		AccountKey:        validKey,
		ServiceURL:        "http://127.0.0.1:1",
		BlobContainerName: "c1",
	}
	// Build a real adapter via the same path as the constructor.
	cred, err := container.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
	require.NoError(t, err)
	client, err := container.NewClientWithSharedKeyCredential(buildBlobContainerURL(cfg.ServiceURL, cfg.BlobContainerName), cred, nil)
	require.NoError(t, err)
	adapter := &containerClientAdapter{client: client}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	// Each call is expected to fail (connection refused), but the code path
	// through the adapter is exercised for coverage.
	_ = adapter.CreateContainer(ctx)
	_ = adapter.UploadBlob(ctx, "name", []byte("data"), map[string]*string{"k": new(string)})
	_, _ = adapter.DownloadBlob(ctx, "name")
	_ = adapter.DeleteBlob(ctx, "name")
	_, _ = adapter.ListBlobs(ctx, "", true)
	_, _ = adapter.ListBlobs(ctx, "prefix", false)
}

func TestAzureBlobProvider_BlobLockMaxAge(t *testing.T) {
	t.Run("default when unset", func(t *testing.T) {
		a := &azureBlobProvider{cfg: AzureBlobProviderConfig{}}
		assert.Equal(t, blobLockMaxAge, a.blobLockMaxAge())
	})

	t.Run("uses configured value", func(t *testing.T) {
		a := &azureBlobProvider{cfg: AzureBlobProviderConfig{BlobLockMaxAgeSeconds: 120}}
		assert.Equal(t, 120*time.Second, a.blobLockMaxAge())
	})

	t.Run("clamps at cap", func(t *testing.T) {
		a := &azureBlobProvider{cfg: AzureBlobProviderConfig{BlobLockMaxAgeSeconds: int(blobLockMaxAgeCap.Seconds()) * 10}}
		assert.Equal(t, blobLockMaxAgeCap, a.blobLockMaxAge())
	})
}

func TestWalFileTimestampMs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int64
		ok    bool
	}{
		{"valid node and timestamp", "node1-1700000000000-5.jsonl", 1700000000000, true},
		{"missing extension", "node1-1700000000000-5", 0, false},
		{"non-numeric timestamp", "node1-abc-5.jsonl", 0, false},
		{"companion file", "node1-1700000000000-5.files.json", 0, false},
		{"empty", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := walFileTimestampMs(tt.input)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewLockToken(t *testing.T) {
	tok1, err := newLockToken()
	require.NoError(t, err)
	assert.Len(t, tok1, 32) // 16 bytes hex-encoded

	tok2, err := newLockToken()
	require.NoError(t, err)
	assert.NotEqual(t, tok1, tok2, "tokens should be unique")
}

func TestIsContainerAlreadyExists(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"matching substring", errors.New("400 ContainerAlreadyExists: whatever"), true},
		{"unrelated error", errors.New("401 Unauthorized"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isContainerAlreadyExists(tt.err))
		})
	}
}

func TestIsTransientAzureError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"connection refused", errors.New("dial tcp: connection refused"), true},
		{"connection reset", errors.New("read: connection reset by peer"), true},
		{"i/o timeout", errors.New("read: i/o timeout"), true},
		{"EOF", errors.New("unexpected EOF"), true},
		{"deadline exceeded is non-transient", context.DeadlineExceeded, false},
		{"canceled is non-transient", context.Canceled, false},
		{"generic unrelated", errors.New("bad request"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isTransientAzureError(tt.err))
		})
	}
}

func TestAzureBlobProvider_MarkIsClearBlobProcessed(t *testing.T) {
	t.Run("mark success then isProcessed returns true", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		store := map[string][]byte{}
		kv.setFn = func(key string, value any, _ ...pluginapi.KVSetOption) (bool, error) {
			b, ok := value.([]byte)
			if !ok {
				return false, errors.New("unexpected value type")
			}
			store[key] = b
			return true, nil
		}
		kv.getFn = func(key string, o any) error {
			pp, ok := o.(*[]byte)
			if !ok {
				return nil
			}
			*pp = store[key]
			return nil
		}

		a.markBlobProcessed("messages/node-1-42.jsonl")
		assert.True(t, a.isBlobProcessed("messages/node-1-42.jsonl"))
	})

	t.Run("mark set error logs warning", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		kv.setFn = func(string, any, ...pluginapi.KVSetOption) (bool, error) {
			return false, errors.New("boom")
		}
		a.markBlobProcessed("b1")
	})

	t.Run("isProcessed false when get errors", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		kv.getFn = func(string, any) error { return errors.New("boom") }
		assert.False(t, a.isBlobProcessed("b1"))
	})

	t.Run("isProcessed false when empty", func(t *testing.T) {
		a, _, _ := newTestBlobProvider(t)
		// default kv.getFn returns nil with no bytes written
		assert.False(t, a.isBlobProcessed("b1"))
	})

	t.Run("clear success", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		a.clearBlobProcessed("b1")
		// Should have recorded a delete for the hashed key.
		require.Len(t, kv.deletes, 1)
		assert.True(t, strings.HasPrefix(kv.deletes[0], blobProcessedKeyPrefix))
	})

	t.Run("clear error does not panic", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		kv.delFn = func(string) error { return errors.New("boom") }
		assert.NotPanics(t, func() { a.clearBlobProcessed("b1") })
	})
}

func TestAzureBlobProvider_ReleaseBlobLock_CachedToken(t *testing.T) {
	writeRaw := func(kv *fakeKV, raw []byte) {
		kv.getFn = func(_ string, o any) error {
			pp, ok := o.(*[]byte)
			if !ok {
				return nil
			}
			*pp = raw
			return nil
		}
	}

	t.Run("KV get error leaves lock", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		a.rememberLockToken("b1", "mytoken")
		kv.getFn = func(string, any) error { return errors.New("getboom") }
		a.releaseBlobLock("b1")
		assert.Empty(t, kv.deletes)
	})

	t.Run("empty raw returns without delete", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		a.rememberLockToken("b1", "mytoken")
		a.releaseBlobLock("b1")
		assert.Empty(t, kv.deletes)
	})

	t.Run("corrupt JSON leaves lock", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		a.rememberLockToken("b1", "mytoken")
		writeRaw(kv, []byte("not-json"))
		a.releaseBlobLock("b1")
		assert.Empty(t, kv.deletes)
	})

	t.Run("reclaimed by other node", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		a.rememberLockToken("b1", "mine")
		other := blobLock{Node: "other", Acquired: time.Now().UnixMilli(), Token: "theirs"}
		raw, err := json.Marshal(other)
		require.NoError(t, err)
		writeRaw(kv, raw)
		a.releaseBlobLock("b1")
		assert.Empty(t, kv.deletes)
	})

	t.Run("token matches, conditional delete succeeds", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		a.rememberLockToken("b1", "mine")
		mine := blobLock{Node: a.nodeID, Token: "mine"}
		raw, err := json.Marshal(mine)
		require.NoError(t, err)
		writeRaw(kv, raw)
		a.releaseBlobLock("b1")
		require.Len(t, kv.deletes, 1)
		assert.True(t, strings.HasPrefix(kv.deletes[0], blobLockKeyPrefix))
	})

	t.Run("token matches, delete error does not panic", func(t *testing.T) {
		a, _, kv := newTestBlobProvider(t)
		a.rememberLockToken("b1", "mine")
		mine := blobLock{Node: a.nodeID, Token: "mine"}
		raw, err := json.Marshal(mine)
		require.NoError(t, err)
		writeRaw(kv, raw)
		kv.delFn = func(string) error { return errors.New("delboom") }
		assert.NotPanics(t, func() { a.releaseBlobLock("b1") })
	})
}

func TestNextListBackoff(t *testing.T) {
	base := 5 * time.Second
	tests := []struct {
		name    string
		current time.Duration
		want    time.Duration
	}{
		{"zero returns base", 0, base},
		{"negative returns base", -1 * time.Second, base},
		{"doubles under cap", 10 * time.Second, 20 * time.Second},
		{"caps at max", listBlobsBackoffMax, listBlobsBackoffMax},
		{"overflow clamps to cap", listBlobsBackoffMax * 4, listBlobsBackoffMax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nextListBackoff(tt.current, base))
		})
	}
}

func TestEnsureContainerWithRetry(t *testing.T) {
	t.Run("success on first attempt", func(t *testing.T) {
		api := &plugintest.API{}
		stubLogs(api)
		ops := &fakeBlobOps{}
		err := ensureContainerWithRetry(context.Background(), ops, api, "test-container")
		require.NoError(t, err)
		ops.mu.Lock()
		assert.Equal(t, 1, ops.createCount)
		ops.mu.Unlock()
	})

	t.Run("container already exists", func(t *testing.T) {
		api := &plugintest.API{}
		stubLogs(api)
		ops := &fakeBlobOps{
			createFn: func(ctx context.Context) error {
				return errors.New(azureErrContainerAlreadyExists)
			},
		}
		err := ensureContainerWithRetry(context.Background(), ops, api, "test-container")
		require.NoError(t, err)
		ops.mu.Lock()
		assert.Equal(t, 1, ops.createCount)
		ops.mu.Unlock()
	})

	t.Run("transient error then success", func(t *testing.T) {
		api := &plugintest.API{}
		stubLogs(api)
		var attempt atomic.Int32
		ops := &fakeBlobOps{
			createFn: func(ctx context.Context) error {
				if attempt.Add(1) == 1 {
					return errors.New("connection refused")
				}
				return nil
			},
		}
		err := ensureContainerWithRetry(context.Background(), ops, api, "test-container")
		require.NoError(t, err)
		ops.mu.Lock()
		assert.Equal(t, 2, ops.createCount)
		ops.mu.Unlock()
	})

	t.Run("non-transient error fails immediately", func(t *testing.T) {
		api := &plugintest.API{}
		stubLogs(api)
		ops := &fakeBlobOps{
			createFn: func(ctx context.Context) error {
				return errors.New("permanent failure")
			},
		}
		err := ensureContainerWithRetry(context.Background(), ops, api, "test-container")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create/access Azure Blob container")
		assert.Contains(t, err.Error(), "permanent failure")
		ops.mu.Lock()
		assert.Equal(t, 1, ops.createCount)
		ops.mu.Unlock()
	})

	t.Run("max attempts exhausted", func(t *testing.T) {
		api := &plugintest.API{}
		stubLogs(api)
		ops := &fakeBlobOps{
			createFn: func(ctx context.Context) error {
				return errors.New("connection refused")
			},
		}
		err := ensureContainerWithRetry(context.Background(), ops, api, "test-container")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "after 5 attempts")
		ops.mu.Lock()
		assert.Equal(t, containerCreateMaxAttempts, ops.createCount)
		ops.mu.Unlock()
	})

	t.Run("context cancelled during retry wait", func(t *testing.T) {
		api := &plugintest.API{}
		stubLogs(api)
		ctx, cancel := context.WithCancel(context.Background())
		ops := &fakeBlobOps{
			createFn: func(ctx context.Context) error {
				// Cancel context after the first transient error so the
				// retry backoff select picks ctx.Done.
				cancel()
				return errors.New("connection refused")
			},
		}
		err := ensureContainerWithRetry(ctx, ops, api, "test-container")
		require.Error(t, err)
		assert.Contains(t, err.Error(), context.Canceled.Error())
	})
}
