package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

const (
	// azureMaxMessageSize is the safe message size limit before Base64 encoding to 64KB.
	azureMaxMessageSize = 48000

	azureVisibilityTimeout = 5 * time.Minute
	azureDequeueBatchSize  = int32(32)

	azureErrQueueAlreadyExists     = "QueueAlreadyExists"
	azureErrContainerAlreadyExists = "ContainerAlreadyExists"
	blobMetadataHeadersKey         = "crossguard_headers"
)

// azureQueuePollInterval is the default queue poll interval for azureProvider.
// Declared as var so tests can shrink it; per-connection overrides flow
// through AzureQueueProviderConfig.PollIntervalSeconds.
var azureQueuePollInterval = 5 * time.Second

// azureBlobPollInterval is the default blob poll interval for azureProvider.
// Declared as var so tests can shrink it; per-connection overrides flow
// through AzureQueueProviderConfig.BlobPollIntervalSeconds.
var azureBlobPollInterval = 15 * time.Second

// azureQueuer abstracts Azure Queue operations for testability.
type azureQueuer interface {
	EnqueueMessage(ctx context.Context, content string, o *azqueue.EnqueueMessageOptions) (azqueue.EnqueueMessagesResponse, error)
	DequeueMessages(ctx context.Context, o *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error)
	DeleteMessage(ctx context.Context, messageID string, popReceipt string, o *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error)
}

// azureProvider implements QueueProvider using Azure Queue Storage and Azure Blob Storage.
type azureProvider struct {
	queueClient     azureQueuer
	containerClient azureBlobOps
	api             plugin.API
	cfg             AzureQueueProviderConfig
	queuePoll       time.Duration
	blobPoll        time.Duration
	cancel          context.CancelFunc
	handler         func(data []byte) error
	pollDone        chan struct{}
}

// buildQueueURL joins a queue service URL and queue name into a full queue URL.
func buildQueueURL(serviceURL, queueName string) string {
	return strings.TrimRight(serviceURL, "/") + "/" + queueName
}

// buildBlobContainerURL joins a blob service URL and container name into a full container URL.
func buildBlobContainerURL(serviceURL, containerName string) string {
	return strings.TrimRight(serviceURL, "/") + "/" + containerName
}

// newAzureProvider constructs the azure-queue provider. The ctx argument is
// the plugin-lifetime context; it threads through to the azidentity token
// credential's internal GetToken calls so AAD requests are cancelled on
// plugin reload. In shared-key mode ctx is used only for the initial
// auto-Create probe.
func newAzureProvider(ctx context.Context, cfg AzureQueueProviderConfig, api plugin.API) (QueueProvider, error) {
	authMode := normalizeAzureAuthMode(cfg.AuthMode)
	azCloud, err := resolveAzureCloud(cfg.AzureCloud)
	if err != nil {
		return nil, fmt.Errorf("azure-queue config: %w", err)
	}

	queueURL := buildQueueURL(cfg.QueueServiceURL, cfg.QueueName)

	var queueClient *azqueue.QueueClient
	switch authMode {
	case AzureAuthServicePrincipal:
		cred, credErr := newAzureProviderSPCredential(cfg, azCloud, api)
		if credErr != nil {
			return nil, credErr
		}
		opts := &azqueue.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		queueClient, err = azqueue.NewQueueClient(queueURL, cred, opts)
		if err != nil {
			return nil, fmt.Errorf("azure-queue NewQueueClient: %s", sanitizeAzureError(err))
		}
	case "", AzureAuthSharedKey:
		queueCred, credErr := azqueue.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
		if credErr != nil {
			return nil, fmt.Errorf("azure-queue shared key credential: %s", sanitizeAzureError(credErr))
		}
		opts := &azqueue.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		queueClient, err = azqueue.NewQueueClientWithSharedKeyCredential(queueURL, queueCred, opts)
		if err != nil {
			return nil, fmt.Errorf("azure-queue NewQueueClientWithSharedKeyCredential: %s", sanitizeAzureError(err))
		}
	default:
		// Defense-in-depth: validateAzureQueueConnection already rejects
		// unknown auth_mode at config-save time. If we get here it means
		// the validator was bypassed; fail closed.
		return nil, fmt.Errorf("azure-queue: unknown auth_mode %q (expected %q or %q)", cfg.AuthMode, AzureAuthSharedKey, AzureAuthServicePrincipal)
	}

	// Auto-Create runs only in shared-key mode. In SP mode the operator is
	// expected to pre-provision the queue (least-privilege RBAC).
	if authMode != AzureAuthServicePrincipal {
		createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if _, createErr := queueClient.Create(createCtx, nil); createErr != nil {
			if !strings.Contains(createErr.Error(), azureErrQueueAlreadyExists) {
				api.LogWarn("Azure Queue: could not create queue (may already exist)",
					"error_code", errcode.AzureQueueCreateQueueFailed,
					"queue", cfg.QueueName, "error", sanitizeAzureError(createErr))
			}
		}
		cancel()
	}

	var blobOps azureBlobOps
	if cfg.BlobContainerName != "" {
		blobOps, err = newAzureProviderBlobSidecar(ctx, cfg, authMode, azCloud, api)
		if err != nil {
			return nil, err
		}
	}

	queuePoll := azureQueuePollInterval
	if cfg.PollIntervalSeconds > 0 {
		queuePoll = time.Duration(cfg.PollIntervalSeconds) * time.Second
	}
	blobPoll := azureBlobPollInterval
	if cfg.BlobPollIntervalSeconds > 0 {
		blobPoll = time.Duration(cfg.BlobPollIntervalSeconds) * time.Second
	}

	return &azureProvider{
		queueClient:     queueClient,
		containerClient: blobOps,
		api:             api,
		cfg:             cfg,
		queuePoll:       queuePoll,
		blobPoll:        blobPoll,
	}, nil
}

