# Cross Guard: XML Wire Format Schema

Referenced from: [Shared Channels API Refactor Plan](26-04-12-02-shared-channels-api-refactor.md)
See also: [Plugin-Owned Wire Types](26-05-11-02-plugin-owned-wire-types.md)

## Overview

All cross-domain traffic between Cross Guard plugin instances is serialized as XML.
This is required by customer content inspection tooling that must parse all traffic
crossing security domain boundaries.

The wire format is owned by the plugin (`server/wire/` package), not by upstream
Mattermost. Each content type is a pruned subset of the corresponding upstream
type, with only the fields that the compliance review has approved to cross a
domain boundary. Fields the receiver computes from its own state (e.g.,
`Post.ReplyCount`) and fields the receiver overwrites on arrival (e.g.,
`User.AuthService`) are dropped at the source.

The authoritative schema is `schema/crossguard.xsd`. Examples that exercise the
schema live in `schema/examples/`. Both are regenerated from the Go wire types;
the documentation below describes the same format in narrative form.

## Conventions

- Element names use `PascalCase` derived from Go field names in the wire structs
  (e.g., `ChannelId` becomes `<ChannelId>`).
- Empty containers (`<Users>`, `<Posts>`, `<Reactions>`, `<Statuses>`,
  `<MembershipChanges>`, `<Acknowledgements>`, `<MentionTransforms>`,
  `<FileIds>`) are suppressed on the wire. The XSD declares each with
  `minOccurs="0"`.
- Map fields are serialized as a sequence of child elements with key attributes:
  - `<Users>` contains `<User id="...">...</User>` per user (no `<UserEntry>` wrapper).
  - `<MentionTransforms>` contains `<Transform key="..." value="..."/>` per entry.
  - `<Timezone>` (open `StringMap`) contains `<Entry key="..." value="..."/>` per entry.
- `Post.Props` and `User.Props` are typed: each known key becomes its own
  child element (e.g., `<FromWebhook>true</FromWebhook>`), not a generic
  `<Entry key=".."/>`. Unknown upstream keys are dropped.
- Boolean fields serialize as `true`/`false`. Integer fields serialize as
  decimal strings. Field order matches the wire-struct declaration order.

## Root Element: CrossGuardEnvelope

```xml
<?xml version="1.0" encoding="UTF-8"?>
<CrossGuardEnvelope version="1" type="sync_msg">
  <ConnName>nats-low-to-high</ConnName>
  <Timestamp>2026-04-14T18:30:00Z</Timestamp>
  <TeamName>test-a</TeamName>
  <ChannelName>town-square</ChannelName>
  <SyncMsg>
    <!-- see SyncMsg below -->
  </SyncMsg>
</CrossGuardEnvelope>
```

| Attribute/Element | Type | Required | Description |
|---|---|---|---|
| `version` (attr) | int | yes | Wire format version. Currently `1`. |
| `type` (attr) | string | yes | Message type: `sync_msg`, `attachment`, `profile_image`, `test`. |
| `ConnName` | string | yes | Connection name for routing. |
| `Timestamp` | RFC 3339 | yes | UTC timestamp the envelope was created. Used for audit and retry-queue age calculations. |
| `TeamName` | string | yes | Source team name (used by receiver to look up local team). |
| `ChannelName` | string | yes | Source channel name (used by receiver to look up local channel). |
| `SyncMsg` | element | when type=`sync_msg` | Content payload. See below. |
| `TestID` | string | when type=`test` | Unique ID for test-connection round-trip. |

### Type values

| Value | Description |
|---|---|
| `sync_msg` | Content sync: posts, users, reactions, memberships, etc. |
| `attachment` | File attachment sync (separate file-transfer channel). |
| `profile_image` | User profile image sync (separate file-transfer channel). |
| `test` | Connectivity test message. |

## SyncMsg

```xml
<SyncMsg>
  <Id>sm-01</Id>
  <ChannelId>ch8f3k...</ChannelId>
  <Users>
    <User id="uid001">...</User>
  </Users>
  <Posts>
    <Post>...</Post>
  </Posts>
  <Reactions>
    <Reaction>...</Reaction>
  </Reactions>
  <Statuses>
    <Status>...</Status>
  </Statuses>
  <MembershipChanges>
    <MembershipChange>...</MembershipChange>
  </MembershipChanges>
  <Acknowledgements>
    <PostAcknowledgement>...</PostAcknowledgement>
  </Acknowledgements>
  <MentionTransforms>
    <Transform key="@alice" value="uid001"/>
  </MentionTransforms>
</SyncMsg>
```

