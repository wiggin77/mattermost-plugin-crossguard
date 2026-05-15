package main

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha1" //nolint:gosec // used for blob lock key hashing, not cryptographic security
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

const (
	// walDirRoot is the root directory holding WAL files under os.TempDir.
	walDirRoot = "crossguard-wal"

	// walMaxFiles is the backpressure threshold for unflushed WAL files.
	walMaxFiles = 100

	// walFileExt is the extension for WAL message batch files.
	walFileExt = ".jsonl"

	// walFilesExt is the extension for the companion deferred file manifest.
	walFilesExt = ".files.json"

	// blobMessagePrefix is the prefix for message batch blobs.
	blobMessagePrefix = "messages/"

	// blobFilesPrefix is the prefix for file attachment blobs.
	blobFilesPrefix = "files/"

	// blobLockKeyPrefix is the KV store prefix for per-blob locks.
	blobLockKeyPrefix = "blob-lock-"

	// blobLockMaxAge is the default maximum lock age before it is considered
	// stale. May be overridden per-provider via
	// AzureBlobProviderConfig.BlobLockMaxAgeSeconds.
	blobLockMaxAge = 5 * time.Minute

	// blobLockMaxAgeCap is the hard upper bound for the configurable
	// BlobLockMaxAgeSeconds to prevent time.Duration overflow and sanity-cap
	// operator mistakes.
	blobLockMaxAgeCap = 24 * time.Hour

	// blobMaxDownloadSize caps the number of bytes read from a single blob.
	// Guards against OOM from a malformed or malicious batch. 100 MiB is well
	// above any realistic message batch size.
	blobMaxDownloadSize = 100 * 1024 * 1024

	// blobProcessedKeyPrefix marks a blob whose handler has already run but
	// whose delete has not yet been confirmed. Prevents duplicate delivery on
	// DeleteBlob failure (next acquirer skips the handler and retries delete).
	blobProcessedKeyPrefix = "blob-processed-"

	// blobProcessedMarkerTTLSeconds is the TTL of the processed marker. It
	// must outlive the longest plausible DeleteBlob retry window.
	blobProcessedMarkerTTLSeconds = 24 * 60 * 60

	// containerCreateMaxAttempts is the number of times startup container
	// create is retried on transient Azure errors before giving up.
	containerCreateMaxAttempts = 5

	// containerCreateRetryBase is the base delay for exponential backoff on
	// container create retries.
	containerCreateRetryBase = 500 * time.Millisecond

	// listBlobsBackoffMax caps the exponential backoff between consecutive
	// failing ListBlobs calls so we do not spin-log on a permanent error.
	listBlobsBackoffMax = 2 * time.Minute
)

// walFileNameRe validates WAL filenames during recovery and captures the
// embedded unix-millis timestamp. Filenames are produced by openWALFileLocked as
// "<nodeID>-<unixMilli>-<seq>.jsonl"; we also accept the shutdown-residue
// companion name "<nodeID>-shutdown-<unixMilli>.files.json" elsewhere.
var walFileNameRe = regexp.MustCompile(`^(?:[A-Za-z0-9-]+)-(\d+)-\d+\.jsonl$`)

// walFileTimestampMs extracts the embedded unix-millis timestamp from a WAL
// file name. Returns false if the name is not a valid WAL filename.
func walFileTimestampMs(name string) (int64, bool) {
	m := walFileNameRe.FindStringSubmatch(name)
	if len(m) != 2 {
		return 0, false
	}
	ts, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return ts, true
}

// kvClient is the minimal KV interface needed by the azure-blob provider for
// distributed blob locking. It wraps the pluginapi KV client so it can be
// mocked in tests.
type kvClient interface {
	Get(key string, o any) error
	Set(key string, value any, options ...pluginapi.KVSetOption) (bool, error)
	Delete(key string) error
}

// blobListing is a minimal view of a blob returned by list operations.
type blobListing struct {
	Name     string
	Metadata map[string]*string
}

// azureBlobOps abstracts the Azure Blob container operations used by
// azureBlobProvider so tests can inject a fake implementation without
// hitting the Azure SDK.
type azureBlobOps interface {
	CreateContainer(ctx context.Context) error
	UploadBlob(ctx context.Context, name string, data []byte, metadata map[string]*string) error
	DownloadBlob(ctx context.Context, name string) ([]byte, error)
	DeleteBlob(ctx context.Context, name string) error
	ListBlobs(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error)
}

// containerClientAdapter wraps *container.Client to implement azureBlobOps.
type containerClientAdapter struct {
	client *container.Client
}

func (c *containerClientAdapter) CreateContainer(ctx context.Context) error {
	_, err := c.client.Create(ctx, nil)
	return err
}

func (c *containerClientAdapter) UploadBlob(ctx context.Context, name string, data []byte, metadata map[string]*string) error {
	bc := c.client.NewBlockBlobClient(name)
	var opts *blockblob.UploadOptions
	if metadata != nil {
		opts = &blockblob.UploadOptions{Metadata: metadata}
	}
	_, err := bc.Upload(ctx, newNopCloser(data), opts)
	return err
}

func (c *containerClientAdapter) DownloadBlob(ctx context.Context, name string) ([]byte, error) {
	bc := c.client.NewBlockBlobClient(name)
	resp, err := bc.DownloadStream(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	// Cap reads so a malformed or malicious blob cannot OOM the plugin. Read
	// one extra byte to detect oversize and reject explicitly.
	limited := io.LimitReader(resp.Body, blobMaxDownloadSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > blobMaxDownloadSize {
		return nil, fmt.Errorf("blob %q exceeds max download size %d bytes", name, blobMaxDownloadSize)
	}
	return data, nil
}

func (c *containerClientAdapter) DeleteBlob(ctx context.Context, name string) error {
	bc := c.client.NewBlockBlobClient(name)
	_, err := bc.Delete(ctx, nil)
	return err
}

func (c *containerClientAdapter) ListBlobs(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
	opts := &container.ListBlobsFlatOptions{}
	if prefix != "" {
		opts.Prefix = &prefix
	}
	if includeMetadata {
		opts.Include = container.ListBlobsInclude{Metadata: true}
	}
	pager := c.client.NewListBlobsFlatPager(opts)
	var result []blobListing
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return result, err
		}
		for _, b := range resp.Segment.BlobItems {
			if b.Name == nil {
				continue
			}
			result = append(result, blobListing{Name: *b.Name, Metadata: b.Metadata})
		}
	}
	return result, nil
}

// blobLock is the JSON value stored under a blob lock key. Token is a
// per-acquire random value used as a fencing token so releaseBlobLock only
// deletes a lock it actually owns (prevents a slow worker from deleting a
// lock that has since been reclaimed by another node after TTL).
type blobLock struct {
	Node     string `json:"node"`
	Acquired int64  `json:"acquired"`
	Token    string `json:"token"`
}

// pendingFileRef captures file metadata to upload after WAL flush confirms.
// Historically included ConnName, but the provider is always scoped to a
// single connection and the field was redundant. Removed fields are safely
// ignored by JSON unmarshal of older companion files.
type pendingFileRef struct {
	PostID   string `json:"post_id"`
	FileID   string `json:"file_id"`
	Filename string `json:"filename"`
}

