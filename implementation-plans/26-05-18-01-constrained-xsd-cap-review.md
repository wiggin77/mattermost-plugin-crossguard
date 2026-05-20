# Constrained-XSD numeric cap review (starter)

## Status

**Draft / starter.** This is the beginning of the follow-up plan
referenced by
[`26-05-17-01-constrained-xsd-fixes.md`](./26-05-17-01-constrained-xsd-fixes.md).
That plan addresses pattern and structural conformance to
`schema/crossguard.xsd`. This one will address
numeric caps (`xs:maxLength` on simple types, `maxOccurs` on
container elements) where the constrained schema is tighter than the
upstream Mattermost product limit. Implementation phases will be
filled in after the producer-side conformance work lands and we have
real telemetry from the new wire-layer error codes.

## Why a separate plan

Pattern and cap negotiation are different conversations with the
compliance team:

- **Patterns** describe the *shape* of accepted values and are
  derived from data semantics; they don't move easily.
- **Caps** describe the *bounded size* of accepted values and are
  derived from product limits; raising them is a quantitative
  argument backed by upstream documentation.

Bundling them slows both. The constrained schema's caps also turned
out to be largely fine on inspection (see the analysis below), so a
focused, short cap-only review is cheaper than another full
schema-review round.

## Methodology

Each `xs:maxLength`-bounded simple type and each `maxOccurs`-bounded
element in
`schema/crossguard.xsd` was checked against the
corresponding upstream Mattermost product limit. Sources for upstream
limits:

- `github.com/mattermost/mattermost/server/public/model/user.go`
- `…/model/team.go`, `…/model/channel.go`
- `…/model/post.go`
- `…/model/emoji.go`

A cap is flagged as "tight" only if the upstream limit can exceed the
schema cap under documented product behavior. Caps that are equal to
upstream (the schema enforces exactly the product limit) are fine.
Caps that are larger than upstream are fine (the schema is generous).

## Itemized review

### Simple-type `xs:maxLength` caps

| Schema type | Cap | Used by | Upstream limit | Verdict |
|---|---:|---|---:|---|
| `MattermostIdType` | 26 | All `*Id` fields, `Epoch` | `mmModel.NewId()` = 26 chars | Exact match — fine |
| `SlugType` | 64 | `ConnName`, `TeamName`, `ChannelName` | `TeamNameMaxLength` = 64, `ChannelNameMaxLength` = 64 | Exact match — fine |
| `UsernameType` | 64 | `Username`, `RemoteUsername` (prop) | `UserNameMaxLength` = 64 | Exact match — fine |
| `DisplayNameType` | 128 | `OverrideUsername`, `WebhookDisplayName` | No documented upstream cap; webhook display name is free-form text | Generous — fine |
| `EmailType` | 254 | `Email`, `RemoteEmail` (prop) | `UserEmailMaxLength` = 128 | Generous — fine |
| `LocaleType` | 35 | `Locale` | BCP-47 codes ≤10 chars in practice | Generous — fine |
| `RolesType` | 1024 | `Roles` | `UserRolesMaxLength` = 256 | Generous — fine |
| `EmojiNameType` | 64 | `Reaction.EmojiName`, `OverrideIconEmoji` | `EmojiNameMaxLength` = 64 | Exact match — fine |
| `UrlType` | 2048 | `OverrideIconURL` | No documented cap; browser-safe URL limit is 2048 | Browser-safe — fine |
| `ShortTextType` | 128 | `Nickname`, `FirstName`, `LastName` | `UserNicknameMaxRunes` = 64, `UserFirstNameMaxRunes` = 64, `UserLastNameMaxRunes` = 64 | Generous — fine |
| `MediumTextType` | 1024 | `Position`, `BotDescription`, `CustomStatus` | `UserPositionMaxRunes` = 128; `BotDescription`/`CustomStatus` no documented cap | Generous — fine |
| `HashtagsType` | 1024 | `Hashtags` | `PostHashtagsMaxRunes` = 1000 | Comfortably above upstream — fine |
| `MessageTextType` | 1048576 (1 MiB) | `Message` | `PostMessageMaxBytesV2` = 65535 (64 KiB) | Schema is 16× upstream — fine |
| `PostTypeStringType` | 64 | `Post.Type` | Column traditionally `varchar(26)`; plugin/custom types may extend | Generous — fine |
| `MapKeyType` | 128 | `StringMap.Entry/@key` (Timezone) | Timezone keys ≤30 chars (`automaticTimezone`, etc.) | Generous — fine |
| `MapValueType` | 4096 | `StringMap.Entry/@value` (Timezone) | Timezone values are timezone names (e.g., `America/Argentina/Buenos_Aires`) ≤50 chars | Generous — fine |
| `MentionKeyType` | 256 | `MentionTransforms.Transform/@key` | Mention strings are `@username[:remoteID]` ≤64+suffix | Generous — fine |
| `MentionValueType` | 256 | `MentionTransforms.Transform/@value` | Same shape as key | Generous — fine |