| Element | Type | Required | Description |
|---|---|---|---|
| `Id` | string | yes | Unique sync message identifier. |
| `ChannelId` | string | yes | Channel ID (rewritten to local ID by receiving plugin). |
| `Users` | container | no | Users referenced by content in this batch. |
| `Posts` | container | no | Posts created, updated, or deleted. |
| `Reactions` | container | no | Reactions added or removed. |
| `Statuses` | container | no | User presence statuses. |
| `MembershipChanges` | container | no | Channel join/leave events. |
| `Acknowledgements` | container | no | Post acknowledgements. |
| `MentionTransforms` | container | no | @mention rewrites. |

## Post

Wire-pruned subset of `model.Post`.

```xml
<Post>
  <Id>post123</Id>
  <CreateAt>1712956800000</CreateAt>
  <UpdateAt>1712956800000</UpdateAt>
  <EditAt>0</EditAt>
  <DeleteAt>0</DeleteAt>
  <UserId>uid001</UserId>
  <ChannelId>ch8f3k...</ChannelId>
  <RootId></RootId>
  <Message>Hello from Server A</Message>
  <Type></Type>
  <Props>
    <FromWebhook>true</FromWebhook>
    <OverrideUsername>Alice</OverrideUsername>
  </Props>
  <Hashtags></Hashtags>
  <FileIds>
    <Id>file001</Id>
    <Id>file002</Id>
  </FileIds>
  <HasReactions>false</HasReactions>
  <RemoteId>remote-post-xyz</RemoteId>
  <IsPinned>false</IsPinned>
</Post>
```

| Element | Type | Required | Description |
|---|---|---|---|
| `Id` | string | yes | Post ID. |
| `CreateAt` | int64 | yes | Creation timestamp (Unix millis). |
| `UpdateAt` | int64 | yes | Last update timestamp. |
| `EditAt` | int64 | no | Last edit timestamp (omitted if never edited). |
| `DeleteAt` | int64 | yes | Deletion timestamp (0 if not deleted). |
| `UserId` | string | yes | Author's user ID. |
| `ChannelId` | string | yes | Channel ID (rewritten by receiver). |
| `RootId` | string | no | Thread root post ID. |
| `OriginalId` | string | no | Original post ID (for cross-posted content). |
| `Message` | string | yes | Post text content. May contain markdown. |
| `Type` | string | no | Post type (empty for normal, or system post type). |
| `Props` | element | no | Typed post properties. See below. |
| `Hashtags` | string | no | Space-separated hashtags. |
| `FileIds` | container | no | Attached file IDs (`<Id>...</Id>` per file). |
| `HasReactions` | bool | no | Whether post has reactions (stored DB column). |
| `RemoteId` | string | no | Source-server post ID (loop-prevention tag). |
| `IsPinned` | bool | no | Whether post is pinned. |

**Dropped fields** (do not cross the wire):

| Field | Reason |
|---|---|
| `MessageSource` | Pre-server-processing artifact; never user-visible. |
| `PendingPostId` | Client-side ephemeral ID; no meaning across servers. |
| `ReplyCount` | Computed by receiver's SQL subquery against the Threads table. |
| `LastReplyAt` | Same as `ReplyCount`. |
| `Participants` | Not stored on Posts; computed by `populateThreadParticipants`. |
| `IsFollowing` | Per-viewer thread hint; personal to the source server. |
| `Metadata`, `Filenames` | Server-internal presentation data. |

### Post.Props (typed)

`PostProps` replaces the upstream `StringInterface` (`map[string]any`) with a
typed struct. Only the whitelisted keys cross the wire; everything else is
dropped at the sender.