// newAzureProviderSPCredential constructs an azidentity ClientSecretCredential
// from the inline cfg.ClientSecret. Emits an audit log line on success so
// operators have a record of which AAD identity is authenticating each
// connection.
func newAzureProviderSPCredential(cfg AzureQueueProviderConfig, azCloud azcoreCloudConfig, api plugin.API) (azcore.TokenCredential, error) {
	cred, err := buildClientSecretCredential(azureServicePrincipalParams{
		TenantID: cfg.TenantID, ClientID: cfg.ClientID, Secret: cfg.ClientSecret, Cloud: azCloud,
	})
	if err != nil {
		api.LogError("Azure Queue: service principal credential failed",
			"error_code", errcode.AzureQueueSPCredentialFailed,
			"tenant_id", cfg.TenantID, "client_id", cfg.ClientID, "error", sanitizeAzureError(err))
		return nil, err
	}
	logAzureAuthAudit(api, "azure-queue", cfg.TenantID, cfg.ClientID, cfg.AzureCloud, cfg.ClientSecret)
	return cred, nil
}

// newAzureProviderBlobSidecar builds the optional blob container client for
// the azure-queue provider's file-transfer path. Uses the same auth_mode as
// the parent queue (the queue and blob sidecar share an account in this
// provider, so they share the credential too). Skips auto-Create in SP mode.
func newAzureProviderBlobSidecar(ctx context.Context, cfg AzureQueueProviderConfig, authMode string, azCloud azcoreCloudConfig, api plugin.API) (azureBlobOps, error) {
	containerURL := buildBlobContainerURL(cfg.BlobServiceURL, cfg.BlobContainerName)

	var containerClient *container.Client
	switch authMode {
	case AzureAuthServicePrincipal:
		cred, err := buildClientSecretCredential(azureServicePrincipalParams{
			TenantID: cfg.TenantID, ClientID: cfg.ClientID, Secret: cfg.ClientSecret, Cloud: azCloud,
		})
		if err != nil {
			return nil, fmt.Errorf("azure-queue blob sidecar SP credential: %w", err)
		}
		opts := &container.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		var clientErr error
		containerClient, clientErr = container.NewClient(containerURL, cred, opts)
		if clientErr != nil {
			return nil, fmt.Errorf("azure-queue blob sidecar NewClient: %s", sanitizeAzureError(clientErr))
		}
	case "", AzureAuthSharedKey:
		blobCred, credErr := container.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
		if credErr != nil {
			return nil, fmt.Errorf("azure-queue blob sidecar shared key: %s", sanitizeAzureError(credErr))
		}
		opts := &container.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		var clientErr error
		containerClient, clientErr = container.NewClientWithSharedKeyCredential(containerURL, blobCred, opts)
		if clientErr != nil {
			return nil, fmt.Errorf("azure-queue blob sidecar NewClientWithSharedKeyCredential: %s", sanitizeAzureError(clientErr))
		}
	default:
		return nil, fmt.Errorf("azure-queue blob sidecar: unknown auth_mode %q", authMode)
	}

	blobOps := &containerClientAdapter{client: containerClient}

	if authMode != AzureAuthServicePrincipal {
		blobCtx, blobCancel := context.WithTimeout(ctx, 30*time.Second)
		defer blobCancel()
		if createErr := blobOps.CreateContainer(blobCtx); createErr != nil {
			if !strings.Contains(createErr.Error(), azureErrContainerAlreadyExists) {
				api.LogWarn("Azure Blob: could not create container (may already exist)",
					"error_code", errcode.AzureQueueCreateContainerFail,
					"container", cfg.BlobContainerName, "error", sanitizeAzureError(createErr))
			}
		}
	}

	return blobOps, nil
}

