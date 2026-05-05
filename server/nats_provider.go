package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

const (
	natsConnectTimeout      = 30 * time.Second
	natsRelayConnectTimeout = 5 * time.Second
	natsPublishMaxRetries   = 3
	natsPublishBaseDelay    = 500 * time.Millisecond
	natsPublishMaxDelay     = 5 * time.Second
	natsMaxReconnects       = -1 // unlimited
	natsReconnectWait       = 2 * time.Second

	objectStoreBucket  = "crossguard-files"
	objectStoreTTL     = time.Hour
	defaultMaxFileSize = 100 * 1024 * 1024 // 100 MB
)

// natsProvider implements QueueProvider using NATS and JetStream Object Store.
type natsProvider struct {
	nc         *nats.Conn
	sub        *nats.Subscription
	subject    string
	queueGroup string
	api        plugin.API
}

func newNATSProvider(cfg NATSProviderConfig, api plugin.API, direction, queueGroup string) (QueueProvider, error) {
	nc, err := connectNATSPersistent(cfg, api, direction)
	if err != nil {
		return nil, err
	}
	return &natsProvider{
		nc:         nc,
		subject:    cfg.Subject,
		queueGroup: queueGroup,
		api:        api,
	}, nil
}

func newNATSProviderForTest(cfg NATSProviderConfig) (*nats.Conn, error) {
	return connectNATSOneShot(cfg)
}

func (n *natsProvider) Publish(ctx context.Context, data []byte) error {
	var lastErr error
	for attempt := range natsPublishMaxRetries {
		if err := n.nc.Publish(n.subject, data); err != nil {
			lastErr = err
		} else if err := n.nc.Flush(); err != nil {
			lastErr = err
		} else {
			return nil
		}

		delay := min(natsPublishBaseDelay*time.Duration(1<<attempt), natsPublishMaxDelay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}

func (n *natsProvider) Subscribe(_ context.Context, handler func(data []byte) error) error {
	msgHandler := func(msg *nats.Msg) {
		_ = handler(msg.Data)
	}

	var sub *nats.Subscription
	var err error
	if n.queueGroup != "" {
		sub, err = n.nc.QueueSubscribe(n.subject, n.queueGroup, msgHandler)
	} else {
		sub, err = n.nc.Subscribe(n.subject, msgHandler)
	}
	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", n.subject, err)
	}
	n.sub = sub
	return nil
}

func (n *natsProvider) UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error {
	objectStore, err := getOrCreateObjectStore(ctx, n.nc, objectStoreBucket)
	if err != nil {
		return fmt.Errorf("failed to open object store: %w", err)
	}

	natsHeaders := nats.Header{}
	for k, v := range headers {
		natsHeaders.Set(k, v)
	}

	meta := jetstream.ObjectMeta{
		Name:    key,
		Headers: natsHeaders,
	}

	_, err = objectStore.Put(ctx, meta, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to upload file %q: %w", key, err)
	}
	return nil
}

func (n *natsProvider) WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error {
	objectStore, err := getOrCreateObjectStore(ctx, n.nc, objectStoreBucket)
	if err != nil {
		return fmt.Errorf("failed to open object store for watcher: %w", err)
	}

	watcher, err := objectStore.Watch(ctx, jetstream.UpdatesOnly())
	if err != nil {
		return fmt.Errorf("failed to start object store watcher: %w", err)
	}
	defer func() { _ = watcher.Stop() }()

	for {
		select {
		case <-ctx.Done():
			return nil
		case info, ok := <-watcher.Updates():
			if !ok {
				return nil
			}
			if info == nil || info.Deleted {
				continue
			}

			headers := make(map[string]string)
			for k := range info.Headers {
				headers[k] = info.Headers.Get(k)
			}

			fileData, err := objectStore.GetBytes(ctx, info.Name)
			if err != nil {
				n.api.LogError("Failed to download file from object store",
					"error_code", errcode.NATSDownloadFileFailed,
					"key", info.Name, "error", err.Error())
				continue
			}

			if err := handler(info.Name, fileData, headers); err != nil {
				n.api.LogWarn("File handler returned error",
					"error_code", errcode.NATSFileHandlerError,
					"key", info.Name, "error", err.Error())
			}
		}
	}
}

func (n *natsProvider) MaxMessageSize() int {
	return 0 // no practical limit
}

func (n *natsProvider) Close() error {
	if n.sub != nil {
		_ = n.sub.Unsubscribe()
	}
	_ = n.nc.Drain()
	n.nc.Close()
	return nil
}

func (n *natsProvider) IsConnected() bool {
	return n.nc.IsConnected() || n.nc.IsReconnecting()
}

// connectNATSOneShot creates a one-shot NATS connection for testing.
func connectNATSOneShot(cfg NATSProviderConfig) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Timeout(natsConnectTimeout),
	}
	opts = appendNATSTLSOptions(opts, cfg)
	opts = appendNATSAuthOptions(opts, cfg)

	nc, err := nats.Connect(cfg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS at %s: %w", cfg.Address, err)
	}
	return nc, nil
}

// connectNATSPersistent creates a persistent NATS connection with auto-reconnect.
func connectNATSPersistent(cfg NATSProviderConfig, api plugin.API, direction string) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Timeout(natsRelayConnectTimeout),
		nats.MaxReconnects(natsMaxReconnects),
		nats.ReconnectWait(natsReconnectWait),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			api.LogWarn(direction+" NATS disconnected",
				"error_code", errcode.NATSDisconnected,
				"name", cfg.Name, "error", errMsg)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			api.LogInfo(direction+" NATS reconnected",
				"error_code", errcode.NATSReconnected,
				"name", cfg.Name)
		}),
	}
	opts = appendNATSTLSOptions(opts, cfg)
	opts = appendNATSAuthOptions(opts, cfg)

	nc, err := nats.Connect(cfg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS at %s: %w", cfg.Address, err)
	}
	return nc, nil
}

func appendNATSTLSOptions(opts []nats.Option, cfg NATSProviderConfig) []nats.Option {
	if !cfg.TLSEnabled {
		return opts
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if cfg.ClientCert != "" && cfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCert, cfg.ClientKey)
		if err == nil {
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
	}

	if cfg.CACert != "" {
		caCert, err := os.ReadFile(cfg.CACert)
		if err == nil {
			caCertPool := x509.NewCertPool()
			if caCertPool.AppendCertsFromPEM(caCert) {
				tlsConfig.RootCAs = caCertPool
			}
		}
	}

	return append(opts, nats.Secure(tlsConfig))
}

func appendNATSAuthOptions(opts []nats.Option, cfg NATSProviderConfig) []nats.Option {
	switch cfg.AuthType {
	case AuthTypeToken:
		return append(opts, nats.Token(cfg.Token))
	case AuthTypeCredentials:
		return append(opts, nats.UserInfo(cfg.Username, cfg.Password))
	}
	return opts
}

func getOrCreateObjectStore(ctx context.Context, nc *nats.Conn, bucketName string) (jetstream.ObjectStore, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}
	obs, err := js.CreateOrUpdateObjectStore(ctx, jetstream.ObjectStoreConfig{
		Bucket: bucketName,
		TTL:    objectStoreTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create/update object store bucket %q: %w", bucketName, err)
	}
	return obs, nil
}
