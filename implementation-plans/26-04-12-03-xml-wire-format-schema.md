# Cross Guard: XML Wire Format Schema

Referenced from: [Shared Channels API Refactor Plan](26-04-12-02-shared-channels-api-refactor.md)

## Overview

All cross-domain traffic between Cross Guard plugin instances is serialized as XML.
This is required by customer content inspection tooling that must parse all traffic
crossing security domain boundaries.

The XML schema mirrors the Mattermost server's shared channel sync types. Each Go
struct field maps to an XML element using PascalCase derived from the Go field name
(e.g., the Go field `ChannelId` with tag `json:"channel_id"` becomes `<ChannelId>`).
The `xml` struct tags added to the server model types define these element names
explicitly. Fields tagged `json:"-"` (internal/non-serialized) are excluded.

## Conventions

- Element names use `PascalCase` derived from Go field names, not from `json` tag
  values (e.g., Go field `ChannelId` with `json:"channel_id"` becomes `<ChannelId>`,
  not `<channel_id>`). The `xml:"..."` struct tags on the server model types define
  these names explicitly.
- Map types (`map[string]*T`, `map[string]string`) are serialized as repeated child
  elements with key attributes, since `encoding/xml` does not natively support Go maps.
  Custom `MarshalXML`/`UnmarshalXML` methods handle these conversions.
- Boolean fields serialize as `true`/`false`.
- Integer fields serialize as decimal strings.
- Optional fields (Go pointer types or `omitempty` tags) are omitted when zero/nil.
  For string fields with `omitempty`, empty strings are also omitted (not serialized
  as empty elements like `<RootId></RootId>`). String fields without `omitempty`
  serialize as empty elements when the value is `""`.
- String arrays serialize as repeated child elements.
- Map element names vary by context for readability: `<Entry>` for generic StringMap
  fields (User.Props, User.NotifyProps, User.Timezone), `<Prop>` for Post.Props
  (StringInterface), and `<Transform>` for MentionTransforms. This is intentional so
  element names are self-documenting in inspection tool output.

## Root Element: CrossGuardEnvelope

The `TransportEnvelope` is the root element of every message on the wire.

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
| `Timestamp` | string | yes | RFC 3339 UTC timestamp of when the envelope was created. Used for message ordering, retry-queue age calculations, and audit trails. |
| `TeamName` | string | yes | Source team name (used by receiver to look up local team). |
| `ChannelName` | string | yes | Source channel name (used by receiver to look up local channel). |
| `SyncMsg` | element | when type=`sync_msg` | Content payload. See below. |
| `TestID` | string | when type=`test` | Unique ID for test-connection round-trip. |

### Type values

| Value | Description |
|---|---|
| `sync_msg` | Content sync: posts, users, reactions, memberships, etc. |
| `attachment` | File attachment sync (stubbed, reserved for future use). |
| `profile_image` | User profile image sync (stubbed, reserved for future use). |
| `test` | Connectivity test message. |

## SyncMsg

Mirrors `model.SyncMsg`. Contains batched content changes for a single channel.

```xml
<SyncMsg>
  <Id>abc123</Id>
  <ChannelId>ch8f3k...</ChannelId>
  <Users>
    <User id="uid001">...</User>
    <User id="uid002">...</User>
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
    <Transform key="@bob" value="uid002"/>
  </MentionTransforms>
</SyncMsg>
```