// getFileFunc loads a file's bytes by file ID. It is injected to avoid a
// direct dependency on plugin.API and to simplify testing.
type getFileFunc func(fileID string) ([]byte, error)

// azureBlobProvider implements QueueProvider using Azure Blob Storage for
// batched message relay via .jsonl files.
type azureBlobProvider struct {
	containerClient azureBlobOps
	api             plugin.API
	kv              kvClient
	cfg             AzureBlobProviderConfig
	nodeID          string
	connName        string
	flushInterval   time.Duration
	batchPoll       time.Duration
	getFile         getFileFunc
	isOutbound      bool

	// Lifetime context (provided by the plugin) used for outbound flush loop.
	outCtx    context.Context
	outCancel context.CancelFunc

	// WAL (outbound only). walSeq is only touched under walMu so no atomic needed.
	walMu   sync.Mutex
	walFile *os.File
	walPath string
	walSeq  int64
	walDir  string

	// Pending file refs (outbound only)
	pendingFilesMu sync.Mutex
	pendingFiles   []pendingFileRef

	// companionMu serializes companion .files.json writes (QueueFileRef and
	// flush) so concurrent writers cannot interleave bytes on disk. Taken
	// after dropping walMu / pendingFilesMu and held only during the actual
	// file write, so publish callers do not stall on disk IO.
	companionMu sync.Mutex

	// Flush control (outbound only)
	flushTicker *time.Ticker
	flushDone   chan struct{}

	// Async WAL recovery (outbound only). recoveryDone is closed when the
	// startup recovery goroutine exits. recoveryStartMs is the provider's
	// start time in unix millis; WAL recovery skips any file whose embedded
	// timestamp is >= this value to avoid colliding with live Publish writes.
	recoveryDone    chan struct{}
	recoveryStartMs int64

	// subMu guards Subscribe/WatchFiles lifecycle fields (cancel, pollDone,
	// handler, watchCancel, watchDone, subscribed, watching) against races
	// with Close.
	subMu       sync.Mutex
	subscribed  bool
	cancel      context.CancelFunc
	pollDone    chan struct{}
	handler     func(data []byte) error
	watching    bool
	watchCancel context.CancelFunc
	watchDone   chan struct{}

	// closeOnce guards Close from concurrent invocation.
	closeOnce sync.Once

	// lockTokensMu guards lockTokens, which tracks the fencing token this
	// node stored when it acquired each blob lock. releaseBlobLock verifies
	// the stored KV token still matches before deleting so a lock reclaimed
	// by another node after stale-timeout is not clobbered.
	lockTokensMu sync.Mutex
	lockTokens   map[string]string
}

// newAzureBlobProvider constructs an azure-blob provider. If isOutbound is
// true, the provider immediately runs WAL crash recovery and starts the flush
// loop using the supplied ctx (which should be the plugin lifetime context).
//
// In service-principal auth_mode the container auto-create call is skipped:
// the operator is expected to pre-provision the container so the SP can
// run with data-plane-only RBAC (Storage Blob Data Contributor).
func newAzureBlobProvider(ctx context.Context, cfg AzureBlobProviderConfig, api plugin.API, kv kvClient, nodeID, connName string, getFile getFileFunc, isOutbound bool) (*azureBlobProvider, error) {
	authMode := normalizeAzureAuthMode(cfg.AuthMode)
	azCloud, err := resolveAzureCloud(cfg.AzureCloud)
	if err != nil {
		return nil, fmt.Errorf("azure-blob config: %w", err)
	}

	containerURL := buildBlobContainerURL(cfg.ServiceURL, cfg.BlobContainerName)

	var containerClient *container.Client
	switch authMode {
	case AzureAuthServicePrincipal:
		secret, secErr := resolveAzureSecret(azureSecretSource{
			Inline: cfg.ClientSecret, EnvVar: cfg.ClientSecretEnv, FilePath: cfg.ClientSecretFile,
		})
		if secErr != nil {
			return nil, fmt.Errorf("azure-blob service principal: %w", secErr)
		}
		cred, credErr := buildClientSecretCredential(azureServicePrincipalParams{
			TenantID: cfg.TenantID, ClientID: cfg.ClientID, Secret: secret, Cloud: azCloud,
		})
		if credErr != nil {
			api.LogError("Azure Blob: service principal credential failed",
				"error_code", errcode.AzureBlobSPCredentialFailed,
				"tenant_id", cfg.TenantID, "client_id", cfg.ClientID, "error", sanitizeAzureError(credErr))
			return nil, credErr
		}
		logAzureAuthAudit(api, "azure-blob", cfg.TenantID, cfg.ClientID, cfg.AzureCloud, secret)
		opts := &container.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		containerClient, err = container.NewClient(containerURL, cred, opts)
		if err != nil {
			return nil, fmt.Errorf("azure-blob NewClient: %s", sanitizeAzureError(err))
		}
	case "", AzureAuthSharedKey:
		cred, credErr := container.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
		if credErr != nil {
			return nil, fmt.Errorf("azure-blob shared key credential: %s", sanitizeAzureError(credErr))
		}
		opts := &container.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		containerClient, err = container.NewClientWithSharedKeyCredential(containerURL, cred, opts)
		if err != nil {
			return nil, fmt.Errorf("azure-blob NewClientWithSharedKeyCredential: %s", sanitizeAzureError(err))
		}
	default:
		return nil, fmt.Errorf("azure-blob: unknown auth_mode %q", authMode)
	}

	ops := &containerClientAdapter{client: containerClient}

	// In SP mode the container must be pre-provisioned (data-plane-only RBAC
	// cannot create containers). Skip the ensure-exists probe entirely.
	if authMode != AzureAuthServicePrincipal {
		// Ensure container exists (idempotent). Retry with exponential backoff on
		// transient errors so a brief Azure blip at plugin activation does not
		// prevent startup.
		if err := ensureContainerWithRetry(ctx, ops, api, cfg.BlobContainerName); err != nil {
			return nil, err
		}
	}

	return newAzureBlobProviderFromOps(ctx, cfg, api, kv, nodeID, connName, getFile, isOutbound, ops)
}

// ensureContainerWithRetry tries to create the blob container, tolerating
// transient failures with exponential backoff. "Already exists" is treated
// as success.
func ensureContainerWithRetry(ctx context.Context, ops azureBlobOps, api plugin.API, containerName string) error {
	var lastErr error
	for attempt := range containerCreateMaxAttempts {
		createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := ops.CreateContainer(createCtx)
		cancel()
		if err == nil || isContainerAlreadyExists(err) {
			return nil
		}
		lastErr = err
		if !isTransientAzureError(err) {
			return fmt.Errorf("failed to create/access Azure Blob container %q: %w", containerName, err)
		}
		if ctx.Err() != nil {
			return fmt.Errorf("failed to create/access Azure Blob container %q: %w", containerName, ctx.Err())
		}
		delay := containerCreateRetryBase << attempt
		api.LogWarn("Azure Blob: transient container create error, retrying",
			"error_code", errcode.AzureBlobContainerCreateRetry,
			"container", containerName, "attempt", attempt+1, "delay", delay.String(), "error", sanitizeAzureError(err))
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return fmt.Errorf("failed to create/access Azure Blob container %q: %w", containerName, ctx.Err())
		}
	}
	return fmt.Errorf("failed to create/access Azure Blob container %q after %d attempts: %w",
		containerName, containerCreateMaxAttempts, lastErr)
}

