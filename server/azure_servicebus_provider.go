package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

// Package-level constants for the Service Bus provider.
const (
	serviceBusDefaultMaxMessageSize  = 192 * 1024       // 192 KiB, below Standard's 256 KiB ceiling
	serviceBusReceiveBatchSize       = 32               // matches azureDequeueBatchSize
	serviceBusReceiveMaxWait         = 5 * time.Second  // per-call ctx deadline for ReceiveMessages
	serviceBusAckTimeout             = 10 * time.Second // detached timeout for Complete/Abandon/Close
	azureSBErrContainerAlreadyExists = "ContainerAlreadyExists"
)

// Note: message lock duration is an ENTITY property configured on the Service
// Bus queue (via ARM/portal/Terraform), NOT a client knob. The plugin does not
// and cannot set it.

// serviceBusBlobPollInterval is the default blob poll interval (matches
// azure-queue's 15 s default). Declared var so tests can shrink it;
// per-connection override flows through BlobPollIntervalSeconds.
var serviceBusBlobPollInterval = 15 * time.Second

// serviceBusSender abstracts Service Bus send operations for testability.
type serviceBusSender interface {
	SendMessage(ctx context.Context, msg *azservicebus.Message, opts *azservicebus.SendMessageOptions) error
	Close(ctx context.Context) error
}

// serviceBusReceiver abstracts Service Bus receive + settle operations for testability.
type serviceBusReceiver interface {
	ReceiveMessages(ctx context.Context, maxMessages int, opts *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error)
	PeekMessages(ctx context.Context, maxMessageCount int, opts *azservicebus.PeekMessagesOptions) ([]*azservicebus.ReceivedMessage, error)
	CompleteMessage(ctx context.Context, msg *azservicebus.ReceivedMessage, opts *azservicebus.CompleteMessageOptions) error
	AbandonMessage(ctx context.Context, msg *azservicebus.ReceivedMessage, opts *azservicebus.AbandonMessageOptions) error
	Close(ctx context.Context) error
}

// serviceBusClientCloser is just the Close surface of *azservicebus.Client; we
// keep a reference only so Close() can tear it down after sender/receiver.
type serviceBusClientCloser interface {
	Close(ctx context.Context) error
}

// azureServiceBusProvider implements QueueProvider using Azure Service Bus
// queues for messages and Azure Blob Storage (sidecar container) for file
// transfer. It mirrors the simpler azureProvider (azure-queue) pattern rather
// than the batched-WAL azureBlobProvider.
type azureServiceBusProvider struct {
	client          serviceBusClientCloser
	sender          serviceBusSender
	receiver        serviceBusReceiver
	containerClient azureBlobOps
	api             plugin.API
	cfg             AzureServiceBusProviderConfig

	maxMsgSize int
	blobPoll   time.Duration

	// lifecycleMu guards Subscribe/Close lifecycle transitions: cancel,
	// handler, pollDone must only be read/written while holding this mutex.
	// Without it, a Close() racing with Subscribe() from a different
	// goroutine (e.g., plugin teardown vs. OnConfigurationChange) is a data
	// race flagged by `go test -race` and may leave the poll goroutine
	// orphaned or cause Close to read a stale nil pollDone.
	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	handler     func(data []byte) error
	pollDone    chan struct{}
}

// Compile-time conformance check.
var _ QueueProvider = (*azureServiceBusProvider)(nil)

// sanitizeServiceBusError delegates to the package-wide sanitizeAzureError
// helper (server/azure_errors.go). Kept as a thin wrapper so existing call
// sites in this file read naturally; the shared helper covers SAS material,
// OAuth2 client-credentials form bodies, AAD bearer tokens, and access/
// refresh token JSON.
func sanitizeServiceBusError(err error) string {
	return sanitizeAzureError(err)
}