| Element | Type | Required | Description |
|---|---|---|---|
| `Id` | string | yes | Unique sync message identifier. |
| `ChannelId` | string | yes | Channel ID (rewritten to local ID by receiving plugin). |
| `Users` | container | no | Users referenced by posts in this batch. |
| `Users/User` | element | repeating | User profile. `id` attribute = user ID (map key). |
| `Posts` | container | no | Posts created, updated, or deleted. |
| `Posts/Post` | element | repeating | Single post. |
| `Reactions` | container | no | Reactions added or removed. |
| `Reactions/Reaction` | element | repeating | Single reaction. |
| `Statuses` | container | no | User online/away/offline statuses. |
| `Statuses/Status` | element | repeating | Single status. |
| `MembershipChanges` | container | no | Channel membership join/leave events. |
| `MembershipChanges/MembershipChange` | element | repeating | Single membership change. |
| `Acknowledgements` | container | no | Post acknowledgements. |
| `Acknowledgements/PostAcknowledgement` | element | repeating | Single acknowledgement. |
| `MentionTransforms` | container | no | @mention username to user ID mappings. |
| `MentionTransforms/Transform` | element | repeating | `key` attr = @mention, `value` attr = user ID. |

### Map serialization

Two fields in `SyncMsg` are Go maps that require custom XML handling:

**`Users` (`map[string]*User`)**: Serialized as `<Users>` containing repeated `<User>`
elements. The map key (user ID) is carried as the `id` attribute on each `<User>`
element, and also appears as the `<Id>` child element within the User.

**`MentionTransforms` (`map[string]string`)**: Serialized as `<MentionTransforms>`
containing repeated `<Transform>` elements with `key` and `value` attributes.

## Post

Mirrors `model.Post`. Fields tagged `json:"-"` in the Go struct are excluded.

```xml
<Post>
  <Id>post123</Id>
  <CreateAt>1712956800000</CreateAt>
  <UpdateAt>1712956800000</UpdateAt>
  <EditAt>0</EditAt>
  <DeleteAt>0</DeleteAt>
  <IsPinned>false</IsPinned>
  <UserId>uid001</UserId>
  <ChannelId>ch8f3k...</ChannelId>
  <RootId></RootId>
  <OriginalId></OriginalId>
  <Message>Hello from Server A</Message>
  <Type></Type>
  <Props>
    <Prop key="from_webhook" value="false"/>
  </Props>
  <Hashtags></Hashtags>
  <FileIds>
    <Id>file001</Id>
    <Id>file002</Id>
  </FileIds>
  <PendingPostId></PendingPostId>
  <HasReactions>false</HasReactions>
  <RemoteId>remote-post-xyz</RemoteId>
  <ReplyCount>0</ReplyCount>
  <LastReplyAt>0</LastReplyAt>
</Post>
```

| Element | Type | Required | Description |
|---|---|---|---|
| `Id` | string | yes | Post ID. |
| `CreateAt` | int64 | yes | Creation timestamp (Unix millis). |
| `UpdateAt` | int64 | yes | Last update timestamp. |
| `EditAt` | int64 | yes | Last edit timestamp (0 if never edited). |
| `DeleteAt` | int64 | yes | Deletion timestamp (0 if not deleted). |
| `IsPinned` | bool | yes | Whether post is pinned. |
| `UserId` | string | yes | Author's user ID. |
| `ChannelId` | string | yes | Channel ID (rewritten by receiver). |
| `RootId` | string | yes | Thread root post ID (empty if top-level). |
| `OriginalId` | string | yes | Original post ID (for cross-posted content). |
| `Message` | string | yes | Post text content. May contain markdown. |
| `MessageSource` | string | no | Original message before server processing. |
| `Type` | string | yes | Post type (empty for normal, or system post type). |
| `Props` | container | no | Post properties (key-value metadata). |
| `Props/Prop` | element | repeating | `key` attr = property name, `value` attr = property value. |
| `Hashtags` | string | yes | Space-separated hashtags. |
| `FileIds` | container | no | Attached file IDs. |
| `FileIds/Id` | string | repeating | Single file ID. |
| `PendingPostId` | string | yes | Client-assigned pending ID. |
| `HasReactions` | bool | no | Whether post has reactions. |
| `RemoteId` | string | no | Remote server's post ID (nil/omitted if local). |
| `ReplyCount` | int64 | yes | Number of replies in thread. |
| `LastReplyAt` | int64 | yes | Timestamp of last reply. |
| `Participants` | container | no | Thread participants. Serialized as repeated `<User>` elements, same schema as `SyncMsg.Users/User` but without the `id` attribute (since this is a slice, not a map). |
| `Metadata` | element | no | **Excluded from XML serialization.** `PostMetadata` contains complex nested types (`Embeds`, `Emojis`, `Files`, `Images`, `Reactions`) that are server-internal presentation data. The receiving server reconstructs metadata from the post content. The `xml:"-"` tag on `Metadata` ensures it is omitted. |

