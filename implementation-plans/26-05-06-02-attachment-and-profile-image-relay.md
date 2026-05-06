# File Attachment and Profile Image Relay

## Context

Two outbound hooks in `server/hooks.go` are stubs:

- `OnSharedChannelsAttachmentSyncMsg` (hooks.go:194) acknowledges the call so the per-remote cursor advances, but does not transport the file.
- `OnSharedChannelsProfileImageSyncMsg` (hooks.go:214) does the same for user profile images.

The `QueueProvider` interface already exposes `UploadFile` and `WatchFiles`, and all four providers (NATS, Azure Queue, Azure Blob batched, Azure Service Bus) implement them. The corresponding inbound plugin API methods exist on the framework side:

- `ReceiveSharedChannelAttachmentSyncMsg(remoteID, channelID, *FileInfo, io.Reader) (*FileInfo, error)`
- `ReceiveSharedChannelProfileImageSyncMsg(remoteID, userID, image []byte) error`

So the gap is: outbound hook bodies, an inbound dispatch path that calls the receive APIs, a header schema for envelope-less file transfer, and per-connection knobs.

## Problem Statement

Posts that include file attachments arrive on the receiver as text-only. User profile images do not propagate. Operators and end users see synthetic remote users with the default avatar regardless of what they look like on the source server, and any post that references an attachment shows a broken file.

## Current State

- Outbound: both hooks log a debug "not yet implemented" and return nil.
- Inbound: `processInboundMessage` in `server/inbound.go:104` returns nil for `TransportTypeAttachment` and `TransportTypeProfileImage` envelope types. (These envelope types are reserved but never used; the file path will not use the message envelope at all.)
- All four providers expose `UploadFile`/`WatchFiles`. None of these are currently called from production code paths.
- Three header constants exist in `server/connections.go:12-14`: `X-Post-Id`, `X-Conn-Name`, `X-Filename`.
- `azureBlobProvider.QueueFileRef` (server/azure_blob_provider.go:520) was built for a deferred-file model where the message batch references files by key. It has no callers today and the framework's separate attachment hook makes the deferred-ordering concern moot. Will be removed as dead code (see Cleanup).
- No concurrency-limiting semaphore exists on the inbound or outbound path. `relaySem` was removed in a prior plan (`26-05-06-03-remove-relay-semaphore.md`) once analysis showed the framework serializes outbound calls into the plugin and every provider's `Subscribe` callback delivers messages serially within a single dispatcher goroutine. This plan does not introduce a new semaphore for the same reason.
- CLAUDE.md mentions `fileSem` and `retry_queue.go`, but those are stale. The shared-channels refactor removed the retry queue; the framework now drives retries by leaving the per-remote cursor in place when a hook returns error.

**The per-connection file config and admin UI already exist** and survived the refactor:

- Backend fields on `ConnectionConfig` (server/configuration.go:61-63): `FileTransferEnabled`, `FileFilterMode`, `FileFilterTypes`. Already on the shared base struct, so all four providers see them.
- `isFileAllowed(filename, filterMode, filterTypes)` helper at server/configuration.go:134.
- Validators already enforce provider-specific blob requirements when `FileTransferEnabled=true`: Azure Queue checks `BlobServiceURL` + `BlobContainerName`; Service Bus checks the four blob fields. NATS has no extra requirements.
- Admin UI in `webapp/src/components/ConnectionSettings.tsx:1346-1414` renders for all four providers with provider-aware help text, an "Enable File Transfer" checkbox, an "Azure Blob Container" field (Azure Queue), and "File Filter Mode" + "File Types" controls.
- Channel and team modals (`CrossguardChannelModal.tsx`, `CrossguardTeamModal.tsx`) already display per-connection file transfer status.

What that means for this plan: zero new config schema, zero new admin UI, zero new validators. The flag is already user-visible and persisted. The backend just needs to **honor** it in the new outbound and inbound paths.

## Prior art

File attachments shipped once before, in a NATS-only form, and were removed in the shared-channels refactor. Useful to lift from rather than redesign:

- `dac8b7f Add file attachment relay via NATS JetStream Object Store` (Apr 5) — the original implementation. Added `FileTransferEnabled`, `FileFilterMode`, `FileFilterTypes` on the NATS connection config; added a `fileSem` semaphore (since removed by plan 03's analysis); added a NATS Object Store outbound upload + inbound watcher path; added admin UI toggles. Original plan: `implementation-plans/26-04-02-01-file-attachment-relay.md`.
- `4c41669 Add file transfer status to UI, slash commands, and smoke tests` (follow-up) — slash command surface and smoke-test harness pieces, including a multi-attachment round-trip.
- `e546c62 Add file transfer documentation to API spec, help pages, and PDFs` (docs follow-up) — operator-facing copy and the OpenAPI spec entries.
- `e37edd5 Refactor to use shared channels.` (May 4) — gutted the path: deleted `nats.go`, shrunk `inbound.go` from ~950 to ~300 lines, removed `retry_queue.go` / `retry_dispatch.go` / `sync_user.go`, and stubbed the two file hooks. The QueueProvider's `UploadFile` / `WatchFiles` surface and the file-related Azure provider code (header constants, blob sidecar machinery) were kept but lost their callers. Profile images were never implemented at all.

What survived the refactor and is still in the tree (no work needed):

- `FileTransferEnabled`, `FileFilterMode`, `FileFilterTypes` on `ConnectionConfig` (already promoted from the original `NATSConnection`-specific home to the shared base struct).
- `isFileAllowed` and `fileFilterModeAllow` / `fileFilterModeDeny` constants in `server/configuration.go`.
- Validator rules for `FileFilterMode` values, `FileFilterTypes` non-empty when mode is set, and provider-specific blob requirements for Azure Queue and Service Bus.
- Admin UI in `ConnectionSettings.tsx` with provider-aware copy. Channel and team modals also surface file transfer state.

What is genuinely new versus the prior art:

- Adapting to the framework's separate `OnSharedChannelsAttachmentSyncMsg` hook. The original packaged file metadata into the same NATS message that carried the post body; the framework's hook fires per-attachment and decoupled from the post.
- Provider-agnostic header schema (`X-Crossguard-Kind`, `X-File-Info`, `X-User-Id`, etc.) — original used a NATS-specific Object Store metadata layout.
- Profile image relay (entirely new).
- Inbound dispatch via the framework's `ReceiveSharedChannelAttachmentSyncMsg` / `ReceiveSharedChannelProfileImageSyncMsg` rather than custom watcher-to-`UpdatePost` plumbing.

## Design Principles

| Pattern | Approach | Rationale |
|---|---|---|
| Channel selection | Files ride the existing `QueueProvider.UploadFile` / `WatchFiles` per-provider file path, not the message envelope. | NATS has no message size limit but Azure Queue has 48 KB. The file-channel path is already provider-agnostic and sized appropriately for each transport. |
| Independence from message path | Attachment/profile-image transport is fully decoupled from `OnSharedChannelsSyncMsg`. | Mirrors the framework's own decoupling; lets a slow file upload not block message relay, and vice versa. |
| Idempotency | `ReceiveSharedChannelAttachmentSyncMsg` and `ReceiveSharedChannelProfileImageSyncMsg` are framework-idempotent. The plan does not add its own dedupe layer. | Avoids a second source of truth. Re-delivery on transient transport errors is safe. |
| Out-of-order delivery (inbound) | The plugin's inbound side returns error from `WatchFiles` handlers when prerequisites are missing (e.g. profile image arrives before the synthetic user, attachment arrives before the post). The provider redelivers on the next poll/watch cycle. | Same retry mechanism the message path uses; no new retry queue needed. |
| Outbound retry asymmetry | Attachments retry correctly: `sendAttachmentToPlugin` (`mattermost/server/platform/services/sharedchannel/attachment.go:144-150`) skips `saveSharedAttachment` when the hook returns error, so `shouldSyncAttachment` returns true next cycle. Profile images do NOT retry: `sendProfileImageToPlugin` (`sync_send.go:655-665`) calls `recordProfileImageSuccess` unconditionally after the hook returns, advancing `LastSyncAt` regardless. The next opportunity to resync a failed profile image is the user's next image change (when `user.LastPictureUpdate > scu.LastSyncAt` again). | Documented framework behavior. Returning error from the profile-image hook is still useful for diagnostics but does not buy a retry. |
| Per-connection controls | Honor the existing `FileTransferEnabled`, `FileFilterMode` (`""`/`"allow"`/`"deny"`), `FileFilterTypes` fields on `ConnectionConfig`. | Lets ops disable file relay per connection (e.g. low-bandwidth connections) and apply allow/deny lists for compliance. |
| No concurrency limiter | Do not add a semaphore around `GetFile` / `UploadFile` / `Receive*`. | Framework serializes outbound calls into the plugin; every provider's `Subscribe` callback delivers serially within a single dispatcher goroutine. Realistic worst case is N concurrent handlers across N inbound connections, which needs no cap. |
| Logging | Never log file content, file IDs of remote-origin files (synthetic and may collide), or profile image bytes. Log local file ID, post ID, channel ID, sizes, conn name. | Matches existing logging discipline. |

## Requirements

- [ ] Files attached to posts in a relay-linked channel arrive on the receiver and bind to the correct post.
- [ ] User profile images for synced senders propagate from source to receiver.
- [ ] Per-connection toggle: file transfer can be disabled.
- [ ] Per-connection allowlist or denylist of file extensions.
- [ ] All four providers (NATS, Azure Queue, Azure Blob batched, Azure Service Bus) work for both attachments and profile images.
- [ ] `make check-style` and `make test` clean.

## Out of Scope

- Cancelling or expiring in-flight uploads when the source post is deleted before delivery. The receiver will create the FileInfo; if the post never arrives or arrives as deleted, the FileInfo orphans. Acceptable.
- File previews/thumbnails (the framework regenerates these on the receiver side from the uploaded bytes).
- End-to-end encryption of file content. Files in transit ride whatever TLS the transport provides.
- Streaming. `p.API.GetFile` returns `[]byte`; `ReceiveSharedChannelAttachmentSyncMsg` takes `io.Reader`. We will wrap the buffer in `bytes.NewReader`. Streaming would require `GetFileStream` which the plugin API does not expose.
- Profile image change detection. The framework fires `OnSharedChannelsProfileImageSyncMsg` when it decides to. We always honour the call; we do not maintain a content hash.

## Technical Approach

### 1. Header schema (server/connections.go)

Extend the existing `X-` prefixed headers, with one new discriminator and one new payload header:

```go
const (
    headerPostID    = "X-Post-Id"        // existing
    headerConnName  = "X-Conn-Name"      // existing
    headerFilename  = "X-Filename"       // existing
    headerKind      = "X-Crossguard-Kind"  // NEW: "attachment" | "profile_image"
    headerTeamName  = "X-Team-Name"      // NEW: sender's team name (for resolution)
    headerChanName  = "X-Channel-Name"   // NEW: sender's channel name (for resolution)
    headerFileInfo  = "X-File-Info"      // NEW: base64(JSON) FileInfo, attachment only
    headerUserID    = "X-User-Id"        // NEW: remote user ID, profile_image only
)
```

`X-File-Info` carries the remote `FileInfo` JSON (CreatorId, Name, Size, MimeType, etc.). The receive API consumes the struct directly. Base64 wraps the JSON so newlines and quotes survive every provider's metadata-encoding rules (Azure Blob metadata is HTTP-header-grade, NATS object store headers are nats.Header).

The kind header dispatches to the right handler. We deliberately do NOT reuse the existing `TransportTypeAttachment` / `TransportTypeProfileImage` envelope types, because the file payload does not flow through the JSON/XML message envelope.

### 2. Outbound: attachment hook (server/hooks.go)

Replace the stub body of `OnSharedChannelsAttachmentSyncMsg`:

1. Resolve `connName := p.connNameForRemote(rc.RemoteId)`. If empty: log + return nil (cursor advances; nothing to do).
2. If `!p.hasOutboundProvider(connName)`: return nil (inbound-only connection).
3. Look up the connection config to read `FileTransferEnabled`, `FileFilterMode`, `FileFilterTypes`. If file transfer disabled: return nil.
4. If filter rejects `fi.Name`: log info (`"error_code", errcode.OutboundAttachmentFiltered`) and return nil.
5. `data, appErr := p.API.GetFile(fi.Id)`. On error: log + return error so the framework retries.
6. Resolve `team` and `channel` from `post.ChannelId` (mirror what `OnSharedChannelsSyncMsg` does). On error: log + return error.
7. Build headers: `kind=attachment`, `conn_name=connName`, `team=team.Name`, `channel=channel.Name`, `post_id=post.Id`, `filename=fi.Name`, `file_info=base64(json(fi))`.
8. `key := "attachment/" + post.Id + "/" + fi.Id` (post-scoped to keep keys unique and to make blob listings inspectable).
9. Call `provider.UploadFile(p.ctx, key, data, headers)`. On error: log + return error.

A non-nil error from the attachment hook causes the framework to skip `saveSharedAttachment` (`attachment.go:144-150`), so the per-`(file, remote)` attachment record is not written. `shouldSyncAttachment` returns true on the next sync cycle, retrying the upload. `ReceiveSharedChannelAttachmentSyncMsg` is idempotent (framework returns the existing FileInfo if the data is already present), so retries are safe.

### 3. Outbound: profile image hook (server/hooks.go)

`OnSharedChannelsProfileImageSyncMsg(user, rc)` follows the same pattern, but the lookup space is different. Profile images are global to a user, not scoped to a channel. The framework calls this hook once per `(user, remote)` pair when it decides the image needs to sync.

1. Resolve `connName := p.connNameForRemote(rc.RemoteId)`. Return nil on miss.
2. If `!p.hasOutboundProvider(connName)`: return nil.
3. Read `FileTransferEnabled` for the connection. If disabled: return nil. (Profile images use the same toggle as attachments.)
4. `data, appErr := p.API.GetProfileImage(user.Id)`. On error: log + return error.
5. Headers: `kind=profile_image`, `conn_name=connName`, `user_id=user.Id`, `filename="profile.png"` (informational).
6. `key := "profile_image/" + user.Id`. Re-uploads overwrite, which is the intent.
7. `provider.UploadFile(p.ctx, key, data, headers)`. On error: log + return error (for diagnostics; framework will not retry, see below).

No team/channel headers because profile image sync is not channel-scoped on the receive side.

**No outbound retry from the framework**: the framework's `sendProfileImageToPlugin` (`mattermost/server/platform/services/sharedchannel/sync_send.go:655-665`) calls `recordProfileImageSuccess` unconditionally after the hook returns, even when the hook returned error. The cursor (`LastSyncAt` on `SharedChannelUsers`) advances regardless, and the next sync only fires when the user's `LastPictureUpdate > scu.LastSyncAt` again (i.e., the user changes their picture). Returning a non-nil error from this hook is still useful for operator diagnostics, but it does not buy a retry. Single-shot semantics on profile image upload failure is acceptable: profile images are not load-bearing for any other workflow, and the next user-driven change picks up the previously failed transfer.

### 4. Inbound dispatch (new server/inbound_files.go)

A separate file because the watch loop and per-kind handlers add ~150 lines that do not belong in `inbound.go`.

```go
// startInboundFileWatchers is called from connectInbound for each inbound conn
// that has a live provider. It runs WatchFiles in its own goroutine and
// dispatches by header kind.
func (p *Plugin) startInboundFileWatchers(ctx context.Context, ic inboundConn) {
    go func() {
        _ = ic.provider.WatchFiles(ctx, p.handleInboundFile(ic.name))
    }()
}

func (p *Plugin) handleInboundFile(connName string) func(key string, data []byte, headers map[string]string) error {
    return func(key string, data []byte, headers map[string]string) error {
        switch headers[headerKind] {
        case "attachment":
            return p.handleInboundAttachment(connName, key, data, headers)
        case "profile_image":
            return p.handleInboundProfileImage(connName, key, data, headers)
        default:
            p.API.LogWarn("Inbound file: unknown kind",
                "error_code", errcode.InboundFileUnknownKind,
                "conn_name", connName, "key", key, "kind", headers[headerKind])
            return nil // permanent: malformed headers will not heal on retry
        }
    }
}
```

#### handleInboundAttachment

1. Pull `teamName := headers[headerTeamName]`, `chanName := headers[headerChanName]`, `postID := headers[headerPostID]`, `fileInfoB64 := headers[headerFileInfo]`.
2. Validate: missing required header => log warn + return nil (don't retry malformed forever).
3. Decode `fi := *FileInfo` from base64+JSON.
4. Resolve local team via the existing `findTeamByRewrite` + `GetTeamByName` path used by `resolveTeamAndChannel`. On miss: log info + return error so the watcher retries (user may not have linked yet).
5. Resolve channel by name within team. On miss: same.
6. Verify the channel is linked for `connName` via `kvstore.GetChannelConnections` + `hasInboundConnection`. If not: return nil (drop). Note: this matches the message path's prompt-and-drop semantics. The sender's framework already saved `SharedChannelAttachment` when the outbound hook returned nil, so this individual file is permanently lost even if the channel is later linked. Subsequent attachments after acceptance arrive normally.
7. Look up `remoteID := p.remoteIDs["inbound:"+connName]`. Empty: log error + return nil.
8. Call `p.API.ReceiveSharedChannelAttachmentSyncMsg(remoteID, channel.Id, &fi, bytes.NewReader(data))`. On error: log + return error (retry).
9. Return nil. The provider deletes the blob/object after successful processing.

#### handleInboundProfileImage

1. Pull `userID := headers[headerUserID]`. Missing: warn + return nil.
2. Look up `remoteID := p.remoteIDs["inbound:"+connName]`. Empty: log error + return nil.
3. Call `p.API.ReceiveSharedChannelProfileImageSyncMsg(remoteID, userID, data)`. On error: log + return error so the watcher retries.

The user ID from the sender is the same as the local synthetic user's ID. The framework's `upsertSyncUser` (`sharedchannel/sync_recv.go:310`) looks up the user by the same ID the sender shipped, and inserts new synthetic users preserving that ID. User IDs are globally unique UUIDs, so no translation is needed at the plugin layer. The "image arrives before user is created" case (SyncMsg with the user record hasn't landed yet) shows up as an error from `ReceiveSharedChannelProfileImageSyncMsg`; returning that error redelivers via the provider on the next poll.

### 5. Lifecycle wiring (server/inbound.go)

In `connectInbound`, after each successful `provider.Subscribe`, call:

```go
p.startInboundFileWatchers(ctx, inboundConn{provider: provider, name: conn.Name})
```

`closeInbound` already cancels `p.inboundCtx`, which is the parent of the watcher's context, so no extra teardown is needed. Each provider's `WatchFiles` honours `ctx.Done` and returns.

### 6. Per-connection configuration (already present)

No schema, validator, or UI work in this plan. The fields exist on `ConnectionConfig`, `isFileAllowed` exists in `server/configuration.go`, and the validators already enforce provider-specific blob requirements. The admin UI in `ConnectionSettings.tsx` already renders the toggle and filter controls for all four providers, and `CrossguardChannelModal` / `CrossguardTeamModal` surface the resulting state.

The outbound and inbound paths described above call `conn.FileTransferEnabled` and `isFileAllowed(filename, conn.FileFilterMode, conn.FileFilterTypes)` directly. If either gate is closed, the corresponding hook returns nil (cursor advances; nothing to do).

`FileTransferEnabled` defaults to `false` (the Go zero value). Existing connections persisted before this plan ships will not start relaying files until an admin explicitly opts in, which is the intended migration behaviour.

Max file size check: use `*p.API.GetConfig().FileSettings.MaxFileSize` rather than a plugin-side constant. Honors the existing operator setting. Reject upload if `len(data) > maxFileSize` with a warn log; do not return error to the framework (the file will never get smaller).

### 7. Provider-specific notes

- **NATS** (server/nats_provider.go): uses JetStream Object Store with a 1-hour TTL on the bucket. Profile image overwrites work natively (Object Store keys are name-based). The watcher uses `jetstream.UpdatesOnly()` (`nats_provider.go:128`), which excludes entries that exist in the bucket at the moment the watcher subscribes. If a receiver is offline when a file is uploaded and reconnects within the 1-hour TTL, the new watcher will not see the file. This is a pre-existing correctness gap that was harmless while no callers existed; once attachments and profile images flow through this path, files uploaded during a receiver outage are silently lost. Drop `UpdatesOnly()` (or replace with a startup-list-and-replay) as part of this work.
- **Azure Queue** (server/azure_provider.go): blob sidecar in the configured `BlobContainerName`. Already polls and deletes after processing. As-is.
- **Azure Blob batched** (server/azure_blob_provider.go): files go to `files/<key>` prefix, distinct from messages at `messages/<connName>/`. `WatchFiles` already handles HA locking, processed-marker idempotency, and per-blob delete-after-process. Use `UploadFile` directly, not `QueueFileRef`. Mark `QueueFileRef`, `pendingFileRef`, `pendingFiles*`, `companion*` plumbing for removal once the new path is in (see Cleanup).
- **Azure Service Bus** (server/azure_servicebus_provider.go): blob sidecar identical to Azure Queue. As-is.

### 8. Cleanup of dead code

Once attachments and profile images flow through the new path, remove from `azureBlobProvider`:

- `QueueFileRef` (server/azure_blob_provider.go:520)
- `pendingFileRef`, `pendingFilesMu`, `pendingFiles`, `companionMu`
- `flushPendingFilesList`, `persistShutdownResidue`, `recoverCompanionFiles`
- Companion-file logic in `flush` and `recoverDirectory`
- `getFile` injection point in `newAzureBlobProvider` and the closure in `connections.go:187-195` (callers will use `p.API.GetFile` directly from hooks.go)
- Associated error codes and tests
- All `newAzureBlobProvider` callers in test files need their signature updated when the `getFile` parameter is removed. Grep for `newAzureBlobProvider(` across `server/` to find them.

The WAL itself (message-batch path) stays; only the deferred-file companion code goes. Doing this in a separate commit after the new path is verified working keeps the diff reviewable.

### 9. Error codes (server/errcode/codes.go)

Reserved blocks for new file-relay codes. CLAUDE.md says each file owns a 1000-range:

- Outbound hook codes go in `hooks.go`'s range (10000-10999; currently in use through 10109).
- Inbound dispatch in the new `inbound_files.go` gets its own 1000-range. The next unallocated thousand at implementation time is the right pick (current allocations include 10000, 14000, 15000, 16000, 18000, 19000; 20000-20999 is the obvious next slot).
- Codes that fire from existing inbound message dispatch stay in `inbound.go`'s range (15000-15999) only if added to that file. Anything in `inbound_files.go` uses the new range.

Append at next free integer in each block:

- `OutboundAttachmentChannelLookupFailed`
- `OutboundAttachmentFileFetchFailed`
- `OutboundAttachmentFiltered`
- `OutboundAttachmentSizeExceeded`
- `OutboundAttachmentUploadFailed`
- `OutboundProfileImageFetchFailed`
- `OutboundProfileImageUploadFailed`
- `OutboundProfileImageDisabled`
- `InboundFileUnknownKind`
- `InboundAttachmentMissingHeader`
- `InboundAttachmentFileInfoDecodeFailed`
- `InboundAttachmentTeamLookupFailed`
- `InboundAttachmentChannelLookupFailed`
- `InboundAttachmentReceiveFailed`
- `InboundProfileImageMissingHeader`
- `InboundProfileImageUserLookupFailed`
- `InboundProfileImageReceiveFailed`

Each gets its own constant + `AllCodes` entry. Numbers assigned at implementation time, never reused.

### 10. Tests

Backend unit tests (server/):

- `hooks_test.go`: extend with attachment- and profile-image-hook cases. Mock `p.API.GetFile`, `p.API.GetProfileImage`, `provider.UploadFile`. Cover: disabled connection, filter reject, missing channel, GetFile error, UploadFile error, success path.
- `inbound_files_test.go` (new): `handleInboundAttachment` and `handleInboundProfileImage`. Cover: missing headers, malformed FileInfo, unlinked channel, missing local user (retry), success path. Use a fake `WatchFiles`-driven harness that calls the handler directly.
- `configuration_test.go` and `isFileAllowed` tests already cover the validators and filter helper. No changes required there unless the new code surfaces a gap.

Per-provider tests (already exist for the file paths) keep passing — the new code calls the same `UploadFile`/`WatchFiles` surface they already cover.

### 11. Verification

- `make check-style`
- `make test`
- `make docker-setup` then `make deploy`
- `make docker-integration-test` covers attachments via the existing harness — extend it to assert profile image propagation. The smoke tests already round-trip files for NATS, Azure Queue, Azure Blob batched, and Service Bus, so the same script with attachment-bearing posts and profile-image changes is the integration validation.

Manual smoke check on each provider:

1. Server A user posts a message with one PDF and one PNG. Verify both arrive on Server B and open correctly.
2. Server A user changes their profile image. Verify Server B's synthetic user shows the new image within one sync cycle.
3. Server A user posts a `.exe` against a connection with `file_filter_mode="deny"` and `.exe` in the list. Verify text arrives, file does not.
4. Server A user posts a 200 MB file against a server with `MaxFileSize=104857600`. Verify the message arrives but a warn is logged on the sender; no upload attempt.
5. Disable file transfer mid-flight on conn `high`. Verify in-flight uploads complete; new attachments are skipped.

## Risks

- **Profile image arrives before user record**: each `SyncMsg` carries the user records for the posts it contains, so the synthetic user normally exists by the time `OnSharedChannelsProfileImageSyncMsg` fires. If transport reorders and the image lands first, `ReceiveSharedChannelProfileImageSyncMsg` returns error, the watcher returns that error, and the provider redelivers on the next cycle. No KV mapping needed: user IDs are globally unique UUIDs and the framework upserts by the same ID the sender ships.
- **Header size limits**: Azure Blob metadata caps at 8 KB total. The base64 FileInfo header is the main risk. Typical FileInfo JSON is well under 1 KB; verify with a representative case before shipping. If it exceeds, fall back to writing a tiny `.json` sidecar blob next to the file blob.
- **NATS durability profile**: the NATS provider uses core NATS for messages (`nats_provider.go:62,88`: `nc.Publish` / `nc.QueueSubscribe`), which has no broker-side persistence. An offline receiver loses messages immediately. Files use JetStream Object Store with a 1-hour bucket TTL, so files actually have stronger durability than messages on this transport (1 h vs 0). After fixing the `UpdatesOnly()` watcher gap noted in section 7, files uploaded while the receiver is offline are recoverable up to the TTL. Document the asymmetry in the operator runbook.
- **Outbound retry asymmetry**: attachments retry naturally on hook error (sender's framework skips `saveSharedAttachment`, so `shouldSyncAttachment` keeps returning true); profile images do not retry (sender's framework calls `recordProfileImageSuccess` unconditionally, advancing the cursor regardless). A failed profile image upload only resyncs the next time the user changes their image. Acceptable: profile images are decorative, not load-bearing for any other workflow. Document.
- **Re-upload churn on profile image overwrites**: if a user changes their image rapidly, the framework will fire the hook each time. The provider will overwrite the same key. Last-writer-wins is the intended behaviour; no extra coordination.
- **Cleanup commit ordering**: removing `QueueFileRef` and the companion-file machinery in the same commit as the new code makes review hard. Two-commit strategy: first commit wires the new path and adds tests; second commit removes the dead code. Both ship together.

## Implementation order

Prerequisite: plan 03 (`26-05-06-03-remove-relay-semaphore.md`) has merged. This plan does not depend on it for correctness, but the design assumes the no-semaphore baseline so the order matters for clean diffs.

Each step lands as its own commit:

1. Add the new header constants in `server/connections.go` (`headerKind`, `headerTeamName`, `headerChanName`, `headerFileInfo`, `headerUserID`). No behavior change.
2. Implement outbound `OnSharedChannelsAttachmentSyncMsg`, gated on `conn.FileTransferEnabled` + `isFileAllowed`. Add error codes. Add `hooks_test.go` cases.
3. Implement outbound `OnSharedChannelsProfileImageSyncMsg`, gated on `conn.FileTransferEnabled`. Add error codes. Add tests.
4. Add `inbound_files.go` with `startInboundFileWatchers`, `handleInboundFile`, `handleInboundAttachment`, `handleInboundProfileImage`. Wire into `connectInbound`. Add tests.
5. Run docker integration smoke tests across all four providers.
6. Cleanup commit: remove `QueueFileRef` and companion-file plumbing from `azureBlobProvider`. Update tests.
