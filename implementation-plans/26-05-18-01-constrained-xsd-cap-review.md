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
| `MembershipChangesContainerType.MembershipChange` | 1000 | Channel join/leave events in metadata envelopes | Mass-onboarding (bulk add) or mass-departure can plausibly exceed 1000 | **Investigate** — see open questions below |
| `AcknowledgementsContainerType.PostAcknowledgement` | 1000 | Acks on a single post | Same shape as reactions; rarely >1000 | Likely fine |
| `MentionTransformsType.Transform` | **100** | Full channel-level transform map, attached to every per-post envelope. Accumulates as the channel sees more distinct mentions over its lifetime | Long-lived busy channels accumulate hundreds of distinct mentioners | **Tight — propose raise to 1000** |
| `StringMapType.Entry` | 100 | `User.Timezone` map | 3 keys per user | Generous — fine |
| `FileIdsType.Id` | 10 | File attachments on a post | `MaxFileAttachments` = 10 | Exact match — fine |

**Two `maxOccurs` caps deserve attention:**

1. **`MentionTransformsType.Transform/@maxOccurs="100"`** (firm).
   The `MentionTransforms` map is the channel-scoped collection of
   source-server mention strings and their receiver-server
   rewrites. It is forwarded verbatim from each upstream
   `mmModel.SyncMsg` and rides with **every** per-post envelope
   produced by the fanout in `buildOutboundEnvelopes`
   (`server/transport.go:198`). In a long-lived channel with
   hundreds of distinct mentioners, the map exceeds 100 entries and
   every subsequent envelope from that channel fails strict
   validation. **Propose: raise to 1000** to match the other
   per-envelope caps.

2. **`MembershipChangesContainerType.MembershipChange/@maxOccurs="1000"`**
   (soft / needs telemetry). Bulk membership operations (mass add,
   team merge, group-sync ingestion) can plausibly produce a single
   `mmModel.SyncMsg` with more than 1000 membership deltas. The
   producer-side fanout does not currently split this; if the
   framework hands us a sync with >1000 changes, we'd fail
   validation. **Propose: investigate whether the framework batches
   above 1000 in real deployments; if yes, either raise to 5000 or
   split membership deltas into multiple metadata envelopes.**

### Cross-references to producer behavior

The cap analysis interacts with the producer code:

- The mention-transforms cap (item 1 above) is reachable today
  because `buildOutboundEnvelopes` does not subset the map per
  envelope; it forwards the full channel-level map. Even raising the
  cap, a separate optimization to subset transforms to mentions
  actually referenced by the carried post would reduce wire size and
  defer the cap concern indefinitely. That is an optional follow-up
  noted here so it isn't lost.
- The membership cap (item 2) is reachable only if the framework
  batches above 1000. Telemetry from the wire-layer error codes
  added by plan 01 will reveal whether this fires in practice.

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

1. **Mention-transforms scope.** Confirm with the framework team that
   the upstream `mmModel.SyncMsg.MentionTransforms` map is
   channel-scoped (accumulating across the channel's life) rather
   than sync-batch-scoped (only the mentions in the current batch).
   If the latter, the cap of 100 is already comfortably above
   typical sync-batch sizes and no change is needed.
2. **Membership-change batching.** Confirm whether
   `OnSharedChannelsSyncMsg` is ever invoked with
   `MembershipChanges` containing >1000 entries, or whether the
   framework caps batch size below that. The integration test suite
   may be the cheapest way to answer this empirically.
3. **Bundle vs. iterate.** Is this single cap change enough to take
   to the compliance team now, or should we wait for the post-Phase-A
   telemetry to identify any additional caps that fire in practice
   and bundle a single review?

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