// newAzureServiceBusProvider constructs the azure-servicebus provider. ctx
// is the plugin-lifetime context; it threads through to the azidentity
// credential's GetToken calls in SP mode and bounds the blob-sidecar
// auto-Create probe in connection-string mode.
//
// Auth mode selection:
//   - "" or "connection-string": legacy SAS-string flow.
//   - "service-principal": azidentity ClientSecretCredential + namespace FQDN.
//     The optional blob sidecar inherits the SAME SP credential; the per-
//     sidecar BlobAccountName/BlobAccountKey fields are forbidden by the
//     validator in SP mode (sidecar mixed-mode is Phase 2).
//
// Note: azservicebus.ClientOptions has NO Cloud field at v1.10.0 - data-plane
// routing is determined by the FQDN namespace itself (e.g.
// "myns.servicebus.usgovcloudapi.net"). Cloud is passed only to the AAD
// credential options.
func newAzureServiceBusProvider(ctx context.Context, cfg AzureServiceBusProviderConfig, api plugin.API) (QueueProvider, error) {
	authMode := normalizeAzureAuthMode(cfg.AuthMode)
	azCloud, err := resolveAzureCloud(cfg.AzureCloud)
	if err != nil {
		return nil, fmt.Errorf("azure-servicebus config: %w", err)
	}

	var (
		client       *azservicebus.Client
		spCredential azcore.TokenCredential // populated only in SP mode; reused for blob sidecar
	)

	switch authMode {
	case AzureAuthServicePrincipal:
		secret, secErr := resolveAzureSecret(azureSecretSource{
			Inline: cfg.ClientSecret, EnvVar: cfg.ClientSecretEnv, FilePath: cfg.ClientSecretFile,
		})
		if secErr != nil {
			return nil, fmt.Errorf("azure-servicebus service principal: %w", secErr)
		}
		cred, credErr := buildClientSecretCredential(azureServicePrincipalParams{
			TenantID: cfg.TenantID, ClientID: cfg.ClientID, Secret: secret, Cloud: azCloud,
		})
		if credErr != nil {
			api.LogError("Service Bus: service principal credential failed",
				"error_code", errcode.ServiceBusSPCredentialFailed,
				"tenant_id", cfg.TenantID, "client_id", cfg.ClientID, "error", sanitizeAzureError(credErr))
			return nil, credErr
		}
		logAzureAuthAudit(api, "azure-servicebus", cfg.TenantID, cfg.ClientID, cfg.AzureCloud, secret)
		spCredential = cred
		// Service Bus has no Cloud option on ClientOptions; the namespace
		// FQDN already encodes the routing for sovereign clouds.
		namespace := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(cfg.ServiceBusNamespace), "sb://"), "/")
		client, err = azservicebus.NewClient(namespace, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("service bus NewClient: %s", sanitizeAzureError(err))
		}
	case "", AzureAuthConnectionString:
		client, err = azservicebus.NewClientFromConnectionString(cfg.ConnectionString, nil)
		if err != nil {
			return nil, fmt.Errorf("service bus client init: %s", sanitizeAzureError(err))
		}
	default:
		return nil, fmt.Errorf("azure-servicebus: unknown auth_mode %q", authMode)
	}

	// closeWithTimeout bounds Service Bus / AMQP cleanup calls so a hung
	// link during a failed activation cannot block plugin startup
	// indefinitely. Mirrors the pattern used by the runtime Close().
	type ctxCloser interface {
		Close(context.Context) error
	}
	closeWithTimeout := func(c ctxCloser) {
		closeCtx, cancel := context.WithTimeout(context.Background(), serviceBusAckTimeout)
		defer cancel()
		_ = c.Close(closeCtx)
	}

	sender, err := client.NewSender(cfg.QueueName, nil)
	if err != nil {
		closeWithTimeout(client)
		return nil, fmt.Errorf("service bus sender init: %s", sanitizeAzureError(err))
	}

	receiver, err := client.NewReceiverForQueue(cfg.QueueName, &azservicebus.ReceiverOptions{
		ReceiveMode: azservicebus.ReceiveModePeekLock,
	})
	if err != nil {
		closeWithTimeout(sender)
		closeWithTimeout(client)
		return nil, fmt.Errorf("service bus receiver init: %s", sanitizeAzureError(err))
	}

	var blobOps azureBlobOps
	if cfg.BlobContainerName != "" {
		blobOps, err = newServiceBusBlobSidecar(ctx, cfg, authMode, azCloud, spCredential, api)
		if err != nil {
			closeWithTimeout(receiver)
			closeWithTimeout(sender)
			closeWithTimeout(client)
			return nil, err
		}
	}

	maxMsgSize := serviceBusDefaultMaxMessageSize
	if cfg.MaxMessageSizeBytes > 0 {
		maxMsgSize = cfg.MaxMessageSizeBytes
	}
	blobPoll := serviceBusBlobPollInterval
	if cfg.BlobPollIntervalSeconds > 0 {
		blobPoll = time.Duration(cfg.BlobPollIntervalSeconds) * time.Second
	}

	return &azureServiceBusProvider{
		client:          client,
		sender:          sender,
		receiver:        receiver,
		containerClient: blobOps,
		api:             api,
		cfg:             cfg,
		maxMsgSize:      maxMsgSize,
		blobPoll:        blobPoll,
	}, nil
}