**Excluded fields** (tagged `json:"-"` in Go): `Filenames`.

**Note on Props**: `Post.Props` is `StringInterface` (`map[string]any`) in Go. For XML
serialization, values are converted to their string representation. Complex nested
values (rare in practice) are JSON-encoded as the value string. On deserialization,
all values are restored as strings. `ReceiveSharedChannelSyncMsg` accepts string values
in Props, so no type reconstruction is needed.

## User

Mirrors `model.User`. Sensitive fields (`Password`, `MfaSecret`, `AuthData`) are
included in the struct definition but are typically empty in sync messages (the server
strips them before delivery to the plugin hook).

```xml
<User id="uid001">
  <Id>uid001</Id>
  <CreateAt>1700000000000</CreateAt>
  <UpdateAt>1712956800000</UpdateAt>
  <DeleteAt>0</DeleteAt>
  <Username>alice</Username>
  <Email>alice@example.com</Email>
  <EmailVerified>true</EmailVerified>
  <Nickname>Alice</Nickname>
  <FirstName>Alice</FirstName>
  <LastName>Smith</LastName>
  <Position>Engineer</Position>
  <Roles>system_user</Roles>
  <Locale>en</Locale>
  <Timezone>
    <Entry key="useAutomaticTimezone" value="true"/>
    <Entry key="automaticTimezone" value="America/New_York"/>
    <Entry key="manualTimezone" value=""/>
  </Timezone>
  <RemoteId>remote-xyz</RemoteId>
  <IsBot>false</IsBot>
  <Props>
    <Entry key="customStatus" value="..."/>
  </Props>
  <NotifyProps>
    <Entry key="desktop" value="default"/>
    <Entry key="email" value="true"/>
  </NotifyProps>
  <LastPictureUpdate>1712000000000</LastPictureUpdate>
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
| `Password` | string | no | Always empty in sync messages. |
| `AuthData` | string | no | External auth identifier. |
| `AuthService` | string | yes | Auth service name (empty for email). |
| `Email` | string | yes | Email address. |
| `EmailVerified` | bool | no | Whether email is verified. |
| `Nickname` | string | yes | Display nickname. |
| `FirstName` | string | yes | First name. |
| `LastName` | string | yes | Last name. |
| `Position` | string | yes | Job title / position. |
| `Roles` | string | yes | Space-separated role names. |
| `AllowMarketing` | bool | no | Marketing email opt-in. |
| `Props` | container | no | User properties (StringMap). |
| `Props/Entry` | element | repeating | `key`/`value` attributes. |
| `NotifyProps` | container | no | Notification preferences (StringMap). |
| `NotifyProps/Entry` | element | repeating | `key`/`value` attributes. |
| `LastPasswordUpdate` | int64 | no | Last password change timestamp. |
| `LastPictureUpdate` | int64 | no | Last profile image change timestamp. |
| `FailedAttempts` | int | no | Failed login attempt count. |
| `Locale` | string | yes | Preferred locale. |
| `Timezone` | container | yes | Timezone settings (StringMap). |
| `Timezone/Entry` | element | repeating | `key`/`value` attributes. |
| `MfaActive` | bool | no | Whether MFA is enabled. |
| `RemoteId` | string | no | Remote cluster ID (set for synced users). |
| `LastActivityAt` | int64 | no | Last activity timestamp. |
| `IsBot` | bool | no | Whether user is a bot. |
| `BotDescription` | string | no | Bot description. |
| `BotLastIconUpdate` | int64 | no | Bot icon last update. |
| `TermsOfServiceId` | string | no | Accepted ToS version. |
| `TermsOfServiceCreateAt` | int64 | no | ToS acceptance timestamp. |
| `DisableWelcomeEmail` | bool | yes | Whether welcome email is disabled. |
| `LastLogin` | int64 | no | Last login timestamp. |

**Excluded fields**: `MfaSecret` (sensitive, never serialized in sync).

## Reaction

Mirrors `model.Reaction`.

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
| `RemoteId` | string | no | Remote cluster ID. |
| `ChannelId` | string | yes | Channel ID. |

## Status

Mirrors `model.Status`. Fields tagged `json:"-"` are excluded.

```xml
<Status>
  <UserId>uid001</UserId>
  <Status>online</Status>
  <Manual>false</Manual>
  <LastActivityAt>1712956800000</LastActivityAt>
  <ActiveChannel>ch8f3k...</ActiveChannel>
  <DNDEndTime>0</DNDEndTime>
