# Mattermost Server: Shared Channels Plugin API Changes

Prerequisite for:
- [Cross Guard: Shared Channels API Refactor Plan](26-04-12-02-shared-channels-api-refactor.md)
- Matrix Bridge plugin (`mattermost-plugin-matrix-bridge`)

## Context

Multiple plugins need the Shared Channels Plugin API to support registering more
than one remote per plugin:

- **Cross Guard** relays messages between Mattermost servers via pluggable
  transport providers (NATS, Azure Queue, Azure Blob). Each configured outbound
  connection represents a distinct remote endpoint that needs independent sync
  cursor tracking, per-remote health pings, and per-remote channel invitations.

- **Matrix Bridge** bridges Mattermost channels to Matrix rooms. Each bridged
  Matrix homeserver is a distinct remote endpoint with its own sync state.

The current API enforces a one-remote-per-plugin constraint. This plan modifies the
server to lift that constraint (Phase 2) and adds XML struct tags to model types so
plugins can serialize `SyncMsg` as XML for content inspection tooling (Phase 1).

## Current State

**Registration** (`server/channels/app/remote_cluster.go:21-87`):
`RegisterPluginForSharedChannels` calls `GetByPluginID(opts.PluginID)`. If a
`RemoteCluster` with that PluginID exists, it updates it (idempotent). If not, it
creates one with `SiteURL: "plugin_" + opts.PluginID` (a synthetic value since
plugins don't have a real URL). This enforces one RemoteCluster per PluginID.

**Unregistration** (`remote_cluster.go:89-105`):
`UnregisterPluginForSharedChannels(pluginID string)` looks up the single
RemoteCluster by PluginID and deletes it.

**Store** (`server/channels/store/sqlstore/remote_cluster_store.go:186-202`):
`GetByPluginID` queries `WHERE PluginID = ?` and returns a single record via
`Get()` (not `Select()`), so multiple rows with the same PluginID would cause an
error.

**Save** (`remote_cluster_store.go:51-68`): The `Save` method also checks
`GetByPluginID` before insert. If a record exists with the same PluginID, it
returns the existing record (idempotent collision avoidance).

**Dispatch** (`server/platform/services/sharedchannel/sync_send_remote.go:1150+`):
`sendSyncMsgToPlugin` calls `scs.app.OnSharedChannelsSyncMsg(msg, rc)` once per
RemoteCluster. The dispatch loop in `processTask` already iterates per remote, so
the hook dispatch architecture naturally supports multiple remotes per plugin. No
changes needed here.

**Cursor tracking**: `SharedChannelRemote` stores cursors per (ChannelId, RemoteId)
pair. Already independent per remote. No changes needed.

**Hook lookup** (`server/channels/app/shared_channel.go:236-245`):
`OnSharedChannelsSyncMsg` calls `getPluginHooks(env, rc.PluginID)` to find the
plugin's hooks. Multiple RemoteClusters with the same PluginID would all resolve to
the same plugin hooks, which is the desired behavior.

**Summary**: The dispatch, cursor, and hook-lookup layers already work per-remote.
The only constraint is in registration and the store's uniqueness assumption.

## Design Principles

| Our Approach | Avoid |
|---|---|
| Minimal, additive changes to existing API | Redesigning the shared channel service |
| Backward compatible (single-remote plugins unchanged) | Breaking existing plugin consumers (e.g., MS Teams plugin) |
| XML tags are additive struct annotations | Changing JSON serialization behavior |
| Multi-remote via multiple calls to `RegisterPluginForSharedChannels` with different SiteURLs | Forcing all plugins to manage multiple remotes |

## Requirements

### Phase 1: XML Struct Tags

- [ ] Model types have `xml` struct tags alongside existing `json` tags
- [ ] `SyncMsg.Users` map serializes to XML via custom `MarshalXML`/`UnmarshalXML`
- [ ] `SyncMsg.MentionTransforms` map serializes to XML via custom methods
- [ ] `Post.Metadata` is excluded from XML serialization (`xml:"-"`)
- [ ] XML round-trip tests pass for `SyncMsg` with all nested types
- [ ] Existing JSON serialization is unaffected
- [ ] Existing tests pass without modification

### Phase 2: Multi-Remote Registration

- [ ] `RegisterPluginForSharedChannels` requires `SiteURL`; multiple calls with different SiteURLs register multiple remotes
- [ ] Each remote has an independent sync cursor
- [ ] `OnSharedChannelsSyncMsg` is called per remote with the correct `RemoteCluster`
- [ ] `OnSharedChannelsPing` is called per remote
- [ ] `InviteRemoteToChannel` targets a specific remote by ID
- [ ] `Receive*` APIs accept `remoteID` to identify which connection the plugin is acting as
- [ ] `UnregisterPluginForSharedChannels` unregisters all remotes for a plugin (bulk cleanup)
- [ ] `UnregisterPluginRemoteForSharedChannels` (new) unregisters a single remote
- [ ] `GetByPluginID` (deprecated) unchanged, new `GetAllByPluginID` added
- [ ] Registration fails on empty SiteURL or SiteURL collision with a different plugin/server-to-server remote
- [ ] No new database column; SiteURL uniqueness provides the dedup key
- [ ] `IsPlugin()` simplified to check `PluginID != ""` only; `SiteURLPlugin` prefix deprecated
- [ ] Existing tests pass without modification

## Out of Scope

- Changes to the non-plugin shared channel flow (remote cluster to remote cluster)
- Changes to the `SharedChannelService` sync loop or cursor advancement logic
- Changes to `OnSharedChannelsAttachmentSyncMsg` or `OnSharedChannelsProfileImageSyncMsg`
  dispatch (they already work per-remote, same as `OnSharedChannelsSyncMsg`)
- `ReceiveSharedChannelSyncMsg` and the other `Receive*` inbound APIs are
  covered in Phase 2 (signature change to accept `remoteID`).

---

## Phase 1: XML Struct Tags

### Approach

Add `xml:"..."` struct tags to the Mattermost server model types that appear in
`SyncMsg`. The tags are purely additive and have zero effect on JSON serialization
or any existing code path. `encoding/xml` only uses these tags when
`xml.Marshal`/`xml.Unmarshal` is explicitly called.

### Target Structs

| Struct | File | Notes |
|---|---|---|
| `SyncMsg` | `shared_channel.go` | Custom `MarshalXML`/`UnmarshalXML` for `Users` map and `MentionTransforms` map |
| `SyncResponse` | `shared_channel.go` | Straightforward tags |
| `MembershipChangeMsg` | `shared_channel.go` | Straightforward tags |
| `Post` | `post.go` | `Metadata` gets `xml:"-"` (excluded). `Props` (`StringInterface`) needs custom marshal. |
| `User` | `user.go` | `Props`, `NotifyProps`, `Timezone` (all `StringMap`) need custom marshal. `Password`, `MfaSecret` included structurally but always empty in sync. |
| `Reaction` | `reaction.go` | Straightforward tags |
| `Status` | `status.go` | `PrevStatus` excluded (`json:"-"` already) |
| `PostAcknowledgement` | `post_acknowledgement.go` | Straightforward tags |
| `FileInfo` | `file_info.go` | For future file sync. `Path`, `ThumbnailPath`, `PreviewPath`, `Content` excluded (`json:"-"` already). |

### Tag Convention

Element names use PascalCase matching the Go field name:

```go
// Before
ChannelId string `json:"channel_id"`

// After
ChannelId string `json:"channel_id" xml:"ChannelId"`
```

Fields already tagged `json:"-"` also get `xml:"-"`.

### Custom XML Methods for Maps

`encoding/xml` does not natively support Go maps. Three map fields need custom
handling.

**`SyncMsg.Users` (`map[string]*User`)**:

```go
// xmlSyncMsg is the XML-friendly representation of SyncMsg.
// Users and MentionTransforms are handled via custom methods.
func (m *SyncMsg) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
    // Marshal all non-map fields via struct tags.
    // Marshal Users as <Users><User id="...">...</User></Users>.
    // Marshal MentionTransforms as <MentionTransforms><Transform key="..." value="..."/></MentionTransforms>.
}

func (m *SyncMsg) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
    // Reverse of above.
}
```

**`Post.Props` (`StringInterface` / `map[string]any`)**:

```go
// Props values are converted to string representation for XML.
// Complex values (nested objects, rare in practice) are JSON-encoded as the
// value string.
func (p StringInterface) MarshalXML(e *xml.Encoder, start xml.StartElement) error
func (p *StringInterface) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error
```

**`User.Props`, `User.NotifyProps`, `User.Timezone` (`StringMap` / `map[string]string`)**:

```go
func (m StringMap) MarshalXML(e *xml.Encoder, start xml.StartElement) error
func (m *StringMap) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error
```

### Post.Metadata Exclusion

`Post.Metadata` is `*PostMetadata` containing complex nested types (`Embeds`,
`Emojis`, `Files`, `Images`, `Reactions`). These are server-internal presentation
data. The receiving server reconstructs metadata from post content. Tag it
`xml:"-"` to exclude from XML serialization.

```go
// Before
Metadata *PostMetadata `json:"metadata,omitempty"`

// After
Metadata *PostMetadata `json:"metadata,omitempty" xml:"-"`
```

### Phase 1 Files to Modify

| File | Changes |
|---|---|
| `server/public/model/shared_channel.go` | Add `xml` tags to `SyncMsg`, `SyncResponse`, `MembershipChangeMsg`. Add `MarshalXML`/`UnmarshalXML` to `SyncMsg`. |
| `server/public/model/shared_channel_test.go` | Add XML round-trip tests for `SyncMsg` with nested types. |
| `server/public/model/post.go` | Add `xml` tags to `Post`. Tag `Metadata` as `xml:"-"`. |
| `server/public/model/user.go` | Add `xml` tags to `User`. |
| `server/public/model/reaction.go` | Add `xml` tags to `Reaction`. |
| `server/public/model/status.go` | Add `xml` tags to `Status`. |
| `server/public/model/post_acknowledgement.go` | Add `xml` tags to `PostAcknowledgement`. |
| `server/public/model/file_info.go` | Add `xml` tags to `FileInfo`. |
| `server/public/model/utils.go` (or new file) | Add `MarshalXML`/`UnmarshalXML` for `StringMap` and `StringInterface` if not scoped to a single type. |

### Phase 1 Tests

| Test | What it verifies |
|---|---|
| `TestSyncMsgXMLRoundTrip` | `SyncMsg` with Users map, Posts, Reactions, MentionTransforms marshals to XML and unmarshals back to identical struct. |
| `TestSyncMsgXMLUsersMap` | `SyncMsg.Users` map round-trips correctly. Map keys preserved. |
| `TestSyncMsgXMLMentionTransforms` | `SyncMsg.MentionTransforms` map round-trips correctly. |
| `TestPostXMLRoundTrip` | `Post` with Props (including nested JSON values) round-trips. `Metadata` field is excluded. |
| `TestUserXMLRoundTrip` | `User` with Props, NotifyProps, Timezone maps round-trips. Sensitive fields (Password, MfaSecret) are empty. |
| `TestReactionXMLRoundTrip` | `Reaction` round-trips. |
| `TestStatusXMLRoundTrip` | `Status` round-trips. `PrevStatus` excluded. |
| `TestPostAcknowledgementXMLRoundTrip` | `PostAcknowledgement` round-trips. |
| `TestFileInfoXMLRoundTrip` | `FileInfo` round-trips. Excluded fields omitted. |
| `TestStringMapXMLRoundTrip` | `StringMap` marshal/unmarshal with various key-value pairs. |
| `TestStringInterfaceXMLRoundTrip` | `StringInterface` marshal/unmarshal, including complex nested values that are JSON-encoded. |
| `TestJSONUnchanged` | Verify JSON marshal/unmarshal of all modified structs produces identical output to before the change. |

---

## Phase 2: Multi-Remote Registration

### Approach

The key insight from the codebase exploration is that the dispatch, cursor, and
hook-lookup layers already work per-remote. The only constraints are in:

1. **`RegisterPluginForSharedChannels`**: Uses `GetByPluginID` to enforce one remote
   per plugin.
2. **`UnregisterPluginForSharedChannels`**: Takes `pluginID`, assumes one remote.
3. **`RemoteClusterStore.Save`**: Checks `GetByPluginID` to prevent duplicate
   PluginID rows.
4. **`RemoteClusterStore.GetByPluginID`**: Uses `Get()` (single row), not
   `Select()` (multiple rows).
5. **`Receive*` app-layer functions** (`shared_channel.go`): Use `GetByPluginID`
   to resolve the calling plugin's remote, which only works for single-remote
   plugins.
6. **`Receive*` Plugin API methods** (PR 35962, not yet merged): Don't accept a
   `remoteID`, so the plugin can't specify which connection it's acting as.

**Deprecation strategy**: Methods already in the public Plugin API (`api.go`) cannot
have their signatures changed. They are marked deprecated with doc comments, and new
methods are added alongside them. The store-level `GetByPluginID` is also deprecated
in place, not modified. The `Receive*` methods in PR 35962 are not yet merged, so
their signatures can be changed before they become part of the public API.

The fix is to add a `SiteURL` field to `RegisterPluginOpts`. The plugin provides
the URL or identifier of the remote endpoint (e.g., `"nats://nats:4222"`,
`"https://matrix.org"`), and it is stored directly as `RemoteCluster.SiteURL`.
The existing unique index on `(SiteURL, RemoteTeamId)` provides the dedup key.
No new database column or migration is needed.

The current `"plugin_"` prefix on plugin SiteURLs is removed. It was redundant
with the `PluginID` field, which already identifies a remote as plugin-based.
`IsPlugin()` is simplified to just check `PluginID != ""`.

### Model Changes

**`RegisterPluginOpts`** (`server/public/model/shared_channel.go`):

```go
type RegisterPluginOpts struct {
    Displayname  string
    PluginID     string
    CreatorID    string
    AutoShareDMs bool
    AutoInvited  bool
    SiteURL      string // Required. Identifies the remote endpoint for this secure
                        // connection. Stored directly as RemoteCluster.SiteURL.
                        // Must be unique across all remote clusters (enforced by
                        // the DB unique index on (SiteURL, RemoteTeamId)).
                        // Registration fails if SiteURL is empty, or if the SiteURL
                        // is already in use by a different plugin or a
                        // server-to-server remote.
                        // Calling RegisterPluginForSharedChannels again with the
                        // same SiteURL returns the existing remoteID and preserves
                        // sync cursors (idempotent re-registration).
                        // A plugin registers multiple remotes by calling this
                        // method multiple times with different SiteURLs.
                        // Examples: "nats://nats:4222", "https://matrix.org"
}
```

**`RemoteCluster`** (`server/public/model/remote_cluster.go`):

No new fields. The existing `SiteURL` field stores the connection identity.

`IsPlugin()` is simplified to remove the redundant `SiteURLPlugin` prefix check:

```go
// Before:
func (rc *RemoteCluster) IsPlugin() bool {
    if rc.PluginID != "" || strings.HasPrefix(rc.SiteURL, SiteURLPlugin) {
        return true
    }
    return false
}

// After:
func (rc *RemoteCluster) IsPlugin() bool {
    return rc.PluginID != ""
}
```

The `SiteURLPlugin` constant (`"plugin_"`) can be deprecated. Existing plugin
remotes with `"plugin_"` prefixed SiteURLs will continue to have `PluginID` set,
so `IsPlugin()` still returns true. New registrations store the plugin-provided
SiteURL directly without the prefix.

### Store Changes

**`RemoteClusterStore` interface** (`server/channels/store/store.go`):

```go
// Deprecated: GetByPluginID returns a single remote for the plugin. Only correct
// when the plugin has one registration. Use GetAllByPluginID instead.
// For dedup during registration, query by SiteURL via GetBySiteURL.
GetByPluginID(pluginID string) (*model.RemoteCluster, error)

// New: returns all remotes registered by this plugin.
GetAllByPluginID(pluginID string) ([]*model.RemoteCluster, error)

// New: returns the remote with the given SiteURL, or sql.ErrNoRows.
GetBySiteURL(siteURL string) (*model.RemoteCluster, error)
```

**`sqlRemoteClusterStore`** (`server/channels/store/sqlstore/remote_cluster_store.go`):

`GetByPluginID` (deprecated): Keeps its existing signature. Updated to query all
rows for the PluginID and return an error if more than one is found. Single-remote
plugins (legacy) get the expected single result. Multi-remote plugins calling this
method get a clear error rather than an arbitrary row. Callers within the server
are migrated to the new methods.

`GetAllByPluginID` (new): Query `WHERE PluginID = ?` using `Select()` (multiple
rows). Return `[]*model.RemoteCluster`. Used by
`UnregisterPluginForSharedChannels` to delete all remotes for a plugin.

`GetBySiteURL` (new): Query `WHERE SiteURL = ?`, single row via `Get()`. Used by
registration for idempotent dedup. Also useful as a general-purpose lookup since
SiteURL is the natural unique identifier for any remote cluster.

`Save`: Replace `GetByPluginID` collision check with `GetBySiteURL` so that two
remotes with the same PluginID but different SiteURLs can be saved independently.

**No database migration needed**. The `SiteURL` column and its uniqueness
expectation already exist.

### Registration Changes

**`RegisterPluginForSharedChannels`** (`server/channels/app/remote_cluster.go`):

```go
func (a *App) RegisterPluginForSharedChannels(
    rctx request.CTX, opts model.RegisterPluginOpts,
) (remoteID string, err error) {
    if opts.SiteURL == "" {
        return "", fmt.Errorf("SiteURL is required")
    }

    // Check if this SiteURL is already registered
    rc, err := a.Srv().Store().RemoteCluster().GetBySiteURL(opts.SiteURL)
    if err != nil && !errors.Is(err, sql.ErrNoRows) {
        return "", err
    }

    if rc != nil {
        // SiteURL exists. Verify it belongs to this plugin.
        if rc.PluginID != opts.PluginID {
            return "", fmt.Errorf("SiteURL %q is already in use by another remote",
                opts.SiteURL)
        }

        // Same plugin, same SiteURL: idempotent re-registration
        if rc.DeleteAt != 0 {
            rc.DeleteAt = 0
        }
        rc.DisplayName = opts.Displayname
        rc.Options = opts.GetOptionFlags()
        if _, err = a.Srv().Store().RemoteCluster().Update(rc); err != nil {
            return "", err
        }
        return rc.RemoteId, nil
    }

    // New connection
    rc = &model.RemoteCluster{
        Name:        opts.Displayname,
        DisplayName: opts.Displayname,
        SiteURL:     opts.SiteURL,
        Token:       model.NewId(),
        CreatorId:   opts.CreatorID,
        PluginID:    opts.PluginID,
        Options:     opts.GetOptionFlags(),
    }

    rcSaved, err := a.Srv().Store().RemoteCluster().Save(rc)
    if err != nil {
        return "", err
    }

    // Ping immediately if service is running
    rcService, _ := a.GetRemoteClusterService()
    if rcService != nil {
        rcService.PingNow(rcSaved)
    }

    return rcSaved.RemoteId, nil
}
```

**Validation**:
- Empty `SiteURL` returns an error. All plugins must provide one.
- If the `SiteURL` is already registered to a different plugin (different
  `PluginID`) or to a server-to-server remote (`PluginID == ""`), registration
  fails with an error.
- If the `SiteURL` matches an existing record with the same `PluginID`, it is an
  idempotent re-registration (update display name, restore if deleted, preserve
  sync cursors).

**Backward compatibility for existing plugins**: Existing plugins (e.g., MS Teams)
that currently call `RegisterPluginForSharedChannels` without setting `SiteURL`
must be updated to provide one. Since `SiteURL` was not previously a field on
`RegisterPluginOpts`, all existing callers pass the zero value (`""`), which now
fails validation. This is a breaking change requiring a one-line update in each
consuming plugin: set `SiteURL` to a stable identifier for their remote endpoint.

On first registration with the new `SiteURL`, the server creates a new
`RemoteCluster` record. The old `"plugin_"` prefixed record is orphaned. It can
be cleaned up by the plugin's `OnDeactivate` call (which calls
`UnregisterPluginForSharedChannels`, deleting all records for the plugin), or
it can be left to be garbage-collected by the server's remote cluster cleanup.

