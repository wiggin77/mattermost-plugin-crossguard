# One post per envelope

## Context

Compliance content-inspection tools sit in the transport path between
sender and receiver, accept or reject envelopes as units, and base
their decision on inspectable content. The only inspectable content
on the Cross Guard wire is the post body. Today the sender bundles
every post the upstream shared-channels cursor accumulated in one
sync cycle into a single `TransportEnvelope`, so a compliance
rejection of any single post discards the whole envelope: every
other post in that batch, plus every reaction, acknowledgement, and
metadata change tied to those posts, is lost as collateral.

The fix narrows the blast radius to exactly one post by making the
envelope the unit of one logical post message. Reactions,
acknowledgements, memberships, statuses, and orphan content (items
that reference posts from a previous sync cycle) ride in a separate
metadata envelope per cycle. The metadata envelope has no post body
and is therefore not subject to content rejection.

This change is also a small simplification of the wire format. The
`<Posts>` container under `<SyncMsg>` becomes a bare optional
`<Post>` element, since the new invariant is that each envelope
carries at most one post. The schema becomes one line shorter and
the wire saves the `<Posts></Posts>` wrapper bytes on every post
envelope.

## Scope

In scope:

- Sender-side change to `splitTransportEnvelope` so it always emits
  one post per envelope, regardless of provider `MaxMessageSize`.
- Generation of an optional metadata envelope per sync cycle for
  non-post content (orphan reactions, orphan acknowledgements,
  membership changes, status changes, and the users referenced by
  any of those).
- XSD update: bare `<Post>` element with `minOccurs="0"` replacing
  the `<Posts>` container and `PostsContainerType`.
- Go wire type update: `wire.SyncMsg.Posts []*Post` becomes
  `wire.SyncMsg.Post *Post`. Custom marshaling for the `<Posts>`
  wrapper is removed.
- Example fixtures regenerated to match the new wire shape.
- README convention update.

Out of scope:

- Receiver changes. The sequencer (Phase 4 of plan
  26-05-13-01) already handles per-envelope dispatch and per-channel
  ordering. More envelopes mean finer-grained gap detection, which
  the existing state machine handles.
- Compliance tool configuration. The schema narrowing helps but
  classification logic is the customer's domain.
- Cross-channel ordering changes. Each channel still has its own
  cursor and reorder buffer.

## Wire format

### Schema

`server/wire/` and `schema/crossguard.xsd` change in lockstep.

In the XSD, replace the existing `PostsContainerType` slot inside
`SyncMsgType`:

```xml
<xs:element name="Posts" type="PostsContainerType" minOccurs="0"/>
```

with:

```xml
<xs:element name="Post" type="PostType" minOccurs="0"/>
```

and delete the `PostsContainerType` complex type entirely. The
`PostType` complex type is unchanged (the per-post field set is the
same as today).

A post envelope on the wire looks like:

```xml
<CrossGuardEnvelope version="1" type="sync_msg">
  <ConnName>nats-low-to-high</ConnName>
  <Timestamp>2026-05-13T10:00:00Z</Timestamp>
  <Epoch>epoch01aaaaaaaaaaaaaaaaaaa</Epoch>
  <Sequence>42</Sequence>
  <TeamName>team-a</TeamName>
  <ChannelName>general</ChannelName>
  <SyncMsg>
    <Id>sm-42</Id>
    <ChannelId>ch0aaaaaaaaaaaaaaaaaaaaaaa</ChannelId>
    <Users>...the post's author only...</Users>
    <Post>...the one post...</Post>
    <Reactions>...reactions on this post...</Reactions>
    <Acknowledgements>...acks on this post...</Acknowledgements>
    <MentionTransforms>...the map...</MentionTransforms>
  </SyncMsg>
</CrossGuardEnvelope>
```

A metadata envelope on the wire looks like:

```xml
<CrossGuardEnvelope version="1" type="sync_msg">
  <ConnName>nats-low-to-high</ConnName>
  <Timestamp>2026-05-13T10:00:00Z</Timestamp>
  <Epoch>epoch01aaaaaaaaaaaaaaaaaaa</Epoch>
  <Sequence>43</Sequence>
  <TeamName>team-a</TeamName>
  <ChannelName>general</ChannelName>
  <SyncMsg>
    <Id>sm-43</Id>
    <ChannelId>ch0aaaaaaaaaaaaaaaaaaaaaaa</ChannelId>
    <Users>...users referenced by metadata...</Users>
    <Reactions>...orphan reactions...</Reactions>
    <Acknowledgements>...orphan acks...</Acknowledgements>
    <MembershipChanges>...all membership events...</MembershipChanges>
    <Statuses>...all status changes...</Statuses>
  </SyncMsg>
</CrossGuardEnvelope>
```

There is no separate envelope `type` attribute for post vs.
metadata. Both are `type="sync_msg"`. The compliance tool
distinguishes by presence of `<Post>`: a post envelope has it, a
metadata envelope does not. Rejection logic keys on the post body
inside `<Post>` and is a no-op (accept) when `<Post>` is absent.

Other plural containers stay as `maxOccurs="unbounded"` because
they legitimately carry multiple items. The change to bare `<Post>`
is the only asymmetry, and it is justified by the asymmetric
semantic that compliance inspection is post-specific.

### Go wire types

`server/wire/sync_msg.go`:

- `SyncMsg.Posts []*Post` becomes `SyncMsg.Post *Post`.
- Custom marshaling for the `<Posts>` wrapper is removed. The
  generic `encoding/xml` tag `xml:"Post,omitempty"` is sufficient.
- `SyncMsgFromModel`: when the upstream `*mmModel.SyncMsg.Posts`
  slice has more than one entry, this is a sender-side
  bug (the sender split should have ensured at most one). The
  constructor logs a warning, takes the first post, and discards
  the rest. The warning surfaces any future drift between
  `splitTransportEnvelope`'s policy and the wire constructor's
  assumption.
- `(*SyncMsg).ToModel`: produces a `*mmModel.SyncMsg` with at most
  one post in the slice.

### Sender split policy

`server/transport.go::splitTransportEnvelope` becomes a fanout
function rather than a size-gated splitter:

1. **Inputs**: one `*TransportEnvelope` wrapping the upstream
   `*mmModel.SyncMsg` (the framework's batch).
2. **Outputs**: a slice of envelopes, one per post in the batch
   plus at most one metadata envelope.

The fanout walks the `SyncMsg` as follows:

- For each post in `SyncMsg.Posts`, build a post envelope
  carrying:
  - The post itself.
  - The post's author from `SyncMsg.Users` (filtered to the one
    `UserId` referenced).
  - Reactions from `SyncMsg.Reactions` whose `PostId` matches.
  - Acknowledgements from `SyncMsg.Acknowledgements` whose
    `PostId` matches.
  - The entire `MentionTransforms` map. (Filtering to only
    in-message mentions would save a few bytes but add complexity
    without changing semantics.)
- After all posts are emitted, build one metadata envelope (only
  if any non-post-related content remains) carrying:
  - Every user referenced by reactions, acks, memberships, or
    statuses that does not appear in a post envelope.
  - Reactions and acknowledgements whose post is not in this sync
    cycle (orphans).
  - All `MembershipChanges`.
  - All `Statuses`.
  - The `MentionTransforms` map only if it is needed by an orphan
    reaction (in practice, never; mention transforms are
    post-specific).

Each emitted envelope is then passed through the existing
sequence-stamping path in `publishToOutboundConn`, which already
assigns one `Sequence` per part after splitting. No change to the
stamping logic.