// newServiceBusBlobSidecar constructs the optional blob container client
// for Service Bus file transfer. In connection-string mode the sidecar
// uses independent shared-key credentials (BlobAccountName/Key on the
// config). In SP mode the sidecar inherits the parent SP credential;
// validation forbids BlobAccountName/Key in that mode so this branch
// never reads them.
func newServiceBusBlobSidecar(ctx context.Context, cfg AzureServiceBusProviderConfig, authMode string, azCloud azcoreCloudConfig, spCredential azcore.TokenCredential, api plugin.API) (azureBlobOps, error) {
	containerURL := buildBlobContainerURL(cfg.BlobServiceURL, cfg.BlobContainerName)

	var containerClient *container.Client
	var err error
	switch authMode {
	case AzureAuthServicePrincipal:
		if spCredential == nil {
			return nil, errors.New("service bus blob sidecar: SP credential is nil (internal bug)")
		}
		opts := &container.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		containerClient, err = container.NewClient(containerURL, spCredential, opts)
		if err != nil {
			return nil, fmt.Errorf("service bus blob sidecar NewClient: %s", sanitizeAzureError(err))
		}
	case "", AzureAuthConnectionString:
		blobCred, credErr := container.NewSharedKeyCredential(cfg.BlobAccountName, cfg.BlobAccountKey)
		if credErr != nil {
			return nil, fmt.Errorf("service bus blob sidecar shared key: %s", sanitizeAzureError(credErr))
		}
		opts := &container.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		containerClient, err = container.NewClientWithSharedKeyCredential(containerURL, blobCred, opts)
		if err != nil {
			return nil, fmt.Errorf("service bus blob sidecar NewClientWithSharedKeyCredential: %s", sanitizeAzureError(err))
		}
	default:
		return nil, fmt.Errorf("service bus blob sidecar: unknown auth_mode %q", authMode)
	}

	blobOps := &containerClientAdapter{client: containerClient}

	// Auto-Create runs only in connection-string mode (shared-key has full
	// control plane). SP mode requires pre-provisioned container.
	if authMode != AzureAuthServicePrincipal {
		blobCtx, blobCancel := context.WithTimeout(ctx, 30*time.Second)
		defer blobCancel()
		if createErr := blobOps.CreateContainer(blobCtx); createErr != nil {
			if !strings.Contains(createErr.Error(), azureSBErrContainerAlreadyExists) {
				api.LogWarn("Service Bus: could not create blob container (may already exist)",
					"error_code", errcode.ServiceBusSendFailed,
					"container", cfg.BlobContainerName,
					"error", sanitizeAzureError(createErr))
			}
		}
	}

	return blobOps, nil
}