| Element | Type | Upstream key | Reason carried |
|---|---|---|---|
| `FromWebhook` | bool | `from_webhook` | Bot/integration provenance flag. |
| `FromBot` | bool | `from_bot` | Same. |
| `FromPlugin` | bool | `from_plugin` | Same. |
| `FromOAuthApp` | bool | `from_oauth_app` | Same. |
| `OverrideUsername` | string | `override_username` | Webhook display-name override. |
| `OverrideIconURL` | string | `override_icon_url` | Webhook icon override. |
| `OverrideIconEmoji` | string | `override_icon_emoji` | Webhook icon emoji override. |
| `WebhookDisplayName` | string | `webhook_display_name` | Webhook display name. |
| `AddedUserId` | string | `addedUserId` | "User X was added" system message data. |
| `DeleteBy` | string | `deleteBy` | Audit trail for deletions. |
| `AddChannelMember` | string | `add_channel_member` | System message data. |
| `MentionHighlightDisabled` | bool | `mentionHighlightDisabled` | Rendering control. |
| `DisableGroupHighlight` | bool | `disable_group_highlight` | Rendering control. |
| `AIGeneratedByUserId` | string | `ai_generated_by` | AI provenance, compliance-relevant. |
| `AIGeneratedByUsername` | string | `ai_generated_by_username` | Same. |

**Dropped Post.Props keys**: `attachments`, `previewed_post`, `force_notification`,
`channel_mentions`, `current_team_id`, `unsafe_links`, `expire_at`,
`read_duration`, `shared_channel_state`, `workspace_name`.

Dropping `attachments` is a known UX regression for slash-command and webhook
posts (they relay as plain text on the receiver). Acceptable for this pass; see
the wire-types plan for the rationale.

## User

Wire-pruned subset of `model.User`.

```xml
<User id="uid001">
  <Id>uid001</Id>
  <CreateAt>1700000000000</CreateAt>
  <UpdateAt>1712956800000</UpdateAt>
  <DeleteAt>0</DeleteAt>
  <Username>alice</Username>
  <Email>alice@example.com</Email>
  <Nickname>Alice</Nickname>
  <FirstName>Alice</FirstName>
  <LastName>Smith</LastName>
  <Position>Engineer</Position>
  <Roles>system_user</Roles>
  <Locale>en</Locale>
  <Timezone>
    <Entry key="automaticTimezone" value="America/New_York"/>
    <Entry key="manualTimezone" value=""/>
    <Entry key="useAutomaticTimezone" value="true"/>
  </Timezone>
  <Props>
    <CustomStatus>{"emoji":":wave:","text":"hi"}</CustomStatus>
    <RemoteUsername>alice@source</RemoteUsername>
    <RemoteEmail>alice@source.example.com</RemoteEmail>
    <OriginalRemoteId>remote-source</OriginalRemoteId>
  </Props>
  <RemoteId>remote-xyz</RemoteId>
  <IsBot>false</IsBot>
</User>
```

| Element | Type | Required | Description |
|---|---|---|---|
| `id` (attr) | string | yes | User ID (map key from `SyncMsg.Users`). |
| `Id` | string | yes | User ID. |
| `CreateAt` | int64 | no | Account creation timestamp. |
| `UpdateAt` | int64 | no | Last profile update timestamp. |
| `DeleteAt` | int64 | yes | Deletion timestamp (0 if active). |
| `Username` | string | yes | Username. |
| `Email` | string | yes | Email address. The receiver reads this to populate `Props.RemoteEmail`, then overwrites the local column with `model.NewId()`. The receiver's User row does not carry the original email. |
| `Nickname` | string | no | Display nickname. |
| `FirstName` | string | no | First name. |
| `LastName` | string | no | Last name. |
| `Position` | string | no | Job title / position. |
| `Roles` | string | yes | Always `system_user`; the receiver forces this regardless of the wire value. |
| `Locale` | string | no | Preferred locale. |
| `Timezone` | `StringMapType` | no | Timezone settings as `<Entry key= value=>` sequence (sorted by key). |
| `Props` | element | no | Typed user properties. See below. |
| `RemoteId` | string | no | Source-server remote ID (loop-prevention tag). |
| `IsBot` | bool | no | Whether user is a bot. |
| `BotDescription` | string | no | Bot description (omitted if not bot). |
| `BotLastIconUpdate` | int64 | no | Bot icon last update (omitted if not bot). |

**Dropped fields** (do not cross the wire):

