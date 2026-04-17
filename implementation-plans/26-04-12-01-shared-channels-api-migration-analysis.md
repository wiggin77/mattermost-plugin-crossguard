# Cross Guard: Migration to Shared Channels Plugin APIs

## Overview

This document analyzes the Mattermost Shared Channels plugin APIs and how they can replace
Cross Guard's current hook-based approach to content synchronization. The transport layer
(NATS, Azure Queue, Azure Blob) remains unchanged. What changes is how the plugin captures
outbound content and applies inbound content.

## Current Architecture

Cross Guard currently uses the standard Mattermost Plugin Hook interface to capture content
changes. Five hooks in `server/hooks.go` fire synchronously whenever content changes:

| Hook | Trigger |
|------|---------|
| `MessageHasBeenPosted` (line 72) | New post created |
| `MessageHasBeenUpdated` (line 98) | Post edited |
| `MessageHasBeenDeleted` (line 120) | Post deleted |
| `ReactionHasBeenAdded` (line 145) | Reaction added |
| `ReactionHasBeenRemoved` (line 179) | Reaction removed |

Each hook filters (system messages, bot posts, already-relayed posts), checks channel/team
eligibility via the KV store, builds an `Envelope`, and publishes to outbound providers.

On the inbound side, `server/inbound.go` (600+ lines) handles team/channel resolution, user
creation via `sync_user.go`, post creation/update/delete, reaction handling, and an in-memory
retry queue (`retry_queue.go`, `retry_dispatch.go`) for out-of-order delivery.

### Limitations of the current approach

- **Fire-and-forget at every layer**: The outbound hooks publish to providers with no
  confirmation that the message was received by anyone. Core NATS `Publish` succeeds as
  soon as the message is buffered in the local client library, not when a subscriber has
  received it. If the receiving plugin is disconnected, the message is silently lost with
  no way to detect or recover. There is no cursor, replay, or acknowledgement mechanism
  at either the plugin or transport layer. Moving to Shared Channels solves the
  server-to-plugin leg (cursor-based tracking with replay on failure). The
  plugin-to-remote-plugin leg (transport) remains fire-and-forget with core NATS, but
  switching to NATS JetStream in a follow-up would close this gap with persisted streams
  and ack-based durable consumers.
- **Out-of-order delivery**: The retry queue (1000 entries, 3 retries, 2min max age) is a
  best-effort workaround. If a parent post arrives after the retry window expires, the reply
  becomes a standalone post. Shared Channels eliminates this problem because the server
  handles dependency ordering and `ReceiveSharedChannelSyncMsg` processes content atomically.
- **No membership sync**: Channel membership changes are not relayed.
- **No profile image sync**: User avatar changes are not relayed.
- **No mention transforms**: Cross-server @mentions are not resolved.
- **No acknowledgement sync**: Post acknowledgements are not relayed.
- **Manual idempotency**: Post mappings (remote ID to local ID) and delete flags (relay loop
  prevention) are maintained in the KV store by plugin code.
- **Manual user management**: Synthetic "sync users" are created and managed by the plugin
  (`sync_user.go`), with username format `{remote}.{connName}`.

## Shared Channels Plugin APIs

The Shared Channels plugin APIs were designed to let plugins implement their own transport
while leveraging the Shared Channel Service's reliable change tracking and content packaging.

### Outbound: Server to Plugin (exists since v9.5)

The server's Shared Channel Service watches for content changes and delivers them to the
plugin via hooks. The service handles:

- Cursor-based tracking of what has been sent per channel per remote
- Dependency ordering (users before posts, files before posts)
- Batching and deduplication (2s minimum coalesce delay)
- Mention transforms (@username to user ID mapping)
- Membership change tracking
- Profile image change tracking
- Retry logic (up to 3 retries per task, individual post retry on failure)

#### Hooks the plugin implements

