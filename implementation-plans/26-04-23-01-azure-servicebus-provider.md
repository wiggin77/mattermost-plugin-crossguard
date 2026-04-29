# Azure Service Bus Transport Provider

## Overview

Add a third Azure transport provider, **Azure Service Bus**, that sits alongside the existing `azure-queue` (Azure Queue Storage) and `azure-blob` (batched WAL) providers. The new provider will follow the `azure-queue` model: a simple publish/subscribe relay using Service Bus queues for messages, with Azure Blob Storage reused for file transfer. Operators get Service Bus for deployments that already standardize on it (FIFO sessions available on Premium, larger message payloads, enterprise auth stories) without taking on the batched-WAL complexity.

## Problem Statement

Customers running cross-domain Mattermost deployments on Azure have standardized on Service Bus rather than Queue Storage for two recurring reasons:

1. **Message size**: Queue Storage caps messages at 64 KB (~48 KB after Base64). Service Bus Standard is 256 KB and Premium is 100 MB, which lets long Mattermost posts relay without truncation.
2. **Enterprise features**: Service Bus is the first-party Azure messaging product with dead-letter queues, duplicate detection, and (Premium) sessions / ordering guarantees. Operators who already run it want to reuse it for Cross Guard rather than stand up a second queue product.

Today the plugin only supports NATS, Azure Queue Storage, and an Azure Blob batched WAL. There is no Service Bus option.

## Current State

**Provider interface** is stable and well-defined in `server/provider.go:1-32`:

```go
type QueueProvider interface {
    Publish(ctx context.Context, data []byte) error
    Subscribe(ctx context.Context, handler func(data []byte) error) error
    UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error
    WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error
    MaxMessageSize() int
    Close() error
}
```

**Three implementations** conform today:

| File | Provider constant | Pattern |
|------|-------------------|---------|
| `server/nats_provider.go` | `nats` | Push-based, JetStream Object Store for files |
| `server/azure_provider.go` | `azure-queue` | Poll queue + reuse Azure Blob for files |
| `server/azure_blob_provider.go` | `azure-blob` | WAL batching + distributed KV locks |

**Provider wiring** is already polymorphic:

- `ConnectionConfig` in `server/configuration.go:44-60` carries optional sub-structs (`NATS`, `AzureQueue`, `AzureBlob`)
- `parseConnections` + `validateConnectionList` dispatch per provider (`server/configuration.go:169-256`)
- `Plugin.createProvider` in `server/connections.go:370-399` is the single construction site
- `RedactedConnection` in `server/service.go:43-58` exposes safe display fields for admin UI
- Slash command summary via `providerDetails` in `server/command.go:43-68`
- Test-connection API per provider in `server/api.go:78-87, 236-320`
- Webapp `ConnectionSettings.tsx` has provider dropdown and conditional fields

**Current gaps:**
- No Service Bus SDK dependency in `go.mod` (only `azqueue` 1.0.1 and `azblob` 1.6.4)
- No Service Bus emulator in `docker-compose.dev.yml` (only Azurite)
- No plan for authentication beyond shared-key (Service Bus canonically uses connection strings with SAS)

## Design Principles

| Pattern | Our Approach | Avoid | Reference |
|---------|-------------|-------|-----------|
| Provider style | Follow `azureProvider` (simple poll + ack/delete) | Replicating `azureBlobProvider` WAL complexity | `server/azure_provider.go` |
| File transfer | Reuse Azure Blob Storage (same adapter as `azure-queue`) | Inventing a Service Bus file channel | Service Bus is message-only; blob container is already the standard file side-channel |
| Receive semantics | PeekLock + `CompleteMessage` on success, `AbandonMessage` on handler error | Receive-and-delete mode | We need ack semantics for at-least-once; handler errors must retry |
| Auth | Connection string (SAS) only in Phase 1 | Managed Identity / Azure AD in Phase 1 | Matches how `azure-queue` shipped (shared key first, deferred MI) |
| Lock renewal | Rely on the queue's configured `LockDuration` (an entity-level Azure property, max 5 min); plugin does not set or renew it | Auto-renewal goroutines; a plugin-side lock-duration knob | Lock duration is configured on the queue in Azure (ARM/portal/Terraform), not by the client. Cross Guard handlers complete in milliseconds, so even a minimal entity-configured 30 s lock is fine. |
| Batch receive | `ReceiveMessages(BatchSize=32)` wrapped in a per-iteration `context.WithTimeout(ctx, 5s)` | Per-message blocking receive, or trying to pass `MaxWait` on the options struct (it does not exist in v1.x; ctx is the sole timeout mechanism) | Matches `azureDequeueBatchSize = 32` in Queue provider, reduces round-trips |
| Max message size | 192 KB default (conservative below Standard 256 KB) | 256 KB hard limit | Envelope overhead + Base64 of attached file pointers leaves headroom; override via config |
| Close semantics | Cancel ctx, wait for poll goroutine, close Sender/Receiver/Client | Leaking Service Bus links | Matches existing `Close()` pattern across providers |
| Testing | Mock `serviceBusSender` / `serviceBusReceiver` interfaces, identical to `azureQueuer` mocking | Hitting live Service Bus in unit tests | `server/azure_provider_test.go` is the template |
| Integration | `mcr.microsoft.com/azure-messaging/servicebus-emulator` in docker-compose | Forcing devs to provision Azure | Microsoft-supplied emulator exists and runs locally; Azurite does NOT support Service Bus |

## Reference Patterns

Similar features to follow:

- `server/azure_provider.go:72-135` — Constructor pattern: build credential, create client, idempotent queue create, optional blob container init
- `server/azure_provider.go:137-144` — Simple `Publish` (no retries; caller retries via `retry_dispatch.go`)
- `server/azure_provider.go:159-227` — `pollQueue` loop with select/ctx.Done, batch dequeue, per-message ack on handler success, do-not-delete on handler error
- `server/azure_provider.go:234-302` — `UploadFile` + `WatchFiles` reusing `azureBlobOps`
- `server/azure_provider.go:318-350` — `testAzureQueueConnection` seam
- `server/configuration.go:78-88` — `AzureQueueProviderConfig` struct layout and JSON tags
- `server/configuration.go:230-258` — `validateAzureQueueConnection` pattern
- `server/connections.go:370-399` — `createProvider` switch for provider selection
- `server/azure_provider_test.go:186-212` — `mockAzureQueue` struct: the mocking pattern to reuse

## Requirements

