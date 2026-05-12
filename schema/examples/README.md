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
transforms, typed Props, typed Users, bot users).

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
  example 19 (`Timezone` and the bot user's `Users` map) emit map entries
  sorted by key, so successive runs produce byte-identical output.
- **Required-but-empty fields are explicit.** Example 12 has empty
  `<TeamName></TeamName><ChannelName></ChannelName>` for a test envelope where
  those fields are not semantically meaningful. Example 08 has an empty
  `<Message></Message>` for a deletion. Both validate against the XSD.

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
python3 -c "
import xmlschema, os
s = xmlschema.XMLSchema('schema/crossguard.xsd')
for f in sorted(os.listdir('schema/examples')):
    if f.endswith('.xml'):
        s.validate('schema/examples/' + f)
        print('OK', f)
"
```

Both must succeed before a wire-type change is merged. After regeneration the
new XSD also needs compliance re-review.
