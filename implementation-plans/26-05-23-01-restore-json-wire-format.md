# Restore JSON as an optional wire format

## Context

Before the shared-channels refactor (plan `26-04-12-02`), Cross Guard
supported two outbound wire formats, JSON and XML, selectable per
outbound connection via a `message_format` setting. Inbound messages
were auto-detected by inspecting the first non-whitespace byte (a
leading `<` meant XML, a leading `{` meant JSON), with UTF-8 BOM
stripping for CDS compatibility. That work landed in commit `1e4b075`
("Add XML wire format support for Cross Domain Solutions") on top of
the older JSON-only path.

The shared-channels refactor rewrote the wire layer (`server/model/`
became `server/wire/`) and, in the process, dropped JSON entirely.
The `server/wire/*.go` types now carry only `xml:` struct tags;
`server/transport.go::MarshalEnvelope` and `UnmarshalEnvelope` are
hardwired to `encoding/xml`; `ConnectionConfig` has no
`MessageFormat` field; and the inbound dispatch path no longer
auto-detects format.

Several artifacts in the tree still reference the old shape:

- The webapp's `ConnectionSettings.tsx` still carries
  `message_format: 'json' | 'xml'` in its form state, defaults to
  `'json'`, and renders an "XML" badge on outbound cards. The
  backend silently ignores the field, so the UI is currently
  orphaned.
- `schema/crossguard-api.yaml` (OpenAPI 1.1.0) still documents both
  formats, the auto-detect rule, and the per-connection
  `message_format` setting. Documentation is ahead of code.
- `server/integration/xml_test.go` comments that the test is
  "table-driven on the `format` axis ... even though there is only
  one row at migration time."

The intent has always been that JSON would come back. This plan
restores it.

## Goals

- Re-introduce JSON as a fully-supported outbound wire format,
  selectable per outbound connection via `message_format: "xml" |
  "json"`. Default is `"xml"`.
- Restore inbound auto-detection: a single inbound subscription on a
  given connection accepts envelopes in either format, distinguished
  by the first non-whitespace byte.
- Keep `schema/crossguard.xsd` as the authoritative compliance
  contract. The same field whitelist, value patterns, and length
  caps apply to both encodings.
- Ship a compliance-reviewable `schema/crossguard.schema.json`
  alongside the XSD. The JSON Schema is generated mechanically from
  the XSD by an in-tree tool so that any XSD change re-generates the
  JSON Schema in lockstep.
- At runtime, validate JSON envelopes against the XSD by round-trip
  through the wire types (decode JSON, re-encode XML, hand to
  `xmllint --schema crossguard.xsd`). One enforcement path, one
  schema, no chance of drift between two runtime validators.

## Non-goals

- Adding a third wire format (e.g. CBOR, Protobuf). The plan is to
  restore the historical JSON/XML pair, not to generalize the
  encoding layer.
- Per-connection encoding negotiation or content-type sniffing on
  the transport. The outbound `message_format` setting picks the
  encoding; inbound auto-detects from the bytes. No headers.
- Changing the wire field set, the XSD constraints, or any
  compliance-reviewed value. JSON is a re-encoding of exactly the
  same data.
- Mixed-format encoding within a single envelope. Each envelope is
  either entirely JSON or entirely XML.
- Backwards compatibility shims for envelopes produced by versions
  that predate the shared-channels refactor. The pre-refactor JSON
  shape was nested differently (two-layer `Message` struct with the
  payload as a JSON string field, per the commit message of
  `1e4b075`); the new JSON shape is the field-for-field mirror of
  the current XML wire, not a revival of the old shape.

## Wire format

### Source-of-truth XSD; mechanically derived JSON Schema

`schema/crossguard.xsd` remains the single compliance contract. A
new in-tree generator at `build/xsd2jsonschema/` walks the XSD and
emits `schema/crossguard.schema.json`. Both files are checked in;
CI fails if the JSON Schema is stale relative to the XSD.