Existing oversize handling (a single post that exceeds the
provider's `MaxMessageSize`) is preserved: the provider's
`Publish` returns an error and the upstream cursor does not
advance, surfaced through the existing failure path in
`publishToOutboundConn`. Single-post envelopes are already as small
as we can make them; we cannot split them further.

## Compliance review impact

The schema change is a **narrowing**, not a broadening:

- Every previously-valid post envelope shape (zero or one post) is
  still valid under the new schema.
- The new schema additionally rejects envelopes with more than one
  post.
- The wire bytes for a single-post envelope drop the
  `<Posts></Posts>` wrapper, shortening the envelope by ~15 bytes.

This is the kind of change that should not require re-review of
the per-field content classification logic. The compliance team
would need to:

1. Confirm the new XSD parses and validates cleanly in their
   pipeline.
2. Note that the wire shape `<SyncMsg>...<Post>...</Post>...</SyncMsg>`
   replaces `<SyncMsg>...<Posts><Post>...</Post></Posts>...</SyncMsg>`
   for envelopes that have a post.
3. Confirm that an envelope with no `<Post>` element is treated as
   "no post content to classify, accept."

There is no change to the per-post field set inside `<Post>` and
no change to any other envelope-level field, so the field-level
content classification rules are untouched.

## Example fixtures

All 20 fixtures already carry at most one post per envelope, so
the regeneration is mechanical:

- Fixtures 01-08 (post lifecycle) and 18 (post with props): replace
  `<Posts><Post>...</Post></Posts>` with `<Post>...</Post>`.
- Fixtures 09-11 (reactions), 12 (test), 13-15 (membership and
  status), 16-17 (ack and mentions), 19-20 (users and bots) are
  unaffected by the structural change since they don't have posts.

Re-running `UPDATE_EXAMPLES=1 go test -run TestExampleFiles
./server/` rewrites them deterministically from the existing
builder code. The fixture text shrinks by 15 bytes per
post-bearing envelope.

The example builders in `server/examples_roundtrip_test.go` need a
minor update: each builder currently sets `Posts: []*mmModel.Post{
{...} }` (a single-element slice). The wire-side constructor
`SyncMsgFromModel` already handles 0-or-1 by reading
`msg.Posts[0]` and discarding the rest; the builder code itself
does not need to change since the upstream `mmModel.SyncMsg.Posts`
is still a slice.

Add one new example fixture demonstrating a metadata envelope from
a sync cycle that produces no posts (e.g., a reaction add on a
previously-synced post). This is the new wire shape that compliance
reviewers need to see explicitly. Suggested name and slot:

| #  | File                              | Exercises                                 |
|----|-----------------------------------|-------------------------------------------|
| 21 | `21_metadata_orphan_reaction.xml` | Metadata envelope with no `<Post>`, one reaction on a post from a previous sync cycle |

## Phases

The plan is a single PR because the schema, wire types, sender
policy, and examples are tightly coupled. Splitting would leave
an intermediate state where the sender emits multi-post envelopes
that no longer validate against the schema or are inconsistent
with the wire struct.

The work is small enough (estimated 200-300 lines plus tests) that
landing it as one change is the lower-risk path.

### Single PR contents

1. `schema/crossguard.xsd`:
   - Replace the `<Posts>` element + `PostsContainerType` complex
     type with a bare `<Post>` element of type `PostType`,
     `minOccurs="0"`.

2. `server/wire/sync_msg.go`:
   - Field rename `Posts []*Post` -> `Post *Post`.
   - Remove the `Posts` wrapper-suppression logic from custom
     `MarshalXML` / `UnmarshalXML`. `encoding/xml`'s default for a
     single optional pointer field with `omitempty` is correct.
   - `SyncMsgFromModel`: pick the first post from the upstream
     slice; log a warn-level audit if the slice has more than one
     (signals a sender bug).
   - `(*SyncMsg).ToModel`: produce a `*mmModel.SyncMsg` whose
     `Posts` slice is either empty or has one element.

3. `server/transport.go`:
   - Replace `splitTransportEnvelope`'s size-gated logic with the
     post-fanout-plus-metadata-envelope policy described above.
   - Helper `buildPostEnvelope(src, post, reactionsByPost,
     acksByPost)` returns one post envelope.
   - Helper `buildMetadataEnvelope(src, orphanReactions, orphanAcks,
     memberships, statuses, usersByID)` returns the optional
     metadata envelope.
   - Existing oversize handling is unchanged; the provider's
     `Publish` returns an error for single-post envelopes that
     exceed `MaxMessageSize`, propagating up the call chain as
     today.

4. `server/transport_test.go`:
   - Update `TestSplitTransportEnvelope*` cases for the new fanout
     policy.
   - Add `TestFanoutSinglePostNoMetadata` (one post, no reactions
     or memberships → one envelope).
   - Add `TestFanoutMultiPostMetadata` (three posts plus an orphan
     reaction → three post envelopes plus one metadata envelope).
   - Add `TestFanoutMetadataOnly` (zero posts, only a membership
     change → one metadata envelope).

5. `schema/examples/`:
   - Regenerate all 20 fixtures via `UPDATE_EXAMPLES=1` (only the
     post-bearing ones change).
   - Add `21_metadata_orphan_reaction.xml` plus its builder in
     `examples_roundtrip_test.go`.

6. `schema/examples/README.md`:
   - Update the fixture table to add example 21.
   - Note the new wire shape: `<Post>` is a bare element, not
     wrapped.
   - Document the "post envelope vs metadata envelope" convention
     and that compliance rejection only happens on envelopes with
     a `<Post>` element.

7. `CLAUDE.md`:
   - Update the "Backend Message Flow" section: outbound flow now
     fans out to N+1 envelopes (N post envelopes, optional metadata
     envelope) instead of one envelope per cursor cycle.

## Risks and open questions

1. **Bandwidth.** Each post envelope carries its own
   `<CrossGuardEnvelope>` header (~300 bytes after the
   `<Posts>` wrapper removal saves ~15 bytes). For sync cycles
   that previously bundled 10 posts (~3KB header overhead each)
   the per-cycle byte budget grows from one ~50KB envelope to ten
   ~5KB envelopes plus ~3KB of duplicated headers. For typical
   compliance-traffic volume this is small. Worth instrumenting
   the existing `make docker-smoke-test` to spot regressions.

2. **Provider throughput.** NATS, Service Bus, and Azure Blob WAL
   each get ten publish calls instead of one. NATS is fine (no
   per-message overhead). Service Bus has a per-message AMQP
   transaction; ten of them is more work but still well below the
   per-second limits of the emulator and any cloud tier. Azure
   Blob accumulates ten lines in the WAL instead of one large
   JSONL entry, no real difference.

3. **Sender retry semantics.** If `publishToOutboundConn` fails
   after publishing 4 of 10 envelopes, the upstream cursor does
   not advance, the framework retries the whole sync cycle, and
   the 4 already-published envelopes become duplicates from the
   receiver's perspective. Each retry gets a fresh sequence
   number (the counter has advanced for the 4 that landed), so
   the sequencer cannot use seq-based deduplication; the upstream
   receiver dedups by post `Id` and `UpdateAt`. This is the
   existing behavior for size-based splits and does not regress.

4. **Concurrency of `make docker-smoke-test`.** The integration
   tests assert post counts; they should be unaffected since the
   receiver still applies one post per upstream `SyncMsg` worth
   of content, just via more transport-level envelopes. Confirm.

5. **MentionTransforms duplication.** Each post envelope carries
   the full mention map. For sync cycles with one big
   mention-heavy message, this is one copy. For cycles with ten
   posts that each mention one person, the map is duplicated ten
   times. The map is small (one entry per mentioned name) so the
   overhead is bounded. Filtering per envelope is possible but
   not worth the complexity.

## Validation gate

- `make check-style` clean (Go side).
- `go test ./server/...` green.
- `python3 -m xmlschema` validates all 21 fixtures against the
  updated XSD.
- `make docker-smoke-test` and `make docker-integration-test`
  green.
- Manual compliance review of the schema diff (one-line
  removal of `<Posts>` container and `PostsContainerType` complex
  type, replaced with one-line bare `<Post>` element). The diff
  should fit in a single screen for the reviewer.
