# Plugin-owned wire types

## Context

Today the plugin marshals and unmarshals `*mmModel.SyncMsg` directly. The
bytes on the wire are whatever upstream Mattermost's `MarshalXML` emits:
`<Fields>` wrapper around the non-map fields, `<UserEntry id="...">
<User>...</User></UserEntry>` for the Users map, `<MembershipChangeMsg>`
for membership change leaves, and every field of every nested type
(`Post`, `User`, `Reaction`, ...) regardless of whether it should cross
a security boundary.

This is wrong for three reasons:

1. **Compliance review.** The XSD that ships to security review must
   enumerate every field that crosses a domain boundary. Today it
   either tracks upstream blindly (a maintenance treadmill that breaks
   on every `server/public` version bump) or uses `xs:any` placeholders
   that obscure the actual fields. Neither is acceptable for approval.
2. **Privilege leakage.** Fields like `User.MfaActive`,
   `User.LastPasswordUpdate`, and `Post.Props["force_notification"]`
   carry data that should not cross domains. The receiver's
   `sanitizeUserForSync` wipes some of them on arrival, but the bytes
   still appear on the wire and in inspection logs.
3. **Wasted bandwidth and review surface.** Fields the receiver
   computes from its own database (`Post.ReplyCount`, `Post.LastReplyAt`,
   `Post.Participants`) are sent and immediately discarded by the
   receiver's SQL subqueries.

The fix is to own the wire types in the plugin. Define a `server/wire/`
package with seven structs that mirror upstream's shared-channel
content types but with a pruned, explicitly-typed field set. Marshal
those instead of `*mmModel.SyncMsg`; convert at the boundaries.

## Out of scope

- Changing the envelope or its routing fields. `TransportEnvelope`
  stays as it is (version, type, ConnName, Timestamp, TeamName,
  ChannelName, plus SyncMsg or TestID).
- File attachments and profile images. Those travel on the
  `QueueProvider.UploadFile` / `WatchFiles` path, not through
  `TransportEnvelope.SyncMsg`. The wire types do not include them.
- `Post.Props["attachments"]` (`MessageAttachment` rich content).
  Dropping this is a known UX regression for slash-command and
  webhook posts (they relay as plain text). Acceptable for this pass;
  revisit if a customer reports degraded integration rendering.
- Server-side changes. Nothing in upstream Mattermost moves; the
  framework still hands us `*mmModel.SyncMsg` and we still feed
  `*mmModel.SyncMsg` into `ReceiveSharedChannelSyncMsg`.

## Target structure

```
server/wire/
├── doc.go                 package overview + the security argument
├── wire.go                shared helpers (StringMap encoding, etc.)
├── envelope.go            (no, stays in server/transport.go)
├── sync_msg.go            SyncMsg + custom MarshalXML for the Users map
├── post.go                Post, PostProps
├── user.go                User, UserProps
├── reaction.go            Reaction
├── status.go              Status
├── membership_change.go   MembershipChange
├── post_acknowledgement.go PostAcknowledgement
├── sync_msg_test.go
├── post_test.go
├── user_test.go
├── reaction_test.go
├── status_test.go
├── membership_change_test.go
└── post_acknowledgement_test.go
```

Package `main` (in `server/`) imports `wire`. The reverse must not
happen; `wire` has no plugin-internal dependencies. `wire` imports
`encoding/xml` and `github.com/mattermost/mattermost/server/public/model`
(the latter only inside `*FromModel` / `*ToModel` constructors so the
upstream import stays contained).

## Wire field set

The complete pruned field list. Element names match the Go field names
verbatim (e.g., `Post.UserId` → `<UserId>`). The `xml:"...,omitempty"`
tag is used for any field that should be elided when zero or empty,
which is every optional field below.

### `SyncMsg`