The generator's mapping ruleset is intentionally small and fixed
because `crossguard.xsd` is a constrained schema (no `xs:any`, no
recursion, no mixed content, a finite set of primitive types). The
full mapping is:

| XSD construct | JSON Schema output |
|---|---|
| `xs:element name="X" type="T"` | property `"X"` of type `T` |
| `xs:attribute name="x"` | sibling property `"x"` at same level |
| `xs:complexType` with `xs:sequence` | `{"type": "object", "properties": {...}, "required": [...]}` |
| `xs:element minOccurs="0"` | absent from `required` |
| `xs:element maxOccurs > 1` inside wrapper | wrapper property becomes `{"type": "array"}` |
| `xs:simpleType` with `xs:pattern` | `{"type": "string", "pattern": "^...$"}` (anchors made explicit) |
| `xs:simpleType` with `xs:maxLength` | `maxLength` |
| `xs:simpleType` with `xs:enumeration` | `enum` |
| `xs:long`, `xs:int`, `xs:integer` | `{"type": "integer", "format": "int64"}` |
| `xs:dateTime` | `{"type": "string", "format": "date-time"}` |
| `xs:boolean` | `{"type": "boolean"}` |
| `xs:string` | `{"type": "string"}` |

The three shape conventions that are *not* derivable from the XSD
alone (they are choices in the XSD-to-JSON translation) are:

1. **Repeated elements become JSON arrays.** `<Users><User
   id="..."/></Users>` becomes `{"Users": [{"id": "...", ...}]}`,
   not `{"Users": {"u01": {...}}}`. The literal mapping preserves
   the OpenAPI promise that "JSON field names exactly mirror the
   XML element and attribute names."
2. **Attributes become sibling object keys.** `<User id="..."
   ...>` becomes `{"id": "...", ...}` at the same level as the
   user's other properties.
3. **Empty optional containers are omitted, not emitted as `[]`.**
   Matches the existing XML behavior (custom marshalers suppress
   empty wrappers because the XSD declares them with
   `minOccurs="0"`).

These three conventions become the rules the JSON marshalers in
`server/wire/` are written to, so the generator output and the Go
type shapes stay aligned by construction.

### Envelope examples

The XML envelope shape is unchanged. The equivalent JSON envelope
for a single-post sync message looks like this (whitespace added for
readability; the wire bytes are compact):

```json
{
  "version": 1,
  "type": "sync_msg",
  "ConnName": "nats-low-to-high",
  "Timestamp": "2026-05-23T10:00:00Z",
  "Epoch": "epoch01aaaaaaaaaaaaaaaaaaa",
  "Sequence": 1,
  "TeamName": "team-a",
  "ChannelName": "general",
  "SyncMsg": {
    "Id": "sm01aaaaaaaaaaaaaaaaaaaaaa",
    "ChannelId": "ch0aaaaaaaaaaaaaaaaaaaaaaa",
    "Users": [
      {
        "id": "u01aliceaaaaaaaaaaaaaaaaaa",
        "Id": "u01aliceaaaaaaaaaaaaaaaaaa",
        "CreateAt": 1700000000000,
        "UpdateAt": 1712956800000,
        "DeleteAt": 0,
        "Username": "alice",
        "Email": "alice@server-a.example.com",
        "Roles": "system_user"
      }
    ],
    "Post": {
      "Id": "p01postaaaaaaaaaaaaaaaaaaa",
      "CreateAt": 1712956800000,
      "UpdateAt": 1712956800000,
      "DeleteAt": 0,
      "UserId": "u01aliceaaaaaaaaaaaaaaaaaa",
      "ChannelId": "ch0aaaaaaaaaaaaaaaaaaaaaaa",
      "Message": "Morning team. Standup at 9:30 in the usual room, see you there."
    }
  }
}
```

A test envelope (no `SyncMsg`, carries a `TestID`):

