# Cross Guard: Refactor to Shared Channels Plugin APIs

## Context

Cross Guard currently captures content changes via five standard plugin hooks
(`MessageHasBeenPosted`, `MessageHasBeenUpdated`, `MessageHasBeenDeleted`,
`ReactionHasBeenAdded`, `ReactionHasBeenRemoved`) and handles inbound message delivery
manually (team/channel resolution, user creation, post CRUD, idempotency tracking, retry
queue). The Mattermost Shared Channels plugin APIs provide a purpose-built alternative:
the server packages up all content changes reliably with cursor-based tracking and
delivers them to the plugin, and the plugin can push received content back into
Mattermost via `ReceiveSharedChannelSyncMsg` (landed in PR 35962, available since v11.7).

This refactor replaces the custom hook-based capture and manual inbound handling with the
Shared Channels APIs while keeping the transport layer (NATS, Azure Queue, Azure Blob,
Azure Service Bus) unchanged.

## Current State

**Outbound** (`hooks.go`, `connections.go`): Five Mattermost hooks fire on content
changes. Each hook filters, checks channel eligibility via KV store, builds an `Envelope`
(custom `server/model/` types), acquires a semaphore slot, and publishes to all matching
outbound providers. File uploads handled separately via `uploadPostFiles()`. Fire-and-forget
with no replay on failure.

**Inbound** (`inbound.go`, `sync_user.go`, `retry_queue.go`, `retry_dispatch.go`): Each
inbound provider subscribes and delivers raw bytes to `handleInboundMessage()`. The handler
unmarshals the `Envelope`, resolves team/channel by name, creates synthetic sync users,
performs post CRUD via standard plugin API calls, tracks post ID mappings and delete flags
in the KV store, and enqueues out-of-order messages to an in-memory retry queue (1000
entries, 3 retries, 2min max age).

## Design Principles

