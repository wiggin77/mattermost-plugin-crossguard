package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

// generateSelfSignedCert creates a self-signed certificate and key in the given
// directory and returns the paths to the cert and key PEM files.
func generateSelfSignedCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	require.NoError(t, err)

	certPath = filepath.Join(dir, "cert.pem")
	certFile, err := os.Create(certPath) //nolint:gosec // test-only temp file
	require.NoError(t, err)
	require.NoError(t, pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	require.NoError(t, certFile.Close())

	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)

	keyPath = filepath.Join(dir, "key.pem")
	keyFile, err := os.Create(keyPath) //nolint:gosec // test-only temp file
	require.NoError(t, err)
	require.NoError(t, pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	require.NoError(t, keyFile.Close())

	return certPath, keyPath
}

func TestIsOutboundLinked(t *testing.T) {
	tests := []struct {
		name         string
		outboundName string
		connNames    []store.TeamConnection
		expected     bool
	}{
		{
			name:         "linked outbound connection",
			outboundName: "high",
			connNames: []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "inbound", Connection: "high"},
			},
			expected: true,
		},
		{
			name:         "not linked outbound connection",
			outboundName: "other",
			connNames: []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "inbound", Connection: "high"},
			},
			expected: false,
		},
		{
			name:         "inbound name does not match outbound check",
			outboundName: "high",
			connNames: []store.TeamConnection{
				{Direction: "inbound", Connection: "high"},
			},
			expected: false,
		},
		{
			name:         "empty connection list",
			outboundName: "high",
			connNames:    nil,
			expected:     false,
		},
		{
			name:         "partial name match does not count",
			outboundName: "hig",
			connNames: []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isOutboundLinked(tc.outboundName, tc.connNames)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// appendNATSTLSOptions tests
// ---------------------------------------------------------------------------

func TestAppendNATSTLSOptions_Disabled(t *testing.T) {
	opts := []nats.Option{nats.Name("base")}
	cfg := NATSProviderConfig{TLSEnabled: false}
	result := appendNATSTLSOptions(opts, cfg)
	assert.Len(t, result, len(opts), "opts should be unchanged when TLS is disabled")
}

func TestAppendNATSTLSOptions_Enabled_NoCerts(t *testing.T) {
	opts := []nats.Option{}
	cfg := NATSProviderConfig{TLSEnabled: true}
	result := appendNATSTLSOptions(opts, cfg)
	assert.Len(t, result, 1, "should add exactly one option (Secure) when TLS enabled with no certs")
}

func TestAppendNATSTLSOptions_Enabled_WithClientCert(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateSelfSignedCert(t, dir)

	opts := []nats.Option{}
	cfg := NATSProviderConfig{
		TLSEnabled: true,
		ClientCert: certPath,
		ClientKey:  keyPath,
	}
	result := appendNATSTLSOptions(opts, cfg)
	// Still one option (Secure with a configured TLS config), but the client cert was loaded.
	assert.Len(t, result, 1, "should add one Secure option with client cert loaded")
}

func TestAppendNATSTLSOptions_Enabled_WithCACert(t *testing.T) {
	dir := t.TempDir()
	certPath, _ := generateSelfSignedCert(t, dir)

	opts := []nats.Option{}
	cfg := NATSProviderConfig{
		TLSEnabled: true,
		CACert:     certPath, // use the generated cert as a CA cert
	}
	result := appendNATSTLSOptions(opts, cfg)
	assert.Len(t, result, 1, "should add one Secure option with CA cert loaded")
}

func TestAppendNATSTLSOptions_Enabled_WithInvalidPaths(t *testing.T) {
	opts := []nats.Option{}
	cfg := NATSProviderConfig{
		TLSEnabled: true,
		ClientCert: "/nonexistent/cert.pem",
		ClientKey:  "/nonexistent/key.pem",
		CACert:     "/nonexistent/ca.pem",
	}
	result := appendNATSTLSOptions(opts, cfg)
	// Invalid paths are silently ignored; Secure option still added.
	assert.Len(t, result, 1, "should still add Secure option even with invalid cert paths")
}

// ---------------------------------------------------------------------------
// appendNATSAuthOptions tests
// ---------------------------------------------------------------------------

func TestAppendNATSAuthOptions_None(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		opts := []nats.Option{}
		cfg := NATSProviderConfig{AuthType: ""}
		result := appendNATSAuthOptions(opts, cfg)
		assert.Len(t, result, 0)
	})

	t.Run("explicit none", func(t *testing.T) {
		opts := []nats.Option{}
		cfg := NATSProviderConfig{AuthType: AuthTypeNone}
		result := appendNATSAuthOptions(opts, cfg)
		assert.Len(t, result, 0)
	})
}