// isTransientAzureError reports whether an Azure SDK error is worth retrying.
func isTransientAzureError(err error) bool {
	if err == nil {
		return false
	}
	if bloberror.HasCode(err, bloberror.ServerBusy) ||
		bloberror.HasCode(err, bloberror.OperationTimedOut) ||
		bloberror.HasCode(err, bloberror.InternalError) {
		return true
	}
	// Fallback: treat context deadline exceeded as non-transient (caller's
	// intent) but network-level errors as transient.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "EOF")
}

// newAzureBlobProviderFromOps constructs a provider from a pre-built
// azureBlobOps. Used by both the public constructor (after wrapping the SDK
// client in an adapter) and tests, which can pass a fake ops implementation.
func newAzureBlobProviderFromOps(ctx context.Context, cfg AzureBlobProviderConfig, api plugin.API, kv kvClient, nodeID, connName string, getFile getFileFunc, isOutbound bool, ops azureBlobOps) (*azureBlobProvider, error) {
	flushSec := cfg.FlushIntervalSeconds
	if flushSec <= 0 {
		flushSec = defaultAzureBlobFlushIntervalSec
	}

	batchPoll := azureBlobBatchPollInterval
	if cfg.BatchPollIntervalSeconds > 0 {
		batchPoll = time.Duration(cfg.BatchPollIntervalSeconds) * time.Second
	}

	// Scope the WAL directory by (nodeID, connName) so multiple azure-blob
	// connections on the same node do not collide on filenames or recovery.
	walDir := filepath.Join(os.TempDir(), walDirRoot, nodeID, connName)
	if err := os.MkdirAll(walDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory %q: %w", walDir, err)
	}

	api.LogWarn("Azure Blob provider: WAL directory is in temp storage and may not survive container restarts",
		"error_code", errcode.AzureBlobWALInTempStorage,
		"wal_dir", walDir)

	a := &azureBlobProvider{
		containerClient: ops,
		api:             api,
		kv:              kv,
		cfg:             cfg,
		nodeID:          nodeID,
		connName:        connName,
		flushInterval:   time.Duration(flushSec) * time.Second,
		batchPoll:       batchPoll,
		getFile:         getFile,
		walDir:          walDir,
		isOutbound:      isOutbound,
		recoveryStartMs: time.Now().UnixMilli(),
		lockTokens:      make(map[string]string),
	}

	if isOutbound {
		a.outCtx, a.outCancel = context.WithCancel(ctx)
		a.recoveryDone = make(chan struct{})
		go func() {
			defer close(a.recoveryDone)
			a.recoverWALOnStartup(a.outCtx)
		}()
		a.startFlushLoop(a.outCtx)
	}

	return a, nil
}

// Publish appends the message to the current WAL file.
func (a *azureBlobProvider) Publish(_ context.Context, data []byte) error {
	a.walMu.Lock()
	defer a.walMu.Unlock()

	// Backpressure: if too many unflushed WAL files accumulated, reject.
	if n := a.countWALFiles(); n >= walMaxFiles {
		return fmt.Errorf("WAL backpressure: %d unflushed files (limit %d)", n, walMaxFiles)
	}

	if a.walFile == nil {
		if err := a.openWALFileLocked(); err != nil {
			return err
		}
	}

	// Two writes to avoid `append(data, '\n')` mutating the caller's buffer
	// when cap(data) > len(data).
	if _, err := a.walFile.Write(data); err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}
	if _, err := a.walFile.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}
	return nil
}

// openWALFileLocked opens a new WAL file. Must be called with walMu held.
// After creating the file, the parent directory is fsynced so that the
// dirent survives a host/kernel crash before the first flush.
func (a *azureBlobProvider) openWALFileLocked() error {
	a.walSeq++
	seq := a.walSeq
	name := fmt.Sprintf("%s-%d-%d%s", a.nodeID, time.Now().UnixMilli(), seq, walFileExt)
	path := filepath.Join(a.walDir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // path built from walDir + nodeID/seq/timestamp
	if err != nil {
		return fmt.Errorf("failed to open WAL file %q: %w", path, err)
	}
	a.walFile = f
	a.walPath = path

	// Best effort: fsync the parent directory so the dirent for the new WAL
	// file is durable. On filesystems where this fails (Windows, some FUSE),
	// a failure is logged but not fatal.
	if dir, derr := os.Open(a.walDir); derr == nil {
		if syncErr := dir.Sync(); syncErr != nil {
			a.api.LogWarn("Azure Blob: WAL dir fsync failed",
				"error_code", errcode.AzureBlobWALDirFsyncFailed,
				"dir", a.walDir, "error", syncErr.Error())
		}
		_ = dir.Close()
	}
	return nil
}

// countWALFiles returns the number of .jsonl files in walDir.
func (a *azureBlobProvider) countWALFiles() int {
	entries, err := os.ReadDir(a.walDir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), walFileExt) {
			n++
		}
	}
	return n
}

// QueueFileRef records a pending file upload to be performed after the next
// WAL flush. The file ref is also persisted to a companion .files.json file
// so it can be recovered on crash. The companion file is written outside
// walMu / pendingFilesMu so Publish callers do not stall on disk IO;
// companionMu serializes the actual disk write against flush().
func (a *azureBlobProvider) QueueFileRef(postID, fileID, filename string) {
	a.pendingFilesMu.Lock()
	a.pendingFiles = append(a.pendingFiles, pendingFileRef{
		PostID:   postID,
		FileID:   fileID,
		Filename: filename,
	})
	snapshot := append([]pendingFileRef(nil), a.pendingFiles...)
	a.pendingFilesMu.Unlock()

	a.walMu.Lock()
	walPath := a.walPath
	a.walMu.Unlock()

	// No open WAL file yet: skip the companion write. The next Publish will
	// open a WAL file and the next flush will observe the in-memory refs.
	if walPath == "" {
		return
	}

	companion := walPath[:len(walPath)-len(walFileExt)] + walFilesExt
	data, err := json.Marshal(snapshot)
	if err != nil {
		a.api.LogWarn("Azure Blob: failed to marshal pending files",
			"error_code", errcode.AzureBlobMarshalPendingFailed,
			"error", sanitizeAzureError(err))
		return
	}
	a.companionMu.Lock()
	defer a.companionMu.Unlock()
	if err := os.WriteFile(companion, data, 0o600); err != nil {
		a.api.LogError("Azure Blob: failed to write companion files.json",
			"error_code", errcode.AzureBlobWriteCompanionFailed,
			"path", companion, "error", sanitizeAzureError(err))
	}
}