func (a *azureServiceBusProvider) Publish(ctx context.Context, data []byte) error {
	if len(data) > a.maxMsgSize {
		return fmt.Errorf("service bus message %d bytes exceeds max %d", len(data), a.maxMsgSize)
	}
	msg := &azservicebus.Message{Body: data}
	if err := a.sender.SendMessage(ctx, msg, nil); err != nil {
		// Do NOT wrap with %w: the upstream retry_dispatch path logs the
		// returned error via p.API.LogError, and %w would expose the raw SDK
		// string (possibly containing SAS fragments) in the log.
		return fmt.Errorf("failed to send service bus message: %s", sanitizeServiceBusError(err))
	}
	return nil
}

func (a *azureServiceBusProvider) Subscribe(ctx context.Context, handler func(data []byte) error) error {
	ctx, cancel := context.WithCancel(ctx)

	a.lifecycleMu.Lock()
	if a.pollDone != nil {
		a.lifecycleMu.Unlock()
		cancel()
		return errors.New("service bus provider is already subscribed")
	}
	a.cancel = cancel
	a.handler = handler
	a.pollDone = make(chan struct{})
	// Capture locals to avoid reading shared fields in the goroutine; once
	// assigned under the lock these values live for the duration of the
	// poll loop.
	done := a.pollDone
	a.lifecycleMu.Unlock()

	go func() {
		defer close(done)
		a.pollQueue(ctx, handler)
	}()
	return nil
}

func (a *azureServiceBusProvider) pollQueue(ctx context.Context, handler func(data []byte) error) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// azservicebus v1.x has no MaxWait option on ReceiveMessagesOptions;
		// ctx deadline is the only way to bound wait on an empty queue.
		recvCtx, recvCancel := context.WithTimeout(ctx, serviceBusReceiveMaxWait)
		msgs, err := a.receiver.ReceiveMessages(recvCtx, serviceBusReceiveBatchSize, nil)
		recvCancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				// Empty queue for this interval; normal case.
				continue
			}
			if ctx.Err() != nil {
				return
			}
			a.api.LogError("Service Bus receive failed",
				"error_code", errcode.ServiceBusReceiveFailed,
				"queue", a.cfg.QueueName,
				"error", sanitizeServiceBusError(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(serviceBusReceiveMaxWait):
			}
			continue
		}

		for _, msg := range msgs {
			if msg.DeliveryCount > 1 {
				a.api.LogWarn("Service Bus message redelivered",
					"error_code", errcode.ServiceBusRedelivery,
					"queue", a.cfg.QueueName,
					"delivery_count", msg.DeliveryCount,
					"message_id", msg.MessageID)
			}

			// Malformed message: nil/empty body can never be parsed by the
			// handler and will never succeed on retry. Drop it via Complete
			// (not Abandon) to prevent an infinite redelivery loop.
			if len(msg.Body) == 0 {
				a.api.LogWarn("Service Bus message has empty body; completing to drop",
					"error_code", errcode.ServiceBusMalformedBody,
					"queue", a.cfg.QueueName,
					"message_id", msg.MessageID)
				ackCtx, cancelAck := context.WithTimeout(context.Background(), serviceBusAckTimeout)
				if cerr := a.receiver.CompleteMessage(ackCtx, msg, nil); cerr != nil {
					// Settlement failed; lock will expire and the empty
					// message will redeliver. Log so the repeated
					// redelivery is not silent.
					a.api.LogWarn("Service Bus complete failed on empty-body drop; message will redeliver",
						"error_code", errcode.ServiceBusCompleteFailed,
						"queue", a.cfg.QueueName,
						"message_id", msg.MessageID,
						"error", sanitizeServiceBusError(cerr))
				}
				cancelAck()
				continue
			}

			herr := handler(msg.Body)

			// ACK operations MUST NOT use the poll ctx: Close() cancels the
			// poll ctx, and a cancelled ack would leave the message locked
			// until expiry (up to the queue's lock duration). Detached short
			// timeout ctx lets Close races vs ack finish cleanly.
			ackCtx, cancelAck := context.WithTimeout(context.Background(), serviceBusAckTimeout)
			if herr != nil {
				if aerr := a.receiver.AbandonMessage(ackCtx, msg, nil); aerr != nil {
					a.api.LogWarn("Service Bus abandon failed",
						"error_code", errcode.ServiceBusAbandonFailed,
						"queue", a.cfg.QueueName,
						"error", sanitizeServiceBusError(aerr))
				}
			} else {
				if cerr := a.receiver.CompleteMessage(ackCtx, msg, nil); cerr != nil {
					a.api.LogWarn("Service Bus complete failed; message will redeliver",
						"error_code", errcode.ServiceBusCompleteFailed,
						"queue", a.cfg.QueueName,
						"error", sanitizeServiceBusError(cerr))
				}
			}
			cancelAck()
		}
	}
}

