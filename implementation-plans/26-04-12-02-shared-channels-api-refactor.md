# Cross Guard: Refactor to Shared Channels Plugin APIs

## Context

Cross Guard currently captures content changes via five standard plugin hooks
(`MessageHasBeenPosted`, `MessageHasBeenUpdated`, `MessageHasBeenDeleted`,
`ReactionHasBeenAdded`, `ReactionHasBeenRemoved`) and handles inbound message delivery
manually (team/channel resolution, user creation, post CRUD, idempotency tracking, retry
queue). The Mattermost Shared Channels plugin APIs provide a purpose-built alternative:
the server packages up all content changes reliably with cursor-based tracking and
delivers them to the plugin, and (with PR 35962 merged) the plugin can push received
content back into Mattermost via `SendSharedChannelSyncMsg`.

This refactor replaces the custom hook-based capture and manual inbound handling with the
Shared Channels APIs while keeping the transport layer (NATS, Azure Queue, Azure Blob)
unchanged.

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

| Our Approach | Avoid |
|---|---|
| Delegate content tracking to the server's Shared Channel Service | Reimplementing cursor logic, change detection, or dependency ordering in the plugin |
| Serialize `model.SyncMsg` as XML for transport (customer content inspection requirement) | Defining new custom message types that duplicate what the server already provides |
| Clean break requiring Mattermost v11.7+ | Backward-compatibility shims for older server versions |
| Stub file attachment sync with clear `// TODO` markers | Partially implementing file sync that cannot be tested end-to-end |
| Remove dead code immediately | Leaving unused code behind commented out or gated by flags |

## Requirements

- [ ] Mattermost server model types (`SyncMsg`, `Post`, `User`, etc.) have `xml` struct tags
- [ ] Shared Channels Plugin API supports multiple remotes per plugin with per-remote hooks
- [ ] Plugin registers one remote per outbound connection on activation
- [ ] Wire format between servers is XML (customer content inspection requirement)
- [ ] Outbound: `OnSharedChannelsSyncMsg` serializes to XML and publishes to outbound providers
- [ ] Inbound: XML messages from providers are deserialized and pushed via `SendSharedChannelSyncMsg`
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
   for the complete plan. Both server PRs must merge before this plugin work can land.
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

Cross Guard manages multiple outbound connections (e.g., `nats-low-to-high`,
`azure-low-to-dmz`). Each connection represents a distinct remote endpoint. The
Shared Channels Plugin API must support registering multiple remotes per plugin so
that:

- The server tracks sync cursors independently per remote (each connection syncs at
  its own pace).
- `OnSharedChannelsSyncMsg` receives the `RemoteCluster` identifying which remote
  triggered the sync, so the plugin publishes only to the correct outbound provider.
- `OnSharedChannelsPing` is called per remote, so the plugin can report health for
  each provider independently.
- `InviteRemoteToChannel` targets a specific remote, so a channel can be shared with
  some connections but not others.

This requires a server-side API change (see [Server Shared Channels Changes](26-04-12-04-server-shared-channels-changes.md), Phase 2).

**OnActivate** changes (`plugin.go`):

After initializing the KV store and bot user, register one remote per outbound
connection:

```go
p.remoteIDs = make(map[string]string) // connName -> remoteID

for _, connCfg := range p.configuration.OutboundConnections {
    remoteID, err := p.API.RegisterPluginForSharedChannels(mmModel.RegisterPluginOpts{
        Displayname:  fmt.Sprintf("Cross Guard (%s)", connCfg.Name),
        PluginID:     manifest.Id,
        CreatorID:    p.botUserID,
        AutoShareDMs: false,
        AutoInvited:  false,
    })
    if err != nil {
        return fmt.Errorf("failed to register remote for connection %s: %w", connCfg.Name, err)
    }
    p.remoteIDs[connCfg.Name] = remoteID
}
```

Add `remoteIDs map[string]string` field to the `Plugin` struct (connName to remoteID).
This replaces the single `remoteID string` field.

**OnDeactivate** changes:

Before closing providers, unregister all remotes:

```go
for connName, remoteID := range p.remoteIDs {
    if err := p.API.UnregisterPluginForSharedChannels(remoteID); err != nil {
        p.API.LogWarn("Failed to unregister remote from shared channels",
            "error_code", errcode.PluginUnregisterFailed,
            "conn_name", connName, "error", err.Error())
    }
}
```