**No simple-type cap requires negotiation.** All are at or above
upstream limits.

### Container `maxOccurs` caps

| Container | Cap | Per-envelope content | Upstream / product reality | Verdict |
|---|---:|---|---|---|
| `UsersType.User` | 1000 | Author of the post + reaction/ack authors filtered to this post | Per the one-post-per-envelope fanout, realistically a handful | Generous — fine |
| `ReactionsContainerType.Reaction` | 1000 | Reactions on a single post | Popular posts can accumulate hundreds, rarely >1000 | Likely fine; flag for monitoring |
| `StatusesContainerType.Status` | 1000 | Status snapshots in metadata envelopes | Bulk presence syncs; product behavior bounded | Likely fine |
| `MembershipChangesContainerType.MembershipChange` | 1000 | Channel join/leave events in metadata envelopes | Producer caps fetch at `ConnectedWorkspacesSettingsDefaultMemberSyncBatchSize` = 20 per cycle (`sync_send_remote.go:373-376, 481-482`); excess history triggers another cycle via `resultRepeat`, not a larger SyncMsg | Two orders of magnitude headroom — fine |
| `AcknowledgementsContainerType.PostAcknowledgement` | 1000 | Acks on a single post | Same shape as reactions; rarely >1000 | Likely fine |
| `MentionTransformsType.Transform` | **100** | Per-sync-batch transform map: unique `@mention -> userID` pairs from posts in the current batch only. Map is initialized fresh per syncData and populated in `fetchPostUsersForSync` (`sync_send_remote.go:59, 621-651, 675-684`). NOT channel-scoped or accumulating | Typical sync batches: well under 100. Large catch-up syncs (long-paused remote reconnecting): could plausibly exceed 100 across many posts | **Borderline** — raise to 1000 as cheap headroom; not urgent |
| `StringMapType.Entry` | 100 | `User.Timezone` map | 3 keys per user | Generous — fine |
| `FileIdsType.Id` | 10 | File attachments on a post | `MaxFileAttachments` = 10 | Exact match — fine |

**One `maxOccurs` cap deserves attention:**

1. **`MentionTransformsType.Transform/@maxOccurs="100"`** (borderline,
   not urgent). The upstream producer builds `MentionTransforms`
   per-sync-batch from the mentions in the posts carried by that
   batch (`sync_send_remote.go:59, 621-651, 675-684`), so the map
   does **not** accumulate across the channel's lifetime, contrary
   to an earlier reading of this plan. Practical sizes for normal
   traffic are well under 100. A large catch-up sync (e.g., a
   long-paused remote reconnecting and replaying many posts in a
   single batch) could still plausibly exceed 100 distinct mentions.
   **Propose: raise to 1000** as cheap headroom that matches the
   other per-envelope caps; not blocking.

   `MembershipChangesContainerType.MembershipChange/@maxOccurs="1000"`
   was previously flagged as soft. After reading the upstream
   producer (`sync_send_remote.go:373-376, 481-482`) the framework
   caps each sync's `MembershipChanges` at
   `ConnectedWorkspacesSettingsDefaultMemberSyncBatchSize = 20` rows
   from `ChannelMemberHistory().GetMembershipChanges` and uses
   `resultRepeat` to iterate when more history is pending, so the
   1000 cap has two orders of magnitude headroom over the default.
   No change needed.

### Cross-references to producer behavior

The cap analysis interacts with the producer code:

- The mention-transforms cap (item 1 above) is reachable only by
  batches with many posts that collectively mention >100 distinct
  users. `buildOutboundEnvelopes` forwards the per-batch map
  verbatim to every fanned-out per-post envelope, so the same map
  rides with every envelope from one inbound sync. An optimization
  to subset transforms to the mentions actually referenced by the
  carried post would reduce wire size and defer this cap concern
  indefinitely. That is an optional follow-up
  noted here so it isn't lost.
- The membership cap is not reachable under default settings
  (producer batch size 20). Any validation failure on this container
  would indicate either a non-default
  `ConnectedWorkspacesSettings.MemberSyncBatchSize` configured
  unusually high, or an upstream change in batching behavior, and
  should be investigated rather than papered over with a cap raise.

