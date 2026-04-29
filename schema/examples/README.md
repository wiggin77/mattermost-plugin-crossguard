# Crossguard message examples

Schema-valid sample payloads for every message type defined in
[`crossguard.xsd`](../crossguard.xsd). Each file is a single
`<CrossguardMessage>` envelope in the `urn:mattermost-crossguard` namespace.

The examples are intentionally written as a single coherent timeline on
2026-04-15 so they can be read top to bottom: Alice posts a standup note and a
retro recap, Bob files a bug repro with code blocks, Carol triages the release,
Dave replies in a thread, an ops bot posts an incident write-up, Alice edits
her retro, the early standup is deleted, Bob reacts then unreacts, Carol drops
a custom emoji, and a connectivity test closes the log.

## Files

| #  | File                            | `Type`                       | What it exercises                                      |
|----|---------------------------------|------------------------------|--------------------------------------------------------|
| 01 | `01_post_simple.xml`            | `crossguard_post`            | Minimal top-level post, plain text, no `RootID`        |
| 02 | `02_post_markdown_rich.xml`     | `crossguard_post`            | Headings, lists, bold/italic, inline code, links, mentions |
| 03 | `03_post_code_blocks.xml`       | `crossguard_post`            | Fenced Go / SQL / shell / XML code blocks, stack trace, exercises `<`, `>`, `&` escaping inside `MessageText` |
| 04 | `04_post_table_and_links.xml`   | `crossguard_post`            | GFM table, task list, reference-style links            |
| 05 | `05_post_thread_reply.xml`      | `crossguard_post`            | Threaded reply: `RootID` set, blockquote of parent     |
| 06 | `06_post_incident_report.xml`   | `crossguard_post`            | Long-form post-mortem with timeline table, checklist, and a log line containing literal `<Message>` |
| 07 | `07_update_edited_post.xml`     | `crossguard_update`          | Edit of #02 (same `PostID`, same `CreateAt`, later envelope `Timestamp`) |
| 08 | `08_delete.xml`                 | `crossguard_delete`          | Deletion of #01                                        |
| 09 | `09_reaction_add.xml`           | `crossguard_reaction_add`    | Standard emoji (`thumbsup`) on #02                     |
| 10 | `10_reaction_remove.xml`        | `crossguard_reaction_remove` | Same user removes the reaction from #09                |
| 11 | `11_reaction_custom_emoji.xml`  | `crossguard_reaction_add`    | Custom emoji exercising `-`, `_`, `+` in `EmojiName`   |
| 12 | `12_test.xml`                   | `crossguard_test`            | Minimal connectivity-check message                     |

## Notes on conventions

- All IDs are 26 characters of `[a-z0-9]`, per `MattermostIDType`.
- Envelope `Timestamp` is ISO 8601 (RFC 3339). `PostMessage/CreateAt` is Unix
  epoch **milliseconds** (`xs:long`), and is earlier than the envelope
  timestamp to reflect "post was created, then relayed."
- Each example is **byte-for-byte identical** to what the plugin puts on the
  wire. The files are the output of `model.Marshal(env, FormatXML)` plus a
  trailing newline, nothing else. That means: single line per message after
  the `<?xml?>` header (no inter-element indentation), `MessageText` content
  entity-escaped (`<` becomes `&lt;`, `>` becomes `&gt;`, `&` becomes `&amp;`,
  newlines inside the body become `&#xA;`), and no CDATA. `TestExampleFiles_Symmetric`
  in `server/model/` enforces this byte equality.
- Examples 02, 03, and 06 intentionally include literal `<Message>`,
  `<CrossguardMessage>`, and other tag-looking text in the markdown body so
  you can see the escaping in action in the rendered file.
- The files are not line-wrapped; open them in an editor that soft-wraps, or
  feed one through `xmllint --format` for a human-readable view.
- The update example (#07) keeps the original `CreateAt` and only advances the
  envelope `Timestamp`, which is the semantics downstream consumers should
  expect.

## Validate

From the repo root:

```sh
for f in schema/examples/*.xml; do
  xmllint --noout --schema schema/crossguard.xsd "$f"
done
```

Every file should report `validates`.