**`IsPlugin()` simplification**: See model changes above. The `PluginID` field is
the sole indicator. The `SiteURLPlugin` prefix check is removed.

### Unregistration Changes

**`UnregisterPluginForSharedChannels`** (`server/channels/app/remote_cluster.go`):

The existing method is updated to use `GetAllByPluginID` (since there may now be
multiple remotes) but keeps its signature. It serves as a bulk cleanup method,
used in `OnDeactivate` to unregister all remotes without the plugin needing to
track individual remote IDs.

A new `UnregisterPluginRemoteForSharedChannels` method is added for surgical
removal of a single remote (useful for config change reconciliation when a
connection is removed but others remain).

```go
// Unregisters all remotes for this plugin. Used in OnDeactivate for cleanup.
func (a *App) UnregisterPluginForSharedChannels(pluginID string) error {
    remotes, err := a.Srv().Store().RemoteCluster().GetAllByPluginID(pluginID)
    if err != nil {
        return err
    }
    for _, rc := range remotes {
        if rc.DeleteAt != 0 {
            continue
        }
        if _, appErr := a.DeleteRemoteCluster(rc.RemoteId); appErr != nil {
            return appErr
        }
    }
    return nil
}

// Unregisters a specific remote by ID.
func (a *App) UnregisterPluginRemoteForSharedChannels(remoteID string) error {
    rc, err := a.Srv().Store().RemoteCluster().Get(remoteID, false)
    if err != nil {
        return err
    }
    if rc.DeleteAt != 0 {
        return nil
    }
    _, appErr := a.DeleteRemoteCluster(rc.RemoteId)
    if appErr != nil {
        return appErr
    }
    return nil
}
```