| Field | Reason |
|---|---|
| `AuthService` | `sanitizeUserForSync` wipes to `""`. |
| `EmailVerified` | Local verification state; never crosses meaningfully. |
| `AllowMarketing` | `sanitizeUserForSync` wipes to `false`. |
| `NotifyProps` | `sanitizeUserForSync` wipes to empty map. |
| `LastPasswordUpdate` | `sanitizeUserForSync` wipes to `0`. |
| `LastPictureUpdate` | `sanitizeUserForSync` wipes to `0`; profile image sync uses a separate channel. |
| `FailedAttempts` | `sanitizeUserForSync` wipes to `0`. |
| `MfaActive` | `sanitizeUserForSync` wipes to `false`. |
| `LastActivityAt` | Status sync carries this separately. |
| `TermsOfServiceId`, `TermsOfServiceCreateAt` | Local ToS acceptance. |
| `DisableWelcomeEmail` | Local notification preference. |
| `LastLogin` | Local session telemetry. |
| `Password`, `AuthData`, `MfaSecret`, `MfaUsedTimestamps` | Credential material; tagged `xml:"-"` upstream. |

### User.Props (typed)

`UserProps` replaces the upstream `StringMap` with a typed struct. Only the
keys set by the shared-channels framework cross the wire.

| Element | Type | Upstream key | Reason |
|---|---|---|---|
| `CustomStatus` | string | `customStatus` | User-visible emoji and text. |
| `RemoteUsername` | string | `RemoteUsername` | Real source-server username (the local username may be mangled for uniqueness). |
| `RemoteEmail` | string | `RemoteEmail` | Real source-server email (the local Email column is replaced by `model.NewId()`). |
| `OriginalRemoteId` | string | `OriginalRemoteId` | Provenance through multi-hop relays. |

Unknown User.Props keys are dropped.

## Reaction

All upstream fields cross. No pruning.

```xml
<Reaction>
  <UserId>uid001</UserId>
  <PostId>post123</PostId>
  <EmojiName>thumbsup</EmojiName>
  <CreateAt>1712956800000</CreateAt>
  <UpdateAt>1712956800000</UpdateAt>
  <DeleteAt>0</DeleteAt>
  <RemoteId>remote-xyz</RemoteId>
  <ChannelId>ch8f3k...</ChannelId>
</Reaction>
```

| Element | Type | Required | Description |
|---|---|---|---|
| `UserId` | string | yes | User who reacted. |
| `PostId` | string | yes | Post the reaction is on. |
| `EmojiName` | string | yes | Emoji short code (e.g., `thumbsup`). |
| `CreateAt` | int64 | yes | Creation timestamp. |
| `UpdateAt` | int64 | yes | Update timestamp. |
| `DeleteAt` | int64 | yes | Deletion timestamp (0 if active). |
| `RemoteId` | string | no | Source-server remote ID. |
| `ChannelId` | string | yes | Channel ID. |

## Status

```xml
<Status>
  <UserId>uid001</UserId>
  <Status>online</Status>
  <Manual>false</Manual>
  <LastActivityAt>1712956800000</LastActivityAt>
  <DNDEndTime>0</DNDEndTime>
</Status>
```

| Element | Type | Required | Description |
|---|---|---|---|
| `UserId` | string | yes | User ID. |
| `Status` | string | yes | Status value: `online`, `away`, `offline`, `dnd`, `ooo`. |
| `Manual` | bool | yes | Whether status was set manually. |
| `LastActivityAt` | int64 | yes | Last activity timestamp. |
| `DNDEndTime` | int64 | yes | DND expiry (seconds, not milliseconds; 0 if not DND). |

**Dropped fields**: `ActiveChannel` (channel ID is local to the source server;
meaningless on the receiver), `PrevStatus` (tagged `json:"-"` upstream).

## MembershipChange

All upstream fields cross. The Go type is renamed from `MembershipChangeMsg` to
`MembershipChange` to drop the redundant `Msg` suffix; the XML element follows.

```xml
<MembershipChange>
  <ChannelId>ch8f3k...</ChannelId>
  <UserId>uid001</UserId>
  <IsAdd>true</IsAdd>
  <RemoteId>remote-xyz</RemoteId>
  <ChangeTime>1712956800000</ChangeTime>
</MembershipChange>
```

| Element | Type | Required | Description |
|---|---|---|---|
| `ChannelId` | string | yes | Channel ID. |
| `UserId` | string | yes | User joining or leaving. |
| `IsAdd` | bool | yes | `true` = join, `false` = leave. |
| `RemoteId` | string | no | Source-server remote ID. |
| `ChangeTime` | int64 | yes | Timestamp of the change. |