- [ ] New provider constant `azure-servicebus` and config struct `AzureServiceBusProviderConfig`
- [ ] `server/azure_servicebus_provider.go` implementing `QueueProvider` using `azservicebus` SDK
- [ ] Shared-key (connection string) auth via `NewClientFromConnectionString`
- [ ] PeekLock receive with `CompleteMessage` on handler success, `AbandonMessage` on handler error
- [ ] Blob Storage file transfer reusing `azureBlobOps` (same as Azure Queue provider)
- [ ] Provider-aware validation in `configuration.go`
- [ ] `createProvider` switch case in `connections.go`
- [ ] Test-connection API handler in `api.go`
- [ ] `RedactedConnection` / `providerDetails` / `redactConnection` updates for admin display
- [ ] Admin UI (`ConnectionSettings.tsx`) gets a third option `Azure Service Bus` with conditional fields
- [ ] `plugin.json` changes if any (connection schema lives in webapp, so likely no change)
- [ ] OpenAPI schema additions in `schema/crossguard-api.yaml`
- [ ] Help docs update in `public/help/admin.html`
- [ ] Docker-compose: add `servicebus-emulator` service + required SQL Server 2022 Linux dependency
- [ ] Smoke test target `make docker-servicebus-smoke-test` mirroring `docker-azure-smoke-test`
- [ ] Unit tests in `server/azure_servicebus_provider_test.go` using mocked Sender/Receiver
- [ ] Error code block 26000-26999 allocated in `server/errcode/codes.go`

## Out of Scope

- Service Bus **topics** and **subscriptions** (pub/sub fan-out). Phase 1 uses queues only; topics can be a follow-up with minimal surface changes (`queue_name` becomes optional if `topic_name` + `subscription_name` are set).
- Service Bus **sessions** (FIFO ordering guarantee). Cross Guard already tolerates out-of-order delivery via post-mapping idempotency; sessions add complexity without clear operator demand yet.
- **Managed Identity / Azure AD auth**. Connection string only in Phase 1. Defer until we also add MI to `azure-queue` and `azure-blob` in a coordinated auth refresh.
- **Dead-letter queue processing**. The plugin will let Service Bus DLQ messages after MaxDeliveryCount naturally; surfacing DLQ contents to admins is future work.
- **Duplicate detection** configuration. We rely on our own post-mapping idempotency; enabling Service Bus duplicate detection is an operator choice, not a plugin requirement.
- **Premium-tier-specific features** (100 MB messages, partitioned queues, geo-DR). `MaxMessageSize` will be tunable; everything else works unchanged on Standard and Premium.
- **Auto-creating the queue.** `azure-queue` auto-creates the queue with a data-plane call. Service Bus queue creation requires the management SDK (`armservicebus`) and RBAC the data-plane connection string often lacks. Phase 1 requires operators to pre-create the queue; startup returns a clear error if it is missing.

## Technical Approach

### 1. SDK dependency (`go.mod`)

Add `github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus`. Single public package; pulls in `azcore` (already indirect) and adds `go-amqp` transitively. No CGO, pure-Go AMQP client. Version pin matches patterns of existing Azure SDK deps (current stable ~1.8.x).

### 2. Configuration struct (`server/configuration.go`)

Add a third provider constant and a new config struct:

```go
const (
    ProviderNATS            = "nats"
    ProviderAzureQueue      = "azure-queue"
    ProviderAzureBlob       = "azure-blob"
    ProviderAzureServiceBus = "azure-servicebus"
)

type AzureServiceBusProviderConfig struct {
    ConnectionString    string `json:"connection_string"`               // SAS / shared-key
    QueueName           string `json:"queue_name"`

    // Optional Blob sidecar for file transfer (enabled only when
    // FileTransferEnabled is true on the parent ConnectionConfig).
    BlobServiceURL      string `json:"blob_service_url,omitempty"`
    BlobAccountName     string `json:"blob_account_name,omitempty"`
    BlobAccountKey      string `json:"blob_account_key,omitempty"`
    BlobContainerName   string `json:"blob_container_name,omitempty"`

    // Tunables (optional). The only client-controllable knobs we expose are
    // MaxMessageSizeBytes (Standard vs Premium payload ceiling) and
    // BlobPollIntervalSeconds (blob sidecar). Package-level consts (batch
    // size 32, receive max-wait 5 s) match the azure-queue provider's
    // constants. NOTE: lock duration is an Azure entity property set on
    // the queue definition, NOT a client knob — the plugin cannot set it,
    // so there is no corresponding field.
    MaxMessageSizeBytes     int `json:"max_message_size_bytes,omitempty"`     // default 192000; Premium operators may raise up to 100 MB
    BlobPollIntervalSeconds int `json:"blob_poll_interval_seconds,omitempty"` // default 15 (mirrors azure-queue)
}
```

Extend `ConnectionConfig` with `AzureServiceBus *AzureServiceBusProviderConfig \`json:"azure_servicebus,omitempty"\``.

Introduce `errMissingAzureServiceBusConfig` and update `errUnknownProvider` to list the new provider in its message.

### 3. Validation (`server/configuration.go`)

Add `validateAzureServiceBusConnection(conn, prefix)` mirroring `validateAzureQueueConnection`:

- `connection_string` required (non-empty). We do NOT pre-parse: the `azservicebus` SDK does not expose a public connection-string parser; instead we let `NewClientFromConnectionString` (called at provider construction and at test-connection time) return its own error for malformed strings.
- `queue_name` required. Service Bus queue paths allow up to 260 characters and the allowed set is letters, digits, periods, hyphens, underscores, and forward slashes (for hierarchical paths). Regex validate against that set; do not impose a tighter length cap than Service Bus itself.
- When `FileTransferEnabled`: require `blob_service_url`, `blob_account_name`, `blob_account_key`, `blob_container_name`. Validate URL.
- Reuse `validatePollInterval` for `BlobPollIntervalSeconds`.
- Validate `MaxMessageSizeBytes` is `0` (default) or in `[1024, 100*1024*1024]`.

Add a dispatch arm in `validateConnectionList` and update the default-case error message.

### 4. Provider implementation (`server/azure_servicebus_provider.go`)

Package-level constants (mirrors the pattern at `server/azure_provider.go:20-30`):

```go
const (
    serviceBusDefaultMaxMessageSize = 192 * 1024      // 192 KB (below Standard 256 KB ceiling)
    serviceBusReceiveBatchSize      = 32              // matches azureDequeueBatchSize
    serviceBusReceiveMaxWait        = 5 * time.Second // per-call ctx deadline for ReceiveMessages (no MaxWait option in SDK)
    serviceBusAckTimeout            = 10 * time.Second // detached timeout for Complete/Abandon
)

// Note: message lock duration is an ENTITY property (set on the queue in Azure
// via ARM/portal/Terraform), not a client knob. The plugin does not and cannot
// set it; operators configure it on the queue definition.

// serviceBusBlobPollInterval is the default blob poll interval (matches azure-queue's 15s).
// Declared var so tests can shrink it; per-connection override flows through
// AzureServiceBusProviderConfig.BlobPollIntervalSeconds.
var serviceBusBlobPollInterval = 15 * time.Second
```

