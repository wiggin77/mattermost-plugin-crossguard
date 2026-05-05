package main

import "context"

// QueueProvider abstracts the message transport and file transfer layer.
// Implementations: NATS, Azure Queue, Azure Blob, Azure Service Bus.
type QueueProvider interface {
	// Publish sends a message. Includes internal retries appropriate to transport.
	// A returned error is a final failure.
	Publish(ctx context.Context, data []byte) error

	// Subscribe starts delivering messages to the handler. The handler's
	// return value is the universal delivery acknowledgement signal:
	//   nil   = processed (provider may ack/delete the message)
	//   error = not processed (provider should redeliver if supported)
	// Core NATS ignores the return (fire-and-forget). Azure Queue and
	// Azure Service Bus already respect it. Future NATS JetStream will
	// depend on it. Handlers must be synchronous: returning before
	// processing completes would ack a message the plugin never handled.
	Subscribe(ctx context.Context, handler func(data []byte) error) error

	// UploadFile uploads a file with metadata.
	UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error

	// WatchFiles watches for new files and calls handler.
	// Handler returning nil = file processed (provider may clean up).
	// Handler returning error = file not processed (retry later).
	WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error

	// MaxMessageSize returns the provider's message size limit in bytes.
	// Returns 0 for no limit. Caller checks before Publish.
	MaxMessageSize() int

	// Close gracefully shuts down. In-flight operations complete or are abandoned.
	Close() error
}