```json
{
  "version": 1,
  "type": "test",
  "ConnName": "nats-low-to-high",
  "Timestamp": "2026-05-23T10:00:00Z",
  "Epoch": "epoch01aaaaaaaaaaaaaaaaaaa",
  "TeamName": "nats-low-to-high",
  "ChannelName": "nats-low-to-high",
  "TestID": "test01aaaaaaaaaaaaaaaaaaaaa"
}
```

### Detection rule

Inbound detection inspects the first non-whitespace byte after BOM
stripping:

- A leading UTF-8 BOM (`0xEF 0xBB 0xBF`) is stripped before
  inspection. The XML pipeline used to accept BOMs upstream; CDS
  systems can introduce them in either direction, so the rule is
  applied uniformly to both encodings.
- After BOM stripping, leading ASCII whitespace (`\x09`, `\x0A`,
  `\x0D`, `\x20`) is skipped.
- The next byte selects the decoder:
  - `<` (0x3C) means XML; decode with `encoding/xml`.
  - `{` (0x7B) means JSON; decode with `encoding/json`.
  - Anything else fails with a single audit event
    (`errcode.InboundFormatUnrecognized`, new in `inbound.go`'s
    block) and the envelope is dropped permanently (returns `nil`
    so the provider does not redeliver).

The detector lives in `server/transport.go` alongside `MarshalEnvelope`
and `UnmarshalEnvelope`. It is the only place format detection
happens; downstream code sees a `*TransportEnvelope` and does not
care which encoding produced it.

## Go changes

### `server/wire/`

Add explicit `json:` tags to every wire-type field, matching the
existing `xml:` element names. For example:

```go
type Post struct {
    Id        string `xml:"Id" json:"Id"`
    CreateAt  int64  `xml:"CreateAt" json:"CreateAt"`
    ...
    Props     *PostProps `xml:"Props,omitempty" json:"Props,omitempty"`
    ...
}
```

Five types currently have custom `MarshalXML`/`UnmarshalXML` and
therefore need parallel `MarshalJSON`/`UnmarshalJSON` to produce the
shape the JSON Schema mandates:

- `StringMap` (used for `User.Timezone`). XML form is `<Timezone>
  <Entry key="..." value="..."/>...</Timezone>`. JSON form is an
  array of `{"key": ..., "value": ...}` objects, sorted by key for
  deterministic output. (Not an object, because the XSD models it
  as a repeated `<Entry>` element with attributes.)
- `MentionTransforms` (`SyncMsg.MentionTransforms`). XML form is
  `<MentionTransforms><Transform key="..." value="..."/>...
  </MentionTransforms>`. JSON form is an array of `{"key": ...,
  "value": ...}`, sorted by key.