### Plugin API Surface Changes

**Already in master (cannot change, can deprecate)**:

```go
// Unregisters all remotes for this plugin. Used in OnDeactivate for bulk cleanup.
UnregisterPluginForSharedChannels(pluginID string) error

// New: unregisters a specific remote by its remoteID. Used for config change
// reconciliation when a connection is removed but others remain.
UnregisterPluginRemoteForSharedChannels(remoteID string) error
```

No signature change to `RegisterPluginForSharedChannels` since
`RegisterPluginOpts` is a struct (adding the `SiteURL` field is backward
compatible at the struct level, though the validation requiring it is a
breaking change for callers that previously omitted it).

**Still in PR 35962 (change before merge)**:

The `Receive*` methods currently resolve the plugin's remote implicitly via
`api.id` (the calling plugin's ID), which only works for single-connection
plugins. With multi-remote, the plugin must specify which connection it is acting
on behalf of. These signatures should be changed before merge:

```go
// Before (PR 35962 current):
ReceiveSharedChannelSyncMsg(msg *model.SyncMsg) (model.SyncResponse, error)
ReceiveSharedChannelAttachmentSyncMsg(channelID string, fi *model.FileInfo, data io.Reader) (*model.FileInfo, error)
ReceiveSharedChannelProfileImageSyncMsg(userID string, image []byte) error

// After (add remoteID parameter):
ReceiveSharedChannelSyncMsg(remoteID string, msg *model.SyncMsg) (model.SyncResponse, error)
ReceiveSharedChannelAttachmentSyncMsg(remoteID, channelID string, fi *model.FileInfo, data io.Reader) (*model.FileInfo, error)
ReceiveSharedChannelProfileImageSyncMsg(remoteID, userID string, image []byte) error
```

The `remoteID` is the value the plugin received from
`RegisterPluginForSharedChannels`.

**Security**: The app-layer `Receive*` functions in `shared_channel.go` must
accept both `pluginID string` and `remoteID string`. The `PluginAPI` wrappers
pass `api.id` (server-injected, unforgeable) as `pluginID` and the caller-supplied
`remoteID`. The app-layer implementation calls `RemoteCluster().Get(remoteID, false)`
and then validates `rc.PluginID == pluginID`. This prevents a plugin from passing
a `remoteID` belonging to a different plugin or a server-to-server remote.

```go
// App-layer signature:
func (a *App) ReceiveSharedChannelSyncMsg(
    rctx request.CTX, pluginID, remoteID string, msg *model.SyncMsg,
) (model.SyncResponse, error) {
    rc, err := a.Srv().Store().RemoteCluster().Get(remoteID, false)
    if err != nil {
        return model.SyncResponse{}, fmt.Errorf("remote %s not found: %w", remoteID, err)
    }
    if rc.PluginID != pluginID {
        return model.SyncResponse{}, fmt.Errorf("remote %s does not belong to plugin %s", remoteID, pluginID)
    }
    // ... process msg using rc ...
}

// PluginAPI wrapper:
func (api *PluginAPI) ReceiveSharedChannelSyncMsg(remoteID string, msg *model.SyncMsg) (model.SyncResponse, error) {
    return api.app.ReceiveSharedChannelSyncMsg(api.ctx, api.id, remoteID, msg)
}
```

### Phase 2 Files to Modify

| File | Changes |
|---|---|
| `server/public/model/shared_channel.go` | Add `SiteURL` field to `RegisterPluginOpts`. |
| `server/public/model/remote_cluster.go` | Simplify `IsPlugin()` to check `PluginID != ""` only. Deprecate `SiteURLPlugin` constant. |
| `server/channels/store/store.go` | Deprecate `GetByPluginID` (keep unchanged). Add `GetAllByPluginID`, `GetBySiteURL`. |
| `server/channels/store/sqlstore/remote_cluster_store.go` | Implement `GetAllByPluginID` (multi-row), `GetBySiteURL` (single-row). Update `Save` collision check to use `GetBySiteURL`. |
| `server/channels/store/storetest/remote_cluster_store.go` | Add tests for new store methods. Keep existing `GetByPluginID` tests. |
| `server/channels/store/storetest/mocks/RemoteClusterStore.go` | Regenerate mock. |
| `server/channels/store/timerlayer/timerlayer.go` | Add timer wrappers for new methods. |
| `server/channels/store/retrylayer/retrylayer.go` | Add retry wrappers for new methods. |
| `server/channels/app/remote_cluster.go` | Rewrite `RegisterPluginForSharedChannels` to construct SiteURL from opts and dedup via `GetBySiteURL`. Change `UnregisterPluginForSharedChannels` to use `GetAllByPluginID` and delete all. Add `UnregisterPluginRemoteForSharedChannels`. |
| `server/channels/app/shared_channel.go` | Change `Receive*` app-layer functions to accept both `pluginID` and `remoteID`. Validate `rc.PluginID == pluginID` after looking up the remote. Outbound hook dispatch (`OnSharedChannelsSyncMsg`, `OnSharedChannelsPing`) unchanged. |
| `server/channels/app/plugin_api.go` | Add `UnregisterPluginRemoteForSharedChannels` wrapper. Change `Receive*` wrappers to pass `api.id` as `pluginID` and caller-supplied `remoteID`. |
| `server/public/plugin/api.go` | Add `UnregisterPluginRemoteForSharedChannels` to interface. Change `Receive*` signatures to add `remoteID` parameter (PR 35962, not yet merged). |

### Phase 2 Files Unchanged

These already work per-remote and need no modification:

| File | Why unchanged |
|---|---|
| `server/platform/services/sharedchannel/sync_send.go` | `processTask` iterates remotes by channel membership, already per-remote. |
| `server/platform/services/sharedchannel/sync_send_remote.go` | `sendSyncMsgToPlugin` calls hook once per RemoteCluster. |
| `server/platform/services/sharedchannel/service_api.go` | `InviteRemoteToChannel` takes `remoteID`, already per-remote. |

### Phase 2 Tests

| Test | What it verifies |
|---|---|
| `TestRegisterMultipleRemotesPerPlugin` | Two calls with same PluginID but different SiteURL values create two RemoteCluster records with different RemoteIds. |
| `TestRegisterSameSiteURLIdempotent` | Two calls with same SiteURL return the same RemoteId and preserve cursors. |
| `TestRegisterEmptySiteURLFails` | Call with empty SiteURL returns an error. |
| `TestRegisterSiteURLCollisionDifferentPlugin` | Registration fails when SiteURL is already used by a different plugin. |
| `TestRegisterSiteURLCollisionServerToServer` | Registration fails when SiteURL is already used by a server-to-server remote. |
| `TestUnregisterAllRemotes` | `UnregisterPluginForSharedChannels(pluginID)` deletes all remotes for the plugin. |
| `TestUnregisterSingleRemote` | `UnregisterPluginRemoteForSharedChannels(remoteID)` deletes only the specified remote. |
| `TestGetAllByPluginID` | `GetAllByPluginID` returns all remotes for a plugin. |
| `TestGetBySiteURL` | `GetBySiteURL` returns the specific remote for a given SiteURL. |
| `TestGetByPluginIDSingleRemote` | Deprecated `GetByPluginID` returns the single remote for a single-connection plugin. |
| `TestGetByPluginIDMultiRemoteErrors` | Deprecated `GetByPluginID` returns an error when the plugin has multiple remotes registered. |
| `TestSaveSiteURLCollision` | `Save` with duplicate SiteURL returns existing record. |
| `TestSyncDispatchPerRemote` | Two remotes from same plugin invited to same channel each receive independent `OnSharedChannelsSyncMsg` calls. |
| `TestPingPerRemote` | Two remotes from same plugin each receive independent `OnSharedChannelsPing` calls. |
| `TestCursorsIndependentPerRemote` | Two remotes on same channel track sync position independently. |
| `TestInviteSpecificRemote` | `InviteRemoteToChannel` with one remote ID does not affect the other remote from the same plugin. |
| `TestReceiveSyncMsgWithRemoteID` | `ReceiveSharedChannelSyncMsg(remoteID, msg)` resolves the correct remote and processes the message. |
| `TestReceiveSyncMsgWrongPlugin` | `ReceiveSharedChannelSyncMsg` returns an error when remoteID belongs to a different plugin (ownership validation). |
| `TestSingleRemotePluginWithSiteURL` | Single-remote plugin providing a SiteURL registers successfully and behaves identically to the old single-remote behavior (one remote, one cursor). |

---

## Tasks

### Phase 1

1. [ ] Add `xml` struct tags to `SyncMsg`, `SyncResponse`, `MembershipChangeMsg`
       in `server/public/model/shared_channel.go`. Add `MarshalXML`/`UnmarshalXML`
       methods to `SyncMsg` for the `Users` map and `MentionTransforms` map.

2. [ ] Add `xml` struct tags to `Post` in `server/public/model/post.go`. Tag
       `Metadata` as `xml:"-"`. Add `MarshalXML`/`UnmarshalXML` for
       `StringInterface` (Post.Props).

3. [ ] Add `xml` struct tags to `User` in `server/public/model/user.go`. Add
       `MarshalXML`/`UnmarshalXML` for `StringMap` (Props, NotifyProps, Timezone).

4. [ ] Add `xml` struct tags to `Reaction`, `Status`, `PostAcknowledgement`,
       `FileInfo` in their respective files.

5. [ ] Add XML round-trip tests for all modified types. Verify JSON serialization
       is unchanged.

6. [ ] Run existing test suite. Verify zero regressions.

### Phase 2

7. [ ] Add `SiteURL` field to `RegisterPluginOpts` in
       `server/public/model/shared_channel.go`.

8. [ ] Deprecate `GetByPluginID` (add deprecation comment, keep unchanged). Add
       `GetAllByPluginID` and `GetBySiteURL` to `RemoteClusterStore` interface.
       Implement in SQL store, timer layer, retry layer. Regenerate mocks. Update
       `Save` collision check to use `GetBySiteURL`.

9. [ ] Rewrite `RegisterPluginForSharedChannels` to construct SiteURL from opts
       and dedup via `GetBySiteURL`. Rewrite `UnregisterPluginForSharedChannels`
       to use `GetAllByPluginID` and delete all remotes. Add
       `UnregisterPluginRemoteForSharedChannels` for single-remote removal.

10. [ ] Add `UnregisterPluginRemoteForSharedChannels` to the Plugin API
        interface and `plugin_api.go` implementation.

11. [ ] Change `Receive*` Plugin API signatures in PR 35962 to accept `remoteID`
        parameter. Update app-layer functions in `shared_channel.go` to accept
        both `pluginID` and `remoteID`, validate `rc.PluginID == pluginID` after
        lookup. Update `PluginAPI` wrappers in `plugin_api.go` to pass `api.id`
        as `pluginID` and caller-supplied `remoteID`.

12. [ ] Migrate remaining `GetByPluginID` callers within the server to use
        `GetAllByPluginID` or `GetBySiteURL` as appropriate.

13. [ ] Add all Phase 2 tests (multi-remote registration, per-remote dispatch,
        per-remote cursor tracking, `Receive*` with remoteID, backward
        compatibility).

14. [ ] Run full test suite including shared channel integration tests. Verify
        zero regressions for existing single-remote plugins.

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| XML struct tags change JSON behavior | Tags are independent. `json` and `xml` tags don't interact. Verify with `TestJSONUnchanged`. |
| Custom `MarshalXML` methods introduce bugs for non-plugin JSON consumers | JSON callers never invoke `xml.Marshal`. The custom methods only run when `encoding/xml` is explicitly used. |
| Breaking change for existing plugin consumers (e.g., MS Teams) | `SiteURL` is now required. Existing plugins need a one-line change to set `SiteURL` in their `RegisterPluginOpts`. Old `"plugin_"` prefixed records are orphaned and cleaned up via `UnregisterPluginForSharedChannels` in `OnDeactivate`. |
| Deprecated `GetByPluginID` called for multi-remote plugin | Returns an error if more than one remote exists for the PluginID. Single-remote plugins continue to work. Multi-remote plugins get a clear failure, not a silent wrong answer. |
| SiteURL collision | A collision occurs whenever any combination of plugins or server-to-server connections attempt to create RemoteCluster records with a non-unique SiteURL. `RegisterPluginForSharedChannels` detects this and returns a clear error indicating the SiteURL is already in use. The DB unique index on `(SiteURL, RemoteTeamId)` is the final safeguard. |
| Legacy `"plugin_"` prefixed SiteURLs after upgrade | On plugin upgrade, `OnDeactivate` calls `UnregisterPluginForSharedChannels(pluginID)`, which deletes all records for the plugin (including the legacy prefixed one) via `GetAllByPluginID`. The subsequent `OnActivate` registers fresh records with the new SiteURL. No records are orphaned. The `SiteURLPlugin` constant is deprecated but not removed, keeping backward compat for any external code that references it. |
| `Receive*` signature change in PR 35962 | PR is not yet merged, so the public API is not yet committed. Change signatures before merge. No backward compat concern. |
| Hook dispatch calls plugin multiple times for same content | This is intentional. Each remote is a distinct "destination." The plugin receives the call once per remote and publishes to the corresponding provider. Same as how non-plugin remotes each get their own sync call. |

## Acceptance Criteria

### Phase 1
- [ ] All model types in the target list have `xml` struct tags
- [ ] `SyncMsg` XML round-trips correctly with Users map and MentionTransforms map
- [ ] `Post.Metadata` is excluded from XML
- [ ] `StringMap` and `StringInterface` custom XML methods work correctly
- [ ] JSON serialization for all modified types is byte-identical to before
- [ ] All existing tests pass

### Phase 2
- [ ] `SiteURL` is required; registration fails on empty
- [ ] Registration fails if SiteURL is already used by a different plugin or server-to-server remote
- [ ] A plugin can register multiple remotes by calling register with different SiteURLs
- [ ] Each remote gets an independent sync cursor
- [ ] `OnSharedChannelsSyncMsg` is called once per remote for shared channels
- [ ] `OnSharedChannelsPing` is called once per remote
- [ ] `InviteRemoteToChannel` targets a specific remote
- [ ] `Receive*` APIs accept remoteID, validate plugin ownership via server-injected pluginID, and resolve the correct remote
- [ ] Unregistering by pluginID removes all remotes (bulk cleanup)
- [ ] Unregistering by remoteID removes only that remote
- [ ] `GetByPluginID` is deprecated but continues to work for single-connection plugins
- [ ] No new database column or migration required
- [ ] Existing single-remote plugins work after setting `SiteURL` in their `RegisterPluginOpts` (one-line change)
- [ ] All existing tests pass

## Decisions

| Question | Decision | Rationale |
|---|---|---|
| One PR or two? | Two PRs (Phase 1 then Phase 2) | Phase 1 is low risk, additive struct tags only. Phase 2 has API and store changes. Separate PRs keep review scope manageable. |
| Uniqueness key for multi-remote | SiteURL (stored directly from opts, no prefix) | Reuses the existing SiteURL column and its unique index. No new column, no migration. The plugin provides the endpoint identifier directly, same as server-to-server where SiteURL identifies the remote. The redundant `"plugin_"` prefix is removed; `PluginID` field is the sole plugin indicator. |
| `IsPlugin()` simplification | Check `PluginID != ""` only, remove `SiteURL` prefix check | The `PluginID` field is authoritative. The `SiteURLPlugin` prefix was a synthetic workaround. Existing records still have `PluginID` set, so `IsPlugin()` returns the correct result. |
| Unregister API | Keep `UnregisterPluginForSharedChannels(pluginID)` as bulk cleanup, add `UnregisterPluginRemoteForSharedChannels(remoteID)` for single-remote removal | Bulk method is the safe choice for `OnDeactivate` (no state tracking needed). Single-remote method enables config change reconciliation. Both have clear, distinct use cases. |
| `GetByPluginID` store method | Deprecated, not changed | Avoids breaking external callers. New methods `GetAllByPluginID` and `GetBySiteURL` cover all use cases. Existing method continues to work for single-connection plugins. |
| `Receive*` API signatures | Changed in PR 35962 to accept `remoteID` | PR is not yet merged, so the public API surface is not committed. Adding `remoteID` before merge avoids a future deprecation cycle. |
| XML element names | PascalCase from Go field name | Consistent with Go conventions. `xml` tag values are explicit, not derived from `json` tags. |
| Post.Metadata in XML | Excluded (`xml:"-"`) | Complex nested presentation data. Receiving server reconstructs from post content. Not needed for cross-domain relay. |
| StringMap XML format | `<Entry key="..." value="..."/>` | Matches Go community conventions for map-to-XML. Self-documenting element names. |
