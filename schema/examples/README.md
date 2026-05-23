# Cross Guard wire-format examples

Schema-valid sample payloads for the plugin-owned wire format defined in
[`../crossguard.xsd`](../crossguard.xsd) and produced by `server/wire/`.
Each file is a single `<CrossGuardEnvelope>` exactly as the plugin puts it on
the wire (no indentation, content escaped, byte-for-byte stable).

The first 11 are intentionally written as a single coherent timeline on
2024-04-12 so they read top-to-bottom: Alice posts a standup note, then a
markdown-rich update, a code-block dump, a table, a thread reply, and an
incident write-up; the standup is edited, then deleted; Alice adds a reaction,
removes it, and adds a custom-emoji reaction. Examples 12-20 cover the
remaining wire-type variants (test envelope, membership, status, ack, mention
transforms, typed Props, typed Users, bot users). Example 21 demonstrates a
metadata envelope: a sync cycle that emitted no post, only an orphan
reaction (a reaction on a post from a previous sync cycle). Example 22
demonstrates a system message (a `system_add_to_channel` post with whitelisted
system Props).

## Post envelopes vs. metadata envelopes

Every sender-originated envelope is `type="sync_msg"`. Each carries at most
one `<Post>` element so compliance content inspection can reject a single
post without losing other content as collateral damage.

- A **post envelope** has a `<Post>` child of `<SyncMsg>`. It carries the
  post, its author in `<Users>`, any reactions or acknowledgements on this
  post in `<Reactions>` / `<Acknowledgements>`, and the full
  `<MentionTransforms>` map.
- A **metadata envelope** has no `<Post>` child. It carries non-post
  content from the same sync cycle: orphan reactions and acks (on posts
  from previous cycles), all membership changes, all statuses, and the
  users referenced by any of those. Metadata envelopes have no
  inspectable content, so compliance tools should accept them
  unconditionally.

A single upstream sync cycle fans out to **N post envelopes plus an
optional metadata envelope**, all sharing the same channel sequence space
and arriving at the receiver in seq order.

## Files

| #  | File                                       | Exercises                                                  |
|----|--------------------------------------------|------------------------------------------------------------|
| 01 | `01_post_simple.xml`                       | Minimal top-level post; one user inline                    |
| 02 | `02_post_markdown_rich.xml`                | Headings, lists, bold/italic, inline code, mentions        |
| 03 | `03_post_code_blocks.xml`                  | Fenced code block with embedded `<Tag>` text (escaped)     |
| 04 | `04_post_table_and_links.xml`              | GFM table, links                                           |
| 05 | `05_post_thread_reply.xml`                 | Threaded reply (`RootId` set to parent post ID)            |
| 06 | `06_post_incident_report.xml`              | Multi-line post-mortem with newlines escaped as `&#xA;`    |
| 07 | `07_update_edited_post.xml`                | Edit of an earlier post (`EditAt` set)                     |
| 08 | `08_delete.xml`                            | Deletion (`DeleteAt` set, `Message` empty)                 |
| 09 | `09_reaction_add.xml`                      | Standard emoji (`thumbsup`) reaction                       |
| 10 | `10_reaction_remove.xml`                   | Same reaction removed (`DeleteAt` set)                     |
| 11 | `11_reaction_custom_emoji.xml`             | Custom emoji name (`shipit_squirrel`)                      |
| 12 | `12_test.xml`                              | Connectivity-check envelope (`type="test"`, no `SyncMsg`)  |
| 13 | `13_membership_change_join.xml`            | `<MembershipChange>` join (`IsAdd=true`)                   |
| 14 | `14_membership_change_leave.xml`           | `<MembershipChange>` leave (`IsAdd=false`)                 |
| 15 | `15_status_dnd.xml`                        | `<Status>` DND with `DNDEndTime`; `ActiveChannel` dropped  |
| 16 | `16_post_acknowledgement.xml`              | `<PostAcknowledgement>` carrying ack-at timestamp          |
| 17 | `17_mention_transforms.xml`                | `<MentionTransforms>` sorted-key map                       |
| 18 | `18_post_with_props_webhook.xml`           | Typed `<Props>` with webhook + AI provenance keys          |
| 19 | `19_user_with_timezone_and_props.xml`      | User `<Timezone>` (open map) and typed `<Props>`           |
| 20 | `20_bot_user.xml`                          | Bot user (`IsBot`, `BotDescription`, `BotLastIconUpdate`)  |
| 21 | `21_metadata_orphan_reaction.xml`          | Metadata envelope: no `<Post>`, one orphan reaction        |
| 22 | `22_system_add_to_channel.xml`             | System message (`Type=system_add_to_channel`) with Props   |