| Field | Go type | Required | Note |
|---|---|---|---|
| Id | string | yes | Sync message identifier |
| ChannelId | string | yes | Target channel |
| Users | `map[string]*User` | no | Custom marshaler: `<Users><User id="...">...</User></Users>` (no `<UserEntry>` wrapper) |
| Posts | `[]*Post` | no | `<Posts><Post>...</Post></Posts>` |
| Reactions | `[]*Reaction` | no | `<Reactions><Reaction>...</Reaction></Reactions>` |
| Statuses | `[]*Status` | no | `<Statuses><Status>...</Status></Statuses>` |
| MembershipChanges | `[]*MembershipChange` | no | Element name is `<MembershipChange>` (drop the `Msg` suffix; cosmetic only) |
| Acknowledgements | `[]*PostAcknowledgement` | no | `<Acknowledgements><PostAcknowledgement>...` |
| MentionTransforms | `map[string]string` | no | `<MentionTransforms><Transform key="..." value="..."/></MentionTransforms>` |

### `Post` (17 fields)

| Field | Go type | Cross? | Reason |
|---|---|---|---|
| Id, CreateAt, UpdateAt, EditAt, DeleteAt | various | KEEP | Identity and lifecycle |
| UserId, ChannelId, RootId, OriginalId | string | KEEP | Routing and edit/thread linkage |
| Message | string | KEEP | The content |
| Type | string | KEEP | System vs normal post |
| Props | `*PostProps` | KEEP | See typed struct below |
| Hashtags | string | KEEP | Receiver indexes |
| FileIds | `[]string` | KEEP | Resolved against synced attachments |
| HasReactions | bool | KEEP | Stored DB column; not derived |
| RemoteId | string | KEEP | Loop-prevention tag |
| IsPinned | bool | KEEP | User-visible state |