func TestAppendNATSAuthOptions_Token(t *testing.T) {
	opts := []nats.Option{}
	cfg := NATSProviderConfig{
		AuthType: AuthTypeToken,
		Token:    "secret-token",
	}
	result := appendNATSAuthOptions(opts, cfg)
	assert.Len(t, result, 1, "should add one token auth option")
}

func TestAppendNATSAuthOptions_Credentials(t *testing.T) {
	opts := []nats.Option{}
	cfg := NATSProviderConfig{
		AuthType: AuthTypeCredentials,
		Username: "user",
		Password: "pass",
	}
	result := appendNATSAuthOptions(opts, cfg)
	assert.Len(t, result, 1, "should add one credentials auth option")
}

// ---------------------------------------------------------------------------
// Embedded NATS server tests: Publish
// ---------------------------------------------------------------------------

func TestNATSPublish_Success(t *testing.T) {
	addr := startEmbeddedNATS(t)
	subject := "test.publish.success"
	provider := connectToEmbeddedNATS(t, addr, subject)

	received := make(chan []byte, 1)
	sub, err := provider.nc.Subscribe(subject, func(msg *nats.Msg) {
		received <- msg.Data
	})
	require.NoError(t, err)
	defer func() { _ = sub.Unsubscribe() }()

	payload := []byte("hello world")
	err = provider.Publish(context.Background(), payload)
	require.NoError(t, err)

	select {
	case data := <-received:
		assert.Equal(t, payload, data)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for published message")
	}
}

func TestNATSPublish_ContextCancelled(t *testing.T) {
	// Create a provider pointing at an unreachable address so Publish enters
	// the retry loop, then cancel the context during backoff.
	nc, err := nats.Connect("nats://127.0.0.1:0", nats.NoReconnect(), nats.Timeout(50*time.Millisecond))
	if err == nil {
		// If somehow it connected (extremely unlikely), close and skip.
		nc.Close()
		t.Skip("unexpectedly connected to dummy address")
	}

	// Use a real embedded server but close the connection before publishing
	// so publishes fail and we enter the retry/backoff path.
	addr := startEmbeddedNATS(t)
	rawNC, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)

	provider := &natsProvider{nc: rawNC, subject: "test.ctx.cancel"}
	rawNC.Close() // force publish to fail

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err = provider.Publish(ctx, []byte("data"))
	assert.ErrorIs(t, err, context.Canceled)
}

// ---------------------------------------------------------------------------
// Embedded NATS server tests: Subscribe
// ---------------------------------------------------------------------------

func TestNATSSubscribe_ReceivesMessages(t *testing.T) {
	addr := startEmbeddedNATS(t)
	subject := "test.subscribe.receive"
	provider := connectToEmbeddedNATS(t, addr, subject)

	var mu sync.Mutex
	var received []byte
	done := make(chan struct{})

	err := provider.Subscribe(context.Background(), func(data []byte) error {
		mu.Lock()
		received = data
		mu.Unlock()
		close(done)
		return nil
	})
	require.NoError(t, err)

	// Publish via the raw connection
	err = provider.nc.Publish(subject, []byte("subscribe-test"))
	require.NoError(t, err)
	require.NoError(t, provider.nc.Flush())

	select {
	case <-done:
		mu.Lock()
		assert.Equal(t, []byte("subscribe-test"), received)
		mu.Unlock()
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for subscription handler")
	}
}

func TestNATSSubscribe_SetsSubField(t *testing.T) {
	addr := startEmbeddedNATS(t)
	subject := "test.subscribe.subfield"
	provider := connectToEmbeddedNATS(t, addr, subject)

	assert.Nil(t, provider.sub, "sub should be nil before Subscribe")

	err := provider.Subscribe(context.Background(), func(data []byte) error {
		return nil
	})
	require.NoError(t, err)
	assert.NotNil(t, provider.sub, "sub should be set after Subscribe")
}