- `FileIDs` (`Post.FileIds`). XML form is `<FileIds><Id>...
  </Id>...</FileIds>`. JSON form is a flat array of strings (the
  outer wrapper element becomes the property name, the inner
  element's text becomes the array entry).
- `UserMap` (`SyncMsg.Users`). XML form is `<Users><User
  id="...">...</User>...</Users>`. JSON form is an array of User
  objects with an `id` property at the same level as the user's
  other properties, sorted by id.
- `SyncMsg`. XML form has the empty-container suppression logic in
  its `MarshalXML`. JSON form has the same suppression at the
  marshaler level: a nil or empty `Users`, `Reactions`,
  `Statuses`, `MembershipChanges`, `Acknowledgements`, or
  `MentionTransforms` omits the property entirely rather than
  emitting `null` or `[]`.

The custom-marshaler set is closed: the other wire types
(`Post`, `Reaction`, `Status`, `MembershipChange`,
`PostAcknowledgement`, `User`, `UserProps`, `PostProps`) work
correctly with default `encoding/json` behavior plus `omitempty`
tags.

Determinism note: every custom JSON marshaler sorts its iteration
order. The wire archive is a forensic record and operators must be
able to byte-compare archived envelopes across nodes; non-
deterministic map iteration would defeat that.

### `server/transport.go`

Add a format selector and the auto-detecting decoder. The marshaler
gains a parameter; the unmarshaler does not, since it sniffs.

```go
type WireFormat int

const (
    FormatXML WireFormat = iota
    FormatJSON
)

func (f WireFormat) String() string {
    switch f {
    case FormatJSON:
        return "json"
    default:
        return "xml"
    }
}

func ParseWireFormat(s string) (WireFormat, error) {
    switch strings.ToLower(strings.TrimSpace(s)) {
    case "", "xml":
        return FormatXML, nil
    case "json":
        return FormatJSON, nil
    default:
        return FormatXML, fmt.Errorf("invalid message_format %q (want \"xml\" or \"json\")", s)
    }
}

func MarshalEnvelope(env *TransportEnvelope, format WireFormat) ([]byte, error) {
    ...
}

// UnmarshalEnvelope auto-detects the format by sniffing the first
// non-whitespace byte after BOM stripping. Returns the format that
// was detected alongside the envelope so the inbound path can audit
// a per-format counter.
func UnmarshalEnvelope(data []byte) (*TransportEnvelope, WireFormat, error) {
    ...
}
```

The `archiveEnvelope` chokepoint stays at the `MarshalEnvelope`
boundary. The archive filename gains an explicit `.xml` or `.json`
suffix so the wire-archive validator can dispatch correctly. See
"Wire archive" below.

### `server/configuration.go`

`ConnectionConfig` gains a `MessageFormat` field. Inbound
connections ignore it (auto-detect overrides whatever is set);
outbound connections use it to choose the encoder.

```go
type ConnectionConfig struct {
    Name     string `json:"name"`
    Provider string `json:"provider"`
    SiteURL  string `json:"site_url,omitempty"`

    // MessageFormat selects the outbound wire encoding: "xml"
    // (default) or "json". Empty defaults to "xml". Ignored on
    // inbound connections (inbound auto-detects).
    MessageFormat string `json:"message_format,omitempty"`

    ...
}
```

`validate()` rejects values that are not in `{"", "xml", "json"}`
with a clear error message. Empty stays empty in storage (the
defaulter at the publish site interprets it as XML); we do not
rewrite empty to `"xml"` at parse time because rewriting on read
muddies the audit-config trail.

### `server/connections.go`

`publishToOutboundConn` and `buildTestEnvelope` read
`MessageFormat` from the outbound connection config and pass the
parsed `WireFormat` to `MarshalEnvelope`. `buildTestEnvelope`
signature changes from `(connName, epoch)` to `(connName, epoch,
format)` and is called from the test-connection HTTP handler in
`api.go` and the slash-command path in `command.go`.

The fanout in `transport.go::buildOutboundEnvelopes` is unchanged.
Each emitted `*TransportEnvelope` is marshaled exactly once by
`publishToOutboundConn` using the connection's format. The
per-`(connName, channelID)` sequence counter is unaffected by the
encoding choice.

### `server/inbound.go`

`processInboundMessage` calls the new
`UnmarshalEnvelope(data) (*TransportEnvelope, WireFormat, error)`
and records the detected format on a per-connection counter for
audit. Add one new error code per envelope handled:

- `InboundFormatXML` (Info): an XML envelope was accepted.
- `InboundFormatJSON` (Info): a JSON envelope was accepted.
- `InboundFormatUnrecognized` (Warn): leading byte was neither `<`
  nor `{` after BOM and whitespace stripping; envelope dropped.

The existing `InboundUnmarshalFailed` covers parser errors after
the format has been chosen. Sampling: the per-envelope Info codes
go through `LogDebug` to avoid log spam, with the counters surfaced
in the existing `/crossguard status` command output. (The
slash-command output already shows the connection table; format
counts join that line.)

`isTestMessage` in `configuration.go` (used by the NATS file
watcher to identify test probes) gets the same auto-detect
treatment by delegating to `UnmarshalEnvelope`.

### `server/envelope_archive.go`

The archive file naming convention changes from
`<timestamp>_<conn>_<type>.xml` to
`<timestamp>_<conn>_<type>.<ext>` where `<ext>` is `xml` or `json`.
The archive helper takes the chosen format as an additional
parameter from `MarshalEnvelope`.

The wire-archive validator (under `make
docker-integration-test-validate-wire`) dispatches on the file
extension:

- `.xml` files: piped directly through `xmllint --schema
  crossguard.xsd` as today.
- `.json` files: decoded via `UnmarshalEnvelope` into a
  `*TransportEnvelope`, re-encoded as XML, then piped through
  `xmllint`. A decode-then-re-encode round trip is the runtime
  enforcement of the JSON Schema: any JSON envelope that fails the
  XSD after re-encoding is a compliance violation by construction,
  because the wire types are the only path from JSON bytes to XML
  bytes.

A small helper binary at `build/wire-archive-validate/` reads the
archive directory and runs both paths. The existing shell-level
xmllint loop in the Makefile invokes the helper instead of running
xmllint directly.

## JSON Schema generator

`build/xsd2jsonschema/main.go` is a single-file Go program. Inputs:
the XSD path; output: a JSON Schema document.

The generator is intentionally not a general-purpose tool. It only
handles the constructs that appear in `crossguard.xsd`. Any
unrecognized XSD construct produces a hard error pointing at the
unsupported element so a future XSD change either fits the existing
ruleset or forces a generator update.

The output is JSON Schema draft 2020-12 with `$schema`,
`$id` (pointing at the same location as `schema/crossguard.xsd` in
the README), `title`, and `description` populated from the XSD's
`xs:annotation/xs:documentation` blocks where present.

`make generate-json-schema` regenerates the file in place. The CI
gate is:

```makefile
check-json-schema:
    go run ./build/xsd2jsonschema schema/crossguard.xsd > /tmp/regen.json
    diff -u schema/crossguard.schema.json /tmp/regen.json
```

If the diff is non-empty, CI fails with a message instructing the
developer to run `make generate-json-schema` and commit the result.
This is the same pattern used today for `server/manifest.go`
(generated from `plugin.json`).

The generator is also responsible for emitting the JSON shape
conventions documented above. The custom-marshaler types in
`server/wire/` are written to those conventions; a small unit test
in `server/wire/` decodes each shipped JSON example, re-encodes it,
and asserts the bytes are byte-identical (modulo whitespace). That
test is the in-process verification that the generator and the Go
marshalers agree.

## Examples

The fixture set in `schema/examples/` currently contains 22 XML
files plus a `README.md`. Add a JSON sibling for **every** XML
fixture (one-for-one), not a representative subset. The
`schema/examples/` directory is the authoritative reference handed
to compliance and security reviewers approving each wire format, so
a reviewer of the JSON spec must see every element shape, pruning
case, and deterministic-ordering case the XML reviewer sees. Any
fixture present only on one side hides surface area from review and
silently shifts the approval contract.

The numbering convention becomes `NN_name.xml` plus `NN_name.json`
for every numbered fixture. The `TestExampleFiles` round-trip test
in `server/examples_roundtrip_test.go` grows a sibling
`TestExampleFilesJSON` that:

1. Reads each `*.json` example.
2. Decodes via `UnmarshalEnvelope` (the auto-detector picks JSON).
3. Re-encodes as XML.
4. Validates the re-encoded XML against `crossguard.xsd` via
   `xmllint`.
5. Decodes the corresponding `NN_name.xml` and asserts the two
   decoded `*TransportEnvelope` values are equal.

Step 5 is the equivalence anchor: it ensures the JSON example and
the XML example carry the same data and that the wire types treat
them identically. Step 4 is the schema gate via the XSD.

A separate test `TestExampleFilesJSONValidateAgainstJSONSchema`
validates each `*.json` directly against
`schema/crossguard.schema.json` with a JSON Schema validator (no
XML round-trip). This is the standalone JSON-side compliance gate:
if `build/xsd2jsonschema` ever drifts from the XSD's semantics in
a way that lets an example pass the XML re-encode but fail the
JSON Schema (or vice versa), this catches it. The validator uses
an ECMA 262 regex engine (dlclark/regexp2) because the schema
contains repeat counts up to `{0,1536}` that exceed Go stdlib
`regexp/syntax`'s 1000-count cap.

`UPDATE_EXAMPLES=1 go test -run TestExampleFiles ./server/` continues
to regenerate the XML fixtures from the in-test models. A new flag,
`UPDATE_EXAMPLES_JSON=1`, regenerates the JSON fixtures the same
way. Two flags rather than one because the XML and JSON sets are
encoded independently, but both must be regenerated together on any
wire-type change so the sibling-parity invariant holds.

## Webapp

`webapp/src/components/ConnectionSettings.tsx` already carries
`message_format: 'json' | 'xml'`, defaults to `'json'`, and shows
an "XML" badge. The plan flips the default to `'xml'` to match the
backend's new default, hides the field on inbound forms (it has no
effect there), and threads the value through the test-connection
probe and the status display.