// UploadFile uploads a file to the blob sidecar container. Mirrors
// azureProvider.UploadFile byte-for-byte; file transfer for Service Bus is a
// Blob Storage side channel, not a Service Bus feature.
func (a *azureServiceBusProvider) UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error {
	if a.containerClient == nil {
		return fmt.Errorf("blob container not configured")
	}

	meta := azureBlobMetadata{Headers: headers}
	metaJSON, _ := json.Marshal(meta)

	encoded := base64.StdEncoding.EncodeToString(metaJSON)
	blobMeta := map[string]*string{
		blobMetadataHeadersKey: &encoded,
	}

	if err := a.containerClient.UploadBlob(ctx, key, data, blobMeta); err != nil {
		// Do NOT use %w: upstream callers log this error, and the blob SDK
		// may echo signed URLs containing SAS tokens. Match the rest of
		// the provider's sanitize-at-boundary discipline.
		return fmt.Errorf("failed to upload blob %q: %s", key, sanitizeServiceBusError(err))
	}
	return nil
}

// WatchFiles polls the blob sidecar container. Mirrors azureProvider.WatchFiles.
func (a *azureServiceBusProvider) WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error {
	if a.containerClient == nil {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(a.blobPoll):
		}

		blobs, err := a.containerClient.ListBlobs(ctx, "", true)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			a.api.LogError("Service Bus blob list failed",
				"error_code", errcode.ServiceBusReceiveFailed,
				"container", a.cfg.BlobContainerName,
				"error", sanitizeServiceBusError(err))
			continue
		}

		for _, blob := range blobs {
			headers := extractBlobHeaders(blob.Metadata)

			data, derr := a.containerClient.DownloadBlob(ctx, blob.Name)
			if derr != nil {
				a.api.LogWarn("Service Bus blob: failed to download",
					"error_code", errcode.ServiceBusReceiveFailed,
					"blob", blob.Name,
					"error", sanitizeServiceBusError(derr))
				continue
			}

			if herr := handler(blob.Name, data, headers); herr != nil {
				a.api.LogWarn("Service Bus blob: handler returned error",
					"error_code", errcode.ServiceBusReceiveFailed,
					"blob", blob.Name,
					"error", herr.Error())
				continue
			}

			if delErr := a.containerClient.DeleteBlob(ctx, blob.Name); delErr != nil {
				a.api.LogWarn("Service Bus blob: failed to delete after processing",
					"error_code", errcode.ServiceBusReceiveFailed,
					"blob", blob.Name,
					"error", sanitizeServiceBusError(delErr))
				continue
			}
		}
	}
}

func (a *azureServiceBusProvider) MaxMessageSize() int {
	return a.maxMsgSize
}

// Close cancels the poll context, waits for the poll goroutine, then closes
// the SDK objects in deterministic order (receiver → sender → client) each
// with its OWN fresh short timeout so a hung earlier call cannot starve
// later ones. Nil-guards pollDone and cancel under lifecycleMu so that
// outbound-only lifecycles (never called Subscribe) close cleanly AND so
// that Close racing with a concurrent Subscribe does not data-race.
func (a *azureServiceBusProvider) Close() error {
	a.lifecycleMu.Lock()
	cancel := a.cancel
	pollDone := a.pollDone
	a.cancel = nil
	a.pollDone = nil
	a.lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if pollDone != nil {
		<-pollDone
	}

	closeWithFreshTimeout := func(c func(context.Context) error) {
		if c == nil {
			return
		}
		ctx, done := context.WithTimeout(context.Background(), serviceBusAckTimeout)
		defer done()
		_ = c(ctx)
	}
	if a.receiver != nil {
		closeWithFreshTimeout(a.receiver.Close)
	}
	if a.sender != nil {
		closeWithFreshTimeout(a.sender.Close)
	}
	if a.client != nil {
		closeWithFreshTimeout(a.client.Close)
	}
	return nil
}