// ---------------------------------------------------------------------------
// Embedded NATS server tests: Close
// ---------------------------------------------------------------------------

func TestNATSClose_Unsubscribes(t *testing.T) {
	addr := startEmbeddedNATS(t)
	subject := "test.close.unsub"
	provider := connectToEmbeddedNATS(t, addr, subject)

	err := provider.Subscribe(context.Background(), func(data []byte) error {
		return nil
	})
	require.NoError(t, err)
	require.NotNil(t, provider.sub)

	err = provider.Close()
	assert.NoError(t, err)
	assert.True(t, provider.nc.IsClosed(), "connection should be closed after Close()")
}

func TestNATSClose_NilSubscription(t *testing.T) {
	addr := startEmbeddedNATS(t)
	subject := "test.close.nilsub"
	provider := connectToEmbeddedNATS(t, addr, subject)

	// Do not subscribe, so provider.sub remains nil.
	assert.Nil(t, provider.sub)

	// Should not panic.
	err := provider.Close()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Embedded NATS server tests: IsConnected
// ---------------------------------------------------------------------------

func TestNATSIsConnected_Connected(t *testing.T) {
	addr := startEmbeddedNATS(t)
	subject := "test.isconnected"
	provider := connectToEmbeddedNATS(t, addr, subject)

	assert.True(t, provider.IsConnected(), "provider should be connected")
}

func TestNATSIsConnected_AfterClose(t *testing.T) {
	addr := startEmbeddedNATS(t)
	subject := "test.isconnected.closed"

	// Create our own connection so the cleanup helper does not race.
	nc, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)

	provider := &natsProvider{nc: nc, subject: subject}
	require.True(t, provider.IsConnected())

	err = provider.Close()
	require.NoError(t, err)

	assert.False(t, provider.IsConnected(), "provider should not be connected after Close")
}

// ---------------------------------------------------------------------------
// MaxMessageSize
// ---------------------------------------------------------------------------

func TestNATSMaxMessageSize(t *testing.T) {
	addr := startEmbeddedNATS(t)
	provider := connectToEmbeddedNATS(t, addr, "test.maxmsg")
	assert.Equal(t, 0, provider.MaxMessageSize())
}

// ---------------------------------------------------------------------------
// newNATSProvider / newNATSProviderForTest / connection tests
// ---------------------------------------------------------------------------

func TestNewNATSProvider_Success(t *testing.T) {
	addr := startEmbeddedNATS(t)
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn")

	cfg := NATSProviderConfig{
		Name:    "test-conn",
		Address: addr,
		Subject: "crossguard.test",
	}

	provider, err := newNATSProvider(cfg, api, "Outbound")
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer func() { _ = provider.Close() }()

	np, ok := provider.(*natsProvider)
	require.True(t, ok)
	assert.True(t, np.IsConnected())
	assert.Equal(t, "crossguard.test", np.subject)
}

func TestNewNATSProvider_InvalidAddress(t *testing.T) {
	api := &plugintest.API{}

	cfg := NATSProviderConfig{
		Name:    "bad",
		Address: "nats://invalid:9999",
		Subject: "crossguard.bad",
	}

	provider, err := newNATSProvider(cfg, api, "Outbound")
	assert.Error(t, err)
	assert.Nil(t, provider)
}

func TestNewNATSProviderForTest_Success(t *testing.T) {
	addr := startEmbeddedNATS(t)

	cfg := NATSProviderConfig{
		Address: addr,
		Subject: "crossguard.test",
	}

	nc, err := newNATSProviderForTest(cfg)
	require.NoError(t, err)
	require.NotNil(t, nc)
	defer nc.Close()

	assert.True(t, nc.IsConnected())
}

func TestNewNATSProviderForTest_InvalidAddress(t *testing.T) {
	cfg := NATSProviderConfig{
		Address: "nats://invalid:9999",
		Subject: "crossguard.bad",
	}

	nc, err := newNATSProviderForTest(cfg)
	assert.Error(t, err)
	assert.Nil(t, nc)
}