```go
type serviceBusSender interface {
    SendMessage(ctx context.Context, msg *azservicebus.Message, opts *azservicebus.SendMessageOptions) error
    Close(ctx context.Context) error
}

type serviceBusReceiver interface {
    ReceiveMessages(ctx context.Context, maxMessages int, opts *azservicebus.ReceiveMessagesOptions) ([]*azservicebus.ReceivedMessage, error)
    CompleteMessage(ctx context.Context, msg *azservicebus.ReceivedMessage, opts *azservicebus.CompleteMessageOptions) error
    AbandonMessage(ctx context.Context, msg *azservicebus.ReceivedMessage, opts *azservicebus.AbandonMessageOptions) error
    Close(ctx context.Context) error
}

type azureServiceBusProvider struct {
    client          *azservicebus.Client
    sender          serviceBusSender
    receiver        serviceBusReceiver
    containerClient azureBlobOps
    api             plugin.API
    cfg             AzureServiceBusProviderConfig

    maxMsgSize      int
    blobPoll        time.Duration

    cancel          context.CancelFunc
    handler         func(data []byte) error
    pollDone        chan struct{}
}
```

**Constructor** `newAzureServiceBusProvider(cfg, api)`:

Every SDK call in the constructor returns `(T, error)` and can leak SAS fragments via the error string. Always check and sanitize:

```go
client, err := azservicebus.NewClientFromConnectionString(cfg.ConnectionString, nil)
if err != nil {
    return nil, fmt.Errorf("service bus client init: %s", sanitizeServiceBusError(err))
}

sender, err := client.NewSender(cfg.QueueName, nil)
if err != nil {
    _ = client.Close(context.Background())
    return nil, fmt.Errorf("service bus sender init: %s", sanitizeServiceBusError(err))
}

receiver, err := client.NewReceiverForQueue(cfg.QueueName, &azservicebus.ReceiverOptions{
    ReceiveMode: azservicebus.ReceiveModePeekLock,
})
if err != nil {
    _ = sender.Close(context.Background())
    _ = client.Close(context.Background())
    return nil, fmt.Errorf("service bus receiver init: %s", sanitizeServiceBusError(err))
}

// Optional: blob sidecar for file transfer. Build container.Client exactly
// as azureProvider does (shared-key credential, adapter), then idempotently
// CreateContainer. Wrap CreateContainer's error through sanitizeServiceBusError
// too (the blob SDK errors can likewise embed account-key URLs).
```

After the SDK handles are created, derive tunables from config with defaults and clamp to validation bounds a second time defensively.

Do **not** pre-flight the queue on construction — we cannot create it from the data plane without elevated perms. Instead, the first `ReceiveMessages` or `SendMessage` call surfaces "queue not found" immediately and we return a clear log. For the test-connection handler (below) we do a 1-message, 1-second peek to catch misconfiguration at config save time.

**Upstream logging invariant**: every site that logs a constructor return error (currently `createProvider` → `connections.go`, and the plugin-startup / `OnConfigurationChange` paths) already logs via `p.API.LogError`. Because the errors returned here are already sanitized strings (not `%w` wrapped), that upstream logging never receives a raw SDK error. If future refactors change the constructor to use `fmt.Errorf("%w", err)`, they must re-wrap with the sanitizer.

**`Publish`**:

```go
func (a *azureServiceBusProvider) Publish(ctx context.Context, data []byte) error {
    if len(data) > a.maxMsgSize {
        return fmt.Errorf("message %d bytes exceeds max %d", len(data), a.maxMsgSize)
    }
    msg := &azservicebus.Message{Body: data}
    if err := a.sender.SendMessage(ctx, msg, nil); err != nil {
        // Do NOT use %w: the upstream retry_dispatch.go logs the unwrapped
        // error via p.API.LogError which would leak SAS tokens from the SDK's
        // error string. Return a sanitized string instead; the retry path
        // only needs the intent, not the raw SDK message.
        return fmt.Errorf("failed to send service bus message: %s", sanitizeServiceBusError(err))
    }
    return nil
}
```

No Base64. Service Bus message bodies are arbitrary bytes; the AMQP transport handles binary payloads natively. Queue Storage had to Base64 because its REST contract is XML/string-only.

**`Subscribe`** mirrors `azureProvider.Subscribe` exactly: spawn a goroutine, own a `pollDone` channel, `pollQueue`-style loop. Inside the loop:

```go
for {
    select { case <-ctx.Done(): return; default: }

    // azservicebus v1.x has no MaxWait option; the per-call ctx deadline is
    // the sole way to bound how long ReceiveMessages blocks. Wrap the poll
    // ctx with a 5s per-iteration deadline so empty queues don't stall
    // shutdown and so we get predictable batch cadence.
    recvCtx, recvCancel := context.WithTimeout(ctx, serviceBusReceiveMaxWait)
    msgs, err := a.receiver.ReceiveMessages(recvCtx, serviceBusReceiveBatchSize, nil)
    recvCancel()
    if err != nil {
        // context.DeadlineExceeded is the normal empty-queue return; treat as empty batch.
        if errors.Is(err, context.DeadlineExceeded) {
            continue
        }
        if ctx.Err() != nil { return }
        a.api.LogError("Service Bus receive failed",
            "error_code", errcode.ServiceBusReceiveFailed,
            "error", sanitizeServiceBusError(err))
        select { case <-ctx.Done(): return; case <-time.After(serviceBusReceiveMaxWait): }
        continue
    }

    for _, msg := range msgs {
        // Observability: warn on redelivery so silent DLQ moves leave a
        // breadcrumb. DeliveryCount >= 2 means Service Bus has re-presented
        // this message at least once.
        if msg.DeliveryCount > 1 {
            a.api.LogWarn("Service Bus message redelivered",
                "error_code", errcode.ServiceBusRedelivery,
                "delivery_count", msg.DeliveryCount,
                "message_id", msg.MessageID)
        }

        // Malformed message: nil/empty body can never be parsed by the handler
        // and will never succeed on retry. Drop it (Complete, not Abandon) to
        // prevent an infinite redelivery loop. Mirrors azureProvider's
        // handling of undecodable Base64 messages.
        if len(msg.Body) == 0 {
            a.api.LogWarn("Service Bus message has empty body; completing to drop",
                "error_code", errcode.ServiceBusMalformedBody,
                "message_id", msg.MessageID)
            ackCtx, cancelAck := context.WithTimeout(context.Background(), serviceBusAckTimeout)
            _ = a.receiver.CompleteMessage(ackCtx, msg, nil)
            cancelAck()
            continue
        }

        herr := a.handler(msg.Body)

        // ACK operations MUST NOT use the poll ctx: Close() cancels the poll
        // ctx, and a cancelled ack would leave the message locked until
        // expiry (up to 300 s) or silently duplicate on expiry. Use a
        // detached short-timeout ctx so Close races vs ack finish cleanly.
        ackCtx, cancelAck := context.WithTimeout(context.Background(), serviceBusAckTimeout)
        if herr != nil {
            if aerr := a.receiver.AbandonMessage(ackCtx, msg, nil); aerr != nil {
                a.api.LogWarn("Service Bus abandon failed",
                    "error_code", errcode.ServiceBusAbandonFailed,
                    "error", sanitizeServiceBusError(aerr))
            }
        } else {
            if cerr := a.receiver.CompleteMessage(ackCtx, msg, nil); cerr != nil {
                a.api.LogWarn("Service Bus complete failed; message will redeliver",
                    "error_code", errcode.ServiceBusCompleteFailed,
                    "error", sanitizeServiceBusError(cerr))
            }
        }
        cancelAck()
    }
}
```