Specifically:

- `ConnectionForm` (the editable struct) keeps the field; its
  default becomes `'xml'`.
- The provider-specific form for outbound connections shows the
  format selector. Inbound forms hide it.
- The connection card already shows an XML badge when
  `message_format === 'xml'`; add a parallel JSON badge for
  `'json'`, and show neither badge on inbound cards.
- The bulk-status endpoint response, which the connection card
  reads, gains a `message_format` field for outbound rows so the
  badge can be rendered without the form state.

The webapp Playwright suite already has fixtures and assertions
keyed on `message_format`; those continue to pass after the default
flip with no code change beyond updating the fixture defaults from
`'json'` to `'xml'`.

## OpenAPI

`schema/crossguard-api.yaml` already documents both formats and
the auto-detect rule (it predates the loss of JSON support). The
update is:

- Bump `info.version` from `1.1.0` to `1.2.0`.
- Update the `MessageEnvelope` description and any references to
  the default format to state explicitly that the default is XML.
- Add an `examples:` block at the schema level pointing at one XML
  and one JSON example file under `schema/examples/`.
- Add a `$ref: "./crossguard.schema.json"` pointer in the
  `MessageEnvelope` JSON content-type block. The XML side keeps
  its existing `$ref` to the XSD-derived schema.

No new endpoints. The API surface is unchanged.