// startFlushLoop kicks off the flush ticker goroutine. Shutdown is driven
// solely by ctx cancellation: on ctx.Done the loop runs a bounded final
// flush and persists any residual refs before exiting.
func (a *azureBlobProvider) startFlushLoop(ctx context.Context) {
	a.flushTicker = time.NewTicker(a.flushInterval)
	a.flushDone = make(chan struct{})

	go func() {
		defer close(a.flushDone)
		defer a.flushTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				// Final flush on graceful shutdown, bounded so a hung Azure
				// upload cannot block plugin deactivation.
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				a.flush(shutdownCtx)
				cancel()
				// Persist any refs that bounced back into pendingFiles so
				// they are not silently lost. Also surface any WAL residue
				// for operator visibility.
				a.persistShutdownResidue()
				if n := a.countWALFiles(); n > 0 {
					a.api.LogWarn("Azure Blob: shutdown left WAL files for recovery",
						"error_code", errcode.AzureBlobShutdownLeftWALFiles,
						"count", n, "wal_dir", a.walDir)
				}
				return
			case <-a.flushTicker.C:
				a.flush(ctx)
			}
		}
	}()
}

// flush rotates the WAL, uploads the closed file, and then uploads deferred
// file refs. On upload failure, the WAL file is left on disk for recovery.
//
// Sync/Close of the old handle are performed *after* dropping walMu so
// concurrent Publish calls are not blocked on disk I/O; the file handle is
// only detached (a.walFile = nil, a.walPath = "") inside the lock.
func (a *azureBlobProvider) flush(ctx context.Context) {
	a.walMu.Lock()
	a.pendingFilesMu.Lock()

	if a.walFile == nil && len(a.pendingFiles) == 0 {
		a.pendingFilesMu.Unlock()
		a.walMu.Unlock()
		return
	}

	var oldFile *os.File
	var oldWALPath string
	if a.walFile != nil {
		oldFile = a.walFile
		oldWALPath = a.walPath
		a.walFile = nil
		a.walPath = ""
	}

	oldPending := a.pendingFiles
	a.pendingFiles = nil

	a.pendingFilesMu.Unlock()
	a.walMu.Unlock()

	// Sync + Close outside the lock so Publish callers can immediately open
	// a new WAL file without waiting on disk I/O.
	if oldFile != nil {
		if err := oldFile.Sync(); err != nil {
			a.api.LogWarn("Azure Blob: WAL fsync failed during rotation",
				"error_code", errcode.AzureBlobWALFsyncRotationFailed,
				"path", oldWALPath, "error", sanitizeAzureError(err))
		}
		if err := oldFile.Close(); err != nil {
			a.api.LogError("Azure Blob: WAL close failed during rotation, skipping upload",
				"error_code", errcode.AzureBlobWALCloseRotationFailed,
				"path", oldWALPath, "error", sanitizeAzureError(err))
			oldWALPath = ""
		}
	}

	// Step 5: Upload the old WAL file.
	if oldWALPath != "" {
		if err := a.uploadWALFile(ctx, oldWALPath); err != nil {
			a.api.LogError("Azure Blob: WAL upload failed, leaving for recovery",
				"error_code", errcode.AzureBlobWALUploadFailed,
				"path", oldWALPath, "error", sanitizeAzureError(err))
			return
		}
		if err := os.Remove(oldWALPath); err != nil && !os.IsNotExist(err) {
			a.api.LogWarn("Azure Blob: failed to delete WAL after upload",
				"error_code", errcode.AzureBlobDeleteWALFailed,
				"path", oldWALPath, "error", sanitizeAzureError(err))
		}
	}

	// Step 6: Upload deferred file refs. Capture failures so the companion
	// file can be rewritten with just the failed refs (instead of deleted),
	// preserving them across a crash before the next flush cycle.
	var failed []pendingFileRef
	if len(oldPending) > 0 {
		failed = a.flushPendingFilesList(ctx, oldPending)
	}

	// Rewrite or remove the companion .files.json for the flushed batch.
	// We rewrite under companionMu so we don't race QueueFileRef on the same
	// path (it writes to the *current* companion, which only matches oldWAL
	// if a new WAL hasn't rotated in).
	if oldWALPath != "" {
		companion := oldWALPath[:len(oldWALPath)-len(walFileExt)] + walFilesExt
		a.companionMu.Lock()
		if len(failed) > 0 {
			data, mErr := json.Marshal(failed)
			if mErr != nil {
				a.api.LogError("Azure Blob: failed to marshal failed refs for companion rewrite",
					"error_code", errcode.AzureBlobMarshalFailedRefsFailed,
					"path", companion, "count", len(failed), "error", mErr.Error())
			} else if wErr := os.WriteFile(companion, data, 0o600); wErr != nil {
				a.api.LogError("Azure Blob: failed to rewrite companion files.json with failed refs",
					"error_code", errcode.AzureBlobRewriteCompanionFailed,
					"path", companion, "count", len(failed), "error", wErr.Error())
			}
		} else {
			if err := os.Remove(companion); err != nil && !os.IsNotExist(err) {
				a.api.LogWarn("Azure Blob: failed to delete companion files.json",
					"error_code", errcode.AzureBlobDeleteCompanionFailed,
					"path", companion, "error", sanitizeAzureError(err))
			}
		}
		a.companionMu.Unlock()
	}
}

// uploadWALFile uploads a WAL file as a message batch blob.
func (a *azureBlobProvider) uploadWALFile(ctx context.Context, path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path is constructed from trusted nodeID/seq/timestamp
	if err != nil {
		return fmt.Errorf("read WAL: %w", err)
	}
	if len(data) == 0 {
		return nil // nothing to upload
	}

	base := filepath.Base(path)
	blobName := blobMessagePrefix + a.connName + "/" + base

	if err := a.containerClient.UploadBlob(ctx, blobName, data, nil); err != nil {
		return fmt.Errorf("upload blob %q: %w", blobName, err)
	}
	return nil
}

// flushPendingFilesList uploads each pending file ref. Failures are re-enqueued
// so they are retried on the next flush cycle. Returns the refs that failed
// so callers (e.g. recovery) can persist them rather than rely on the in-memory
// re-enqueue.
func (a *azureBlobProvider) flushPendingFilesList(ctx context.Context, refs []pendingFileRef) []pendingFileRef {
	var failed []pendingFileRef
	for i, ref := range refs {
		// Respect shutdown: re-enqueue the rest rather than keep uploading.
		if ctx.Err() != nil {
			failed = append(failed, refs[i:]...)
			break
		}
		data, err := a.getFile(ref.FileID)
		if err != nil {
			a.api.LogError("Azure Blob: deferred file fetch failed",
				"error_code", errcode.AzureBlobDeferredFileFetchFailed,
				"file_id", ref.FileID, "post_id", ref.PostID, "error", sanitizeAzureError(err))
			continue
		}
		key := ref.PostID + "/" + ref.FileID
		headers := map[string]string{
			headerPostID:   ref.PostID,
			headerConnName: a.connName,
			headerFilename: ref.Filename,
		}
		if err := a.UploadFile(ctx, key, data, headers); err != nil {
			a.api.LogError("Azure Blob: deferred file upload failed",
				"error_code", errcode.AzureBlobDeferredFileUploadFailed,
				"file_id", ref.FileID, "post_id", ref.PostID, "error", sanitizeAzureError(err))
			failed = append(failed, ref)
		}
	}
	if len(failed) > 0 {
		a.pendingFilesMu.Lock()
		a.pendingFiles = append(failed, a.pendingFiles...)
		a.pendingFilesMu.Unlock()
	}
	return failed
}