```go
// Receives batched sync messages for a single channel.
// The cursor advances based on the SyncResponse returned.
OnSharedChannelsSyncMsg(msg *model.SyncMsg, rc *model.RemoteCluster) (model.SyncResponse, error)

// Receives a file attachment to sync.
OnSharedChannelsAttachmentSyncMsg(fi *model.FileInfo, post *model.Post, rc *model.RemoteCluster) error

// Receives a user profile image to sync.
OnSharedChannelsProfileImageSyncMsg(user *model.User, rc *model.RemoteCluster) error

// Health check. Return true if healthy.
// Multiple failed pings mark the plugin offline and stop sync delivery.
OnSharedChannelsPing(rc *model.RemoteCluster) bool
```

#### SyncMsg structure

```go
type SyncMsg struct {
    Id                string
    ChannelId         string
    Users             map[string]*User
    Posts             []*Post
    Reactions         []*Reaction
    Statuses          []*Status
    MembershipChanges []*MembershipChangeMsg
    Acknowledgements  []*PostAcknowledgement
    MentionTransforms map[string]string
}
```

#### SyncResponse structure

```go
type SyncResponse struct {
    UsersLastUpdateAt            int64
    UserErrors                   []string
    UsersSyncd                   []string
    PostsLastUpdateAt            int64
    PostErrors                   []string
    ReactionsLastUpdateAt        int64
    ReactionErrors               []string
    AcknowledgementsLastUpdateAt int64
    AcknowledgementErrors        []string
    StatusErrors                 []string
    MembershipErrors             []string
}
```

The server sends data in dependency order:
1. Users (profiles needed before posts reference them)
2. Membership changes
3. File attachments (available when post arrives)
4. Posts (with mention transforms)
5. Acknowledgements
6. Reactions
7. User profile images

### Inbound: Plugin to Server (PR 35962, merged, available since v11.7)

Three new API methods allow the plugin to push content into Mattermost:

```go
// Push posts, users, reactions, memberships, acknowledgements into MM.
ReceiveSharedChannelSyncMsg(msg *model.SyncMsg) (model.SyncResponse, error)

// Push a file attachment into MM.
ReceiveSharedChannelAttachmentSyncMsg(channelID string, fi *model.FileInfo, data io.Reader) (*model.FileInfo, error)

// Push a user profile image into MM.
ReceiveSharedChannelProfileImageSyncMsg(userID string, image []byte) error
```

The server handles:
- User upsert (creates or updates remote users)
- Post creation, update, and deletion
- Reaction sync
- Membership changes
- Security validation (user ownership, file size limits, channel share checks)
- File path sanitization and storage management
- Idempotency

#### Security validations enforced by the server

1. Plugin must be registered via `RegisterPluginForSharedChannels`
2. Channel must be shared with this plugin's remote
3. For attachments and profile images, the target user's `RemoteId` must match the plugin's
   remote (prevents modifying users belonging to other remotes or local users)
4. `MaxFileSize` from server config is enforced
5. `EnableFileAttachments` must be true

### Setup and Management APIs (exist since v9.5)

```go
// Register this plugin as a remote for shared channels. Idempotent.
RegisterPluginForSharedChannels(opts model.RegisterPluginOpts) (remoteID string, err error)

// Unregister. Plugin stops receiving sync messages.
UnregisterPluginForSharedChannels(pluginID string) error

// Mark a channel as shared.
ShareChannel(sc *model.SharedChannel) (*model.SharedChannel, error)

// Update shared channel metadata.
UpdateSharedChannel(sc *model.SharedChannel) (*model.SharedChannel, error)

// Unshare a channel. Uninvites all remotes.
UnshareChannel(channelID string) (unshared bool, err error)

// Set the sync cursor (forward to skip old posts, backward to re-sync history).
UpdateSharedChannelCursor(channelID, remoteID string, cursor model.GetPostsSinceForSyncCursor) error

// Force a sync of all changed content for a channel.
SyncSharedChannel(channelID string) error

// Invite this plugin to start receiving sync messages for a channel.
InviteRemoteToChannel(channelID string, remoteID string, userID string, shareIfNotShared bool) error

// Stop receiving sync messages for a channel.
UninviteRemoteFromChannel(channelID string, remoteID string) error
```