## Slash commands

`/crossguard status` already shows a per-connection table. Add a
`Format` column for outbound connections (value: `xml` or `json`)
so operators can see the configured wire format at a glance. For
inbound connections, the column shows `auto` since the inbound side
does not have a configured format.

`/crossguard help` text is updated to mention `message_format` as a
configurable outbound setting and the auto-detect inbound behavior.

## Compliance review impact

The reviewer's surface gains one new artifact: `crossguard.schema.json`.

What does not change:

- The XSD `crossguard.xsd` remains the field whitelist, value
  patterns, and length caps. The JSON Schema is a translation of
  the XSD, not an independent contract.
- The wire types in `server/wire/` still enforce the field
  whitelist on the outbound producer side, in both directions of
  the conversion (FromModel and ToModel).
- The wire archive at integration test time runs every envelope
  (XML or JSON) through `xmllint --schema crossguard.xsd`. For
  JSON envelopes this happens after a decode-then-re-encode round
  trip, so the XSD is the universal final-gate regardless of
  encoding.

What the review needs to confirm:

1. The XSD-to-JSON-Schema generator's mapping ruleset (documented
   in the table above plus the three shape conventions) is
   acceptable. This is a one-time review; subsequent XSD changes
   re-run the generator deterministically and only the diff to
   the JSON Schema needs re-confirmation.
2. The generated `crossguard.schema.json` matches what the
   compliance pipeline expects to ingest (draft 2020-12, the same
   anchoring and additionalProperties posture as the XSD).