// persistShutdownResidue writes any in-memory pendingFiles to a dedicated
// companion file inside walDir so they are recoverable on next startup. Must
// be called after the final flush, before returning from the flush loop.
func (a *azureBlobProvider) persistShutdownResidue() {
	a.pendingFilesMu.Lock()
	refs := a.pendingFiles
	a.pendingFiles = nil
	a.pendingFilesMu.Unlock()

	if len(refs) == 0 {
		return
	}

	name := fmt.Sprintf("%s-shutdown-%d%s", a.nodeID, time.Now().UnixMilli(), walFilesExt)
	path := filepath.Join(a.walDir, name)
	data, err := json.Marshal(refs)
	if err != nil {
		a.api.LogError("Azure Blob: shutdown: failed to marshal residual pending files",
			"error_code", errcode.AzureBlobShutdownMarshalResidualFailed,
			"count", len(refs), "error", sanitizeAzureError(err))
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		a.api.LogError("Azure Blob: shutdown: failed to persist residual pending files",
			"error_code", errcode.AzureBlobShutdownPersistResidualFailed,
			"count", len(refs), "path", path, "error", sanitizeAzureError(err))
		return
	}
	a.api.LogWarn("Azure Blob: shutdown persisted residual pending files for recovery",
		"error_code", errcode.AzureBlobShutdownPersistedResidual,
		"count", len(refs), "path", path)
}

// Subscribe starts the inbound poll loop. The outbound flush loop is started
// separately by the constructor when isOutbound is true, so inbound-only
// providers do not run an idle flush loop. Subscribe is idempotent against
// concurrent Close and errors on double-Subscribe.
func (a *azureBlobProvider) Subscribe(ctx context.Context, handler func(data []byte) error) error {
	a.subMu.Lock()
	if a.subscribed {
		a.subMu.Unlock()
		return fmt.Errorf("azure-blob: Subscribe called twice")
	}
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.handler = handler
	a.pollDone = make(chan struct{})
	a.subscribed = true
	pollDone := a.pollDone
	a.subMu.Unlock()

	go func() {
		defer close(pollDone)
		a.pollBlobs(ctx)
	}()

	return nil
}

// pollBlobs periodically lists and processes message batch blobs. Transient
// list failures back off exponentially (capped at listBlobsBackoffMax) so a
// flapping Azure endpoint does not spam the log with errors every tick.
func (a *azureBlobProvider) pollBlobs(ctx context.Context) {
	prefix := blobMessagePrefix + a.connName + "/"
	backoff := time.Duration(0)
	for {
		wait := a.batchPoll
		if backoff > 0 {
			wait = backoff
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		blobs, err := a.containerClient.ListBlobs(ctx, prefix, false)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.api.LogError("Azure Blob: list failed",
				"error_code", errcode.AzureBlobListFailed,
				"container", a.cfg.BlobContainerName, "error", sanitizeAzureError(err))
			backoff = nextListBackoff(backoff, a.batchPoll)
			continue
		}
		backoff = 0
		for _, b := range blobs {
			if ctx.Err() != nil {
				return
			}
			a.processBlob(ctx, b.Name)
		}
	}
}

// nextListBackoff doubles the current backoff, clamped to listBlobsBackoffMax.
// A zero backoff starts at base (the provider's batch poll interval).
func nextListBackoff(current, base time.Duration) time.Duration {
	if current <= 0 {
		return base
	}
	return min(current*2, listBlobsBackoffMax)
}

// processBlob acquires a lock, downloads, processes line-by-line, and
// deletes the blob on success. To prevent duplicate delivery when DeleteBlob
// fails after the handler has already consumed the data, we write a
// short-lived "processed" marker to the KV store just before deleting. On a
// subsequent run the marker short-circuits re-delivery and only retries the
// delete.
func (a *azureBlobProvider) processBlob(ctx context.Context, blobName string) {
	if !a.tryAcquireBlobLock(blobName) {
		return
	}

	success := false
	defer func() {
		// Release lock on failure so another node can retry.
		if !success {
			a.releaseBlobLock(blobName)
		}
	}()

	// If we already processed this blob on a previous run, skip straight to
	// the delete + marker cleanup path.
	if a.isBlobProcessed(blobName) {
		if err := a.containerClient.DeleteBlob(ctx, blobName); err != nil {
			a.api.LogWarn("Azure Blob: delete retry failed (marker present)",
				"error_code", errcode.AzureBlobDeleteRetryFailed,
				"blob", blobName, "error", sanitizeAzureError(err))
			return
		}
		a.clearBlobProcessed(blobName)
		success = true
		a.releaseBlobLock(blobName)
		return
	}

	data, err := a.containerClient.DownloadBlob(ctx, blobName)
	if err != nil {
		a.api.LogWarn("Azure Blob: download failed",
			"error_code", errcode.AzureBlobDownloadFailed,
			"blob", blobName, "error", sanitizeAzureError(err))
		return
	}

	for line := range bytes.SplitSeq(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if err := a.handler(line); err != nil {
			a.api.LogWarn("Azure Blob: handler error, will retry blob",
				"error_code", errcode.AzureBlobHandlerError,
				"blob", blobName, "error", sanitizeAzureError(err))
			return
		}
	}

	// Mark the blob as processed before deleting so a delete failure cannot
	// cause duplicate delivery on the retry.
	a.markBlobProcessed(blobName)

	if err := a.containerClient.DeleteBlob(ctx, blobName); err != nil {
		a.api.LogWarn("Azure Blob: delete failed",
			"error_code", errcode.AzureBlobDeleteFailed,
			"blob", blobName, "error", sanitizeAzureError(err))
		return
	}

	a.clearBlobProcessed(blobName)
	success = true
	a.releaseBlobLock(blobName)
}

// blobProcessedKey returns the KV key for the "blob processed" marker.
func blobProcessedKey(blobName string) string {
	h := sha1.Sum([]byte(blobName)) //nolint:gosec // non-cryptographic use
	return blobProcessedKeyPrefix + hex.EncodeToString(h[:])
}

func (a *azureBlobProvider) markBlobProcessed(blobName string) {
	key := blobProcessedKey(blobName)
	if _, err := a.kv.Set(key, []byte{1}, pluginapi.SetExpiry(time.Duration(blobProcessedMarkerTTLSeconds)*time.Second)); err != nil {
		a.api.LogWarn("Azure Blob: failed to write processed marker",
			"error_code", errcode.AzureBlobWriteProcessedMarkerFailed,
			"blob", blobName, "error", sanitizeAzureError(err))
	}
}

func (a *azureBlobProvider) isBlobProcessed(blobName string) bool {
	var raw []byte
	if err := a.kv.Get(blobProcessedKey(blobName), &raw); err != nil {
		return false
	}
	return len(raw) > 0
}