## What the examples prove

Compliance reviewers can use these files as the authoritative reference for
what the plugin actually emits. In particular:

- **Pruning is observable.** Example 15 has `ActiveChannel: "should-not-appear-on-wire"`
  set on the source-side `mmModel.Status`, yet the wire output has no
  `<ActiveChannel>` element. Example 18 sets `attachments`, `force_notification`,
  and `channel_mentions` on the source-side `Post.Props`, none of which appear
  on the wire because `PostProps` is a typed whitelist. Example 19 includes a
  `some_local_setting` key in `User.Props` that is similarly dropped.
- **Map ordering is deterministic.** Example 17 (`MentionTransforms`) and
  example 19 (`Timezone`) emit map entries sorted by key, so successive runs
  produce byte-identical output.
- **Required-but-empty fields are explicit.** Example 08 has an empty
  `<Message></Message>` for a deletion and validates cleanly against the XSD.
- **Test envelopes reuse the connection name as a placeholder.** Example 12
  carries `<TeamName>nats-low-to-high</TeamName><ChannelName>nats-low-to-high</ChannelName>`
  for fields that have no semantic meaning on a connectivity check, since the
  XSD requires both elements unconditionally. The receiver ignores both fields
  for `type="test"`.

## Envelope ordering: `Epoch` and `Sequence`

Each sender-originated envelope carries an `<Epoch>` element that identifies
the sender process generation, and `sync_msg` envelopes additionally carry a
`<Sequence>` element that is monotonic per `(ConnName, ChannelId)` within an
epoch. Together they let a receiver detect out-of-order delivery, suppress
duplicates, and notice a sender restart (epoch change resets the cursor).

- **`Epoch`**: 26-character Mattermost ID (`[a-z0-9]{26}`) generated once at
  plugin activation. Shared across every channel on every outbound connection
  from a single sender instance. Emitted on every sender-originated envelope
  including `test`, so a connectivity ping can flag a restart even when no
  payload is in flight.
- **`Sequence`**: unsigned 64-bit decimal starting at `1` for the first
  envelope on a given `(ConnName, ChannelId)` within an epoch and incrementing
  by one for each subsequent envelope. **Emitted on `sync_msg` envelopes
  only.** Test envelopes have no channel scope and intentionally omit it.

How the example timeline demonstrates this:

- Examples 01-11 are one logical sender session: all share `Epoch =
  epoch01aaaaaaaaaaaaaaaaaaa` and number sequences `1..11` in timeline order.
- Example 12 (a `test` envelope) carries the same `Epoch` but no `Sequence`,
  modelling the receiver-side rule "Sequence applies to sync_msg only".
- Examples 13-20 each define their own `Epoch`
  (`epoch13aaaaaaaaaaaaaaaaaaa` through `epoch20aaaaaaaaaaaaaaaaaaa`) and
  start from `Sequence=1`. A receiver seeing one of these after example 11
  would observe a new epoch and reset its cursor for that connection.

A receiver that sees a `<Sequence>` value lower than its current
`nextExpected` (with matching `Epoch`) treats the envelope as a duplicate
and drops it; a higher value with no intervening fill triggers a bounded
reorder wait. The wire format itself is just two elements; the state machine
lives in the receiver.

## Conventions

- All Mattermost IDs are 26 characters of `[a-z0-9]`.
- Envelope `<Timestamp>` is RFC 3339 UTC. Per-content timestamps (`CreateAt`,
  `UpdateAt`, `AcknowledgedAt`, etc.) are Unix epoch **milliseconds**, except
  `Status.DNDEndTime` which is **seconds** (an upstream quirk preserved
  faithfully).
- Files are not line-wrapped; the wire format is a single line per message
  after the `<?xml?>` header. Open in an editor with soft-wrap, or run through
  `xmllint --format` for a human-readable view (do not commit the formatted
  output — `TestExampleFiles` enforces byte equality with the raw form).
- Newlines inside string fields are escaped as `&#xA;`; `<`/`>`/`&` inside
  string fields are escaped as `&lt;`/`&gt;`/`&amp;`. No CDATA is used.

## Regenerate

If a wire-type struct changes, regenerate the fixtures and re-validate:

```sh
UPDATE_EXAMPLES=1 go test -run TestExampleFiles ./server/
for f in schema/examples/*.xml; do
  xmllint --noout --schema schema/crossguard.xsd "$f"
done
```

`xmllint` is the same validator used by `make docker-integration-test-validate-wire`,
so a clean run here matches the integration-time contract.

Both must succeed before a wire-type change is merged. After regeneration the
new XSD also needs compliance re-review.