3. The runtime enforcement strategy (XSD as final gate via xmllint
   after JSON-to-XML round trip) is acceptable as a substitute for
   running a JSON Schema validator at the wire-archive step. The
   round-trip path cannot encode anything the XSD forbids because
   the wire types do not have fields outside the whitelist.

## Phased rollout

The change is independently mergeable in three phases.

### Phase 1: JSON Schema generator and shape conventions

- Build `build/xsd2jsonschema/main.go`.
- Generate and check in `schema/crossguard.schema.json`.
- Add `make generate-json-schema` and `make check-json-schema`.
- No runtime change yet. JSON encoding is not enabled on any
  connection. The default format selector and inbound auto-detect
  are not wired up.

This phase is independently reviewable: the JSON Schema is
inspectable by compliance without affecting any deployed plugin's
behavior.

### Phase 2: JSON encoder and decoder in `server/wire/`

- Add `json:` struct tags to every wire type.
- Add `MarshalJSON`/`UnmarshalJSON` to the five custom-marshaler
  types per the shape conventions.
- Add unit tests under `server/wire/` that round-trip each type
  through both XML and JSON and assert equality.
- Add the `schema/examples/*.json` siblings and the
  `TestExampleFilesJSON` round-trip test.

This phase exercises the JSON encoding paths in isolation. No
runtime envelope produces JSON yet.

### Phase 3: Format selector wiring

- Add `MessageFormat` to `ConnectionConfig` and validation.
- Refactor `MarshalEnvelope` to take a `WireFormat`.
- Refactor `UnmarshalEnvelope` to auto-detect and return the format.
- Wire `publishToOutboundConn`, `buildTestEnvelope`, `isTestMessage`,
  and `processInboundMessage` to the new signatures.
- Update the envelope archive naming and the wire-archive
  validator helper.
- Update the webapp to un-orphan `message_format` (un-hide on
  outbound forms, hide on inbound forms, flip default to `xml`).
- Update `/crossguard status` and `/crossguard help`.
- Add `outbound:json-low-to-high` / `inbound:json-low-to-high`
  baseline connections in `build/devbaseline/` for the integration
  test.
- Add the JSON row to the renamed `wire_format_test.go`
  (formerly `xml_test.go`).
- Update the OpenAPI doc to 1.2.0.

This is the user-visible phase. After Phase 3 lands, operators can
flip an outbound connection to `message_format: "json"` and JSON
envelopes will appear on the wire.

## Test plan

### Unit (`server/wire/`)

- For each wire type, round-trip through XML and through JSON,
  assert decoded values are equal across encodings.
- For the five custom-marshaler types, assert key ordering is
  deterministic across runs (sort keys, marshal twice, byte-equal).
- For `StringMap`, `MentionTransforms`, `FileIDs`, `UserMap`,
  assert empty maps and empty slices are omitted from the JSON
  output (no `null`, no `[]`).
- For `SyncMsg`, assert the empty-container suppression matches the
  XML side: a metadata envelope with no `Post` does not emit a
  `Post` key in JSON.

### Unit (`server/`)

- `MarshalEnvelope` with each format produces well-formed output
  parseable by the corresponding `encoding/{xml,json}` decoder.
- `UnmarshalEnvelope` correctly detects XML, JSON, and unrecognized
  inputs across BOM/whitespace variants.
- `UnmarshalEnvelope` with a leading UTF-8 BOM followed by `{`
  decodes as JSON.
- `UnmarshalEnvelope` with leading whitespace before the first
  non-whitespace byte detects correctly.
- `isTestMessage` correctly identifies test envelopes in both
  formats.

### Schema (`server/examples_roundtrip_test.go`)

- `TestExampleFiles` (existing): XML examples round-trip and
  validate against the XSD.
- `TestExampleFilesJSON` (new): JSON examples decode, re-encode as
  XML, and validate against the XSD. The decoded JSON example and
  the decoded XML example for the same numbered fixture are equal.
- `TestJSONSchemaIsFresh` (new): regenerates the JSON Schema from
  the XSD and asserts it byte-matches the checked-in
  `crossguard.schema.json`.