## PostAcknowledgement

All upstream fields cross.

```xml
<PostAcknowledgement>
  <UserId>uid001</UserId>
  <PostId>post123</PostId>
  <AcknowledgedAt>1712956800000</AcknowledgedAt>
  <ChannelId>ch8f3k...</ChannelId>
  <RemoteId>remote-xyz</RemoteId>
</PostAcknowledgement>
```

| Element | Type | Required | Description |
|---|---|---|---|
| `UserId` | string | yes | User who acknowledged. |
| `PostId` | string | yes | Acknowledged post ID. |
| `AcknowledgedAt` | int64 | yes | Acknowledgement timestamp. |
| `ChannelId` | string | yes | Channel ID. |
| `RemoteId` | string | no | Source-server remote ID. |

## File attachments and profile images

File content does not travel inside `SyncMsg`. It is sent on a separate
file-transfer channel via the `QueueProvider.UploadFile` / `WatchFiles` path.
Posts reference attached files by ID through the `<FileIds>` container; the
file payloads themselves are matched on the receiver by post and file ID.

## Test Message Example

```xml
<?xml version="1.0" encoding="UTF-8"?>
<CrossGuardEnvelope version="1" type="test">
  <ConnName>nats-low-to-high</ConnName>
  <Timestamp>2026-04-14T18:30:00Z</Timestamp>
  <TeamName></TeamName>
  <ChannelName></ChannelName>
  <TestID>test-5f8a3c2b-1d4e-4a6f-9b8c-7e2d1f3a5b4c</TestID>
</CrossGuardEnvelope>
```

## Complete sync_msg Example

A full envelope with one user, one post, and one mention transform:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<CrossGuardEnvelope version="1" type="sync_msg">
  <ConnName>nats-low-to-high</ConnName>
  <Timestamp>2026-04-14T18:30:00Z</Timestamp>
  <TeamName>test-a</TeamName>
  <ChannelName>town-square</ChannelName>
  <SyncMsg>
    <Id>sm-001</Id>
    <ChannelId>ch8f3kabcdef1234567890</ChannelId>
    <Users>
      <User id="uid001abcdef1234567890">
        <Id>uid001abcdef1234567890</Id>
        <CreateAt>1700000000000</CreateAt>
        <UpdateAt>1712956800000</UpdateAt>
        <DeleteAt>0</DeleteAt>
        <Username>alice</Username>
        <Email>alice@server-a.example.com</Email>
        <Nickname>Alice</Nickname>
        <FirstName>Alice</FirstName>
        <LastName>Smith</LastName>
        <Roles>system_user</Roles>
        <Locale>en</Locale>
        <RemoteId>remote-cluster-abc123</RemoteId>
      </User>
    </Users>
    <Posts>
      <Post>
        <Id>post001abcdef1234567890</Id>
        <CreateAt>1712956800000</CreateAt>
        <UpdateAt>1712956800000</UpdateAt>
        <DeleteAt>0</DeleteAt>
        <UserId>uid001abcdef1234567890</UserId>
        <ChannelId>ch8f3kabcdef1234567890</ChannelId>
        <Message>Hello from Server A.</Message>
      </Post>
    </Posts>
    <MentionTransforms>
      <Transform key="@alice" value="uid001abcdef1234567890"/>
    </MentionTransforms>
  </SyncMsg>
</CrossGuardEnvelope>
```

## Versioning

The `version` attribute on `CrossGuardEnvelope` allows future schema evolution:

- **Version 1**: Current schema (plugin-owned wire types).
- Receivers should ignore unknown elements for forward compatibility.
- Adding new fields to wire types is a Version 1 schema regeneration; both the
  schema (`schema/crossguard.xsd`) and the compliance review must be updated
  before deployment.
- Removing or renaming existing elements requires a version bump.

## When to regenerate

The XSD and the example files in `schema/examples/` are derived from the Go
wire types. Regenerate them whenever a wire type changes:

1. Update the Go struct in `server/wire/`.
2. Regenerate examples: `UPDATE_EXAMPLES=1 go test -run TestExampleFiles ./server/`.
3. Update `schema/crossguard.xsd` to match the new field set or order.
4. Validate: `python3 -c "import xmlschema; [...]"` or equivalent, running the
   examples through the new XSD.
5. Re-run the compliance review against the new XSD before deployment.