#### RegisterPluginOpts

```go
type RegisterPluginOpts struct {
    Displayname  string
    PluginID     string
    CreatorID    string  // bot user ID
    AutoShareDMs bool    // automatically share all DMs to this remote
    AutoInvited  bool    // automatically invite to ALL shared channels
}
```

When `AutoInvited` is true, the plugin is automatically invited to every shared channel,
which is the simplest approach for a universal relay plugin.

## What Changes in Cross Guard

### Code that gets replaced

| Current code | Replaced by | Notes |
|---|---|---|
| `hooks.go` (5 hook implementations, filtering, envelope building) | `OnSharedChannelsSyncMsg` delivers pre-packaged `SyncMsg` | Server handles filtering, batching, dependency ordering |
| `inbound.go` (600+ lines: team/channel resolution, post create/update/delete, reaction handling) | `ReceiveSharedChannelSyncMsg` | Server handles user upsert, post CRUD, reactions, memberships |
| `sync_user.go` (synthetic user creation and lookup) | Server manages remote users natively | Users have `RemoteId` linking them to the plugin's remote |
| `retry_queue.go` + `retry_dispatch.go` (out-of-order handling) | Server's cursor-based sync and dependency ordering | No retry queue needed; server replays from cursor on reconnect |
| `store/` post mappings (remote to local ID tracking) | Server tracks this internally | No more `post_mapping:` KV keys |
| Delete flag logic in `inbound.go` (relay loop prevention) | Server handles idempotency | No more `deleting_flag:` KV keys |
| File relay in `connections.go` (`uploadPostFiles`) and `inbound.go` (`handleInboundFile`) | `OnSharedChannelsAttachmentSyncMsg` / `ReceiveSharedChannelAttachmentSyncMsg` | Server manages file storage paths and uploads |
| `model/message.go`, `model/post_message.go` (Envelope, PostMessage, DeleteMessage, ReactionMessage) | `model.SyncMsg` and `model.SyncResponse` from the server | Plugin serializes/deserializes the server's types instead of custom ones |

### Code that stays (with modifications)

| Current code | What changes |
|---|---|
| Transport providers (`nats_provider.go`, `azure_provider.go`, `azure_blob_provider.go`) | Unchanged. They still Publish/Subscribe, but the payload is serialized `SyncMsg` instead of `Envelope` |
| `connections.go` (connection lifecycle, provider creation) | Simplified. Outbound publishing called from `OnSharedChannelsSyncMsg` hook instead of from individual post hooks |
| `service.go` (team/channel init/teardown) | Calls `ShareChannel`/`InviteRemoteToChannel` instead of writing to KV store directly |
| `api.go` (REST endpoints) | Endpoints remain but delegate to shared channel APIs |
| `command.go` (slash commands) | Same commands, different backend calls |
| `prompt.go` (accept/block prompts) | Still needed for admin approval workflow before sharing a channel |
| `configuration.go` (connection config parsing) | Unchanged |
| Admin UI (`ConnectionSettings.tsx`) | Unchanged |
| Modals (`CrossguardChannelModal.tsx`, `CrossguardTeamModal.tsx`) | Unchanged UI, backend calls change |
| `connection_state.ts` (observer pattern) | Unchanged |
| Channel indicator, user popover | Unchanged |

### Code that can be removed entirely

- `server/model/` (custom message types: Envelope, PostMessage, DeleteMessage, ReactionMessage, TestMessage, format detection)
- `server/retry_queue.go` and `server/retry_dispatch.go`
- `server/sync_user.go`
- KV store methods for post mappings and delete flags
- Cache entries for post mappings
- The `crossguard_relayed` post prop (server handles loop prevention)
- The `IsDeletingFlag` mechanism