func (a *azureProvider) Publish(ctx context.Context, data []byte) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	_, err := a.queueClient.EnqueueMessage(ctx, encoded, nil)
	if err != nil {
		return fmt.Errorf("failed to enqueue message: %w", err)
	}
	return nil
}

func (a *azureProvider) Subscribe(ctx context.Context, handler func(data []byte) error) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.handler = handler
	a.pollDone = make(chan struct{})

	go func() {
		defer close(a.pollDone)
		a.pollQueue(ctx)
	}()
	return nil
}

func (a *azureProvider) pollQueue(ctx context.Context) {
	visTimeout := int32(azureVisibilityTimeout.Seconds())
	batchSize := azureDequeueBatchSize
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		resp, err := a.queueClient.DequeueMessages(ctx, &azqueue.DequeueMessagesOptions{
			NumberOfMessages:  &batchSize,
			VisibilityTimeout: &visTimeout,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.api.LogError("Azure Queue dequeue failed",
				"error_code", errcode.AzureQueueDequeueFailed,
				"queue", a.cfg.QueueName, "error", sanitizeAzureError(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(a.queuePoll):
			}
			continue
		}

		if len(resp.Messages) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(a.queuePoll):
			}
			continue
		}

		for _, msg := range resp.Messages {
			if msg.MessageText == nil || msg.MessageID == nil || msg.PopReceipt == nil {
				continue
			}

			data, err := base64.StdEncoding.DecodeString(*msg.MessageText)
			if err != nil {
				a.api.LogError("Azure Queue: failed to decode message",
					"error_code", errcode.AzureQueueDecodeFailed,
					"queue", a.cfg.QueueName, "error", sanitizeAzureError(err))
				// Delete malformed message to avoid reprocessing.
				_, _ = a.queueClient.DeleteMessage(ctx, *msg.MessageID, *msg.PopReceipt, nil)
				continue
			}

			if err := a.handler(data); err != nil {
				a.api.LogWarn("Azure Queue: handler returned error, message will retry",
					"error_code", errcode.AzureQueueHandlerRetry,
					"queue", a.cfg.QueueName, "error", sanitizeAzureError(err))
				continue
			}

			// Handler succeeded, delete the message.
			if _, err := a.queueClient.DeleteMessage(ctx, *msg.MessageID, *msg.PopReceipt, nil); err != nil {
				a.api.LogWarn("Azure Queue: failed to delete processed message",
					"error_code", errcode.AzureQueueDeleteProcessedFail,
					"queue", a.cfg.QueueName, "error", sanitizeAzureError(err))
			}
		}
	}
}

// azureBlobMetadata is stored as JSON in blob metadata for header transport.
type azureBlobMetadata struct {
	Headers map[string]string `json:"headers"`
}

func (a *azureProvider) UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error {
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
		return fmt.Errorf("failed to upload blob %q: %w", key, err)
	}
	return nil
}