</Status>
```

| Element | Type | Required | Description |
|---|---|---|---|
| `UserId` | string | yes | User ID. |
| `Status` | string | yes | Status value: `online`, `away`, `offline`, `dnd`. |
| `Manual` | bool | yes | Whether status was set manually. |
| `LastActivityAt` | int64 | yes | Last activity timestamp. |
| `ActiveChannel` | string | no | Currently active channel ID. |
| `DNDEndTime` | int64 | yes | DND expiry timestamp (0 if not DND). |

**Excluded fields**: `PrevStatus` (tagged `json:"-"`).

## MembershipChange

Mirrors `model.MembershipChangeMsg`.

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
| `RemoteId` | string | yes | Remote cluster ID. |
| `ChangeTime` | int64 | yes | Timestamp of the change. |

## PostAcknowledgement

Mirrors `model.PostAcknowledgement`.

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
| `RemoteId` | string | no | Remote cluster ID. |

## FileInfo (reserved for future use)

Mirrors `model.FileInfo`. Included here for completeness; file attachment sync is
stubbed in the initial implementation. Fields tagged `json:"-"` are excluded.

```xml
<FileInfo>
  <Id>file001</Id>
  <CreatorId>uid001</CreatorId>
  <PostId>post123</PostId>
  <ChannelId>ch8f3k...</ChannelId>
  <CreateAt>1712956800000</CreateAt>
  <UpdateAt>1712956800000</UpdateAt>
  <DeleteAt>0</DeleteAt>
  <Name>document.pdf</Name>
  <Extension>pdf</Extension>
  <Size>204800</Size>
  <MimeType>application/pdf</MimeType>
  <Width>0</Width>
  <Height>0</Height>
  <HasPreviewImage>false</HasPreviewImage>
  <RemoteId>remote-xyz</RemoteId>
  <Archived>false</Archived>