**Dropped** (with the receiver's behavior justifying each):

| Field | Why |
|---|---|
| MessageSource | Pre-server-processing artifact; never user-visible |
| PendingPostId | Client-side ephemeral ID, no meaning across servers |
| ReplyCount | Receiver's SQL queries compute this; sender's value ignored |
| LastReplyAt | Same as ReplyCount; computed from Threads table |
| IsFollowing | Per-viewer thread hint, personal to the source server |
| Participants | Never stored on Posts; computed by `populateThreadParticipants` |

### `PostProps`

Typed struct replacing `StringInterface` map. Fifteen whitelisted keys.

| Field | Upstream key | Type | Reason |
|---|---|---|---|
| FromWebhook | from_webhook | bool | Bot/integration provenance flag |
| FromBot | from_bot | bool | Same |
| FromPlugin | from_plugin | bool | Same |
| FromOAuthApp | from_oauth_app | bool | Same |
| OverrideUsername | override_username | string | Webhook display-name override |
| OverrideIconURL | override_icon_url | string | Webhook icon override |
| OverrideIconEmoji | override_icon_emoji | string | Webhook icon emoji override |
| WebhookDisplayName | webhook_display_name | string | Webhook display name |
| AddedUserId | addedUserId | string | "User X was added" system message data |
| DeleteBy | deleteBy | string | Audit trail for deletions |
| AddChannelMember | add_channel_member | string | System message data |
| MentionHighlightDisabled | mentionHighlightDisabled | bool | Rendering control |
| DisableGroupHighlight | disable_group_highlight | bool | Rendering control |
| AIGeneratedByUserId | ai_generated_by | string | AI provenance, compliance-relevant |
| AIGeneratedByUsername | ai_generated_by_username | string | Same |

**Dropped** keys: `attachments`, `previewed_post`, `force_notification`,
`channel_mentions`, `current_team_id`, `unsafe_links`, `expire_at`,
`read_duration`, `shared_channel_state`, `workspace_name`.

`PostPropsFromModel(map[string]any) *PostProps` returns nil when no
known keys are populated, so an empty Props produces an omitted XML
element rather than a `<Props/>` self-closer.

### `User` (~17 fields)

| Field | Cross? | Reason |
|---|---|---|
| Id, CreateAt, UpdateAt, DeleteAt | KEEP | Identity and lifecycle |
| Username | KEEP | Display name on receiver |
| Email | KEEP | Receiver reads to populate `Props.RemoteEmail`, then replaces the column with `model.NewId()`. Worth flagging for compliance: the receiver's User row does not actually carry the original email |
| Nickname, FirstName, LastName, Position | KEEP | Display fields |
| Roles | KEEP | Always `system_user`; receiver forces this regardless of what we send |
| Locale, Timezone | KEEP | Affects timestamp rendering |
| Props | KEEP | `*UserProps` (typed) — see below |
| RemoteId | KEEP | Loop-prevention tag |
| IsBot, BotDescription, BotLastIconUpdate | KEEP | Bot users render differently |

**Dropped** (receiver behavior confirms each):

| Field | Why |
|---|---|
| AuthService | `sanitizeUserForSync` wipes to `""` |
| EmailVerified | Local verification state, never crosses meaningfully |
| AllowMarketing | `sanitizeUserForSync` wipes to false |
| NotifyProps | `sanitizeUserForSync` wipes to empty map |
| LastPasswordUpdate | `sanitizeUserForSync` wipes to 0 |
| LastPictureUpdate | `sanitizeUserForSync` wipes to 0; profile image sync uses a separate API |
| FailedAttempts | `sanitizeUserForSync` wipes to 0 |
| MfaActive | `sanitizeUserForSync` wipes to false |
| LastActivityAt | Status sync carries this separately |
| TermsOfServiceId, TermsOfServiceCreateAt | Local ToS acceptance |
| DisableWelcomeEmail | Local notification preference |
| LastLogin | Local session telemetry |
| Password, AuthData, MfaSecret, MfaUsedTimestamps | Already `xml:"-"` upstream |

### `UserProps`

Typed struct replacing `StringMap`. Four whitelisted keys.

| Field | Upstream key | Reason |
|---|---|---|
| CustomStatus | customStatus | User-visible emoji + text |
| RemoteUsername | RemoteUsername | Real source-server username, set by framework |
| RemoteEmail | RemoteEmail | Real source-server email, set by framework |
| OriginalRemoteId | OriginalRemoteId | Provenance through multi-hop relays |

`UserPropsFromModel(StringMap) *UserProps` returns nil when no known
keys are populated.

### `Reaction` (8 fields, no pruning)

UserId, PostId, EmojiName, CreateAt, UpdateAt, DeleteAt, RemoteId,
ChannelId — all kept. Already minimal.

### `Status` (5 fields)

| Field | Cross? | Reason |
|---|---|---|
| UserId, Status, Manual, LastActivityAt, DNDEndTime | KEEP | Presence display |
| ActiveChannel | DROP | Channel ID is local to source server; meaningless on receiver |

### `MembershipChange` (5 fields, no pruning)

ChannelId, UserId, IsAdd, RemoteId, ChangeTime — all kept. The Go
type is renamed from `MembershipChangeMsg` to `MembershipChange` to
drop the redundant `Msg` suffix; the XML element follows.

### `PostAcknowledgement` (5 fields, no pruning)

UserId, PostId, AcknowledgedAt, ChannelId, RemoteId — all kept.

## Constructor pattern

Each wire type exposes two functions:

```go
func PostFromModel(p *mmModel.Post) *Post   // outbound: upstream → wire
func (p *Post) ToModel() *mmModel.Post      // inbound: wire → upstream
```

Both are explicit field-by-field copies. No reflection, no struct
tag tricks. The whole point of owning these types is that adding a
field requires editing both directions by hand, which is exactly
when the security review should fire.

For nested types:

```go
func SyncMsgFromModel(m *mmModel.SyncMsg) *SyncMsg {
    if m == nil { return nil }
    out := &SyncMsg{
        Id:                m.Id,
        ChannelId:         m.ChannelId,
        MentionTransforms: m.MentionTransforms,
    }
    for _, p := range m.Posts        { out.Posts        = append(out.Posts, PostFromModel(p)) }
    for _, r := range m.Reactions    { out.Reactions    = append(out.Reactions, ReactionFromModel(r)) }
    ...
    if len(m.Users) > 0 {
        out.Users = make(map[string]*User, len(m.Users))
        for id, u := range m.Users { out.Users[id] = UserFromModel(u) }
    }
    return out
}

func (m *SyncMsg) ToModel() *mmModel.SyncMsg { /* mirror */ }
```

Pointer fields on upstream (`*string` for `RemoteId`, `*bool` for
`IsFollowing`) become value fields on the wire types with `omitempty`,
since we control marshaling and don't need the pointer distinction.

## Custom XML for the Users map

Go's encoding/xml does not natively support maps. `SyncMsg.Users`
needs a `MarshalXML` / `UnmarshalXML` pair that produces:

```xml
<Users>
  <User id="uid001">...</User>
  <User id="uid002">...</User>
</Users>
```

(No `<UserEntry>` wrapper, unlike upstream.) Implementation lives in
`sync_msg.go`. Keys are sorted in `MarshalXML` for deterministic
output, matching the existing example-pinning pattern.

Same pattern for `MentionTransforms` (`<Transform key="..." value="..."/>`)
and `UserProps`/`PostProps` only when we need explicit omitempty
handling (which is most of them).

## Phases

### Phase 1: Foundation

1. Create `server/wire/` package with `doc.go` and the seven type
   files. Skeleton structs only, no constructors yet, no tests.
2. Write the custom `MarshalXML` / `UnmarshalXML` for `SyncMsg`
   (Users map + MentionTransforms).
3. Write the constructors and reverse-constructors for the simplest
   type (`PostAcknowledgement`, 5 fields, no nesting) and verify the
   round-trip pattern.

**Exit criterion:** `wire.PostAcknowledgement` round-trips through
`MarshalEnvelope` + `UnmarshalEnvelope` (with a temporary envelope
type wrapping just it).

### Phase 2: Type-by-type buildout

Implement, with tests, in this order (simplest → most nested):

1. `MembershipChange`
2. `Reaction`
3. `Status`
4. `UserProps`
5. `User`
6. `PostProps`
7. `Post`
8. `SyncMsg` (composes all of the above)

For each type, the test suite covers:

- `XFromModel(nil)` returns nil.
- `XFromModel(populated)` carries every kept field through.
- `XFromModel(populated)` does NOT carry any dropped field (assert
  the dropped fields are zero/empty on the wire type, or absent from
  the XML output).
- Round trip: `populated → FromModel → MarshalXML → UnmarshalXML →
  ToModel` equals the original on the fields we carry.
- Map ordering (Users, MentionTransforms): sorted by key.

**Exit criterion:** All seven types covered, package tests green,
no upstream model import outside `*FromModel` / `*ToModel`.

### Phase 3: Plumb through the envelope

1. Change `TransportEnvelope.SyncMsg` from `*mmModel.SyncMsg` to
   `*wire.SyncMsg`.
2. `hooks.go` `OnSharedChannelsSyncMsg`: call `wire.SyncMsgFromModel(msg)`
   before building the envelope.
3. `inbound.go`: after `UnmarshalEnvelope`, call `env.SyncMsg.ToModel()`
   before handing to `ReceiveSharedChannelSyncMsg`.
4. `splitTransportEnvelope` in `transport.go`: update to walk
   `wire.SyncMsg.Posts` rather than `mmModel.SyncMsg.Posts`. The
   bucketing logic is identical.
5. `augmentSyncMsgUsers` in `hooks.go`: still operates on
   `mmModel.SyncMsg` (before conversion), since it needs `p.API.GetUser`
   to look up local users.

**Exit criterion:** `go test ./...` passes. Backend integration test
(`make docker-smoke-test`) relays a post end-to-end with the new wire
types.

### Phase 4: Schema and examples regeneration

1. Rewrite `schema/crossguard.xsd` against the wire-package types.
   Strict throughout: every field enumerated, `xs:sequence` ordering,
   proper type constraints. No `xs:any`, no `xs:openContent`.
2. Update `server/examples_roundtrip_test.go` builders to use the new
   wire types directly. Run with `UPDATE_EXAMPLES=1` to regenerate
   `schema/examples/*.xml`.
3. Validate every regenerated example against the new XSD with the
   existing Python `xmlschema` script. Update the helper if needed.

**Exit criterion:** All 12 examples regenerate clean and validate
against the new strict XSD.

### Phase 5: Documentation

1. Update `implementation-plans/26-04-12-03-xml-wire-format-schema.md`
   to reflect the actual wire format the plugin emits: flat `SyncMsg`
   (no `<Fields>` wrapper), `<User id="...">` directly under `<Users>`,
   `<MembershipChange>` not `<MembershipChangeMsg>`, the pruned field
   set, and the typed `<Props>` and `<NotifyProps>`/`<UserProps>`
   shapes.
2. Add a short section to `CLAUDE.md` and to the threat model under
   `public/help/threatmodel.html` documenting:
   - Why wire types are owned by the plugin.
   - What the pruning rules are.
   - What requires schema regeneration (any wire-type struct edit).
3. Regenerate the help PDFs.

**Exit criterion:** Schema doc and threat model accurately describe
the wire format. PDFs match HTML.

## Risks and mitigations

- **`Post.Props["attachments"]` UX regression.** Webhook and slash-
  command posts will relay as plain text on the receiver. **Mitigation:**
  decide explicitly during phase 1 review whether to add
  `wire.MessageAttachment` now or defer. If deferred, surface in the
  release notes so customers know.
- **Map iteration nondeterminism in custom MarshalXML.** Go map
  iteration order is random; without sorting, example files won't be
  byte-stable. **Mitigation:** sort keys in `MarshalXML` for Users and
  MentionTransforms. The existing `TestExampleFiles` pins this.
- **`augmentSyncMsgUsers` operates on the upstream type.** It looks up
  local users before the conversion to `wire.SyncMsg`. If we ever move
  it to operate on `wire.SyncMsg`, we'd need to plumb `p.API` into the
  wire package, which violates the dependency direction. **Mitigation:**
  leave `augmentSyncMsgUsers` on `mmModel.SyncMsg`, call it before
  `SyncMsgFromModel`. Documented in code.
- **Upstream model changes silently drop new fields.** If Mattermost
  adds, say, `User.PhoneNumber`, our `UserFromModel` doesn't carry it.
  That's the design. **Mitigation:** none required; this is the feature.
  Document explicitly in `wire/doc.go` so a future reader doesn't
  "fix" it by mirroring upstream.
- **Receiver-side `mmModel.User` is missing fields after `ToModel`.**
  `ReceiveSharedChannelSyncMsg` may expect fields we don't carry (e.g.,
  it may check `NotifyProps` even though it'll wipe them). **Mitigation:**
  `ToModel` produces an `mmModel.User` with sensible zero values for
  dropped fields; the receiver tolerates this today because
  `sanitizeUserForSync` would have zeroed them anyway. Phase 2 tests
  pin behavior against the actual receiver via the integration suite.

## Effort estimate

- Phase 1: half a day. Package skeleton, custom map marshaler, one
  proof-of-concept type.
- Phase 2: one to one-and-a-half days. Mechanical, six types remain,
  each ~30 to 60 minutes with tests.
- Phase 3: half a day. Mostly find-and-replace in `hooks.go`,
  `inbound.go`, `transport.go`, plus integration-test fix-ups.
- Phase 4: half a day. XSD rewrite is the bulk; examples regenerate
  automatically from phase 2/3.
- Phase 5: a couple of hours. Doc updates and PDF regen.

Total: about three focused days. Spread over a week with other work.

## Why not other options

- **Stay on `mmModel.SyncMsg`, accept the XSD churn.** Every upstream
  field change requires a new XSD revision and a new compliance review.
  Treadmill we will lose.
- **`<xs:any>` everywhere.** Rejected earlier: hides exactly the
  fields compliance needs to see.
- **XSD 1.1 with `<xs:openContent>`.** Gives order-agnostic plus
  tolerant validation, but commits us to XSD 1.1 tooling. Many
  compliance shops are XSD 1.0 only (libxml2, Altova older versions).
  Not worth the deployment headache for the convenience.
- **Generate the wire types from upstream via codegen.** Same
  treadmill; the whole point is to decouple.