**OnConfigurationChange** considerations:

When the configuration changes (connections added/removed), the plugin must reconcile
registered remotes. Connections added since last activation get a new
`RegisterPluginForSharedChannels` call. Connections removed get
`UnregisterPluginForSharedChannels`. Connections unchanged keep their existing
remoteID. This reconciliation happens in the existing config change handler.

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
    // outbound connection by looking up rc.RemoteId in our remoteIDs map.
    connName := p.connNameForRemote(rc.RemoteId)
    if connName == "" {
        p.API.LogWarn("No outbound connection found for remote",
            "error_code", errcode.OutboundSyncMsgNoRemoteMatch,
            "remote_id", rc.RemoteId)
        return mmModel.SyncResponse{}, nil
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
(posts are upserted by ID on the receiving side via `SendSharedChannelSyncMsg`).

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
that deserializes the `TransportEnvelope` and calls `SendSharedChannelSyncMsg`.

```go
// server/inbound.go (rewritten, dramatically smaller)

func (p *Plugin) handleInboundMessage(connName string) func(data []byte) error {
    return func(data []byte) error {
        select {
        case p.relaySem <- struct{}{}:
        default:
            p.API.LogWarn("Relay semaphore full, dropping inbound message",
                "error_code", errcode.InboundSemaphoreFull, "conn_name", connName)
            return nil
        }

        p.wg.Go(func() {
            defer func() { <-p.relaySem }()
            p.processInboundMessage(connName, data)
        })
        return nil
    }
}

func (p *Plugin) processInboundMessage(connName string, data []byte) {
    env, err := UnmarshalEnvelope(data)
    if err != nil {
        p.API.LogError("Failed to unmarshal transport envelope",
            "error_code", errcode.InboundUnmarshalFailed,
            "conn_name", connName, "error", err.Error())
        return
    }

    switch env.Type {
    case TransportTypeSyncMsg:
        p.handleInboundSyncMsg(connName, env)
    case TransportTypeTest:
        p.API.LogInfo("Received test message",
            "error_code", errcode.InboundTestReceived,
            "conn_name", connName, "test_id", env.TestID)
    case TransportTypeAttachment:
        p.API.LogDebug("Attachment sync not yet implemented",
            "error_code", errcode.InboundAttachmentStubbed, "conn_name", connName)
    case TransportTypeProfileImage:
        p.API.LogDebug("Profile image sync not yet implemented",
            "error_code", errcode.InboundProfileImageStubbed, "conn_name", connName)
    default:
        p.API.LogWarn("Unknown transport message type",
            "error_code", errcode.InboundUnknownType,
            "conn_name", connName, "type", env.Type)
    }
}

func (p *Plugin) handleInboundSyncMsg(connName string, env *TransportEnvelope) {
    if env.SyncMsg == nil {
        return
    }

    // Look up local channel by team name + channel name
    team, appErr := p.API.GetTeamByName(env.TeamName)
    if appErr != nil {
        // Check rewrite index
        teamID, err := p.kvstore.GetTeamRewriteIndex(connName, env.TeamName)
        if err != nil || teamID == "" {
            p.handleUnlinkedInbound(connName, env.TeamName)
            return
        }
        team, appErr = p.API.GetTeam(teamID)
        if appErr != nil {
            return
        }
    }

    channel, appErr := p.API.GetChannelByName(env.ChannelName, team.Id, false)
    if appErr != nil {
        p.handleUnlinkedInboundChannel(connName, env.TeamName, env.ChannelName, team.Id)
        return
    }

    // Verify connection is linked
    conns, err := p.kvstore.GetChannelConnections(channel.Id)
    if err != nil || !hasInboundConnection(conns, connName) {
        p.handleUnlinkedInboundChannel(connName, env.TeamName, env.ChannelName, team.Id)
        return
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

    resp, err := p.API.SendSharedChannelSyncMsg(env.SyncMsg)
    if err != nil {
        p.API.LogError("Failed to send sync message",
            "error_code", errcode.InboundSendSyncFailed,
            "conn_name", connName, "channel_id", channel.Id, "error", err.Error())
        return
    }

    if len(resp.PostErrors) > 0 {
        p.API.LogWarn("Some posts failed to sync",
            "error_code", errcode.InboundPostSyncErrors,
            "conn_name", connName, "errors", strings.Join(resp.PostErrors, ", "))
    }
}
```

**Removed from inbound.go**:
- `resolveTeamAndChannel()` (replaced by simple team/channel name lookup above)
- `findTeamByRewrite()` (inlined in `handleInboundSyncMsg`)
- `handleInboundPost()`, `handleInboundUpdate()`, `handleInboundDelete()`,
  `handleInboundReaction()` (all replaced by `SendSharedChannelSyncMsg`)
- `watchFiles()`, `handleInboundFile()` (file sync stubbed)

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
    // outbound connection and report its provider health.
    connName := p.connNameForRemote(rc.RemoteId)
    if connName == "" {
        // Unknown remote. This could be a stale registration.
        return false
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
- Remove retry queue max age recalculation from `OnConfigurationChange()`
  (`computeRetryMaxAge()` itself lives in `retry_dispatch.go` and is deleted with
  that file)
- Keep `FileTransferEnabled`, `FileFilterMode`, `FileFilterTypes` in config struct
  (used when file sync is implemented, and needed for status display)

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
| 15100 | `InboundSendSyncFailed` | inbound.go |
| 15101 | `InboundPostSyncErrors` | inbound.go |
| 15102 | `InboundAttachmentStubbed` | inbound.go |
| 15103 | `InboundProfileImageStubbed` | inbound.go |
| 15104 | `InboundUnknownType` | inbound.go |
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
| `server/plugin.go` | Add `remoteIDs map[string]string` field and `connNameForRemote` helper. OnActivate: register one remote per outbound connection, remove retry queue init. OnDeactivate: unregister all remotes, remove retry queue wait. Remove `retryQueue`, `fileSem`, `fileWatcherWg` fields. |
| `server/hooks.go` | Replace five MM hooks with `OnSharedChannelsSyncMsg`, `OnSharedChannelsAttachmentSyncMsg` (stub), `OnSharedChannelsProfileImageSyncMsg` (stub), `OnSharedChannelsPing`. Remove `isChannelRelayEnabled`, `relayToOutbound`. |
| `server/hooks_test.go` | Rewrite: test `OnSharedChannelsSyncMsg` with mock API and providers. Remove tests for old hooks. |
| `server/inbound.go` | Rewrite: `handleInboundMessage` deserializes `TransportEnvelope` and calls `SendSharedChannelSyncMsg`. Remove all handler functions, file watching, team/channel resolution. Dramatically smaller. |
| `server/inbound_test.go` | Rewrite: test `processInboundMessage` and `handleInboundSyncMsg` with mock `SendSharedChannelSyncMsg`. Remove tests for old handlers. Remove `testKVStore`, `rewriteTestKVStore`, `unlinkTestKVStore` (replaced by `flexibleKVStore`). |
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
| `server/configuration.go` | Remove `MessageFormat` from `ConnectionConfig`. Remove `computeRetryMaxAge()`. Remove retry queue recalc from `OnConfigurationChange()`. |
| `server/configuration_test.go` | Remove tests for `MessageFormat` validation and `computeRetryMaxAge`. |
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

### Provider files (no changes)

| File | Notes |
|---|---|
| `server/nats_provider.go` | Unchanged. Publishes/subscribes raw bytes. |
| `server/nats_test.go` | Unchanged. |
| `server/azure_provider.go` | Unchanged. |
| `server/azure_provider_test.go` | Unchanged. |
| `server/azure_blob_provider.go` | Unchanged. |
| `server/azure_blob_provider_test.go` | Unchanged. |
| `server/provider.go` | Unchanged. QueueProvider interface stays the same. |

### Frontend files (no changes)

The frontend is unaffected. Backend API response shapes for status, init, teardown
remain compatible. The `connection_state.ts` observer, modals, admin settings, channel
indicator, and user popover are unchanged.

## Prerequisites

Both Mattermost server PRs must merge before plugin tasks can land. See
[Server Shared Channels Changes](26-04-12-04-server-shared-channels-changes.md) for
the complete server-side plan:

- **Phase 1 (XML struct tags)**: Additive `xml` tags on model types, custom
  `MarshalXML`/`UnmarshalXML` for maps. Low risk, can merge first.
- **Phase 2 (multi-remote API)**: `RegisterPluginOpts.Name` field, `PluginName`
  column on `RemoteClusters`, `GetByPluginIDAndName` store method, new
  `UnregisterPluginRemoteForSharedChannels` API method. Requires migration.

Plugin development on tasks 1-17 can proceed in parallel using local model patches,
but merge is blocked until both server PRs land.

## Tasks

1. [ ] Create `server/transport.go` with `TransportEnvelope` type (XML struct tags),
       `MarshalEnvelope`/`UnmarshalEnvelope` functions, `buildSyncResponse()`, and
       `splitTransportEnvelope()`. Create `server/transport_test.go` with XML
       round-trip, splitting, and edge case tests.

2. [ ] Update `server/store/store.go`: remove post mapping and deleting flag methods
       from the `KVStore` interface. Update `client.go` and `client_test.go` to match.

3. [ ] Update `server/plugin.go`: add `remoteIDs map[string]string` field and
       `connNameForRemote` helper. OnActivate: register one remote per outbound
       connection via `RegisterPluginForSharedChannels`. OnDeactivate: unregister
       all remotes via `UnregisterPluginForSharedChannels`. Remove `retryQueue`,
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
       `SendSharedChannelSyncMsg` call. Rewrite ChannelId on all nested types
       (Posts, Reactions, MembershipChanges, Acknowledgements). Remove file watching.
       Rewrite `inbound_test.go`.

7. [ ] Update `server/service.go`: add `ShareChannel`/`InviteRemoteToChannel` to
       channel init (inviting each relevant remote) and
       `UninviteRemoteFromChannel`/`UnshareChannel` to channel teardown. Update
       `service_test.go`.

8. [ ] Update `server/prompt.go`: verify accept handlers work with updated service
       layer. Update `prompt_test.go` with shared channel API mock expectations.

9. [ ] Update `server/api.go`: change `handleTestConnection` to use
       `TransportEnvelope`. Update `api_test.go`.

10. [ ] Update `server/configuration.go`: remove `MessageFormat` field, remove retry
        queue recalc from `OnConfigurationChange()`. Add remote registration
        reconciliation (register new connections, unregister removed ones via
        `UnregisterPluginRemoteForSharedChannels`). Update `configuration_test.go`.

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
| PR 35962 API surface changes before merge | Pin to the API as documented. Adapt if signatures change. The outbound hooks (v9.5) are stable. |
| Server PRs blocked or delayed | Both server PRs (XML tags, multi-remote API) are prerequisites. See [Server Shared Channels Changes](26-04-12-04-server-shared-channels-changes.md). Plugin development can proceed in parallel using local model patches, but merge is blocked until server PRs land. |
| SyncMsg XML serialization is large for Azure Queue's 48KB limit | XML is more verbose than JSON. `splitTransportEnvelope` splits by post count. Single-post messages that exceed the limit are logged as errors. Azure Blob provider has no limit. |
| `model.SyncMsg.Users` map not natively XML-serializable | `MarshalXML`/`UnmarshalXML` methods on `SyncMsg` handle the map-to-element conversion. Covered by server PR tests. |
| Channel ID rewriting on inbound misses nested references | Rewrite `ChannelId` on `SyncMsg`, `Post`, `Reaction`, `MembershipChange`, and `PostAcknowledgement`. `Status.ActiveChannel` is not rewritten (it is informational, not used for routing by `SendSharedChannelSyncMsg`). |
| Shared channel cursor advances but provider publish fails | Return error from `OnSharedChannelsSyncMsg` so server does not advance cursor. Server retries the batch. Duplicate delivery is safe because `SyncMsg` content is idempotent (posts upserted by ID). |
| Test-connection feature breaks without Envelope type | Replaced by `TransportEnvelope` with `Type: "test"`. Same round-trip test, different wire format. |
| Plugin deployed against pre-11.7 server | `RegisterPluginForSharedChannels` fails, `OnActivate` returns error, plugin does not start. This is intentional (clean break). Admins must upgrade the server before deploying the new plugin version. Documented in release notes. |
| Multi-remote API not available in server | Server Phase 2 PR is a prerequisite. If the API only supports single-remote registration, the plugin falls back to a single remote and fans out internally (degraded mode, loses per-connection cursor tracking). |

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
| `server/inbound_test.go` | `TestProcessInboundMessage_SyncMsg` | Deserializes TransportEnvelope, rewrites ChannelId on all nested types, calls SendSharedChannelSyncMsg |
| `server/inbound_test.go` | `TestProcessInboundMessage_Test` | Handles test message type |
| `server/inbound_test.go` | `TestProcessInboundMessage_UnknownType` | Logs warning for unknown type |
| `server/inbound_test.go` | `TestProcessInboundMessage_InvalidXML` | Handles unmarshal failure |
| `server/inbound_test.go` | `TestHandleInboundSyncMsg_UnlinkedTeam` | Triggers prompt for unknown team |
| `server/inbound_test.go` | `TestHandleInboundSyncMsg_UnlinkedChannel` | Triggers prompt for unknown channel |
| `server/inbound_test.go` | `TestHandleInboundSyncMsg_RewriteIndex` | Uses rewrite index to resolve team |
| `server/inbound_test.go` | `TestHandleInboundSyncMsg_SendErrors` | Logs partial sync errors |
| `server/service_test.go` | `TestInitChannel_SharesChannel` | Verifies ShareChannel + InviteRemoteToChannel called |
| `server/service_test.go` | `TestTeardownChannel_UnsharesChannel` | Verifies UninviteRemoteFromChannel + UnshareChannel called |
| `server/plugin_test.go` | `TestOnActivate_RegistersMultipleRemotes` | Verifies RegisterPluginForSharedChannels called once per outbound connection |
| `server/plugin_test.go` | `TestOnDeactivate_UnregistersAllRemotes` | Verifies UnregisterPluginForSharedChannels called for each remote |
| `server/plugin_test.go` | `TestConnNameForRemote` | Maps remote ID back to connection name |
| `server/connections_test.go` | `TestPublishToOutboundConn_TransportEnvelope` | Publishes TransportEnvelope to a single named provider |

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
// RegisterPluginForSharedChannels is called once per outbound connection.
// Each call returns a unique remote ID.
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
api.On("SendSharedChannelSyncMsg", mock.AnythingOfType("*model.SyncMsg")).
    Return(mmModel.SyncResponse{}, nil)
```

## Acceptance Criteria

- [ ] Shared Channels Plugin API supports multiple remotes per plugin with per-remote hooks
- [ ] Mattermost server model types have `xml` struct tags and `SyncMsg` XML round-trips correctly
- [ ] Plugin activates successfully and registers one remote per outbound connection
- [ ] Plugin deactivates cleanly and unregisters all remotes
- [ ] Outbound: posts, edits, deletes, and reactions made in a shared channel are
      serialized to XML and published to outbound providers
- [ ] Inbound: XML `TransportEnvelope` messages received from providers are deserialized
      and pushed into Mattermost via `SendSharedChannelSyncMsg`
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
| Minimum server version | v11.7.0 (hard requirement) | `SendSharedChannelSyncMsg` requires PR 35962. Clean break, no backward-compat shims. |
| File attachment sync | Stubbed with `// TODO` and Debug-level logging | Cannot be tested end-to-end without implementing both sides. Follow-up plan. |
| Post mapping and delete flag KV operations | Removed | Server's shared channel service tracks post ID mappings and handles delete idempotency internally. |
| Sync user creation code | Removed | Server manages remote users natively via `RemoteId` on user records. |
| Retry queue | Removed | Server's cursor-based sync replays missed changes. No need for in-plugin retry. |
| MessageFormat config field | Removed | Wire format is always XML, not per-connection configurable. |
| Team-level connection tracking | Kept | Still needed for UI grouping and for routing inbound messages to the correct channel lookup scope. |
| Channel-level connection tracking | Kept | Still needed for routing: determines which providers to publish to for a given channel. |
| Health reporting (OnSharedChannelsPing) | Per-remote: returns the health of the specific provider associated with the pinged remote | Each remote maps to one outbound connection. The server pings each remote independently, so health reporting is precise per-connection. |
| Registration model | One remote per outbound connection (multi-remote) | Enables independent sync cursors per connection, per-remote health pings, and per-remote channel sharing. Requires server API change ([Server plan, Phase 2](26-04-12-04-server-shared-channels-changes.md)). |
| SyncMsg splitting for size-limited providers | Split by reducing posts per envelope | Keeps the full Users map in each split (needed for post context). Reactions included only with their associated posts. |