func (a *azureBlobProvider) clearBlobProcessed(blobName string) {
	if err := a.kv.Delete(blobProcessedKey(blobName)); err != nil {
		a.api.LogWarn("Azure Blob: failed to clear processed marker",
			"error_code", errcode.AzureBlobClearProcessedMarkerFailed,
			"blob", blobName, "error", sanitizeAzureError(err))
	}
}

// tryAcquireBlobLock acquires the KV lock for a blob name. Returns true if
// this node owns the lock (either freshly acquired or stale-reclaimed).
// On success the fencing token is cached in a.lockTokens so releaseBlobLock
// can verify ownership before deleting.
func (a *azureBlobProvider) tryAcquireBlobLock(blobName string) bool {
	key := blobLockKey(blobName)

	var raw []byte
	if err := a.kv.Get(key, &raw); err != nil {
		a.api.LogWarn("Azure Blob: lock get failed",
			"error_code", errcode.AzureBlobLockGetFailed,
			"blob", blobName, "error", sanitizeAzureError(err))
		return false
	}

	token, err := newLockToken()
	if err != nil {
		a.api.LogWarn("Azure Blob: lock token generation failed",
			"error_code", errcode.AzureBlobLockTokenGenFailed,
			"blob", blobName, "error", sanitizeAzureError(err))
		return false
	}
	newLock := blobLock{Node: a.nodeID, Acquired: time.Now().UnixMilli(), Token: token}

	if len(raw) == 0 {
		ok, setErr := a.kv.Set(key, newLock, pluginapi.SetAtomic(nil))
		if setErr != nil {
			a.api.LogWarn("Azure Blob: lock set failed",
				"error_code", errcode.AzureBlobLockSetFailed,
				"blob", blobName, "error", setErr.Error())
			return false
		}
		if ok {
			a.rememberLockToken(blobName, token)
		}
		return ok
	}

	var current blobLock
	if unmarshalErr := json.Unmarshal(raw, &current); unmarshalErr != nil {
		// Corrupt lock value; try to reclaim.
		ok, setErr := a.kv.Set(key, newLock, pluginapi.SetAtomic(raw))
		if setErr != nil {
			a.api.LogWarn("Azure Blob: corrupt lock reclaim failed",
				"error_code", errcode.AzureBlobCorruptLockReclaimFailed,
				"blob", blobName, "error", setErr.Error())
			return false
		}
		if ok {
			a.rememberLockToken(blobName, token)
		}
		return ok
	}

	age := time.Since(time.UnixMilli(current.Acquired))
	if age < a.blobLockMaxAge() {
		return false
	}

	ok, err := a.kv.Set(key, newLock, pluginapi.SetAtomic(raw))
	if err != nil {
		a.api.LogWarn("Azure Blob: stale lock reclaim failed",
			"error_code", errcode.AzureBlobStaleLockReclaimFailed,
			"blob", blobName, "error", sanitizeAzureError(err))
		return false
	}
	if ok {
		a.rememberLockToken(blobName, token)
	}
	return ok
}

// newLockToken returns a 128-bit random hex string used as a fencing token.
func newLockToken() (string, error) {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// rememberLockToken stores the token this node just wrote so releaseBlobLock
// can compare-then-delete.
func (a *azureBlobProvider) rememberLockToken(blobName, token string) {
	a.lockTokensMu.Lock()
	if a.lockTokens == nil {
		a.lockTokens = make(map[string]string)
	}
	a.lockTokens[blobName] = token
	a.lockTokensMu.Unlock()
}

// takeLockToken removes and returns the cached token for blobName, if any.
func (a *azureBlobProvider) takeLockToken(blobName string) (string, bool) {
	a.lockTokensMu.Lock()
	defer a.lockTokensMu.Unlock()
	token, ok := a.lockTokens[blobName]
	if ok {
		delete(a.lockTokens, blobName)
	}
	return token, ok
}

// blobLockMaxAge returns the configured stale-lock TTL or the default.
// The returned value is clamped at blobLockMaxAgeCap as a defense-in-depth
// sanity bound; operators who configure a larger value are caught at
// validation time, but a stale on-disk config must still not be able to hold
// a lock for longer than the cap.
func (a *azureBlobProvider) blobLockMaxAge() time.Duration {
	if a.cfg.BlobLockMaxAgeSeconds > 0 {
		d := time.Duration(a.cfg.BlobLockMaxAgeSeconds) * time.Second
		if d > blobLockMaxAgeCap {
			return blobLockMaxAgeCap
		}
		return d
	}
	return blobLockMaxAge
}

// releaseBlobLock deletes the KV lock for a blob name, but only if the lock
// we acquired is still the one in the KV store. If the cached fencing token
// no longer matches the stored lock, another node has reclaimed it (stale
// timeout) and we must not clobber their state.
func (a *azureBlobProvider) releaseBlobLock(blobName string) {
	key := blobLockKey(blobName)

	ourToken, had := a.takeLockToken(blobName)
	if !had {
		// No cached token (tests, corrupt state). Fall back to an unconditional
		// delete so legacy callers still work.
		if err := a.kv.Delete(key); err != nil {
			a.api.LogWarn("Azure Blob: lock release failed",
				"error_code", errcode.AzureBlobLockReleaseFailed,
				"blob", blobName, "error", sanitizeAzureError(err))
		}
		return
	}

	var raw []byte
	if err := a.kv.Get(key, &raw); err != nil {
		a.api.LogWarn("Azure Blob: lock release get failed",
			"error_code", errcode.AzureBlobLockReleaseGetFailed,
			"blob", blobName, "error", sanitizeAzureError(err))
		return
	}
	if len(raw) == 0 {
		// Already gone.
		return
	}
	var current blobLock
	if err := json.Unmarshal(raw, &current); err != nil {
		// Corrupt lock value: safer to leave it for the stale-reclaim path.
		a.api.LogWarn("Azure Blob: lock release saw corrupt value, leaving",
			"error_code", errcode.AzureBlobLockReleaseCorrupt,
			"blob", blobName, "error", sanitizeAzureError(err))
		return
	}
	if current.Token != ourToken {
		a.api.LogWarn("Azure Blob: skipping release, lock reclaimed by another node",
			"error_code", errcode.AzureBlobLockReleaseReclaimedByOther,
			"blob", blobName, "current_node", current.Node)
		return
	}
	if err := a.kv.Delete(key); err != nil {
		a.api.LogWarn("Azure Blob: conditional lock release failed",
			"error_code", errcode.AzureBlobLockReleaseConditionalFailed,
			"blob", blobName, "error", sanitizeAzureError(err))
	}
}

// blobLockKey returns a KV key for a blob name, hashing to stay under the KV
// key length limit.
func blobLockKey(blobName string) string {
	h := sha1.Sum([]byte(blobName)) //nolint:gosec // key derivation, not crypto
	return blobLockKeyPrefix + hex.EncodeToString(h[:])
}

// recoverWALOnStartup scans the parent WAL root for leftover files from
// previous runs and re-uploads them. This handles the case where nodeID
// changes across restarts (orphaning the previous node's WAL directory).
// Only directories for this provider's connName are processed, so multiple
// azure-blob connections on the same node do not interfere.
func (a *azureBlobProvider) recoverWALOnStartup(ctx context.Context) {
	root := filepath.Join(os.TempDir(), walDirRoot)
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			a.api.LogWarn("Azure Blob: WAL recovery: failed to scan root",
				"error_code", errcode.AzureBlobWALRecoveryScanRootFailed,
				"root", root, "error", sanitizeAzureError(err))
		}
		return
	}

	for _, e := range entries {
		if ctx.Err() != nil {
			return
		}
		if !e.IsDir() {
			continue
		}
		nodeDir := filepath.Join(root, e.Name())
		connDir := filepath.Join(nodeDir, a.connName)
		if info, statErr := os.Stat(connDir); statErr != nil || !info.IsDir() {
			continue
		}
		isCurrent := e.Name() == a.nodeID
		a.recoverDirectory(ctx, connDir, isCurrent)
		if !isCurrent {
			// Remove the old nodeID parent if now empty. Ignore non-empty errors.
			_ = os.Remove(nodeDir)
		}
	}
}