## Proposed schema change

Based on the analysis above:

```diff
-  <xs:complexType name="MentionTransformsType">
-    <xs:sequence>
-      <xs:element name="Transform" minOccurs="0" maxOccurs="100">
+  <xs:complexType name="MentionTransformsType">
+    <xs:sequence>
+      <xs:element name="Transform" minOccurs="0" maxOccurs="1000">
```

No other cap changes are proposed at this time. A second change for
`MembershipChange` will be added if telemetry justifies it.

## Open questions (to resolve before promoting this plan from draft)

1. **Mention-transforms scope.** *Resolved by reading upstream code.*
   The `mmModel.SyncMsg.MentionTransforms` map is **per-sync-batch**,
   not channel-scoped. The map is initialized fresh on every sync in
   the per-task `syncData`
   (`server/platform/services/sharedchannel/sync_send_remote.go:59`)
   and populated in `fetchPostUsersForSync` by iterating
   `sd.posts` (the posts in the current batch only), calling
   `MentionsToTeamMembers` per post message, and recording one entry
   per `(mention, userID)` pair that survives the
   `mentionUserID == userID` filter
   (`sync_send_remote.go:621-651, 675-684`). The map is then assigned
   to `msg.MentionTransforms` in `sendPostSyncData`
   (`sync_send_remote.go:900`).
   - **Practical bound:** unique mention strings across the posts in
     a single sync batch. For typical batches this is well under 100,
     so the current cap is comfortably above normal traffic.
   - **Edge case:** large catch-up syncs (e.g., a long-paused remote
     reconnecting) can collect many posts in one batch and could
     plausibly produce more than 100 distinct mentions across them.
     Raising the cap to 1000 to match the other per-envelope caps is
     still defensible as cheap headroom, but is no longer urgent
     and could be deferred until telemetry justifies it.

2. **Membership-change batching.** *Resolved by reading upstream
   code.* The producer caps the fetch at the database query before
   ever building the `SyncMsg`, and does **not** chunk further before
   invoking the plugin hook. `fetchMembershipsForSync` reads at most
   `GetMemberSyncBatchSize()` raw history rows from
   `ChannelMemberHistory().GetMembershipChanges`
   (`server/platform/services/sharedchannel/sync_send_remote.go:373-376`),
   dedupes them by user, and appends the survivors to
   `sd.membershipChanges`
   (`sync_send_remote.go:385-460`). `sendMembershipSyncData` assigns
   that slice verbatim to `msg.MembershipChanges`
   (`sync_send_remote.go:481-482`). The configured batch size defaults
   to `ConnectedWorkspacesSettingsDefaultMemberSyncBatchSize = 20`
   (`server/public/model/config.go:294, 3803-3804`) and is exposed by
   `Service.GetMemberSyncBatchSize`
   (`server/platform/services/sharedchannel/service.go:236-241`). If
   the fetch hits the limit, `sd.resultRepeat = true` schedules another
   cycle (`sync_send_remote.go:464-467`) rather than producing a
   larger `SyncMsg`.
   - **Conclusion:** the current cap of 1000 is two orders of
     magnitude above the default batch size and remains safe even if
     an operator multiplies the batch size by 10x. **No change
     needed.** A `MembershipChange` validation failure in production
     would indicate either a configuration anomaly (extreme batch
     size) or an upstream change in batching behavior, and should be
     treated as a signal, not a routine cap-raise.

3. **Bundle vs. iterate.** Process question, not a server-code
   question. Recommendation given the answers to (1) and (2): the
   mention-transforms raise is the only proposed cap change, and the
   urgency is lower than originally thought (per-batch, not
   channel-scoped). Either land it alone as a small change request,
   or defer until telemetry from plan 01 surfaces other caps and
   bundle a single review. Defer is the cheaper option.

## Next steps

- Land plan 01 (`26-05-17-01-constrained-xsd-fixes.md`) and let it
  bake long enough in pre-prod to collect telemetry from
  `WireUsernameTruncatedAtColon`,
  `WireUserDroppedNonConformingUsername`, and
  `WireUserDroppedNonConformingEmail`.
- Add operator metrics for envelope-validation outcomes (out of
  scope for plan 01) so the cap conversation is evidence-based.
- Answer the open questions above.
- Fill in implementation phases (schema edit, fixture regeneration
  if needed, compliance change request) and promote this plan from
  draft to active.