func (a *azureProvider) WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error {
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
			a.api.LogError("Azure Blob list failed",
				"error_code", errcode.AzureQueueBlobListFailed,
				"container", a.cfg.BlobContainerName, "error", sanitizeAzureError(err))
			continue
		}

		for _, blob := range blobs {
			headers := extractBlobHeaders(blob.Metadata)

			data, err := a.containerClient.DownloadBlob(ctx, blob.Name)
			if err != nil {
				a.api.LogWarn("Azure Blob: failed to download",
					"error_code", errcode.AzureQueueBlobDownloadFailed,
					"blob", blob.Name, "error", sanitizeAzureError(err))
				continue
			}

			if err := handler(blob.Name, data, headers); err != nil {
				a.api.LogWarn("Azure Blob: handler returned error",
					"error_code", errcode.AzureQueueBlobHandlerError,
					"blob", blob.Name, "error", sanitizeAzureError(err))
				continue
			}

			if err := a.containerClient.DeleteBlob(ctx, blob.Name); err != nil {
				a.api.LogWarn("Azure Blob: failed to delete after processing",
					"error_code", errcode.AzureQueueBlobDeleteFailed,
					"blob", blob.Name, "error", sanitizeAzureError(err))
				continue
			}
		}
	}
}

func (a *azureProvider) MaxMessageSize() int {
	return azureMaxMessageSize
}

func (a *azureProvider) Close() error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.pollDone != nil {
		<-a.pollDone
	}
	return nil
}

// testAzureQueueConnection probes connectivity to Azure Queue Storage using
// the configured auth_mode. The probe is non-destructive (GetProperties)
// so the operator's Service Principal can pass the test with data-plane-
// only RBAC (Storage Queue Data Contributor); the queue must be
// pre-provisioned in SP mode. In shared-key mode the legacy behavior of
// using GetProperties is preserved (also non-destructive; this drops the
// previous Create+Enqueue+Delete cycle, which required management-plane
// access that the legacy path happened to have).
func testAzureQueueConnection(cfg AzureQueueProviderConfig) error {
	authMode := normalizeAzureAuthMode(cfg.AuthMode)
	azCloud, err := resolveAzureCloud(cfg.AzureCloud)
	if err != nil {
		return fmt.Errorf("azure-queue config: %w", err)
	}

	queueURL := buildQueueURL(cfg.QueueServiceURL, cfg.QueueName)

	var queueClient *azqueue.QueueClient
	switch authMode {
	case AzureAuthServicePrincipal:
		cred, credErr := buildClientSecretCredential(azureServicePrincipalParams{
			TenantID: cfg.TenantID, ClientID: cfg.ClientID, Secret: cfg.ClientSecret, Cloud: azCloud,
		})
		if credErr != nil {
			return credErr
		}
		opts := &azqueue.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		queueClient, err = azqueue.NewQueueClient(queueURL, cred, opts)
		if err != nil {
			return fmt.Errorf("azure-queue NewQueueClient: %s", sanitizeAzureError(err))
		}
	case "", AzureAuthSharedKey:
		cred, credErr := azqueue.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
		if credErr != nil {
			return fmt.Errorf("azure-queue shared key credential: %s", sanitizeAzureError(credErr))
		}
		opts := &azqueue.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: azCloud}}
		queueClient, err = azqueue.NewQueueClientWithSharedKeyCredential(queueURL, cred, opts)
		if err != nil {
			return fmt.Errorf("azure-queue NewQueueClientWithSharedKeyCredential: %s", sanitizeAzureError(err))
		}
	default:
		return fmt.Errorf("azure-queue: unknown auth_mode %q", authMode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err = queueClient.GetProperties(ctx, nil)
	if err != nil {
		return wrapAzureResourceError(err, "queue", cfg.QueueName, authMode)
	}
	return nil
}

func extractBlobHeaders(metadata map[string]*string) map[string]string {
	headers := make(map[string]string)
	if metadata == nil {
		return headers
	}

	raw, ok := metadata[blobMetadataHeadersKey]
	if !ok || raw == nil {
		return headers
	}

	decoded, err := base64.StdEncoding.DecodeString(*raw)
	if err != nil {
		return headers
	}

	var meta azureBlobMetadata
	if err := json.Unmarshal(decoded, &meta); err != nil {
		return headers
	}

	return meta.Headers
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type nopCloser struct {
	*bytes.Reader
}

func (nopCloser) Close() error { return nil }

func newNopCloser(data []byte) io.ReadSeekCloser {
	return nopCloser{bytes.NewReader(data)}
}