// recoverDirectory uploads WAL files and processes companion .files.json in a
// WAL directory. If isCurrent is false, the directory is removed after
// recovery (it belongs to a previous nodeID). For the current node's
// directory, any WAL file whose embedded timestamp is at or after the
// provider start time is skipped: such a file was created by a live Publish
// call and must not be raced against.
func (a *azureBlobProvider) recoverDirectory(ctx context.Context, dir string, isCurrent bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		a.api.LogWarn("Azure Blob: WAL recovery: failed to scan directory",
			"error_code", errcode.AzureBlobWALRecoveryScanDirFailed,
			"dir", dir, "error", sanitizeAzureError(err))
		return
	}

	for _, e := range entries {
		if ctx.Err() != nil {
			return
		}
		if e.IsDir() {
			continue
		}
		name := e.Name()
		path := filepath.Join(dir, name)

		switch {
		case strings.HasSuffix(name, walFileExt):
			if !walFileNameRe.MatchString(name) {
				a.api.LogWarn("Azure Blob: WAL recovery: skipping unrecognized file",
					"error_code", errcode.AzureBlobWALRecoverySkipUnrecognized,
					"path", path)
				continue
			}
			if isCurrent {
				if ts, ok := walFileTimestampMs(name); ok && ts >= a.recoveryStartMs {
					continue
				}
			}
			if err := a.uploadWALFile(ctx, path); err != nil {
				a.api.LogError("Azure Blob: WAL recovery upload failed",
					"error_code", errcode.AzureBlobWALRecoveryUploadFailed,
					"path", path, "error", sanitizeAzureError(err))
				continue
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				a.api.LogWarn("Azure Blob: WAL recovery delete failed",
					"error_code", errcode.AzureBlobWALRecoveryDeleteFailed,
					"path", path, "error", sanitizeAzureError(err))
			}
		case strings.HasSuffix(name, walFilesExt):
			a.recoverCompanionFiles(ctx, path)
		}
	}

	if !isCurrent {
		// Remove the old nodeID directory if empty.
		_ = os.Remove(dir)
	}
}

// recoverCompanionFiles reads a companion .files.json and uploads the listed
// files. Refs that fail to upload are persisted back to the same companion
// file so they are retried on the next startup; the file is only removed when
// all refs succeed (or when the file is malformed / empty).
func (a *azureBlobProvider) recoverCompanionFiles(ctx context.Context, path string) {
	raw, err := os.ReadFile(path) //nolint:gosec // path from walDir scan
	if err != nil {
		a.api.LogWarn("Azure Blob: WAL recovery: failed to read companion file",
			"error_code", errcode.AzureBlobWALRecoveryReadCompanionFail,
			"path", path, "error", sanitizeAzureError(err))
		return
	}
	var refs []pendingFileRef
	if unmarshalErr := json.Unmarshal(raw, &refs); unmarshalErr != nil {
		a.api.LogWarn("Azure Blob: WAL recovery: malformed companion file",
			"error_code", errcode.AzureBlobWALRecoveryMalformedCompanion,
			"path", path, "error", unmarshalErr.Error())
		_ = os.Remove(path)
		return
	}
	if len(refs) == 0 {
		_ = os.Remove(path)
		return
	}
	failed := a.flushPendingFilesList(ctx, refs)
	if len(failed) == 0 {
		_ = os.Remove(path)
		return
	}
	// Rewrite the companion with only the failed refs so the next startup
	// retries them. On write failure, keep the original file untouched so the
	// data is not lost.
	data, err := json.Marshal(failed)
	if err != nil {
		a.api.LogWarn("Azure Blob: WAL recovery: failed to marshal remaining refs",
			"error_code", errcode.AzureBlobWALRecoveryMarshalRemaining,
			"path", path, "count", len(failed), "error", sanitizeAzureError(err))
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		a.api.LogWarn("Azure Blob: WAL recovery: failed to rewrite companion file",
			"error_code", errcode.AzureBlobWALRecoveryRewriteCompanion,
			"path", path, "count", len(failed), "error", sanitizeAzureError(err))
	}
}

// UploadFile uploads a file blob with metadata headers.
func (a *azureBlobProvider) UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error {
	blobName := blobFilesPrefix + key

	meta := azureBlobMetadata{Headers: headers}
	// Marshal cannot fail for a struct with only a map[string]string field.
	metaJSON, _ := json.Marshal(meta) //nolint:errcheck // string-only map cannot fail to marshal
	encoded := base64.StdEncoding.EncodeToString(metaJSON)
	blobMeta := map[string]*string{
		blobMetadataHeadersKey: &encoded,
	}

	if err := a.containerClient.UploadBlob(ctx, blobName, data, blobMeta); err != nil {
		return fmt.Errorf("failed to upload blob %q: %w", blobName, err)
	}
	return nil
}