// testAzureServiceBusConnection verifies a Service Bus config by peeking one
// message from the configured queue. PeekMessages is non-destructive (does
// not increment DeliveryCount), so repeated test-connection clicks have no
// side effect on queue state.
func testAzureServiceBusConnection(cfg AzureServiceBusProviderConfig) error {
	authMode := normalizeAzureAuthMode(cfg.AuthMode)
	azCloud, err := resolveAzureCloud(cfg.AzureCloud)
	if err != nil {
		return fmt.Errorf("azure-servicebus config: %w", err)
	}

	var client *azservicebus.Client
	switch authMode {
	case AzureAuthServicePrincipal:
		secret, secErr := resolveAzureSecret(azureSecretSource{
			Inline: cfg.ClientSecret, EnvVar: cfg.ClientSecretEnv, FilePath: cfg.ClientSecretFile,
		})
		if secErr != nil {
			return fmt.Errorf("azure-servicebus service principal: %w", secErr)
		}
		cred, credErr := buildClientSecretCredential(azureServicePrincipalParams{
			TenantID: cfg.TenantID, ClientID: cfg.ClientID, Secret: secret, Cloud: azCloud,
		})
		if credErr != nil {
			return credErr
		}
		namespace := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(cfg.ServiceBusNamespace), "sb://"), "/")
		client, err = azservicebus.NewClient(namespace, cred, nil)
		if err != nil {
			return fmt.Errorf("service bus NewClient: %s", sanitizeAzureError(err))
		}
	case "", AzureAuthConnectionString:
		client, err = azservicebus.NewClientFromConnectionString(cfg.ConnectionString, nil)
		if err != nil {
			return fmt.Errorf("service bus client init: %s", sanitizeAzureError(err))
		}
	default:
		return fmt.Errorf("azure-servicebus: unknown auth_mode %q", authMode)
	}

	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), serviceBusAckTimeout)
		_ = client.Close(closeCtx)
		closeCancel()
	}()

	receiver, err := client.NewReceiverForQueue(cfg.QueueName, &azservicebus.ReceiverOptions{
		ReceiveMode: azservicebus.ReceiveModePeekLock,
	})
	if err != nil {
		return fmt.Errorf("service bus receiver init: %s", sanitizeAzureError(err))
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), serviceBusAckTimeout)
		_ = receiver.Close(closeCtx)
		closeCancel()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, peekErr := receiver.PeekMessages(ctx, 1, nil); peekErr != nil {
		var sbErr *azservicebus.Error
		if errors.As(peekErr, &sbErr) {
			switch sbErr.Code {
			case azservicebus.CodeNotFound:
				if authMode == AzureAuthServicePrincipal {
					return fmt.Errorf("service bus queue %q not found: in service-principal mode, the plugin does not auto-create resources. Pre-provision via the Azure portal or ARM template, then retry", cfg.QueueName)
				}
				return fmt.Errorf("queue %q not found (pre-create it in Azure portal or ARM template)", cfg.QueueName)
			case azservicebus.CodeUnauthorizedAccess:
				if authMode == AzureAuthServicePrincipal {
					return fmt.Errorf("unauthorized: verify the Service Principal has 'Azure Service Bus Data Sender' and 'Receiver' roles on queue %q", cfg.QueueName)
				}
				return fmt.Errorf("unauthorized: check connection string SAS rule and permissions")
			}
		}
		return fmt.Errorf("service bus connection test failed: %s", sanitizeAzureError(peekErr))
	}
	return nil
}