### New code needed

- `OnSharedChannelsSyncMsg` hook: serialize `SyncMsg`, publish to outbound providers
- `OnSharedChannelsAttachmentSyncMsg` hook: serialize file info + data, publish via `UploadFile`
- `OnSharedChannelsProfileImageSyncMsg` hook: serialize and publish profile image
- `OnSharedChannelsPing` hook: return health status based on provider connectivity
- Inbound handler: deserialize `SyncMsg` from provider, call `ReceiveSharedChannelSyncMsg`
- Inbound file handler: deserialize file data, call `ReceiveSharedChannelAttachmentSyncMsg`
- Inbound profile image handler: call `ReceiveSharedChannelProfileImageSyncMsg`
- `OnActivate`: call `RegisterPluginForSharedChannels`
- `OnDeactivate`: call `UnregisterPluginForSharedChannels`
- Channel init: call `ShareChannel` + `InviteRemoteToChannel` instead of KV writes

## New capabilities gained

Adopting the Shared Channels APIs provides features Cross Guard does not have today:

1. **Reliable catch-up after downtime**: Cursor-based sync replays all missed changes.
   The server does not advance the cursor until the plugin's hook returns success,
   so nothing is lost between the server and the plugin. This is the first half of
   reliable delivery. The second half (plugin to remote plugin via transport) remains
   fire-and-forget with core NATS, but the refactored inbound handler is designed with
   synchronous processing and error propagation so that switching to NATS JetStream
   (persisted streams, durable consumers, explicit ack) requires only a provider-level
   change, not an inbound architecture rework. Together, cursor-based outbound tracking
   and JetStream-based transport would provide end-to-end reliable delivery with no
   message loss between servers.
2. **Channel membership sync**: Join/leave events are relayed
3. **Profile image sync**: Avatar changes are relayed
4. **Mention transforms**: Cross-server @mentions resolved correctly
5. **Post acknowledgement sync**: Acknowledgements are relayed
6. **Status sync**: User online/away/offline status can be relayed
7. **Server-managed users**: No synthetic user creation; the server handles remote user
   lifecycle natively with proper `RemoteId` tracking

## Dependencies

- **Mattermost Server v11.7 or later**: Required for the inbound `Send*` APIs (PR 35962, now merged)
- **Mattermost Server v9.5 or later**: Required for the outbound hooks and setup APIs

## Open questions

1. **TestMessage support**: The current `test-connection` feature sends a `TestMessage` through
   the provider round-trip. With shared channels APIs, what is the equivalent? The plugin could
   still send a test payload through the provider without involving the shared channel service.

2. **Team rewrite index**: The current `rewrite-team` feature maps remote team names to local
   team IDs. With shared channels, channels are shared individually (not teams). Does the team
   rewrite concept still apply, or is it replaced by the channel-level share/invite model?

3. **Prompt system**: The accept/block prompt for unlinked teams/channels is a Cross Guard
   feature. With shared channels, sharing is explicit via `ShareChannel`/`InviteRemoteToChannel`.
   The prompt system could still be useful for admin approval workflows when a remote side
   initiates sharing, but the mechanics would change.

4. **Message format (XML support)**: The current plugin supports JSON and XML serialization for
   `Envelope`. The `SyncMsg` type is a Go struct. The plugin would need to define its own wire
   format for transporting `SyncMsg` over providers. JSON is the natural choice; XML support
   may no longer be needed.

5. **Message splitting**: The current `splitMessage()` function splits large posts for providers
   with message size limits (Azure Queue: 48KB). With `SyncMsg` containing multiple posts in a
   batch, the splitting strategy needs rethinking. Options: send one post per `SyncMsg` for
   size-limited providers, or implement batch splitting.

6. **Backward compatibility**: Should the plugin support both the old hook-based approach and
   the new shared channels approach, controlled by server version detection? Or should it
   require v11.7+ as a hard minimum?