func TestConnectNATSOneShot_Success(t *testing.T) {
	addr := startEmbeddedNATS(t)
	cfg := NATSProviderConfig{Address: addr}

	nc, err := connectNATSOneShot(cfg)
	require.NoError(t, err)
	require.NotNil(t, nc)
	defer nc.Close()
	assert.True(t, nc.IsConnected())
}

func TestConnectNATSOneShot_WithAuth(t *testing.T) {
	// Token auth against a server that doesn't require it still connects.
	addr := startEmbeddedNATS(t)
	cfg := NATSProviderConfig{
		Address:  addr,
		AuthType: AuthTypeToken,
		Token:    "test-token",
	}

	nc, err := connectNATSOneShot(cfg)
	require.NoError(t, err)
	require.NotNil(t, nc)
	defer nc.Close()
}

func TestConnectNATSPersistent_Success(t *testing.T) {
	addr := startEmbeddedNATS(t)
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn")

	cfg := NATSProviderConfig{
		Name:    "test-conn",
		Address: addr,
	}

	nc, err := connectNATSPersistent(cfg, api, "Inbound")
	require.NoError(t, err)
	require.NotNil(t, nc)
	defer nc.Close()
	assert.True(t, nc.IsConnected())
}

func TestConnectNATSPersistent_InvalidAddress(t *testing.T) {
	api := &plugintest.API{}

	cfg := NATSProviderConfig{
		Name:    "bad",
		Address: "nats://invalid:9999",
	}

	nc, err := connectNATSPersistent(cfg, api, "Outbound")
	assert.Error(t, err)
	assert.Nil(t, nc)
}

// ---------------------------------------------------------------------------
// getOrCreateObjectStore tests
// ---------------------------------------------------------------------------

func TestGetOrCreateObjectStore_Success(t *testing.T) {
	addr := startEmbeddedNATS(t)
	nc, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	defer nc.Close()

	obs, err := getOrCreateObjectStore(context.Background(), nc, "test-bucket")
	require.NoError(t, err)
	require.NotNil(t, obs)
}