Key semantic alignment with `azureProvider`:
- Handler nil → message fully processed → `CompleteMessage` (equivalent to Queue's `DeleteMessage`).
- Handler error → do not ack → `AbandonMessage` is preferable to letting the lock expire because it surfaces the message immediately for retry and lets Service Bus increment `DeliveryCount` against MaxDeliveryCount → DLQ. This is closer to operators' expectations than silent lock expiry.
- **Ack operations use a detached context.** The poll ctx can be cancelled by `Close()` between handler invocation and the ack call; using it for ack would leave messages locked until server-side expiry. Detached 10-second timeout lets Close race cleanly.
- **DeliveryCount observability.** Every message with `DeliveryCount > 1` is logged WARN with message id so an operator can trace silent DLQ disappearances.

**`UploadFile` / `WatchFiles`**: copy the `azureProvider` implementations verbatim against the same `azureBlobOps` adapter. File transfer for Service Bus is an Azure Blob sidecar, not a Service Bus feature. We deliberately keep this code path source-duplicated rather than extracting a mixin: two ~70-line blocks is cheaper than the abstraction.

**`MaxMessageSize`**: return `a.maxMsgSize`. Default 192 KB gives ~60 KB of envelope overhead headroom below the 256 KB Standard ceiling. Operators on Premium can raise via config.

**Oversized envelope handling.** `connections.go:splitMessage` only splits `PostMessage.MessageText`. Update/delete/reaction envelopes (handled in `retry_dispatch.go`) and post envelopes with bulky non-text fields cannot be split. Behavior for all these cases matches the existing `azure-queue` provider: `Publish` rejects with an error → `retry_dispatch.go` logs and drops after retry budget. Service Bus inherits the same semantics; the larger 192 KB default just pushes the rejection threshold higher.

**`Close`**: cancel the poll context, wait on `pollDone`. After the poll goroutine exits, close SDK objects in order (`receiver` → `sender` → `client`) each with its own 10-second timeout. Nil-guard `pollDone` and `cancel` so outbound-only connections (that never called `Subscribe`) close cleanly (mirrors `server/azure_provider.go:308-316`):

```go
func (a *azureServiceBusProvider) Close() error {
    if a.cancel != nil { a.cancel() }
    if a.pollDone != nil { <-a.pollDone }

    ctx, cancel := context.WithTimeout(context.Background(), serviceBusAckTimeout)
    defer cancel()
    if a.receiver != nil { _ = a.receiver.Close(ctx) }
    if a.sender   != nil { _ = a.sender.Close(ctx) }
    if a.client   != nil { _ = a.client.Close(ctx) }
    return nil
}
```

**Config reload behavior**: `OnConfigurationChange` tears down the old provider via `Close()` and constructs a new one via `newAzureServiceBusProvider`. This matches the lifecycle that `azure-queue` and `azure-blob` already honor at `connections.go` teardown/rebuild; no new handshake is needed.

### 5. Test-connection helper

```go
func testAzureServiceBusConnection(cfg AzureServiceBusProviderConfig) error {
    client, err := azservicebus.NewClientFromConnectionString(cfg.ConnectionString, nil)
    if err != nil { return fmt.Errorf("...") }
    defer client.Close(context.Background())

    receiver, err := client.NewReceiverForQueue(cfg.QueueName, &azservicebus.ReceiverOptions{
        ReceiveMode: azservicebus.ReceiveModePeekLock,
    })
    if err != nil { return fmt.Errorf("...") }
    defer receiver.Close(context.Background())

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    // Peek (non-destructive) to confirm the queue exists and auth works.
    // PeekMessages is preferred over Receive+Abandon here because it doesn't
    // increment DeliveryCount, so repeated "test connection" clicks never
    // consume a delivery allocation. Empty queues return ([], nil) which is
    // the success path we want.
    _, err = receiver.PeekMessages(ctx, 1, nil)
    if err != nil {
        // Use the SDK's strongly-typed error codes (CodeNotFound,
        // CodeUnauthorizedAccess, etc.) to return actionable messages.
        // IMPORTANT: never return raw err.Error() to the admin webapp: SDK
        // error strings can embed the full endpoint URL (including SAS key
        // fragments). Map known codes to safe messages and return a generic
        // sanitized string for the catch-all.
        var sbErr *azservicebus.Error
        if errors.As(err, &sbErr) {
            switch sbErr.Code {
            case azservicebus.CodeNotFound:
                return fmt.Errorf("queue %q not found (pre-create it in Azure portal or ARM template)", cfg.QueueName)
            case azservicebus.CodeUnauthorizedAccess:
                return fmt.Errorf("unauthorized: check connection string SAS rule and permissions")
            }
        }
        return fmt.Errorf("service bus connection test failed: %s", sanitizeServiceBusError(err))
    }
    return nil
}

// sanitizeServiceBusError strips secrets (SharedAccessKey=…, ?… query
// suffixes, base64 SAS tokens) from an error string before it is returned to
// the admin webapp. Same helper should wrap every err.Error() call that flows
// from an SDK call into a user-facing string (test-connection, startup log
// lines at WARN/ERROR, etc.).
func sanitizeServiceBusError(err error) string { /* regex-strip; unit-tested */ }
```

Wire via an existing seam pattern:

```go
var testAzureServiceBusConnectionFn = testAzureServiceBusConnection
```

### 6. Wire-up changes

| Site | Change |
|------|--------|
| `server/connections.go:370-399` | Add `case ProviderAzureServiceBus:` that calls `newAzureServiceBusProvider(*cfg.AzureServiceBus, p.API)`. Guard with `errMissingAzureServiceBusConfig`. |
| `server/api.go:78-87` | Add `case ProviderAzureServiceBus: p.handleTestAzureServiceBusConnection(w, conn, direction)`. Update default-case error string. |
| `server/api.go` (new handler) | Mirror `handleTestAzureQueueConnection`: validate non-empty fields, call `testAzureServiceBusConnectionFn`, return `{"status":"ok"}` or 502 with the raw error message. |
| `server/service.go:910-932` | In `redactConnection`, add an `if conn.AzureServiceBus != nil { ... }` branch (matching the existing `if conn.AzureQueue != nil` / `if conn.AzureBlob != nil` style, NOT a `switch` on Provider). Populate `QueueName` and `BlobContainerName` from the struct. No new `RedactedConnection` fields needed. Explicitly do NOT copy `ConnectionString` or `BlobAccountKey`. |
| `server/command.go:43-68` | Add `case ProviderAzureServiceBus:` in `providerDetails` returning `"queue: " + conn.QueueName` (+ blob if set). |
| `server/errcode/codes.go` | New block `26000-26999` with constants like `ServiceBusSendFailed`, `ServiceBusReceiveFailed`, `ServiceBusCompleteFailed`, `ServiceBusAbandonFailed`, `ServiceBusBlobListFailed`, etc. Append to `AllCodes`. |

### 7. Webapp changes (`webapp/src/components/ConnectionSettings.tsx`)

- Extend `ProviderType = 'nats' | 'azure-queue' | 'azure-blob' | 'azure-servicebus'`.
- Define `AzureServiceBusProviderConfig` TS interface matching Go struct.
- Add `azure_servicebus?: AzureServiceBusProviderConfig` to `Connection`.
- `emptyAzureServiceBusConfig` with sensible defaults.
- In the provider `<select>` dropdown, add `<option value='azure-servicebus'>Azure Service Bus</option>`.
- In provider-switch `handleFormChange` effect: when user flips provider, initialize `azure_servicebus` if absent, and null the other provider configs. This mirrors the existing switch blocks at `webapp/src/components/ConnectionSettings.tsx:682-705` (azure-queue / azure-blob cases). Implementer must add a fourth branch that zeros `nats`, `azure_queue`, and `azure_blob` and initializes `azure_servicebus` from `emptyAzureServiceBusConfig`.
- Conditional render block: if `editForm.provider === 'azure-servicebus'`, render fields for connection string (password input, masked), queue name, and when file transfer is on, the four blob fields. Reuse existing field components.
- Add the provider key to test-connection payload builder.

Playwright component tests (`ConnectionSettings_edge_cases.pw.tsx`): add cases mirroring the `azure-queue` coverage (shape round-trips, provider switch clears other fields).

### 8. OpenAPI schema (`schema/crossguard-api.yaml`)

- Extend provider enum: add `azure-servicebus`.
- Add `azure_servicebus` property on the connection schema pointing at new `AzureServiceBusProviderConfig`.
- Add `AzureServiceBusProviderConfig` schema mirroring the Go struct.
- Update narrative text that enumerates provider types (line 327, 358-361, 403).

### 9. Help docs (`public/help/admin.html`, PDFs)

- Add a "Azure Service Bus Provider Fields" section mirroring the existing Azure Queue / Azure Blob sections.
- Update the provider comparison table (around line 77-99) to include Service Bus column / row.
- Add a new setup example under "Azure Service Bus Setup" with connection string.
- Update the FAQ line about polling latency to mention Service Bus (AMQP-based, push-like via long receive).
- Add an explicit **DLQ caveat** note in the Service Bus section: "Messages that exceed the queue's `MaxDeliveryCount` are auto-moved by Service Bus to the dead-letter sub-queue. Cross Guard does NOT inspect or process DLQ messages; they stop appearing in receive polls and no further plugin log is emitted. Inspect DLQ via the Azure Portal, `az servicebus queue message …` CLI, or Azure monitoring alerts. The plugin logs a WARN with `error_code=ServiceBusRedelivery` for every redelivery so operators can detect poison-message buildup before DLQ moves happen."
- Regenerate PDFs via `scripts/generate-pdfs.js` at the end (add-help-docs skill handles this).

### 10. Docker dev environment

Service Bus emulator is a Microsoft-supplied Docker image with a SQL Server Linux dependency. **Important**: Azure SQL Edge was retired in September 2025; the current official emulator docs specify `mcr.microsoft.com/mssql/server` (2022 Linux). Pin both images to concrete version tags — `mssql/server:2022-CU14-ubuntu-22.04` and `servicebus-emulator:1.0.1` — so the dev environment is reproducible. (`:2022-latest` is a floating tag and not suitable for a pinned setup.)

```yaml
servicebus-emulator:
  image: mcr.microsoft.com/azure-messaging/servicebus-emulator:1.0.1
  restart: unless-stopped
  depends_on:
    servicebus-mssql:
      condition: service_healthy
  environment:
    SQL_SERVER: servicebus-mssql
    MSSQL_SA_PASSWORD: "${SERVICEBUS_SQL_PASSWORD:-CrossGuardDev!123}"
    ACCEPT_EULA: "Y"
    CONFIG_PATH: /ServiceBus_Emulator/ConfigFiles/Config.json
  volumes:
    - ./docker/servicebus-emulator-config.json:/ServiceBus_Emulator/ConfigFiles/Config.json:ro
  ports:
    - "${SERVICEBUS_PORT:-5672}:5672"
  healthcheck:
    # nc port-alive is NOT sufficient: the emulator binds 5672 before it
    # finishes bootstrapping SQL schema. The emulator image has limited
    # shell tooling, so we do NOT run grep inside the emulator container —
    # that was the first draft and does not work reliably. Instead, use a
    # dedicated Go probe container (`probe-servicebus`, below) that runs
    # PeekMessages(1) against the canonical queue and exits 0 on success.
    # The emulator container itself carries no healthcheck; readiness is
    # inferred from the probe's success.
    disable: true

servicebus-mssql:
  image: mcr.microsoft.com/mssql/server:2022-CU14-ubuntu-22.04
  environment:
    ACCEPT_EULA: "Y"
    MSSQL_SA_PASSWORD: "${SERVICEBUS_SQL_PASSWORD:-CrossGuardDev!123}"
    MSSQL_PID: "Developer"
  healthcheck:
    test: ["CMD-SHELL", "/opt/mssql-tools18/bin/sqlcmd -S localhost -U sa -P \"$$MSSQL_SA_PASSWORD\" -C -Q \"SELECT 1\" -b -o /dev/null"]
    interval: 10s
    timeout: 5s
    retries: 20
    start_period: 30s
```

Notes:
- `mssql-tools18` path applies to SQL Server 2022 Linux images (the SQL Edge path `mssql-tools` is gone).
- Pin both `servicebus-emulator` and `mssql/server` to concrete version tags (e.g., `servicebus-emulator:1.0.1` and `mssql/server:2022-CU14-ubuntu-22.04`). `:2022-latest` is still a floating tag and undermines reproducibility.
- **Apple Silicon (M1/M2/M3)**: `mssql/server` is amd64-only; Docker Desktop runs it via Rosetta/QEMU. Expect 2-3x slower SQL warm-up (~45-60 s) on ARM hosts. Acceptable for dev/CI; document in Risks.

### Service Bus emulator readiness probe

Since the emulator has limited in-container tooling and its log locations vary between image versions, the smoke-test target uses a dedicated **probe container** rather than an in-container healthcheck. The probe is a ~30-line Go program that:

1. Calls `NewClientFromConnectionString` against the emulator.
2. Opens a receiver for the canonical queue.
3. Retries `PeekMessages(ctx, 1, nil)` every 2 s for up to 60 s.
4. Exits 0 on the first successful peek (queue found + auth OK).
5. Exits 1 on timeout with a diagnostic message.

Build the probe as `build/servicebus-probe/main.go` and reference it from `build/integration-tests.mk` as a prerequisite to `docker-servicebus-smoke-test`. Using the same SDK the plugin uses guarantees the gate validates the exact behavior the plugin needs.

`docker/servicebus-emulator-config.json` declares the `crossguard-relay` queue on startup. The emulator's canonical connection string is `Endpoint=sb://servicebus-emulator;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true`. From plugin code it's `sb://servicebus-emulator:5672`; from the host it's `sb://localhost:5672`.

Add `make docker-servicebus-smoke-test` target in `build/integration-tests.mk` mirroring `docker-azure-smoke-test`. The target MUST run the `servicebus-probe` binary against the emulator and block until it exits 0 before configuring plugin connections; otherwise the first plugin-to-emulator call races the SQL Server warm-up and the test flakes. It configures a loopback connection on Server A that posts a message through Service Bus and asserts arrival. `make nuke` removes the SQL Server data directory (same treatment as `azurite-data`).

### 11. Retry and idempotency

- **Outbound retries**: existing `retry_dispatch.go` machinery wraps every `provider.Publish`; no provider-local retry needed. The Service Bus SDK does its own AMQP link-level retries for transient errors before returning.
- **Inbound idempotency**: post-mapping KV check in `handleInboundPost` already guards against duplicate delivery from any at-least-once transport (added in the original `azure-queue` plan). Service Bus at-least-once delivery plugs straight in.
- **Visibility / redelivery**: `AbandonMessage` immediately returns the message to the queue and increments `DeliveryCount`. When `DeliveryCount > MaxDeliveryCount` (default 10), Service Bus auto-moves to DLQ. Operators control the ceiling on queue definition.

### 12. File transfer edge case

When `file_transfer_enabled=true` with `azure-servicebus`, the operator configures a separate Blob account (possibly the same storage account as their queue was, possibly not — Service Bus queues and Blob Storage accounts are orthogonal). That is why `BlobAccountName` / `BlobAccountKey` live on `AzureServiceBusProviderConfig` as independent fields, whereas `azure-queue` shares one account between queue and blob. This is a small price for supporting deployments that combine Service Bus with a different storage account (or no storage account at all when file transfer is off).

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| Which Azure messaging SDK? | `azservicebus` (GA, v1.x) | First-party, pure Go, no CGO, AMQP 1.0. No alternatives under active Microsoft support. |
| Service Bus queues vs topics? | Queues only in Phase 1 | Matches the point-to-point model Cross Guard uses everywhere else; topics add fan-out semantics with no current caller. |
| Sessions (FIFO)? | Out of scope | Our idempotency/ordering model already tolerates reorder; adding sessions requires handler redesign. |
| ReceiveAndDelete vs PeekLock? | PeekLock | We need ack semantics; RAD loses messages on handler failure. |
| Lock renewal? | No auto-renewal goroutine; no plugin-side lock-duration config field | Lock duration is configured on the queue entity in Azure (not by the client SDK). Our handlers complete in milliseconds, so even a minimal entity-set value is fine. If we ever need longer, add auto-renewal; until then keep the client footprint minimal. |
| Auto-create queue? | No (document it) | Data-plane connection strings usually lack Manage rights; requiring `armservicebus` pulls in identity/RBAC scope better handled in a future auth-overhaul plan. |
| Auth mechanism? | Connection string only | Matches how `azure-queue` and `azure-blob` shipped; Managed Identity tracked as a separate cross-provider effort. |
| Blob sidecar reuse? | Yes, reuse `azureBlobOps` | Zero new abstractions; 70 lines of `UploadFile`/`WatchFiles` duplicated is cheaper than extracting a shared helper for 2 callsites. |
| Default `MaxMessageSize`? | 192 KB | Leaves ~60 KB headroom under Standard's 256 KB ceiling for envelope overhead and Base64-free binary payloads. |
| Default batch size? | 32 | Matches `azureDequeueBatchSize`; consistent operator mental model. |
| Handler error: Abandon or do nothing? | Abandon | Explicit ack failure lets Service Bus increment `DeliveryCount` → DLQ on repeated failure; letting the lock expire is silent and slower. |
| Base64-encode bodies? | No | AMQP sends binary natively; the Queue-Storage-era Base64 was a REST/XML workaround. |
| Provider constant string? | `azure-servicebus` | Consistent kebab-case with `azure-queue`/`azure-blob`. |
| Error code range? | 26000-26999 | Next unused block after 25000 (ChanRequest). |
| Local testing story? | Microsoft's emulator in docker-compose | Azurite does not support Service Bus; the emulator is supported and well-documented. |
| Test-connection probe? | `PeekMessages(1)` | Non-destructive; no DeliveryCount side effect; validates both auth and queue existence. |
| Config tunables scope? | Only `MaxMessageSizeBytes` + `BlobPollIntervalSeconds`; batch-size, lock-duration, receive-max-wait are package consts | Start with what operators have a realistic reason to change (Standard vs Premium payload ceiling; blob latency). Add more knobs only when a ticket justifies the config complexity. |
| Oversized envelope behavior? | Match `azure-queue`: `Publish` rejects; `splitMessage` handles post-text; non-text envelopes drop after retry budget | Service Bus does not change the invariant; 192 KB just moves the threshold. |
| Ack context strategy? | Detached 10 s timeout for Complete/Abandon | Poll ctx is cancelled by `Close()`; using it for ack would silently duplicate via lock expiry. |
| ReceiveMessages timeout? | Per-iteration `context.WithTimeout(ctx, 5s)`; treat `DeadlineExceeded` as empty batch | The ctx deadline is the only way to bound empty-queue wait. (`ReceiveMessagesOptions.TimeAfterFirstMessage` exists but controls intra-batch collection after the first message arrives, which is orthogonal; we rely on ctx for the empty-queue max wait.) |
| SDK error strings in user-facing output OR server logs? | Every site that surfaces an SDK error — test-connection return, `Publish` error wrap, and every provider-side `LogError`/`LogWarn` — must wrap via `sanitizeServiceBusError`. Applied at receive-failed, abandon-failed, complete-failed, and publish-failed sites in the code snippets. | SDK errors can embed full endpoint URLs including SAS key fragments. Declaring the helper is not enough; it must be applied at every boundary. |
| Queue name validation? | Service Bus's own rules (≤260 chars, allowed set `[A-Za-z0-9._/\-]`) | Over-restrictive client-side caps cause avoidable operator pain. |
| Emulator SQL sidecar? | `mcr.microsoft.com/mssql/server:2022-CU14-ubuntu-22.04` (not SQL Edge, which was retired Sept 2025; pin a specific CU, not `:2022-latest`) | Current Microsoft emulator docs pair with SQL Server 2022 Linux. `2022-latest` is a floating tag that defeats reproducibility. |
| Emulator readiness? | Single reliable gate: a `servicebus-probe` Go binary that loops `PeekMessages(1)` until success; no in-container healthcheck | Port-alive fails because 5672 binds before SQL schema is ready. In-container log-scrape fails because log paths differ across emulator versions (`/home/app/EmulatorLogs/` vs `/var/log/servicebus/`) and image shell tooling is limited. A Go probe using the same SDK the plugin uses is the only contract that matches what we actually need. |
| Apple Silicon dev hosts? | Documented Risks row; expect 2-3x slower SQL warm-up via Rosetta/QEMU | `mssql/server` is amd64-only (SQL Edge was arm64 but is retired). Acceptable for dev/CI; no code change needed. |

## Files to Modify

| File | Change |
|------|--------|
| `go.mod` / `go.sum` | Add `github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus` |
| `server/provider.go` | No change (interface stable) |
| `server/azure_servicebus_provider.go` | **NEW** — provider implementation, ~350 lines |
| `server/azure_servicebus_provider_test.go` | **NEW** — unit tests with mocks, ~500 lines |
| `server/configuration.go` | New provider const, new config struct, validation fn, dispatch arms |
| `server/connections.go` | `createProvider` switch case for `ProviderAzureServiceBus` |
| `server/api.go` | New `handleTestAzureServiceBusConnection`, dispatch arm, test seam |
| `server/api_test.go` | Test coverage for new handler (shape + error paths) |
| `server/service.go` | `redactConnection` arm for `AzureServiceBus` |
| `server/command.go` | `providerDetails` case for `ProviderAzureServiceBus` |
| `server/command_test.go` | Extend provider-details table test |
| `server/errcode/codes.go` | New 26000-block constants; append to `AllCodes` |
| `webapp/src/components/ConnectionSettings.tsx` | Third provider option, conditional fields, form wiring |
| `webapp/src/components/ConnectionSettingsStory.tsx` | No change (already generic) |
| `webapp/src/components/ConnectionSettings_edge_cases.pw.tsx` | Add Service Bus coverage |
| `schema/crossguard-api.yaml` | New `AzureServiceBusProviderConfig` schema + enum/prop |
| `public/help/admin.html` | New fields section + setup example; regenerate PDFs |
| `public/help/transport-interface.html` | Mention Service Bus alongside existing providers |
| `docker-compose.dev.yml` | `servicebus-emulator` + `servicebus-mssql` (SQL Server 2022 Linux) services |
| `docker/servicebus-emulator-config.json` | **NEW** — emulator queue config |
| `build/servicebus-probe/main.go` | **NEW** — ~30-line Go binary that retries `PeekMessages(1)` until success, used as the smoke-test readiness gate |
| `build/integration-tests.mk` | `docker-servicebus-smoke-test` target |
| `Makefile` | Expose `docker-servicebus-smoke-test` in help block |
| `CLAUDE.md` (root) | Add the new make target to the docker-commands table |

## Tasks

### Phase 1: Provider core

1. [ ] `go get github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus@latest`; commit `go.mod`/`go.sum`.
2. [ ] Scaffold `configuration.go` + `codes.go`: add `ProviderAzureServiceBus` const, `AzureServiceBusProviderConfig` struct, `validateAzureServiceBusConnection` + dispatch arm, and an initial errcode block of six constants (`ServiceBusSendFailed`, `ServiceBusReceiveFailed`, `ServiceBusCompleteFailed`, `ServiceBusAbandonFailed`, `ServiceBusRedelivery`, `ServiceBusMalformedBody`). Append to `AllCodes`. (Additional errcodes allocated on demand as provider code needs them.)
3. [ ] Create `server/azure_servicebus_provider.go` with interface adapters, constructor, `Publish`, `Subscribe`/`pollQueue`, `UploadFile`, `WatchFiles`, `MaxMessageSize`, `Close`, `testAzureServiceBusConnection`.
4. [ ] Add `case ProviderAzureServiceBus:` in `createProvider` (`connections.go`).
5. [ ] `make check-style && make test`. Checkpoint note: Phase 1 passes because the provider is reachable only via `createProvider`; `test-connection` and UI surfaces come online in Phase 2-3.

### Phase 2: Backend wiring

6. [ ] Add `handleTestAzureServiceBusConnection` in `api.go`; wire dispatch arm; add `testAzureServiceBusConnectionFn` seam.
7. [ ] Extend `redactConnection` in `service.go`.
8. [ ] Extend `providerDetails` in `command.go`.
9. [ ] Tests: `server/azure_servicebus_provider_test.go` (mocked Sender/Receiver covering Publish, Subscribe happy/error paths, ack-context-detached race, Close unblocks poll, malformed bodies, blob round-trip).
10. [ ] Tests: extend `api_test.go` (including redaction/secret-leak guard assertion on the GET-config endpoint JSON) and `command_test.go` for new provider.
11. [ ] `make check-style && make test`.

### Phase 3: Webapp and docs

12. [ ] Update `ConnectionSettings.tsx` with `azure-servicebus` option + conditional fields.
13. [ ] Update `ConnectionSettings_edge_cases.pw.tsx` with Service Bus coverage.
14. [ ] Update `schema/crossguard-api.yaml`.
15. [ ] Update `public/help/admin.html` and `public/help/transport-interface.html`; regenerate PDFs (use `add-help-docs` skill).
16. [ ] `make check-style && make test`.

### Phase 4: Integration

17. [ ] Add `servicebus-emulator` + `servicebus-mssql` services (pinned CU tags) to `docker-compose.dev.yml` and `docker/servicebus-emulator-config.json`. Disable the emulator container's own healthcheck; rely on the probe.
18. [ ] Write `build/servicebus-probe/main.go` (retries `PeekMessages(1)` until success or 60s deadline; exits 0 or 1).
19. [ ] Add `docker-servicebus-smoke-test` target to `build/integration-tests.mk` that builds the probe, runs it against the emulator, and only then configures plugin connections. Expose via Makefile help.
20. [ ] Update `CLAUDE.md` docker commands table.
21. [ ] Run full dev cycle: `make nuke && make docker-setup && make deploy && make docker-servicebus-smoke-test`.

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Service Bus emulator image changes / is deprecated | Pin to a known-good tag (not `:latest`) in docker-compose; document upgrade path. |
| SDK version churn (`azservicebus` is still pre-2.0) | Pin exact version; avoid using alpha-flagged APIs; abstract behind `serviceBusSender`/`serviceBusReceiver` interfaces so the adapter absorbs breaking changes. |
| Connection string is a secret leaked via redaction | `redactConnection` must never copy `ConnectionString` into `RedactedConnection`; add an explicit assertion in a unit test. |
| Operator forgets to pre-create the queue | Constructor logs at WARN on first "queue not found" from `ReceiveMessages`; test-connection endpoint fails fast with clear text. |
| AMQP port 5672 conflicts with other services | Emulator port is env-overridable (`SERVICEBUS_PORT`); same pattern as existing Azurite ports. |
| SQL Server 2022 Linux takes 20-30 s to become healthy; emulator binds 5672 before its schema is ready | The emulator container's healthcheck is explicitly disabled; readiness is gated exclusively by the `servicebus-probe` Go binary that loops `PeekMessages(1)` until success (60 s deadline). This is the single canonical readiness contract for the smoke-test target. Log-scrape approaches were rejected because the emulator's log location varies between image versions. |
| Azure SQL Edge retirement (Sept 2025) | Plan uses `mcr.microsoft.com/mssql/server:2022-CU14-ubuntu-22.04` (supported) with a specific CU tag for reproducibility (not `:2022-latest`, which is still floating). |
| Apple Silicon (M1/M2/M3) dev hosts run `mssql/server` via Rosetta/QEMU | Expect 2-3x slower SQL warm-up (~45-60 s cold start). Acceptable for dev/CI. Document in README; CI should use Linux runners natively. |
| `AbandonMessage` storm if handler always errors | Existing DLQ semantics (`MaxDeliveryCount` on queue definition) auto-quarantine poisoned messages; document the recommended queue definition in admin help. |
| Bigger default `MaxMessageSize` hides regressions when moved to a 64 KB provider | Cross-provider invariant unchanged: every provider checks its own limit in `Publish` and rejects; `retry_dispatch.go` splitting logic already keys off `MaxMessageSize()`. |
| Premium-tier 100 MB payloads trigger OOM on receiver | Cap default at 192 KB; operators who override assume responsibility; inbound handler already streams envelope parsing. |
| Lock expiry during slow handler | Handlers are milliseconds; if profiling shows >10 s p99 we'll add auto-renew, not before. |
| Sender / Receiver / Client close ordering races with in-flight ops | `Close` cancels ctx first, waits on pollDone, then closes SDK objects in deterministic order (receiver → sender → client) with per-call timeouts. |
| Test seams diverge between `Publish`-path and `Subscribe`-path mocks | Mirror the shape of `mockAzureQueue` in `azure_provider_test.go`; single file owns both interfaces to prevent drift. |

## UX Summary

| Scenario | Behavior |
|----------|----------|
| Admin selects "Azure Service Bus" in webapp | Conditional fields appear: connection string, queue name; file-transfer fields gated by the toggle. |
| Admin clicks "Test Connection" | `PeekMessages(1)` runs against the queue; success → green toast, failure → sanitized error message (secrets stripped) with CodeNotFound / CodeUnauthorizedAccess mapped to actionable phrasing. |
| Message larger than `MaxMessageSize` | Same flow as `azure-queue`: `retry_dispatch.go` splits the envelope into parts; each part individually fits. |
| Receiver node restart mid-flight | In-flight messages have lock timeouts; they redeliver automatically; post-mapping idempotency prevents duplicates. |
| Queue does not exist at startup | First `ReceiveMessages` returns a 404-like error; we log errcode + error; retry continues. Operator sees actionable error in server log. |
| Operator pre-creates queue with MaxDeliveryCount=3 and DLQ enabled | Plugin auto-abandons on handler errors; after 3 attempts Service Bus auto-moves to DLQ; Cross Guard does not touch DLQ further. |
| File transfer enabled but blob config missing | Validation rejects at config save time; the admin never sees an empty blob container. |

## Testing Plan

**Unit (`azure_servicebus_provider_test.go`)**
- Publish: body bytes passed through (no Base64 mangling); size-limit enforcement; context cancellation propagation.
- Subscribe happy path: handler called with exact body; `CompleteMessage` called once per success.
- Subscribe error path: handler returns error → `AbandonMessage` called; `CompleteMessage` NOT called.
- Subscribe empty-batch path: per-iteration ctx deadline fires → `context.DeadlineExceeded` is returned by the mocked `ReceiveMessages`; loop continues without hot-spin or error log.
- Close: cancel unblocks `ReceiveMessages`; `pollDone` closes; Sender/Receiver/Client closed with timeout.
- Malformed message (nil or zero-length `ReceivedMessage.Body`): log WARN, then `CompleteMessage` to drop the message from the queue. Rationale: a nil body will never be parseable by `model.Unmarshal` on retry, so redelivery is pointless; `CompleteMessage` prevents an infinite Abandon→redelivery→Abandon loop. This matches how `azureProvider` handles malformed Base64 (it `DeleteMessage`s).
- UploadFile / WatchFiles: reuse existing `fakeBlobOps` from `azure_blob_provider_test.go`.

**Integration (`make docker-servicebus-smoke-test`)**
- Server A outbound → Service Bus emulator → Server A inbound loopback (on `crossguard-loopback-sb` queue).
- Round-trip a text post; verify arrival within 5 s.
- Round-trip a file attachment (using blob container on the same Azurite instance already running).
- Fail over: restart emulator mid-flight; verify messages resume.

**E2E**
- Existing integration tests run unchanged with `nats` and `azure-queue` and `azure-blob`.
- Add Service Bus parallel of `docker-azure-smoke-test` steps in `build/integration-tests.mk`.

**Redaction safety**
- Unit test asserting `redactConnection` on an `AzureServiceBus` config leaves `ConnectionString`, `BlobAccountKey` unset on the redacted view.
- API-level test: GET on the admin status / config-returning endpoints for a saved Service Bus connection must not include `connection_string` or `blob_account_key` in the JSON body, even when a non-redaction code path is hit. This catches the case where a future endpoint bypasses `redactConnection`.
- Unit test for `sanitizeServiceBusError`: given an error string that embeds `Endpoint=sb://…;SharedAccessKey=abc123;...` or a query-suffix `?sig=…`, the sanitizer returns a string that still contains the error intent (e.g., "AMQP handshake failed") but has stripped every secret fragment. Test both the happy case (no secrets) and the leaky case (secrets present).

## Acceptance Criteria

- [ ] Operators can configure an `azure-servicebus` connection via admin UI or JSON.
- [ ] Messages relay end-to-end between two Mattermost servers using Service Bus queue.
- [ ] File transfer works when a blob sidecar is configured.
- [ ] Test-connection button returns success for a good config and a clear error for missing queue / bad key.
- [ ] Existing `nats`, `azure-queue`, `azure-blob` providers still pass all tests.
- [ ] `make check-style && make test && make docker-servicebus-smoke-test` all pass.
- [ ] Help docs (HTML + PDF) describe the new provider; OpenAPI schema validates.
- [ ] `RedactedConnection` never exposes `ConnectionString` or blob account key (enforced by test).

## Checklist

- [ ] **Diagnostics**: Service Bus connection / auth / receive failures post to the diagnostics channel via the existing error hooks (no new diagnostic events needed; existing ones fire on `LogError`).
- [ ] **Slash command**: No new `/crossguard` subcommand needed. `/crossguard status` already enumerates connections by name; provider type surfaces via `providerDetails`. Connection-test and init/teardown commands are provider-agnostic.