</FileInfo>
```

| Element | Type | Required | Description |
|---|---|---|---|
| `Id` | string | yes | File ID. |
| `CreatorId` | string | yes | Uploader's user ID. |
| `PostId` | string | no | Associated post ID. |
| `ChannelId` | string | yes | Channel ID. |
| `CreateAt` | int64 | yes | Upload timestamp. |
| `UpdateAt` | int64 | yes | Update timestamp. |
| `DeleteAt` | int64 | yes | Deletion timestamp (0 if active). |
| `Name` | string | yes | Original filename. |
| `Extension` | string | yes | File extension (without dot). |
| `Size` | int64 | yes | File size in bytes. |
| `MimeType` | string | yes | MIME type. |
| `Width` | int | no | Image width in pixels. |
| `Height` | int | no | Image height in pixels. |
| `HasPreviewImage` | bool | no | Whether a preview was generated. |
| `MiniPreview` | base64 | no | Thumbnail preview bytes (base64-encoded). |
| `RemoteId` | string | no | Remote cluster ID. |
| `Archived` | bool | yes | Whether file is archived. |

**Excluded fields**: `Path`, `ThumbnailPath`, `PreviewPath`, `Content` (all tagged
`json:"-"`, internal storage paths).

## Complete Example

A full `sync_msg` envelope containing one user and one post:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<CrossGuardEnvelope version="1" type="sync_msg">
  <ConnName>nats-low-to-high</ConnName>
  <Timestamp>2026-04-14T18:30:00Z</Timestamp>
  <TeamName>test-a</TeamName>
  <ChannelName>town-square</ChannelName>
  <SyncMsg>
    <Id>sync-msg-001</Id>
    <ChannelId>ch8f3kabcdef1234567890</ChannelId>
    <Users>
      <User id="uid001abcdef1234567890">
        <Id>uid001abcdef1234567890</Id>
        <CreateAt>1700000000000</CreateAt>
        <UpdateAt>1712956800000</UpdateAt>
        <DeleteAt>0</DeleteAt>
        <Username>alice</Username>
        <Email>alice@server-a.example.com</Email>
        <EmailVerified>true</EmailVerified>
        <Nickname>Alice</Nickname>
        <FirstName>Alice</FirstName>
        <LastName>Smith</LastName>
        <Position></Position>
        <Roles>system_user</Roles>
        <Locale>en</Locale>
        <Timezone>
          <Entry key="useAutomaticTimezone" value="true"/>
          <Entry key="automaticTimezone" value="America/New_York"/>
          <Entry key="manualTimezone" value=""/>
        </Timezone>
        <RemoteId>remote-cluster-abc123</RemoteId>
        <IsBot>false</IsBot>
        <DisableWelcomeEmail>false</DisableWelcomeEmail>
      </User>
    </Users>
    <Posts>
      <Post>
        <Id>post001abcdef1234567890</Id>
        <CreateAt>1712956800000</CreateAt>
        <UpdateAt>1712956800000</UpdateAt>
        <EditAt>0</EditAt>
        <DeleteAt>0</DeleteAt>
        <IsPinned>false</IsPinned>
        <UserId>uid001abcdef1234567890</UserId>
        <ChannelId>ch8f3kabcdef1234567890</ChannelId>
        <RootId></RootId>
        <OriginalId></OriginalId>
        <Message>Hello from Server A! This is a cross-domain message.</Message>
        <Type></Type>
        <Hashtags></Hashtags>
        <PendingPostId></PendingPostId>
        <HasReactions>false</HasReactions>
        <ReplyCount>0</ReplyCount>
        <LastReplyAt>0</LastReplyAt>
      </Post>
    </Posts>
    <MentionTransforms>
      <Transform key="@alice" value="uid001abcdef1234567890"/>
    </MentionTransforms>
  </SyncMsg>
</CrossGuardEnvelope>
```

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

## StringMap Serialization Pattern

Go `StringMap` (`map[string]string`) fields appear in User (`Props`, `NotifyProps`,
`Timezone`) and Post (`Props` as `StringInterface`). They serialize as:

```xml
<Props>
  <Entry key="keyname" value="keyvalue"/>
  <Entry key="another" value="anothervalue"/>
</Props>
```

This pattern is used consistently for all map-typed fields throughout the schema.

## StringArray Serialization Pattern

Go `StringArray` (`[]string`) fields appear in Post (`FileIds`) and User
(`MfaUsedTimestamps`). They serialize as repeated child elements:

```xml
<FileIds>
  <Id>fileid001</Id>
  <Id>fileid002</Id>
</FileIds>
```

## Versioning

The `version` attribute on `CrossGuardEnvelope` allows future schema evolution:

- **Version 1**: Initial schema as documented here.
- Future versions may add new elements. Receivers should ignore unknown elements
  for forward compatibility.
- Removing or renaming existing elements requires a version bump. Receivers must
  check the version and reject unsupported versions with an error.