// WatchFiles watches the files/ prefix for new file blobs. It tracks its own
// cancel and done channel so Close can tear it down deterministically.
func (a *azureBlobProvider) WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error {
	a.subMu.Lock()
	if a.watching {
		a.subMu.Unlock()
		return fmt.Errorf("azure-blob: WatchFiles called twice")
	}
	watchCtx, cancel := context.WithCancel(ctx)
	a.watchCancel = cancel
	a.watchDone = make(chan struct{})
	a.watching = true
	watchDone := a.watchDone
	a.subMu.Unlock()

	defer close(watchDone)
	ctx = watchCtx

	backoff := time.Duration(0)
	for {
		wait := a.batchPoll
		if backoff > 0 {
			wait = backoff
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}

		blobs, err := a.containerClient.ListBlobs(ctx, blobFilesPrefix, true)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			a.api.LogError("Azure Blob: file list failed",
				"error_code", errcode.AzureBlobFileListFailed,
				"container", a.cfg.BlobContainerName, "error", sanitizeAzureError(err))
			backoff = nextListBackoff(backoff, a.batchPoll)
			continue
		}
		backoff = 0

		for _, blob := range blobs {
			if ctx.Err() != nil {
				return nil
			}
			// HA: use blob lock to avoid duplicate file processing.
			if !a.tryAcquireBlobLock(blob.Name) {
				continue
			}

			success := false
			func() {
				defer func() {
					if !success {
						a.releaseBlobLock(blob.Name)
					}
				}()

				// If we already delivered this file on a prior run, only
				// retry the delete + marker cleanup.
				if a.isBlobProcessed(blob.Name) {
					if err := a.containerClient.DeleteBlob(ctx, blob.Name); err != nil {
						a.api.LogWarn("Azure Blob: file delete retry failed (marker present)",
							"error_code", errcode.AzureBlobFileDeleteRetryFailed,
							"blob", blob.Name, "error", sanitizeAzureError(err))
						return
					}
					a.clearBlobProcessed(blob.Name)
					success = true
					a.releaseBlobLock(blob.Name)
					return
				}

				headers := extractBlobHeaders(blob.Metadata)
				data, err := a.containerClient.DownloadBlob(ctx, blob.Name)
				if err != nil {
					a.api.LogWarn("Azure Blob: file download failed",
						"error_code", errcode.AzureBlobFileDownloadFailed,
						"blob", blob.Name, "error", sanitizeAzureError(err))
					return
				}
				// Strip the files/ prefix when passing key to handler to match azureProvider behaviour.
				key := strings.TrimPrefix(blob.Name, blobFilesPrefix)
				if err := handler(key, data, headers); err != nil {
					a.api.LogWarn("Azure Blob: file handler error",
						"error_code", errcode.AzureBlobFileHandlerError,
						"blob", blob.Name, "error", sanitizeAzureError(err))
					return
				}
				a.markBlobProcessed(blob.Name)
				if err := a.containerClient.DeleteBlob(ctx, blob.Name); err != nil {
					a.api.LogWarn("Azure Blob: file delete failed",
						"error_code", errcode.AzureBlobFileDeleteFailed,
						"blob", blob.Name, "error", sanitizeAzureError(err))
					return
				}
				a.clearBlobProcessed(blob.Name)
				success = true
				a.releaseBlobLock(blob.Name)
			}()
		}
	}
}

// MaxMessageSize returns 0 because the azure-blob provider batches messages
// into files and does not impose a per-message size limit.
func (a *azureBlobProvider) MaxMessageSize() int {
	return 0
}

// Close flushes pending data and stops background goroutines. Safe to call
// concurrently; subsequent calls are no-ops.
func (a *azureBlobProvider) Close() error {
	a.closeOnce.Do(func() {
		// Cancel the outbound lifetime context so the flush loop runs its
		// final bounded flush, anything derived from it (e.g. in-flight
		// Azure SDK calls, the startup recovery goroutine) unwinds, and
		// then wait for the flush loop to finish.
		if a.outCancel != nil {
			a.outCancel()
		}
		if a.flushDone != nil {
			<-a.flushDone
		}
		if a.recoveryDone != nil {
			<-a.recoveryDone
		}

		// Snapshot the Subscribe/Watch lifecycle fields under the mutex so
		// concurrent late-arriving Subscribe/WatchFiles calls cannot race
		// against Close. After this Close returns, any late Subscribe will
		// still observe subscribed==true (or be cancelled by the closed
		// context) and exit quickly.
		a.subMu.Lock()
		cancel := a.cancel
		pollDone := a.pollDone
		watchCancel := a.watchCancel
		watchDone := a.watchDone
		a.subMu.Unlock()

		if cancel != nil {
			cancel()
		}
		if pollDone != nil {
			<-pollDone
		}
		if watchCancel != nil {
			watchCancel()
		}
		if watchDone != nil {
			<-watchDone
		}
	})
	return nil
}

// isContainerAlreadyExists returns true if err represents the Azure "container
// already exists" condition. Uses the typed bloberror code when available and
// falls back to a string match so tests that wrap a plain error still work.
func isContainerAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	if bloberror.HasCode(err, bloberror.ContainerAlreadyExists) {
		return true
	}
	return strings.Contains(err.Error(), azureErrContainerAlreadyExists)
}

// testAzureBlobConnection probes an azure-blob connection using the
// configured auth_mode. The probe is non-destructive (ListBlobs with a
// prefix that never matches), so it works under data-plane-only RBAC
// (Storage Blob Data Reader/Contributor); the container must be
// pre-provisioned in SP mode.
func testAzureBlobConnection(cfg AzureBlobProviderConfig) error {
	authMode := normalizeAzureAuthMode(cfg.AuthMode)
	azCloud, err := resolveAzureCloud(cfg.AzureCloud)
	if err != nil {
		return fmt.Errorf("azure-blob config: %w", err)
	}

	containerURL := buildBlobContainerURL(cfg.ServiceURL, cfg.BlobContainerName)

	var containerClient *container.Client
	switch authMode {
	case AzureAuthServicePrincipal:
		secret, secErr := resolveAzureSecret(azureSecretSource{
			Inline: cfg.ClientSecret, EnvVar: cfg.ClientSecretEnv, FilePath: cfg.ClientSecretFile,
		})
		if secErr != nil {
			return fmt.Errorf("azure-blob service principal: %w", secErr)
		}
		cred, credErr := buildClientSecretCredential(azureServicePrincipalParams{
			TenantID: cfg.TenantID, ClientID: cfg.ClientID, Secret: secret, Cloud: azCloud,
		})
		if credErr != nil {
			return credErr
		}
		opts := &container.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		containerClient, err = container.NewClient(containerURL, cred, opts)
		if err != nil {
			return fmt.Errorf("azure-blob NewClient: %s", sanitizeAzureError(err))
		}
	case "", AzureAuthSharedKey:
		cred, credErr := container.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
		if credErr != nil {
			return fmt.Errorf("azure-blob shared key credential: %s", sanitizeAzureError(credErr))
		}
		opts := &container.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		containerClient, err = container.NewClientWithSharedKeyCredential(containerURL, cred, opts)
		if err != nil {
			return fmt.Errorf("azure-blob NewClientWithSharedKeyCredential: %s", sanitizeAzureError(err))
		}
	default:
		return fmt.Errorf("azure-blob: unknown auth_mode %q", authMode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return testAzureBlobConnectionOps(ctx, &containerClientAdapter{client: containerClient}, cfg.BlobContainerName, authMode)
}

// testAzureBlobConnectionOps contains the provider-agnostic core of the
// connection test. Extracted so tests can inject a fake azureBlobOps.
// Uses a read-only ListBlobs probe with a sentinel prefix that will not
// match any real blob; the call confirms the container exists AND the
// credential authorizes the data-plane List operation.
func testAzureBlobConnectionOps(ctx context.Context, ops azureBlobOps, containerName, authMode string) error {
	_, err := ops.ListBlobs(ctx, "crossguard-probe-prefix-never-matches", false)
	if err != nil {
		return wrapAzureResourceError(err, "container", containerName, authMode)
	}
	return nil
}

// ensure interface compliance at compile time
var _ QueueProvider = (*azureBlobProvider)(nil)