func TestGetOrCreateObjectStore_ClosedConnection(t *testing.T) {
	addr := startEmbeddedNATS(t)
	nc, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	nc.Close()

	_, err = getOrCreateObjectStore(context.Background(), nc, "test-bucket")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// UploadFile / WatchFiles integration tests
// ---------------------------------------------------------------------------

func TestNATSUploadFile_Success(t *testing.T) {
	addr := startEmbeddedNATS(t)
	provider := connectToEmbeddedNATS(t, addr, "test.upload")

	headers := map[string]string{
		"X-Conn-Name": "high",
		"X-Post-ID":   "post-123",
		"X-Filename":  "doc.pdf",
	}

	err := provider.UploadFile(context.Background(), "file-key-1", []byte("file content"), headers)
	require.NoError(t, err)
}

func TestNATSUploadFile_ClosedConnection(t *testing.T) {
	addr := startEmbeddedNATS(t)
	nc, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	nc.Close()

	provider := &natsProvider{nc: nc, subject: "test.upload.closed"}
	err = provider.UploadFile(context.Background(), "file-key", []byte("data"), nil)
	assert.Error(t, err)
}

func TestNATSWatchFiles_ReceivesUpload(t *testing.T) {
	addr := startEmbeddedNATS(t)

	// Create two providers sharing the same NATS server.
	uploaderNC, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	defer uploaderNC.Close()

	watcherNC, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	defer watcherNC.Close()

	api := &plugintest.API{}
	registerLogMocks(api, "LogError", "LogWarn")

	watcher := &natsProvider{nc: watcherNC, subject: "test.watch", api: api}
	uploader := &natsProvider{nc: uploaderNC, subject: "test.watch"}

	type fileResult struct {
		key     string
		data    []byte
		headers map[string]string
	}
	received := make(chan fileResult, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watchDone := make(chan error, 1)
	go func() {
		watchDone <- watcher.WatchFiles(ctx, func(key string, data []byte, headers map[string]string) error {
			received <- fileResult{key: key, data: data, headers: headers}
			return nil
		})
	}()

	// Give watcher time to start.
	time.Sleep(500 * time.Millisecond)

	// Upload a file.
	headers := map[string]string{"X-Filename": "test.txt"}
	err = uploader.UploadFile(context.Background(), "watch-file-1", []byte("watched content"), headers)
	require.NoError(t, err)

	select {
	case result := <-received:
		assert.Equal(t, "watch-file-1", result.key)
		assert.Equal(t, []byte("watched content"), result.data)
		assert.Equal(t, "test.txt", result.headers["X-Filename"])
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for file watcher to receive upload")
	}

	cancel()
	select {
	case err := <-watchDone:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watcher to exit")
	}
}

func TestNATSWatchFiles_HandlerError(t *testing.T) {
	addr := startEmbeddedNATS(t)

	uploaderNC, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	defer uploaderNC.Close()

	watcherNC, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	defer watcherNC.Close()

	api := &plugintest.API{}
	registerLogMocks(api, "LogError", "LogWarn")

	watcher := &natsProvider{nc: watcherNC, subject: "test.watch.handler-err", api: api}
	uploader := &natsProvider{nc: uploaderNC, subject: "test.watch.handler-err"}

	var callCount atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watchDone := make(chan error, 1)
	go func() {
		watchDone <- watcher.WatchFiles(ctx, func(key string, data []byte, headers map[string]string) error {
			callCount.Add(1)
			if key == "err-file" {
				return errors.New("handler failure")
			}
			return nil
		})
	}()

	time.Sleep(500 * time.Millisecond)

	// Upload a file whose handler will fail.
	require.NoError(t, uploader.UploadFile(context.Background(), "err-file", []byte("data1"), nil))
	// Upload a second file to confirm the watcher continues after handler error.
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, uploader.UploadFile(context.Background(), "ok-file", []byte("data2"), nil))

	// Wait for both files to be processed.
	require.Eventually(t, func() bool { return callCount.Load() >= 2 }, 10*time.Second, 100*time.Millisecond,
		"watcher should continue processing after handler error")

	cancel()
	select {
	case err := <-watchDone:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("WatchFiles did not exit after context cancellation")
	}
}

func TestNATSWatchFiles_DeletedObjectSkipped(t *testing.T) {
	addr := startEmbeddedNATS(t)

	nc1, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	defer nc1.Close()

	nc2, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	defer nc2.Close()

	api := &plugintest.API{}
	registerLogMocks(api, "LogError", "LogWarn")

	watcher := &natsProvider{nc: nc2, subject: "test.watch.del", api: api}
	uploader := &natsProvider{nc: nc1, subject: "test.watch.del"}

	var received atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watchDone := make(chan error, 1)
	go func() {
		watchDone <- watcher.WatchFiles(ctx, func(key string, data []byte, headers map[string]string) error {
			received.Add(1)
			return nil
		})
	}()

	time.Sleep(500 * time.Millisecond)

	// Upload then delete a file; the delete event should be skipped.
	require.NoError(t, uploader.UploadFile(context.Background(), "del-file", []byte("data"), nil))
	time.Sleep(300 * time.Millisecond)

	// Delete the file from the object store.
	os1, err := getOrCreateObjectStore(context.Background(), nc1, objectStoreBucket)
	require.NoError(t, err)
	require.NoError(t, os1.Delete(context.Background(), "del-file"))

	// Upload another file to confirm processing continues.
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, uploader.UploadFile(context.Background(), "ok-file", []byte("data2"), nil))

	require.Eventually(t, func() bool { return received.Load() >= 2 }, 10*time.Second, 100*time.Millisecond,
		"watcher should process both uploaded files and skip delete events")

	cancel()
	select {
	case err := <-watchDone:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("WatchFiles did not exit after context cancellation")
	}
}

func TestNATSWatchFiles_ContextCancelled(t *testing.T) {
	addr := startEmbeddedNATS(t)
	nc, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	defer nc.Close()

	api := &plugintest.API{}
	registerLogMocks(api, "LogError", "LogWarn")

	provider := &natsProvider{nc: nc, subject: "test.watch.cancel", api: api}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- provider.WatchFiles(ctx, func(key string, data []byte, headers map[string]string) error {
			return nil
		})
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("WatchFiles did not exit after context cancellation")
	}
}
