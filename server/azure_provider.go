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

func newAzureProvider(cfg AzureQueueProviderConfig, api plugin.API) (QueueProvider, error) {
	queueCred, err := azqueue.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure Queue shared key credential: %w", err)
	}
	queueClient, err := azqueue.NewQueueClientWithSharedKeyCredential(buildQueueURL(cfg.QueueServiceURL, cfg.QueueName), queueCred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure Queue client: %w", err)
	}

	// Ensure the queue exists (idempotent, returns success if already created).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, createErr := queueClient.Create(ctx, nil); createErr != nil {
		// Ignore "already exists" (409 Conflict); warn on other errors.
		if !strings.Contains(createErr.Error(), azureErrQueueAlreadyExists) {
			api.LogWarn("Azure Queue: could not create queue (may already exist)",
				"error_code", errcode.AzureQueueCreateQueueFailed,
				"queue", cfg.QueueName, "error", createErr.Error())
		}
	}

	var blobOps azureBlobOps
	if cfg.BlobContainerName != "" {
		blobCred, credErr := container.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
		if credErr != nil {
			return nil, fmt.Errorf("failed to create Azure Blob shared key credential: %w", credErr)
		}
		containerClient, clientErr := container.NewClientWithSharedKeyCredential(buildBlobContainerURL(cfg.BlobServiceURL, cfg.BlobContainerName), blobCred, nil)
		if clientErr != nil {
			return nil, fmt.Errorf("failed to create Azure Blob container client: %w", clientErr)
		}
		blobOps = &containerClientAdapter{client: containerClient}

		// Ensure the blob container exists (fresh timeout, independent of queue creation).
		blobCtx, blobCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer blobCancel()
		if createErr := blobOps.CreateContainer(blobCtx); createErr != nil {
			if !strings.Contains(createErr.Error(), azureErrContainerAlreadyExists) {
				api.LogWarn("Azure Blob: could not create container (may already exist)",
					"error_code", errcode.AzureQueueCreateContainerFail,
					"container", cfg.BlobContainerName, "error", createErr.Error())
			}
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
				"queue", a.cfg.QueueName, "error", err.Error())
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
					"queue", a.cfg.QueueName, "error", err.Error())
				// Delete malformed message to avoid reprocessing.
				_, _ = a.queueClient.DeleteMessage(ctx, *msg.MessageID, *msg.PopReceipt, nil)
				continue
			}

			if err := a.handler(data); err != nil {
				a.api.LogWarn("Azure Queue: handler returned error, message will retry",
					"error_code", errcode.AzureQueueHandlerRetry,
					"queue", a.cfg.QueueName, "error", err.Error())
				continue
			}

			// Handler succeeded, delete the message.
			if _, err := a.queueClient.DeleteMessage(ctx, *msg.MessageID, *msg.PopReceipt, nil); err != nil {
				a.api.LogWarn("Azure Queue: failed to delete processed message",
					"error_code", errcode.AzureQueueDeleteProcessedFail,
					"queue", a.cfg.QueueName, "error", err.Error())
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
				"container", a.cfg.BlobContainerName, "error", err.Error())
			continue
		}

		for _, blob := range blobs {
			headers := extractBlobHeaders(blob.Metadata)

			data, err := a.containerClient.DownloadBlob(ctx, blob.Name)
			if err != nil {
				a.api.LogWarn("Azure Blob: failed to download",
					"error_code", errcode.AzureQueueBlobDownloadFailed,
					"blob", blob.Name, "error", err.Error())
				continue
			}

			if err := handler(blob.Name, data, headers); err != nil {
				a.api.LogWarn("Azure Blob: handler returned error",
					"error_code", errcode.AzureQueueBlobHandlerError,
					"blob", blob.Name, "error", err.Error())
				continue
			}

			if err := a.containerClient.DeleteBlob(ctx, blob.Name); err != nil {
				a.api.LogWarn("Azure Blob: failed to delete after processing",
					"error_code", errcode.AzureQueueBlobDeleteFailed,
					"blob", blob.Name, "error", err.Error())
				continue
			}
		}
	}
}

func (a *azureProvider) MaxMessageSize() int {
	return azureMaxMessageSize
}

func (a *azureProvider) IsConnected() bool {
	return true
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

// testAzureQueueConnection tests connectivity to Azure Queue Storage by sending and receiving a test message.
func testAzureQueueConnection(cfg AzureQueueProviderConfig) error {
	cred, err := azqueue.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
	if err != nil {
		return fmt.Errorf("failed to create shared key credential: %w", err)
	}
	queueClient, err := azqueue.NewQueueClientWithSharedKeyCredential(buildQueueURL(cfg.QueueServiceURL, cfg.QueueName), cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create queue client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Ensure the queue exists before testing.
	if _, createErr := queueClient.Create(ctx, nil); createErr != nil {
		if !strings.Contains(createErr.Error(), azureErrQueueAlreadyExists) {
			return fmt.Errorf("failed to create queue: %w", createErr)
		}
	}

	testMsg := base64.StdEncoding.EncodeToString([]byte("crossguard-test-" + time.Now().Format(time.RFC3339)))
	enqResp, err := queueClient.EnqueueMessage(ctx, testMsg, nil)
	if err != nil {
		return fmt.Errorf("failed to enqueue test message: %w", err)
	}

	if len(enqResp.Messages) > 0 && enqResp.Messages[0].MessageID != nil && enqResp.Messages[0].PopReceipt != nil {
		_, _ = queueClient.DeleteMessage(ctx, *enqResp.Messages[0].MessageID, *enqResp.Messages[0].PopReceipt, nil)
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