### Integration (`server/integration/`)

- Rename `xml_test.go` to `wire_format_test.go` and `TestXMLRelay`
  to `TestWireFormatRelay`. The existing table-driven harness
  gains a second row exercising the
  `outbound:json-low-to-high` / `inbound:json-low-to-high`
  baseline connections.
- The `docker-integration-test-validate-wire` target runs against
  both formats. Each provider in `build/devbaseline/` keeps its
  current format; the JSON path is exercised exclusively through
  the JSON baseline connections so an inadvertent format flip on
  an existing connection shows up as a test failure.

### Wire archive

- `archiveEnvelope` writes XML envelopes with `.xml` extension and
  JSON envelopes with `.json` extension. The validator helper
  exits non-zero if any `.json` archive fails the
  decode-then-re-encode-then-xmllint pipeline.

## Risks and mitigations

**Risk: XSD-to-JSON Schema generator output drifts from the wire
types.** The Go marshalers and the JSON Schema must agree on the
shape conventions. Drift is detected by:

- `TestExampleFilesJSON`: every shipped JSON example decodes and
  re-encodes through the wire types without losing data.
- `TestJSONSchemaIsFresh`: the JSON Schema is regenerated from the
  XSD on every CI run and diffed.
- The unit tests in `server/wire/` that round-trip through both
  encodings.

If a Go marshaler changes shape (say, a new field is added or a
property is renamed), the JSON examples will fail to round-trip or
the JSON Schema will fail to validate them, and the CI gate fires.

**Risk: BOM/whitespace edge cases produce false-negative format
detection.** The detection rule is small and unit-tested across the
combinations that matter (no prefix, BOM only, whitespace only, BOM
plus whitespace, leading character is letter/digit/punctuation).
Anything outside the rule fails closed: the envelope is dropped
with `InboundFormatUnrecognized`, surfaced to operators, and not
redelivered.

**Risk: Operators upgrade and find their existing connections
broken because the default format flipped.** It does not flip for
existing connections, because the field defaults to `"xml"` when
empty and existing connections have empty `message_format`. The
post-refactor wire was already XML; this plan keeps it. Operators
opt into JSON explicitly.

**Risk: The webapp's existing default of `'json'` was previously
ignored by the backend, so saved configs from the
post-refactor-pre-JSON-restoration era contain
`message_format: "json"` that the backend ignored. After Phase 3
lands, those connections suddenly start emitting JSON without the
operator's awareness.** This is a real risk: any connection saved
through the current UI carries `message_format: "json"` because
the webapp form defaults the field. Mitigation:

- The webapp default flips to `'xml'` in the same Phase 3
  release. New saves get `"xml"`.
- A one-time migration in `OnConfigurationChange` (in the
  Phase 3 release only, gated by a version-stamp KV key)
  rewrites empty-or-`"json"` outbound `message_format` values to
  `"xml"` when the value originated from a webapp save before the
  current release. The migration is described in a short release
  note and the gate KV key is documented so future releases can
  remove the code without risk.

Alternatively, accept the risk and document the change loudly in
the release notes. The webapp default was effectively never
honored, so practically no production system relied on the JSON
behavior between the refactor and now. I recommend the migration
since it costs little and removes a known surprise.

**Risk: The JSON wire format is larger than XML on some payloads
because property names are repeated (each User object has `"Id":
..., "CreateAt": ..., ...`) where XML uses `<Id>...</Id>`.** This
is a known property of the encoding choice and not new; the
pre-refactor JSON wire had the same characteristic. Operators who
care about payload size pick XML.

## Out-of-scope follow-ups

- Compressing JSON payloads (gzip, zstd) at the provider boundary.
  The current code does not compress XML either; if compression is
  desirable, it should be added orthogonally to both formats.
- A binary wire format. Not on the roadmap; the pair of XML and
  JSON covers the two compliance-tool ecosystems we ship into.
- Per-channel format override. The format is a property of the
  connection, not the channel. There is no use case for mixing.