**Future-proofing for JetStream reliable delivery.** The NATS provider currently uses
core NATS (fire-and-forget). A planned follow-up will switch to NATS JetStream for
reliable, ack-based message delivery. With JetStream, published messages are persisted
to a stream and the receiving side uses a durable consumer with explicit
acknowledgement. This eliminates the gap where the server's outbound cursor advances
(because the plugin's hook returned success) but the message is lost in transit because
no subscriber was connected. To avoid rearchitecting the inbound path when JetStream is
added, this refactor designs the inbound handler to be JetStream-compatible from day
one: synchronous processing, error propagation to the provider, and blocking
backpressure instead of silent drops.

| Our Approach | Avoid |
|---|---|
| Delegate content tracking to the server's Shared Channel Service | Reimplementing cursor logic, change detection, or dependency ordering in the plugin |
| Serialize `model.SyncMsg` as XML for transport (customer content inspection requirement) | Defining new custom message types that duplicate what the server already provides |
| Clean break requiring Mattermost v11.7+ | Backward-compatibility shims for older server versions |
| Stub file attachment sync with clear `// TODO` markers | Partially implementing file sync that cannot be tested end-to-end |
| Remove dead code immediately | Leaving unused code behind commented out or gated by flags |
| Honor the `QueueProvider.Subscribe` handler contract (nil = ack, error = nack) | Async handlers that always return nil, or dropping messages on backpressure. These patterns break ack-based providers and would require rearchitecting when adding JetStream reliable delivery. |

## Requirements

- [ ] Mattermost server model types (`SyncMsg`, `Post`, `User`, etc.) have `xml` struct tags
- [ ] Shared Channels Plugin API supports multiple remotes per plugin with per-remote hooks
- [ ] Plugin registers one remote per unique connection name (shared across inbound/outbound)
- [ ] Connection name uniqueness is per-direction (inbound and outbound may share a name)
- [ ] Wire format between servers is XML (customer content inspection requirement)
- [ ] Outbound: `OnSharedChannelsSyncMsg` serializes to XML and publishes to outbound providers
- [ ] Inbound: XML messages from providers are deserialized and pushed via `ReceiveSharedChannelSyncMsg`
- [ ] Channel init/teardown calls `ShareChannel`/`InviteRemoteToChannel`/`UninviteRemoteFromChannel`
- [ ] `OnSharedChannelsPing` returns health per remote based on its provider connectivity
- [ ] File attachment hooks are stubbed with logging
- [ ] Profile image hook is stubbed with logging
- [ ] All removed code paths have corresponding test removals
- [ ] New code paths have tests
- [ ] `make check-style` and `make test` pass
- [ ] Plugin deploys and relays messages in the Docker dev environment

## Out of Scope

- **File attachment sync**: Stubbed only. Full implementation in a follow-up plan.
- **Profile image sync**: Stubbed only. Follow-up plan.
- **Backward compatibility with pre-11.7 servers**: This is a clean break.
- **DM/GM auto-sharing**: `AutoShareDMs` is set to false. DM relay is out of scope.
- **Frontend changes**: The admin UI, modals, and indicators are unchanged. Backend API
  response shapes stay compatible.

## Technical Approach

### 1. Wire Format

The wire format between servers is **XML**. Customer content inspection tooling requires
all cross-domain traffic to be XML so it can be parsed by standard inspection appliances.
See [XML Wire Format Schema](26-04-12-03-xml-wire-format-schema.md) for the complete
element reference and examples.

This requires two things:
1. Mattermost server changes: XML struct tags on model types, and multi-remote
   registration support. See [Server Shared Channels Changes](26-04-12-04-server-shared-channels-changes.md)
   for the complete plan. Both have merged (PR 36126 + follow-up PR 36309).
2. Define a `TransportEnvelope` in the plugin that wraps `SyncMsg` with routing metadata.

#### Plugin TransportEnvelope

```go
// server/transport.go (new file)

// TransportEnvelope wraps content for XML wire transport between servers.
type TransportEnvelope struct {
    XMLName     xml.Name           `xml:"CrossGuardEnvelope"`
    Version     int                `xml:"version,attr"`
    Type        string             `xml:"type,attr"`
    ConnName    string             `xml:"ConnName"`
    TeamName    string             `xml:"TeamName"`
    ChannelName string             `xml:"ChannelName"`
    SyncMsg     *mmModel.SyncMsg   `xml:"SyncMsg,omitempty"`
    TestID      string             `xml:"TestID,omitempty"`
}

const (
    TransportTypeSyncMsg      = "sync_msg"
    TransportTypeAttachment   = "attachment"   // stubbed
    TransportTypeProfileImage = "profile_image" // stubbed
    TransportTypeTest         = "test"
)
```

Because the server model types have `xml` tags, `encoding/xml` marshals `SyncMsg` and
its nested `Post`, `User`, `Reaction` types directly. No wrapper types or conversion
functions needed in the plugin.

**Serialization functions**:

```go
// MarshalEnvelope serializes a TransportEnvelope to XML with the standard header.
func MarshalEnvelope(env *TransportEnvelope) ([]byte, error)

// UnmarshalEnvelope deserializes XML bytes into a TransportEnvelope.
func UnmarshalEnvelope(data []byte) (*TransportEnvelope, error)
```

- `Version` attribute (`version="1"`) for future wire format changes.
- `TeamName` and `ChannelName` provide human-readable routing. The receiving side looks
  up the local channel by `(teamName, channelName)`, same as today.
- `TestID` supports the existing test-connection feature without needing the old
  `model.TestMessage` type.

**Message size handling**: If the serialized envelope exceeds a provider's
`MaxMessageSize()`, split the `SyncMsg` by reducing the number of posts per envelope.
Each split envelope carries the same Users map (needed as context for the posts) but a
subset of Posts. Reactions and other fields are included only in the envelope containing
their associated posts. XML encoding is more verbose than JSON, so splitting thresholds
must account for the larger overhead.

```go
// server/transport.go

func splitTransportEnvelope(env *TransportEnvelope, maxSize int) ([]*TransportEnvelope, error)
```

If the SyncMsg contains no posts (users-only sync), it is sent as-is. If a single post
exceeds the limit, it is sent alone and the provider handles the overflow (same as today
where Azure Queue has a hard 48KB limit).

### 2. Plugin Registration (Multi-Remote)

Cross Guard manages separate inbound and outbound connections (e.g., outbound
`nats-low-to-high`, inbound `nats-low-to-high`). Each unique connection name
represents a distinct remote endpoint. Inbound and outbound connections that share
the same name are paired and share a single remote registration. This is critical
because the `remoteID` is used by the server for loop prevention (content received
from a remote is not sent back to that same remote) and for tagging synthetic users
with the correct `RemoteId`. If inbound and outbound had separate `remoteID`s,
loop prevention would break and synthetic users would not be associated with the
correct remote.

The Shared Channels Plugin API must support registering multiple remotes per plugin
so that:

- The server tracks sync cursors independently per remote (each connection syncs at
  its own pace).
- `OnSharedChannelsSyncMsg` receives the `RemoteCluster` identifying which remote
  triggered the sync, so the plugin publishes only to the correct outbound provider.
- `OnSharedChannelsPing` is called per remote, so the plugin can report health for
  each provider independently.
- `InviteRemoteToChannel` targets a specific remote, so a channel can be shared with
  some connections but not others.
- `ReceiveSharedChannelSyncMsg` accepts a `remoteID` so the server knows which
  remote the inbound data originated from.

This requires a server-side API change (see [Server Shared Channels Changes](26-04-12-04-server-shared-channels-changes.md), Phase 2).

**Connection identity (pairing key)**: Each connection has a `SiteURL` field on
its config struct (in addition to `Name`). `SiteURL` is the pairing key between
inbound and outbound and the dedup key the server uses to identify the remote.
Two connections with identical `SiteURL` values share a single `remoteID`,
regardless of direction.

`SiteURL` is populated when a connection is first added to the config:

```go
// In the config save/validate path, when a connection has no SiteURL set,
// initialize it from the current Name. After this point it is immutable;
// renames update Name but leave SiteURL alone.
if conn.SiteURL == "" {
    conn.SiteURL = "crossguard:" + conn.Name
}
```

Once persisted, `SiteURL` does not change. This means:
- Renames preserve the remote's identity. The server keeps the same `remoteID`
  and sync cursor, so cross-domain message flow is uninterrupted.
- Inbound and outbound connections that were paired at creation time
  (defaulted from the same `Name`) remain paired even if one side is later
  renamed. The pairing follows `SiteURL`, not the current `Name`.
- A user who wants two previously-paired connections to become unpaired
  must explicitly edit `SiteURL` (or remove and re-add one of the
  connections).

The validation in `configuration.go` currently enforces `Name` uniqueness across
both directions using a shared `allNames` map. This must be relaxed to per-direction
uniqueness so an inbound and outbound connection can share a name (and therefore
the default `SiteURL`). `SiteURL` itself need not be globally unique within plugin
config (since paired inbound+outbound share one), but each direction's set of
`SiteURL` values must be unique within the direction.

Asymmetric configurations (outbound-only or inbound-only) are supported. An
outbound-only connection registers a remote for outbound sync delivery. An
inbound-only connection registers a remote so `ReceiveSharedChannelSyncMsg` has a
valid `remoteID` to pass.

**OnActivate** changes (`plugin.go`):

After initializing the KV store and bot user, register one remote per unique
`SiteURL` across both directions, then populate `p.remoteIDs[connName]` for every
configured connection (paired connections map to the same `remoteID`):

```go
p.remoteIDs = make(map[string]string) // connName -> remoteID

// Build a unique set of SiteURLs across both directions, with a representative
// display name for each (used only as the registration's Displayname).
siteURLToDisplayName := make(map[string]string)
for _, conn := range outboundConns {
    if _, exists := siteURLToDisplayName[conn.SiteURL]; !exists {
        siteURLToDisplayName[conn.SiteURL] = conn.Name
    }
}
for _, conn := range inboundConns {
    if _, exists := siteURLToDisplayName[conn.SiteURL]; !exists {
        siteURLToDisplayName[conn.SiteURL] = conn.Name
    }
}

// Register one remote per unique SiteURL. The server returns the existing
// remoteID for re-registration with the same SiteURL (idempotent).
siteURLToRemoteID := make(map[string]string)
for siteURL, displayName := range siteURLToDisplayName {
    remoteID, err := p.API.RegisterPluginForSharedChannels(mmModel.RegisterPluginOpts{
        Displayname:  fmt.Sprintf("Cross Guard (%s)", displayName),
        PluginID:     manifest.Id,
        CreatorID:    p.botUserID,
        AutoShareDMs: false,
        AutoInvited:  false,
        SiteURL:      siteURL,
    })
    if err != nil {
        return fmt.Errorf("failed to register remote for %s: %w", siteURL, err)
    }
    siteURLToRemoteID[siteURL] = remoteID
}

// Map each connection name to its remoteID. Paired inbound+outbound connections
// (same SiteURL) end up pointing to the same remoteID.
for _, conn := range outboundConns {
    p.remoteIDs[conn.Name] = siteURLToRemoteID[conn.SiteURL]
}
for _, conn := range inboundConns {
    p.remoteIDs[conn.Name] = siteURLToRemoteID[conn.SiteURL]
}
```

Add `remoteIDs map[string]string` field to the `Plugin` struct (connName to
remoteID). This replaces the single `remoteID string` field. Lookups elsewhere
(`p.remoteIDs[connName]` in `inbound.go`, `connNameForRemote(rc.RemoteId)` in
`hooks.go`) work unchanged.

**OnDeactivate** changes:

Before closing providers, unregister all remotes for this plugin via the bulk
cleanup API. One call removes every `RemoteCluster` row for this plugin:

```go
if err := p.API.UnregisterPluginForSharedChannels(manifest.Id); err != nil {
    p.API.LogWarn("Failed to unregister remotes from shared channels",
        "error_code", errcode.PluginUnregisterFailed,
        "error", err.Error())
}
```

`UnregisterPluginForSharedChannels` takes a `pluginID` and deletes all of the
plugin's remotes. Use `UnregisterPluginRemoteForSharedChannels(remoteID)` only
when removing a single remote (config change reconciliation, see below).

**OnConfigurationChange** considerations:

When the configuration changes (connections added/removed/renamed), the plugin
must reconcile registered remotes:

- New connection (no existing `SiteURL` in config): default `SiteURL` to
  `"crossguard:" + Name`, then call `RegisterPluginForSharedChannels` with that
  `SiteURL`. Idempotent re-registration ensures no-op for SiteURLs that already
  have a remote on the server.
- Removed connection: if no other connection in either direction shares the
  removed connection's `SiteURL`, call
  `UnregisterPluginRemoteForSharedChannels(remoteID)` for surgical removal.
  Otherwise, leave the remote in place (it is still in use by the paired
  direction).
- Renamed connection: `SiteURL` is preserved, so no registration change is
  needed. Just update `p.remoteIDs[newName] = oldRemoteID` and remove
  `p.remoteIDs[oldName]`.

**Removed from OnActivate**:
- Retry queue creation and `Start()` call
- `computeRetryMaxAge()` call

**Removed from OnDeactivate**:
- `p.retryQueue.Wait()`

**Removed from Plugin struct**:
- `retryQueue *retryQueue`

### 3. Outbound Flow

Replace the five Mattermost hooks with a single `OnSharedChannelsSyncMsg` hook.

```go
// server/hooks.go (rewritten)

func (p *Plugin) OnSharedChannelsSyncMsg(
    msg *mmModel.SyncMsg,
    rc *mmModel.RemoteCluster,
) (mmModel.SyncResponse, error) {
    if msg == nil || msg.ChannelId == "" {
        return mmModel.SyncResponse{}, nil
    }

    // rc identifies which remote triggered this sync. Find the matching
    // connection by looking up rc.RemoteId in our remoteIDs map.
    connName := p.connNameForRemote(rc.RemoteId)
    if connName == "" {
        p.API.LogWarn("No connection found for remote",
            "error_code", errcode.OutboundSyncMsgNoRemoteMatch,
            "remote_id", rc.RemoteId)
        return mmModel.SyncResponse{}, nil
    }

    // Check if this connection has an outbound provider. Inbound-only
    // connections register a remote (needed for ReceiveSharedChannelSyncMsg)
    // but have no outbound transport. Acknowledge the sync message so the
    // server advances the cursor, but do not attempt to publish.
    if !p.hasOutboundProvider(connName) {
        return buildSyncResponse(msg), nil
    }

    channel, appErr := p.API.GetChannel(msg.ChannelId)
    if appErr != nil {
        return mmModel.SyncResponse{}, fmt.Errorf("channel lookup failed: %s", appErr.Error())
    }

    team, appErr := p.API.GetTeam(channel.TeamId)
    if appErr != nil {
        return mmModel.SyncResponse{}, fmt.Errorf("team lookup failed: %s", appErr.Error())
    }

    env := &TransportEnvelope{
        Version:     1,
        Type:        TransportTypeSyncMsg,
        ConnName:    connName,
        TeamName:    team.Name,
        ChannelName: channel.Name,
        SyncMsg:     msg,
    }

    if err := p.publishToOutboundConn(p.ctx, env, connName); err != nil {
        // Return error so the server does not advance the cursor for this
        // remote. The server will retry the batch.
        return mmModel.SyncResponse{}, fmt.Errorf("publish failed for %s: %w", connName, err)
    }

    return buildSyncResponse(msg), nil
}
```

**`connNameForRemote`** is a helper that iterates `p.remoteIDs` to find the connection
name for a given remote ID. Since the map is small (typically 1-3 entries), a linear
scan is fine.

**`hasOutboundProvider`** checks whether a named connection has an outbound provider.
Returns false for inbound-only connections. This is a simple lookup in `p.outboundConns`.

**`publishToOutboundConn`** publishes to a single named connection (not all connections
for the channel). The server calls `OnSharedChannelsSyncMsg` once per remote, so each
call targets exactly one outbound provider. This eliminates the need to set `ConnName`
per-connection in a loop and avoids the race condition of mutating a shared envelope.

**`buildSyncResponse`** constructs the `SyncResponse` that tells the server which
timestamps have been processed:

```go
func buildSyncResponse(msg *mmModel.SyncMsg) mmModel.SyncResponse {
    resp := mmModel.SyncResponse{}

    for _, post := range msg.Posts {
        if post.UpdateAt > resp.LastSyncAt {
            resp.LastSyncAt = post.UpdateAt
        }
    }
    for _, reaction := range msg.Reactions {
        if reaction.UpdateAt > resp.LastSyncAt {
            resp.LastSyncAt = reaction.UpdateAt
        }
    }
    // Include Users, Statuses, MembershipChanges, Acknowledgements
    // timestamps as well, using the same max-UpdateAt pattern.

    return resp
}
```

Because the hook now returns an error on publish failure, the server does not advance
the cursor. On retry, the server re-delivers the same batch. The outbound provider
receives the duplicate, but this is safe because `SyncMsg` content is idempotent
(posts are upserted by ID on the receiving side via `ReceiveSharedChannelSyncMsg`).

**publishToOutboundConn** replaces `publishToOutbound` (`connections.go`):
- Accepts `*TransportEnvelope` and a connection name
- Publishes to a single outbound provider (the one matching the connection name)
- `ConnName` is already set on the envelope by the hook
- Serializes to XML via `MarshalEnvelope`
- Handles message splitting via `splitTransportEnvelope` for size-limited providers
- Returns an error if publish fails (so the hook can propagate it to the server)
- Health tracking remains unchanged

**Removed hooks**:
- `MessageHasBeenPosted`
- `MessageHasBeenUpdated`
- `MessageHasBeenDeleted`
- `ReactionHasBeenAdded`
- `ReactionHasBeenRemoved`

**Removed functions**:
- `isChannelRelayEnabled()` (no longer needed; server only calls the hook for shared channels)
- `relayToOutbound()` (semaphore wrapper; replaced by direct call from hook)
- `publishToOutbound()` (multi-connection fan-out; replaced by `publishToOutboundConn`)
- `buildPostEnvelope()`, `buildDeleteEnvelope()`, `buildReactionEnvelope()` (Envelope builders)
- `buildTestMessage()` (replaced by `TransportEnvelope` with `Type: "test"`)
- `uploadPostFiles()` (file sync is stubbed)
- `splitMessage()` (text splitting; replaced by `splitTransportEnvelope`)

### 4. Inbound Flow

Replace `handleInboundMessage()` and all downstream handlers with a single handler
that deserializes the `TransportEnvelope` and calls `ReceiveSharedChannelSyncMsg`.

**Design choice: synchronous handler with error propagation.** The handler passed to
`QueueProvider.Subscribe` must be synchronous and return errors that reflect the actual
processing outcome. The `QueueProvider` contract states that a nil return means
"processed" (provider may ack/delete) and a non-nil return means "not processed"
(provider should redeliver if possible). An async handler that always returns nil
breaks this contract for ack-based providers (Azure Queue and Azure Service Bus
today, NATS JetStream in the future). Core NATS ignores the return value, so
synchronous processing has no downside there. The semaphore blocks rather than
drops when full, applying backpressure to the provider instead of silently
losing messages.

```go
// server/inbound.go (rewritten, dramatically smaller)

func (p *Plugin) handleInboundMessage(connName string) func(data []byte) error {
    return func(data []byte) error {
        // Block until a semaphore slot is available. This applies
        // backpressure to the provider rather than dropping messages,
        // which is important for ack-based providers (Azure Queue,
        // Azure Service Bus, future JetStream) where a drop would
        // ack a lost message.
        select {
        case p.relaySem <- struct{}{}:
            defer func() { <-p.relaySem }()
        case <-p.ctx.Done():
            return p.ctx.Err()
        }

        return p.processInboundMessage(connName, data)
    }
}

func (p *Plugin) processInboundMessage(connName string, data []byte) error {
    env, err := UnmarshalEnvelope(data)
    if err != nil {
        p.API.LogError("Failed to unmarshal transport envelope",
            "error_code", errcode.InboundUnmarshalFailed,
            "conn_name", connName, "error", err.Error())
        // Return nil: a malformed message will never succeed on retry.
        return nil
    }

    switch env.Type {
    case TransportTypeSyncMsg:
        return p.handleInboundSyncMsg(connName, env)
    case TransportTypeTest:
        p.API.LogInfo("Received test message",
            "error_code", errcode.InboundTestReceived,
            "conn_name", connName, "test_id", env.TestID)
        return nil
    case TransportTypeAttachment:
        p.API.LogDebug("Attachment sync not yet implemented",
            "error_code", errcode.InboundAttachmentStubbed, "conn_name", connName)
        return nil
    case TransportTypeProfileImage:
        p.API.LogDebug("Profile image sync not yet implemented",
            "error_code", errcode.InboundProfileImageStubbed, "conn_name", connName)
        return nil
    default:
        p.API.LogWarn("Unknown transport message type",
            "error_code", errcode.InboundUnknownType,
            "conn_name", connName, "type", env.Type)
        return nil
    }
}

func (p *Plugin) handleInboundSyncMsg(connName string, env *TransportEnvelope) error {
    if env.SyncMsg == nil {
        return nil
    }

    // Guard: verify this connection is configured as inbound. Data arrives
    // from an external transport, so we must not assume it is well-formed
    // or targeted correctly. Reject messages for connections that have no
    // inbound provider configured (e.g. outbound-only connections).
    if !p.hasInboundProvider(connName) {
        p.API.LogWarn("Received sync message for non-inbound connection",
            "error_code", errcode.InboundNotConfigured, "conn_name", connName)
        return nil
    }

    // Look up local channel by team name + channel name
    team, appErr := p.API.GetTeamByName(env.TeamName)
    if appErr != nil {
        // Check rewrite index
        teamID, err := p.kvstore.GetTeamRewriteIndex(connName, env.TeamName)
        if err != nil || teamID == "" {
            p.handleUnlinkedInbound(connName, env.TeamName)
            return nil
        }
        team, appErr = p.API.GetTeam(teamID)
        if appErr != nil {
            return nil
        }
    }

    channel, appErr := p.API.GetChannelByName(env.ChannelName, team.Id, false)
    if appErr != nil {
        p.handleUnlinkedInboundChannel(connName, env.TeamName, env.ChannelName, team.Id)
        return nil
    }

    // Verify connection is linked
    conns, err := p.kvstore.GetChannelConnections(channel.Id)
    if err != nil || !hasInboundConnection(conns, connName) {
        p.handleUnlinkedInboundChannel(connName, env.TeamName, env.ChannelName, team.Id)
        return nil
    }

    // Rewrite ChannelId to local channel on all types that carry it.
    // SyncMsg contains channel references in multiple nested types.
    env.SyncMsg.ChannelId = channel.Id
    for _, post := range env.SyncMsg.Posts {
        post.ChannelId = channel.Id
    }
    for _, reaction := range env.SyncMsg.Reactions {
        reaction.ChannelId = channel.Id
    }
    for _, mc := range env.SyncMsg.MembershipChanges {
        mc.ChannelId = channel.Id
    }
    for _, ack := range env.SyncMsg.Acknowledgements {
        ack.ChannelId = channel.Id
    }

    // Look up the remoteID for this connection name. Inbound and outbound
    // connections that share the same name share the same remoteID.
    remoteID := p.remoteIDs[connName]
    if remoteID == "" {
        p.API.LogError("No registered remote for inbound connection",
            "error_code", errcode.InboundNoRemoteForConn, "conn_name", connName)
        return nil
    }

    resp, err := p.API.ReceiveSharedChannelSyncMsg(remoteID, env.SyncMsg)
    if err != nil {
        p.API.LogError("Failed to receive sync message",
            "error_code", errcode.InboundReceiveSyncFailed,
            "conn_name", connName, "channel_id", channel.Id, "error", err.Error())
        // Return the error so ack-based providers can redeliver.
        return fmt.Errorf("ReceiveSharedChannelSyncMsg failed: %w", err)
    }

    if len(resp.PostErrors) > 0 {
        p.API.LogWarn("Some posts failed to sync",
            "error_code", errcode.InboundPostSyncErrors,
            "conn_name", connName, "errors", strings.Join(resp.PostErrors, ", "))
    }
    return nil
}
```

**Removed from inbound.go**:
- `resolveTeamAndChannel()` (replaced by simple team/channel name lookup above)
- `findTeamByRewrite()` (inlined in `handleInboundSyncMsg`)
- `handleInboundPost()`, `handleInboundUpdate()`, `handleInboundDelete()`,
  `handleInboundReaction()` (all replaced by `ReceiveSharedChannelSyncMsg`)
- `watchFiles()`, `handleInboundFile()` (file sync stubbed)
- Async `wg.Go` dispatch pattern (handler is now synchronous; provider controls
  concurrency, not the plugin)

**Removed from plugin struct**:
- `fileWatcherWg sync.WaitGroup` (no file watchers)
- `fileSem chan struct{}` (no file semaphore; re-add when file sync is implemented)

The `inboundConn` struct loses `fileTransferEnabled`, `fileFilterMode`,
`fileFilterTypes` for now (re-add with file sync).

### 5. Shared Channel Hooks (Stubs)

```go
// server/hooks.go

func (p *Plugin) OnSharedChannelsAttachmentSyncMsg(
    fi *mmModel.FileInfo,
    post *mmModel.Post,
    rc *mmModel.RemoteCluster,
) error {
    p.API.LogDebug("Outbound attachment sync not yet implemented",
        "error_code", errcode.OutboundAttachmentStubbed,
        "file_id", fi.Id, "post_id", post.Id)
    return nil
}

func (p *Plugin) OnSharedChannelsProfileImageSyncMsg(
    user *mmModel.User,
    rc *mmModel.RemoteCluster,
) error {
    p.API.LogDebug("Outbound profile image sync not yet implemented",
        "error_code", errcode.OutboundProfileImageStubbed,
        "user_id", user.Id)
    return nil
}

func (p *Plugin) OnSharedChannelsPing(rc *mmModel.RemoteCluster) bool {
    // rc identifies which remote is being pinged. Find the matching
    // connection and report its provider health.
    connName := p.connNameForRemote(rc.RemoteId)
    if connName == "" {
        // Unknown remote. This could be a stale registration.
        return false
    }

    // For inbound-only connections, there is no outbound provider to
    // health-check. Return true so the server continues delivering
    // sync messages (which the hook acknowledges to advance the cursor).
    // Returning false would cause the server to mark the remote as
    // offline and stop sync delivery.
    if !p.hasOutboundProvider(connName) {
        return true
    }

    p.outboundMu.RLock()
    defer p.outboundMu.RUnlock()
    for _, conn := range p.outboundConns {
        if conn.name == connName {
            return conn.healthy
        }
    }
    return false
}
```

Return `nil` from attachment/profile image hooks so the server advances the cursor.
These stubs log at Debug level. The ping hook returns the health of the specific
provider associated with the pinged remote. If the remote does not map to any known
connection, it returns false.

### 6. Service Layer Changes

**initTeamForCrossGuard** (`service.go`):

Current behavior: adds TeamConnection to KV store, adds to initialized teams list,
posts announcement.

New behavior: same KV tracking (still needed for connection routing), but also:

```go
// After adding team connection to KV, no additional shared channel calls needed
// at team level. Sharing is per-channel.
```

Team init remains a grouping concept for the UI and for routing. The actual shared
channel operations happen at channel init.

**initChannelForCrossGuard** (`service.go`):

Current behavior: adds ChannelConnection to KV store, updates channel header, publishes
WebSocket event, posts announcement.

New behavior: same KV tracking, plus:

```go
// Share the channel if not already shared
_, err := p.API.ShareChannel(&mmModel.SharedChannel{
    ChannelId:        channelID,
    TeamId:           teamID,
    Home:             true,
    ShareName:        channel.Name,
    ShareDisplayName: channel.DisplayName,
    CreatorId:        userID,
})
if err != nil {
    // Channel may already be shared; log but don't fail
    p.API.LogDebug("ShareChannel returned error (may already be shared)",
        "error_code", errcode.ServiceShareChannelFailed,
        "channel_id", channelID, "error", err.Error())
}

// Invite each remote (one per outbound connection) to this channel.
// Only connections that are linked to this channel (from KV store
// channel connections) get invited.
for connName, remoteID := range p.remoteIDs {
    if !hasOutboundConnection(channelConns, connName) {
        continue
    }
    err = p.API.InviteRemoteToChannel(channelID, remoteID, userID, true)
    if err != nil {
        p.API.LogError("Failed to invite remote to channel",
            "error_code", errcode.ServiceInviteRemoteFailed,
            "channel_id", channelID, "conn_name", connName, "error", err.Error())
        return err
    }
}
```

**teardownChannelForCrossGuard** (`service.go`):

After removing the connection from KV, uninvite the remote:

```go
// Uninvite all remotes from this channel
for connName, remoteID := range p.remoteIDs {
    err = p.API.UninviteRemoteFromChannel(channelID, remoteID)
    if err != nil {
        p.API.LogWarn("Failed to uninvite remote from channel",
            "error_code", errcode.ServiceUninviteRemoteFailed,
            "channel_id", channelID, "conn_name", connName, "error", err.Error())
    }
}

// If no connections remain, unshare the channel
if len(remainingConns) == 0 {
    _, err = p.API.UnshareChannel(channelID)
    if err != nil {
        p.API.LogWarn("Failed to unshare channel",
            "error_code", errcode.ServiceUnshareFailed,
            "channel_id", channelID, "error", err.Error())
    }
}
```

**teardownTeamForCrossGuard** (`service.go`):

Iterate all channels with connections for this team and call teardown on each
(which handles unshare/uninvite). Then remove team from KV.

### 7. Store Layer Changes

**Methods to remove** (no longer needed):

| Method | Reason |
|---|---|
| `SetPostMapping` | Server tracks post ID mappings internally |
| `GetPostMapping` | Server tracks post ID mappings internally |
| `DeletePostMapping` | Server tracks post ID mappings internally |
| `SetDeletingFlag` | Server handles delete idempotency |
| `IsDeletingFlagSet` | Server handles delete idempotency |
| `ClearDeletingFlag` | Server handles delete idempotency |

Remove from `store.go` (interface), `client.go` (implementation), `caching.go` (no
caching changes needed since these were not cached).

**Methods to keep** (still needed):

| Method | Reason |
|---|---|
| Team connection CRUD (8 methods) | Routing: maps channels to transport connections |
| Channel connection CRUD (6 methods) | Routing: same |
| Initialized teams (3 methods) | UI: status display |
| Team/channel prompts (8 methods) | Prompt system still needed |
| Team rewrite index (3 methods) | Team name remapping still needed |

**Test updates**: Remove `TestSetPostMapping`, `TestGetPostMapping`,
`TestDeletePostMapping` from `store/client_test.go`. Remove the `postMappings` and
`deletingFlags` maps from the `testKVStore` and `flexibleKVStore` test helpers.

### 8. Configuration Changes

**plugin.json**:
- Update `min_server_version` from `"6.2.1"` to `"11.7.0"`

**configuration.go**:
- Remove `MessageFormat` from `ConnectionConfig` (wire format is always XML, not
  per-connection configurable)
- Add `SiteURL string` field to `ConnectionConfig`. In `validate()` (or the
  config save path), populate `SiteURL` with the default `"crossguard:" + Name`
  when empty. Once set, never overwrite it on subsequent config saves: this is
  what makes `SiteURL` immutable across renames and the stable identity for the
  registered remote (preserving `remoteID` and sync cursor across renames)
- Remove retry queue max age recalculation from `OnConfigurationChange()`
  (`computeRetryMaxAge()` itself lives in `retry_dispatch.go` and is deleted with
  that file)
- Keep `FileTransferEnabled`, `FileFilterMode`, `FileFilterTypes` in config struct
  (used when file sync is implemented, and needed for status display)
- Relax connection name uniqueness from global to per-direction. Change `validate()`
  to use separate `allNames` maps for inbound and outbound instead of a single shared
  map. This allows an inbound and outbound connection to share the same name, which
  defaults their `SiteURL` to the same value and pairs them as a single logical
  remote (shared `remoteID` for registration, loop prevention, and synthetic
  user tagging)
- Add config-change reconciliation: collect unique `SiteURL` values across both
  directions, register any new ones via `RegisterPluginForSharedChannels` (idempotent
  for unchanged), and unregister removed ones via
  `UnregisterPluginRemoteForSharedChannels(remoteID)` (only when no remaining
  connection in either direction still references that `SiteURL`)

**outboundConn struct** (`connections.go`):
- Remove `messageFormat string` field (always XML)

### 9. Error Code Changes

**New error code range**: `plugin.go` gets 24000-24999 (new range, since it had no
codes previously).

**New codes needed** (add to `errcode/codes.go`):

| Code | Name | File |
|---|---|---|
| 24000 | `PluginRegisterFailed` | plugin.go |
| 24001 | `PluginUnregisterFailed` | plugin.go |
| 10100 | `OutboundAttachmentStubbed` | hooks.go |
| 10101 | `OutboundProfileImageStubbed` | hooks.go |
| 10102 | `OutboundSyncMsgPublishFailed` | hooks.go |
| 10103 | `OutboundSyncMsgChannelLookupFailed` | hooks.go |
| 10104 | `OutboundSyncMsgNoRemoteMatch` | hooks.go |
| 15100 | `InboundReceiveSyncFailed` | inbound.go |
| 15101 | `InboundPostSyncErrors` | inbound.go |
| 15102 | `InboundAttachmentStubbed` | inbound.go |
| 15103 | `InboundProfileImageStubbed` | inbound.go |
| 15104 | `InboundUnknownType` | inbound.go |
| 15105 | `InboundNoRemoteForConn` | inbound.go |
| 15106 | `InboundNotConfigured` | inbound.go |
| 14100 | `ServiceShareChannelFailed` | service.go |
| 14101 | `ServiceInviteRemoteFailed` | service.go |
| 14102 | `ServiceUninviteRemoteFailed` | service.go |
| 14103 | `ServiceUnshareFailed` | service.go |

**Codes to remove**: All codes referencing removed functions (hooks relay codes, inbound
post/update/delete/reaction handler codes, retry dispatch codes, sync user codes). Keep
codes for functions that remain (connections, configuration, service, prompts, providers,
store caching). Exact list determined during implementation by removing references and
running `TestAllCodesComplete`.

### 10. Prompt System Changes

The prompt system (`prompt.go`) remains largely intact. Changes:

- `handleUnlinkedInbound()`: When a prompt is accepted, instead of calling
  `initTeamForCrossGuard()` alone, the accept handler now triggers the shared channel
  setup (ShareChannel + InviteRemoteToChannel) via the updated `initChannelForCrossGuard`.
- The prompt system still posts interactive messages and stores state in KV.
- Team-level prompts still exist for the initial team link approval.
- Channel-level prompts still exist for per-channel approval.

No structural changes. The service layer changes (section 6) propagate through the
existing prompt accept handlers.

## Files to Modify / Create / Remove

### Files to remove

| File | Reason |
|---|---|
| `server/model/message.go` | Custom Envelope type replaced by TransportEnvelope |
| `server/model/post_message.go` | PostMessage, DeleteMessage, ReactionMessage no longer needed |
| `server/model/test_message.go` | TestMessage replaced by TransportEnvelope.TestID |
| `server/model/message_test.go` | Tests for removed types |
| `server/model/post_message_test.go` | Tests for removed types |
| `server/model/test_message_test.go` | Tests for removed types |
| `server/sync_user.go` | Server manages remote users natively |
| `server/sync_user_test.go` | Tests for removed code |
| `server/retry_queue.go` | Server's cursor-based sync replaces retry queue |
| `server/retry_queue_test.go` | Tests for removed code |
| `server/retry_dispatch.go` | Retry dispatch logic no longer needed |
| `server/retry_dispatch_test.go` | Tests for removed code |

### Files to create

| File | Purpose |
|---|---|
| `server/transport.go` | `TransportEnvelope` type, serialization, `splitTransportEnvelope()` |
| `server/transport_test.go` | Tests for wire format, serialization, splitting |

### Files to modify

| File | Changes |
|---|---|
| `server/plugin.go` | Add `remoteIDs map[string]string` field, `connNameForRemote` helper, `hasOutboundProvider` helper, and `hasInboundProvider` helper. OnActivate: collect unique connection names from both inbound and outbound, register one remote per unique name via `RegisterPluginForSharedChannels`, remove retry queue init. OnDeactivate: unregister all remotes, remove retry queue wait. Remove `retryQueue`, `fileSem`, `fileWatcherWg` fields. |
| `server/hooks.go` | Replace five MM hooks with `OnSharedChannelsSyncMsg`, `OnSharedChannelsAttachmentSyncMsg` (stub), `OnSharedChannelsProfileImageSyncMsg` (stub), `OnSharedChannelsPing`. Remove `isChannelRelayEnabled`, `relayToOutbound`. |
| `server/hooks_test.go` | Rewrite: test `OnSharedChannelsSyncMsg` with mock API and providers. Remove tests for old hooks. |
| `server/inbound.go` | Rewrite: `handleInboundMessage` deserializes `TransportEnvelope`, looks up `remoteID` from `p.remoteIDs[connName]`, and calls `ReceiveSharedChannelSyncMsg(remoteID, msg)`. Remove all handler functions, file watching, team/channel resolution. Dramatically smaller. |
| `server/inbound_test.go` | Rewrite: test `processInboundMessage` and `handleInboundSyncMsg` with mock `ReceiveSharedChannelSyncMsg`. Remove tests for old handlers. Remove `testKVStore`, `rewriteTestKVStore`, `unlinkTestKVStore` (replaced by `flexibleKVStore`). |
| `server/connections.go` | Replace `publishToOutbound` with `publishToOutboundConn` (single-connection publish). Remove `buildPostEnvelope`, `buildDeleteEnvelope`, `buildReactionEnvelope`, `buildTestMessage`, `uploadPostFiles`, `splitMessage`. Simplify `outboundConn` struct (remove `messageFormat`). |
| `server/connections_test.go` | Update tests for new `publishToOutbound` signature. Remove `makeEnvelope`, `measureOverhead`, envelope builder tests, split message tests. Add `splitTransportEnvelope` tests (or keep in `transport_test.go`). |
| `server/service.go` | `initChannelForCrossGuard`: add `ShareChannel` + `InviteRemoteToChannel` calls. `teardownChannelForCrossGuard`: add `UninviteRemoteFromChannel` + `UnshareChannel` calls. |
| `server/service_test.go` | Add mock expectations for `ShareChannel`, `InviteRemoteToChannel`, `UninviteRemoteFromChannel`, `UnshareChannel`. |
| `server/api.go` | `handleTestConnection`: use `TransportEnvelope` with `Type: "test"` instead of `model.Envelope` with `TestMessage`. |
| `server/api_test.go` | Update test-connection tests for new wire format. |
| `server/command.go` | No structural changes. Commands call service layer which handles shared channel ops. |
| `server/command_test.go` | Add mock expectations for shared channel API calls triggered by init/teardown commands. |
| `server/prompt.go` | No structural changes. Accept handlers call updated service layer. |
| `server/prompt_test.go` | Add mock expectations for shared channel API calls triggered by prompt acceptance. |
| `server/configuration.go` | Remove `MessageFormat` from `ConnectionConfig`. Remove `computeRetryMaxAge()`. Remove retry queue recalc from `OnConfigurationChange()`. Relax `validate()` name uniqueness from global to per-direction (separate `allNames` maps for inbound and outbound). |
| `server/configuration_test.go` | Remove tests for `MessageFormat` validation and `computeRetryMaxAge`. Add test that same connection name in inbound and outbound passes validation. Add test that duplicate names within the same direction still fail. |
| `server/store/store.go` | Remove `SetPostMapping`, `GetPostMapping`, `DeletePostMapping`, `SetDeletingFlag`, `IsDeletingFlagSet`, `ClearDeletingFlag` from interface. |
| `server/store/client.go` | Remove implementations of removed methods. Remove `postMappingPrefix` and `deletingFlagPrefix` constants. |
| `server/store/client_test.go` | Remove tests for removed methods. |
| `server/store/caching.go` | No changes (removed methods were not cached). |
| `server/store/caching_test.go` | No changes expected. |
| `server/errcode/codes.go` | Add new codes per section 9. Remove codes for deleted files/functions. Update `AllCodes` slice. |
| `server/errcode/codes_test.go` | No changes (tests are auto-verifying via AST). |
| `server/test_helpers_test.go` | Remove `testKVStore` post mapping and deleting flag methods from `flexibleKVStore`. Update `mockQueueProvider` if needed. |
| `server/plugin_test.go` | Add mock for `RegisterPluginForSharedChannels` in OnActivate tests. Remove retry queue assertions. |
| `plugin.json` | Update `min_server_version` to `"11.7.0"`. Remove `MessageFormat` from connection settings schema if present. |

### Provider files (minimal changes)

| File | Notes |
|---|---|
| `server/nats_provider.go` | Unchanged. Publishes/subscribes raw bytes. |
| `server/nats_test.go` | Unchanged. |
| `server/azure_provider.go` | Unchanged. |
| `server/azure_provider_test.go` | Unchanged. |
| `server/azure_blob_provider.go` | Unchanged. |
| `server/azure_blob_provider_test.go` | Unchanged. |
| `server/azure_servicebus_provider.go` | Unchanged. Already implements the `QueueProvider` contract with ack-based delivery (handler nil → `CompleteMessage`, error → `AbandonMessage`) and a 192 KiB default `MaxMessageSize`. |
| `server/azure_servicebus_provider_test.go` | Unchanged. |
| `server/provider.go` | Comment-only update. Strengthen `Subscribe` handler contract documentation to make explicit that all providers should respect the handler's error return for delivery acknowledgement. This is the universal contract: nil = processed (ack/delete), error = not processed (nack/redeliver if supported). Core NATS ignores the return (fire-and-forget); Azure Queue and Azure Service Bus already respect it; future JetStream will depend on it. No interface signature changes. |

### Frontend files (no changes)

The frontend is unaffected. Backend API response shapes for status, init, teardown
remain compatible. The `connection_state.ts` observer, modals, admin settings, channel
indicator, and user popover are unchanged.

## Prerequisites

See [Server Shared Channels Changes](26-04-12-04-server-shared-channels-changes.md)
for the complete server-side plan. Status of server prerequisites:

- [x] **PR 35962 (inbound `Receive*` APIs)**: **Merged.** `ReceiveSharedChannelSyncMsg`,
  `ReceiveSharedChannelAttachmentSyncMsg`, and `ReceiveSharedChannelProfileImageSyncMsg`
  are available in the server. The signatures were updated in PR 36126 to accept
  a leading `remoteID string` parameter so the plugin specifies which connection
  it is acting as.
- [x] **Phase 1 (XML struct tags)**: **Merged in PR 36126** (commit `81d4fe3793`,
  2026-04-28). `xml` struct tags added to `SyncMsg`, `SyncResponse`,
  `MembershipChangeMsg`, `Post`, `User`, `Reaction`, `Status`,
  `PostAcknowledgement`, `FileInfo`. Custom `MarshalXML`/`UnmarshalXML` for
  `SyncMsg.Users`, `SyncMsg.MentionTransforms`, `StringMap`, `StringInterface`
  (in new `server/public/model/xml_helpers.go`). Sensitive fields excluded via
  `xml:"-"`. JSON serialization unaffected.
- [x] **Phase 2 (multi-remote API)**: **Merged in PR 36126** (and follow-up
  PR 36309 commit `fdaea9dec3` for `CleanRemoteName()` slugification). The
  uniqueness key is `SiteURL` (not a new `PluginName` column): plugins register
  multiple remotes by calling `RegisterPluginForSharedChannels` with distinct
  `SiteURL` values on `RegisterPluginOpts`. New store methods `GetAllByPluginID`
  and `GetBySiteURL` were added; `GetByPluginID` is deprecated. New plugin API
  `UnregisterPluginRemoteForSharedChannels(remoteID)` was added; the existing
  `UnregisterPluginForSharedChannels(pluginID)` now deletes all remotes for the
  plugin (bulk cleanup). `IsPlugin()` was simplified to `PluginID != ""`. No
  database migration was needed.
  - **Deviation from server plan**: empty `SiteURL` is no longer rejected. It
    defaults to the legacy `"plugin_<PluginID>"` value for backward
    compatibility with existing single-remote plugins. **Implication for this
    plan**: Cross Guard MUST set a distinct `SiteURL` on every
    `RegisterPluginOpts`, otherwise all multi-remote registrations collide on
    the same default value and only the first succeeds. See the OnActivate code
    in section 2 below, which has been updated accordingly.

**All prerequisites are met.** Server v11.7+ shipped earlier APIs; the remaining
multi-remote and XML changes are in master and are slated for the next server
release. Plugin development can proceed without local model patches.

## Tasks

1. [ ] Create `server/transport.go` with `TransportEnvelope` type (XML struct tags),
       `MarshalEnvelope`/`UnmarshalEnvelope` functions, `buildSyncResponse()`, and
       `splitTransportEnvelope()`. Create `server/transport_test.go` with XML
       round-trip, splitting, and edge case tests.

2. [ ] Update `server/store/store.go`: remove post mapping and deleting flag methods
       from the `KVStore` interface. Update `client.go` and `client_test.go` to match.

3. [ ] Update `server/plugin.go`: add `remoteIDs map[string]string` field and
       `connNameForRemote` helper. OnActivate: collect unique `SiteURL` values
       across both directions, register one remote per unique `SiteURL` via
       `RegisterPluginForSharedChannels` (passing `opts.SiteURL`), then populate
       `p.remoteIDs[connName] = remoteID` for every configured connection
       (paired connections share a `remoteID`). OnDeactivate: bulk-unregister
       all remotes for this plugin via
       `UnregisterPluginForSharedChannels(manifest.Id)`. Remove `retryQueue`,
       `fileSem`, `fileWatcherWg` fields and their initialization. Update
       `plugin_test.go`.

4. [ ] Rewrite `server/hooks.go`: replace the five Mattermost hooks with
       `OnSharedChannelsSyncMsg` (uses `rc` to route to correct connection),
       `OnSharedChannelsAttachmentSyncMsg` (stub),
       `OnSharedChannelsProfileImageSyncMsg` (stub), and `OnSharedChannelsPing`
       (per-remote health). Rewrite `hooks_test.go`.

5. [ ] Update `server/connections.go`: replace `publishToOutbound` with
       `publishToOutboundConn` (single-connection publish accepting
       `*TransportEnvelope` and connection name). Remove envelope builders,
       `uploadPostFiles`, `splitMessage`, `messageFormat` field. Serialize via
       `MarshalEnvelope` (XML). Update `connections_test.go`.

6. [ ] Rewrite `server/inbound.go`: replace `handleInboundMessage` and all handlers
       with `TransportEnvelope` XML deserialization (`UnmarshalEnvelope`) and
       `ReceiveSharedChannelSyncMsg(remoteID, msg)` call. Look up `remoteID` from
       `p.remoteIDs[connName]` (connection name is the pairing key). Handler must
       be synchronous with error propagation: block on semaphore (not drop),
       `processInboundMessage` and `handleInboundSyncMsg` return errors,
       `ReceiveSharedChannelSyncMsg` failures propagate to the provider so
       ack-based transports can redeliver. Rewrite ChannelId on all nested types
       (Posts, Reactions, MembershipChanges, Acknowledgements). Remove file
       watching. Rewrite `inbound_test.go`.

7. [ ] Update `server/service.go`: add `ShareChannel`/`InviteRemoteToChannel` to
       channel init (inviting each relevant remote) and
       `UninviteRemoteFromChannel`/`UnshareChannel` to channel teardown. Update
       `service_test.go`.

8. [ ] Update `server/prompt.go`: verify accept handlers work with updated service
       layer. Update `prompt_test.go` with shared channel API mock expectations.

9. [ ] Update `server/api.go`: change `handleTestConnection` to use
       `TransportEnvelope`. Update `api_test.go`.

10. [ ] Update `server/configuration.go`: remove `MessageFormat` field. Add
        `SiteURL string` field to `ConnectionConfig` and populate it with
        `"crossguard:" + Name` on first save when empty (immutable thereafter).
        Remove retry queue recalc from `OnConfigurationChange()`. Relax
        `validate()` name uniqueness to per-direction (separate maps for inbound
        and outbound). Add remote registration reconciliation (collect unique
        `SiteURL` values from both directions, register new ones, unregister
        removed ones via `UnregisterPluginRemoteForSharedChannels` only when no
        connection still references that `SiteURL`). Update `configuration_test.go`
        with tests for cross-direction name sharing, same-direction uniqueness,
        `SiteURL` defaulting on first save, and `SiteURL` immutability on
        rename.

11. [ ] Update `server/command_test.go`: add mock expectations for shared channel API
        calls triggered by init/teardown commands.

12. [ ] Update `server/errcode/codes.go`: add new codes (including 24000-24999 range
        for plugin.go), remove codes for deleted functions, update `AllCodes`. Run
        `TestCodesUnique` and `TestAllCodesComplete`.

13. [ ] Delete removed files: `server/model/` (all files), `server/sync_user.go`,
        `server/sync_user_test.go`, `server/retry_queue.go`,
        `server/retry_queue_test.go`, `server/retry_dispatch.go`,
        `server/retry_dispatch_test.go`.

14. [ ] Update `server/test_helpers_test.go`: remove post mapping and deleting flag
        fields from `flexibleKVStore`. Clean up any test infrastructure referencing
        removed types.

15. [ ] Update `plugin.json`: set `min_server_version` to `"11.7.0"`.

16. [ ] Run `make check-style` and `make test`. Fix all issues.

17. [ ] Deploy to Docker dev environment (`make deploy`). Verify message relay works
        end-to-end between Server A and Server B via NATS. Capture sample XML from
        NATS monitor to verify content inspection readability.

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| SyncMsg XML serialization is large for Azure Queue's 48KB limit | XML is more verbose than JSON. `splitTransportEnvelope` splits by post count. Single-post messages that exceed the limit are logged as errors. Azure Blob provider has no limit. |
| `model.SyncMsg.Users` map not natively XML-serializable | `MarshalXML`/`UnmarshalXML` methods on `SyncMsg` handle the map-to-element conversion. Covered by server-side tests in `server/public/model/shared_channel_test.go`. |
| Channel ID rewriting on inbound misses nested references | Rewrite `ChannelId` on `SyncMsg`, `Post`, `Reaction`, `MembershipChange`, and `PostAcknowledgement`. `Status.ActiveChannel` is not rewritten (it is informational, not used for routing by `ReceiveSharedChannelSyncMsg`). |
| Shared channel cursor advances but provider publish fails | Return error from `OnSharedChannelsSyncMsg` so server does not advance cursor. Server retries the batch. Duplicate delivery is safe because `SyncMsg` content is idempotent (posts upserted by ID). |
| Test-connection feature breaks without Envelope type | Replaced by `TransportEnvelope` with `Type: "test"`. Same round-trip test, different wire format. |
| Plugin deployed against pre-11.7 server | `RegisterPluginForSharedChannels` fails, `OnActivate` returns error, plugin does not start. This is intentional (clean break). Admins must upgrade the server before deploying the new plugin version. Documented in release notes. |
| Empty `SiteURL` collides on the server's legacy default | The server defaults empty `SiteURL` to `"plugin_<PluginID>"` for backward compatibility. If two of Cross Guard's connections were registered with empty `SiteURL`, all but the first would collide. Mitigated by always populating `SiteURL` on `ConnectionConfig` (defaulted to `"crossguard:" + Name` on first save), and by registering with `opts.SiteURL` set. |
| Synchronous inbound handler blocks provider thread | The handler blocks on the semaphore instead of dropping messages. For core NATS this means the subscription callback blocks, which applies backpressure to the NATS client (acceptable, NATS buffers pending messages). For Azure Queue the poll loop naturally serializes. When JetStream is added, blocking is correct behavior as it prevents premature ack. If throughput becomes an issue, increase the semaphore size rather than reverting to async dispatch. |

## Testing Plan

### Tests to remove (with their files)

| File | Tests removed | Reason |
|---|---|---|
| `server/model/message_test.go` | All | File deleted |
| `server/model/post_message_test.go` | All | File deleted |
| `server/model/test_message_test.go` | All | File deleted |
| `server/sync_user_test.go` | All | File deleted |
| `server/retry_queue_test.go` | All | File deleted |
| `server/retry_dispatch_test.go` | All | File deleted |
| `server/store/client_test.go` | `TestSetPostMapping`, `TestGetPostMapping`, `TestDeletePostMapping` | Methods removed |
| `server/hooks_test.go` | All existing tests | Hooks completely rewritten |
| `server/inbound_test.go` | All existing tests | Inbound completely rewritten |
| `server/connections_test.go` | `TestBuildPostEnvelope`, `TestBuildDeleteEnvelope`, `TestBuildReactionEnvelope`, `TestBuildTestMessage`, `TestSplitMessage`, `TestMakeEnvelope` | Functions removed |
| `server/configuration_test.go` | MessageFormat validation tests | Field removed (`computeRetryMaxAge` tests are in `retry_dispatch_test.go`, deleted with that file) |

### Tests to add

| File | Tests | What they verify |
|---|---|---|
| `server/transport_test.go` | `TestTransportEnvelopeMarshalRoundTrip` | XML serialization of TransportEnvelope with SyncMsg (including Users map) |
| `server/transport_test.go` | `TestSplitTransportEnvelope` | Splitting by post count, single-post edge case, empty SyncMsg, users-only sync |
| `server/transport_test.go` | `TestSplitTransportEnvelope_UTF8Safety` | Splitting does not break multi-byte characters in post text |
| `server/transport_test.go` | `TestTransportEnvelopeXMLReadable` | Verify XML output is human-readable and parseable by standard XML tools |
| `server/hooks_test.go` | `TestOnSharedChannelsSyncMsg_Success` | Hook receives SyncMsg, uses rc to find connection, publishes to correct provider |
| `server/hooks_test.go` | `TestOnSharedChannelsSyncMsg_NoRemoteMatch` | Returns empty response when rc.RemoteId has no matching connection |
| `server/hooks_test.go` | `TestOnSharedChannelsSyncMsg_PublishError` | Returns error when provider publish fails (cursor not advanced) |
| `server/hooks_test.go` | `TestOnSharedChannelsSyncMsg_NilMsg` | Handles nil SyncMsg gracefully |
| `server/hooks_test.go` | `TestOnSharedChannelsPing_Healthy` | Returns true when the pinged remote's provider is healthy |
| `server/hooks_test.go` | `TestOnSharedChannelsPing_Unhealthy` | Returns false when the pinged remote's provider is unhealthy |
| `server/hooks_test.go` | `TestOnSharedChannelsPing_UnknownRemote` | Returns false for unknown remote ID |
| `server/hooks_test.go` | `TestOnSharedChannelsAttachmentStub` | Stub returns nil |
| `server/hooks_test.go` | `TestOnSharedChannelsProfileImageStub` | Stub returns nil |
| `server/hooks_test.go` | `TestOnSharedChannelsSyncMsg_InboundOnly` | Inbound-only connection: hook returns successful SyncResponse without publishing (cursor advances) |
| `server/hooks_test.go` | `TestOnSharedChannelsPing_InboundOnly` | Inbound-only connection: ping returns true (keeps remote online) |
| `server/inbound_test.go` | `TestProcessInboundMessage_SyncMsg` | Deserializes TransportEnvelope, rewrites ChannelId on all nested types, looks up remoteID, calls ReceiveSharedChannelSyncMsg |
| `server/inbound_test.go` | `TestProcessInboundMessage_Test` | Handles test message type |
| `server/inbound_test.go` | `TestProcessInboundMessage_UnknownType` | Logs warning for unknown type |
| `server/inbound_test.go` | `TestProcessInboundMessage_InvalidXML` | Handles unmarshal failure |
| `server/inbound_test.go` | `TestHandleInboundSyncMsg_UnlinkedTeam` | Triggers prompt for unknown team |
| `server/inbound_test.go` | `TestHandleInboundSyncMsg_UnlinkedChannel` | Triggers prompt for unknown channel |
| `server/inbound_test.go` | `TestHandleInboundSyncMsg_RewriteIndex` | Uses rewrite index to resolve team |
| `server/inbound_test.go` | `TestHandleInboundSyncMsg_SendErrors` | Logs partial sync errors |
| `server/inbound_test.go` | `TestHandleInboundSyncMsg_ReceiveFails_ReturnsError` | `ReceiveSharedChannelSyncMsg` failure propagates as error return (enables provider redelivery) |
| `server/inbound_test.go` | `TestHandleInboundMessage_SemaphoreBackpressure` | Handler blocks when semaphore is full rather than dropping; resumes when slot freed |
| `server/inbound_test.go` | `TestProcessInboundMessage_MalformedXML_ReturnsNil` | Malformed XML returns nil (permanent failure, no redelivery) |
| `server/service_test.go` | `TestInitChannel_SharesChannel` | Verifies ShareChannel + InviteRemoteToChannel called |
| `server/service_test.go` | `TestTeardownChannel_UnsharesChannel` | Verifies UninviteRemoteFromChannel + UnshareChannel called |
| `server/inbound_test.go` | `TestHandleInboundSyncMsg_NoRemoteForConn` | Returns nil (permanent failure) when connName has no registered remoteID |
| `server/inbound_test.go` | `TestHandleInboundSyncMsg_OutboundOnlyConn` | Rejects sync message for a connection with no inbound provider configured |
| `server/plugin_test.go` | `TestOnActivate_RegistersMultipleRemotes` | Verifies RegisterPluginForSharedChannels called once per unique SiteURL (not once per connection) |
| `server/plugin_test.go` | `TestOnActivate_SharedSiteURLRegistersOnce` | Inbound and outbound with the same SiteURL result in a single RegisterPluginForSharedChannels call; both connNames map to the same remoteID |
| `server/plugin_test.go` | `TestOnDeactivate_UnregistersAllRemotes` | Verifies a single UnregisterPluginForSharedChannels(pluginID) bulk call |
| `server/plugin_test.go` | `TestConnNameForRemote` | Maps remote ID back to connection name |
| `server/connections_test.go` | `TestPublishToOutboundConn_TransportEnvelope` | Publishes TransportEnvelope to a single named provider |
| `server/configuration_test.go` | `TestValidate_SameNameAcrossDirections` | Inbound "high" and outbound "high" passes validation (per-direction uniqueness) and defaults to the same SiteURL |
| `server/configuration_test.go` | `TestValidate_DuplicateNameSameDirection` | Two inbound connections both named "high" fails validation |
| `server/configuration_test.go` | `TestValidate_SiteURLDefaultedOnFirstSave` | New connection with empty SiteURL gets `"crossguard:" + Name` as default |
| `server/configuration_test.go` | `TestValidate_SiteURLImmutableOnRename` | Renaming a connection (Name change) does not modify its SiteURL |

### Tests to modify

| File | Tests | Changes |
|---|---|---|
| `server/api_test.go` | `TestHandleTestConnection_*` | Use TransportEnvelope instead of Envelope for test-connection |
| `server/command_test.go` | `TestExecuteCommand_InitChannel` | Add mock expectations for ShareChannel, InviteRemoteToChannel |
| `server/command_test.go` | `TestExecuteCommand_TeardownChannel` | Add mock expectations for UninviteRemoteFromChannel |
| `server/prompt_test.go` | `TestHandlePromptAccept` | Add mock expectations for ShareChannel, InviteRemoteToChannel |
| `server/test_helpers_test.go` | `flexibleKVStore` | Remove post mapping and deleting flag function pointers |

### Mock changes

The `plugintest.API` mock needs expectations for new API methods:

```go
// RegisterPluginForSharedChannels is called once per unique connection name
// (shared across inbound and outbound). Each call returns a unique remote ID.
callCount := 0
api.On("RegisterPluginForSharedChannels", mock.AnythingOfType("model.RegisterPluginOpts")).
    Return(func(opts mmModel.RegisterPluginOpts) string {
        callCount++
        return fmt.Sprintf("remote-id-%d", callCount)
    }, nil)
api.On("UnregisterPluginForSharedChannels", mock.AnythingOfType("string")).Return(nil)
api.On("ShareChannel", mock.AnythingOfType("*model.SharedChannel")).
    Return(&mmModel.SharedChannel{}, nil)
api.On("InviteRemoteToChannel", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
    Return(nil)
api.On("UninviteRemoteFromChannel", mock.Anything, mock.Anything).Return(nil)
api.On("UnshareChannel", mock.Anything).Return(true, nil)
api.On("ReceiveSharedChannelSyncMsg", mock.AnythingOfType("string"), mock.AnythingOfType("*model.SyncMsg")).
    Return(mmModel.SyncResponse{}, nil)
```

## Acceptance Criteria

- [ ] Shared Channels Plugin API supports multiple remotes per plugin with per-remote hooks
- [ ] Mattermost server model types have `xml` struct tags and `SyncMsg` XML round-trips correctly
- [ ] Plugin activates successfully and registers one remote per unique connection name
- [ ] Inbound and outbound connections with the same name share a single `remoteID`
- [ ] Plugin deactivates cleanly and unregisters all remotes
- [ ] Inbound-only connections: outbound hooks acknowledge sync messages (cursor
      advances) without publishing; ping returns true (remote stays online)
- [ ] Outbound: posts, edits, deletes, and reactions made in a shared channel are
      serialized to XML and published to outbound providers
- [ ] Inbound: XML `TransportEnvelope` messages received from providers are deserialized
      and pushed into Mattermost via `ReceiveSharedChannelSyncMsg(remoteID, msg)`
- [ ] Wire format is valid, human-readable XML parseable by standard XML tools
- [ ] Channel init creates a shared channel and invites the remote
- [ ] Channel teardown uninvites the remote and unshares if last connection
- [ ] Team rewrite index still works for inbound team name remapping
- [ ] Prompt system works: unlinked inbound messages trigger accept/block prompts
- [ ] Test-connection round-trip works with the new wire format
- [ ] Attachment and profile image hooks are stubbed and log at Debug level
- [ ] `make check-style` passes
- [ ] `make test` passes with all new and modified tests
- [ ] Docker dev environment: messages relay between Server A and Server B

## Decisions

| Question | Decision | Rationale |
|---|---|---|
| Wire format | XML `TransportEnvelope` wrapping `model.SyncMsg` | Customer content inspection tooling requires XML for all cross-domain traffic. `xml` struct tags added to server model types so `encoding/xml` marshals `SyncMsg` directly. |
| XML struct tags location | Added to Mattermost server model types, not wrapper types in the plugin | Keeps plugin in sync automatically when server model fields change. Avoids a parallel set of types and conversion functions. Additive change, does not affect existing JSON serialization. |
| Minimum server version | v11.7.0 (hard requirement) | `ReceiveSharedChannelSyncMsg` requires PR 35962 (now merged). Clean break, no backward-compat shims. |
| File attachment sync | Stubbed with `// TODO` and Debug-level logging | Cannot be tested end-to-end without implementing both sides. Follow-up plan. |
| Post mapping and delete flag KV operations | Removed | Server's shared channel service tracks post ID mappings and handles delete idempotency internally. |
| Sync user creation code | Removed | Server manages remote users natively via `RemoteId` on user records. |
| Retry queue | Removed | Server's cursor-based sync replays missed changes. No need for in-plugin retry. |
| MessageFormat config field | Removed | Wire format is always XML, not per-connection configurable. |
| Team-level connection tracking | Kept | Still needed for UI grouping and for routing inbound messages to the correct channel lookup scope. |
| Channel-level connection tracking | Kept | Still needed for routing: determines which providers to publish to for a given channel. |
| Health reporting (OnSharedChannelsPing) | Per-remote: returns the health of the specific provider associated with the pinged remote | Each remote maps to one outbound connection. The server pings each remote independently, so health reporting is precise per-connection. |
| Registration model | One remote per unique `SiteURL`, shared across inbound and outbound (multi-remote) | `SiteURL` is the pairing key and the dedup key the server uses. Connections with the same `SiteURL` share a single `remoteID`. This is required for loop prevention (content from a remote is not sent back to that remote) and synthetic user tagging (`RemoteId` on users must match across both directions). Requires server API change ([Server plan, Phase 2](26-04-12-04-server-shared-channels-changes.md)). |
| Connection name uniqueness | Per-direction (not global) | Relaxed from the current global uniqueness so that an inbound and outbound can share the same `Name`. When they do, they default to the same `SiteURL` and are paired as one remote. Names are still unique within inbound and within outbound. Asymmetric configs (outbound-only, inbound-only) are supported. |
| `SiteURL` default and lifecycle | Default `"crossguard:" + Name` on first save; immutable thereafter (persisted in `ConnectionConfig`) | Tying `SiteURL` to a connection's initial `Name` keeps the value human-readable for debugging. Persisting it as an immutable field means renames do not change remote identity (server keeps the same `remoteID` and sync cursor). It also means a previously-paired inbound/outbound stay paired even if one side is renamed (pairing follows `SiteURL`, not current `Name`). To deliberately unpair, the user edits `SiteURL` directly or removes and re-adds the connection. |
| Inbound-only hook behavior | Acknowledge and advance cursor, do not publish | Inbound-only connections register a remote (needed for `ReceiveSharedChannelSyncMsg` remoteID), so the server calls outbound hooks for them. `OnSharedChannelsSyncMsg` checks `hasOutboundProvider` and returns a successful `SyncResponse` without publishing if no outbound provider exists. This advances the cursor so it does not get stuck. `OnSharedChannelsPing` returns true for inbound-only connections so the server does not mark the remote as offline. |
| Outbound-only inbound guard | Reject inbound sync messages for connections with no inbound provider | Data arrives from external transport and cannot be trusted. `handleInboundSyncMsg` checks `hasInboundProvider(connName)` before processing. Although outbound-only connections should never have a subscription set up, the guard provides defense in depth against misconfiguration or a misbehaving remote. |
| SyncMsg splitting for size-limited providers | Split by reducing posts per envelope | Keeps the full Users map in each split (needed for post context). Reactions included only with their associated posts. |
| Inbound handler design | Synchronous with error propagation, blocking semaphore | The handler's return value is the delivery acknowledgement signal per the `QueueProvider.Subscribe` contract. Async dispatch (goroutine + return nil) breaks this contract for ack-based providers (Azure Queue and Azure Service Bus today, NATS JetStream in the future). Blocking on a full semaphore applies backpressure instead of silently dropping messages. `processInboundMessage` and `handleInboundSyncMsg` return errors so that transient failures (e.g. `ReceiveSharedChannelSyncMsg` error) can trigger redelivery. Permanent failures (malformed XML, unknown type, unlinked team/channel) return nil since retrying would not help. |
