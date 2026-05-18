# Constrained-XSD fixes

## Context

`schema/crossguard-wiggin77-constrained.xsd` is a derivative of
`schema/crossguard.xsd` that tightens every string element with a
pattern and/or `maxLength` and replaces every `maxOccurs="unbounded"`
with a concrete cap. The intent is to give compliance content
inspection a schema strict enough to reject malformed traffic byte-by-
byte.

Validating the 21 fixtures in `schema/examples/` against the new
schema, all 21 fail. Most of the failures are fixture-only (synthetic
placeholder IDs like `sm-01` and `remote-low`), but five failures are
real schema bugs that would reject valid wire traffic emitted by the
running plugin today, and one more is a stricter-than-upstream pattern
that will drop valid user records. A seventh issue is a per-envelope
cap that is plausible to exceed in busy channels.

This plan fixes the schema so it admits every envelope the plugin
emits, then regenerates the fixtures so the example suite validates
end-to-end.

## Scope

In scope:

- `schema/crossguard-wiggin77-constrained.xsd` edits to types and
  occurrence caps that reject real traffic.
- `schema/examples/*.xml` regeneration with real `mmModel.NewId()`
  IDs.
- A targeted Go test that validates every fixture against the
  constrained schema via `xmllint`, so regressions are caught in CI
  rather than at a compliance review.

Out of scope:

- Renaming or repurposing the constrained file (it stays a sibling
  of the original until the security/compliance review accepts it).
- Changes to the wire structs in `server/wire/` or to
  `TransportEnvelope`. The schema must match the producer, not the
  other way around.
- Changes to `crossguard.xsd` (the loose schema). It stays as the
  current production contract.

## Problem inventory

### P1: blocking, will reject real wire traffic

| # | Element / type | Symptom | Root cause |
|---|---|---|---|
| 1 | `TestID` typed as `MattermostIdType` | Every `test` envelope rejected | `buildTestEnvelope` (`server/connections.go:52-70`) generates `test-<unix_nano>`, not a 26-char Mattermost ID |
| 2 | `TeamName`, `ChannelName` typed as `SlugType` (1+ char) | Every `test` envelope rejected | `buildTestEnvelope` leaves both empty; the constrained `SlugType` pattern requires at least one character |
| 3 | `OverrideIconEmoji` typed as `EmojiNameType` | Webhook-relayed posts rejected | Mattermost stores `override_icon_emoji` in `:emoji_name:` form (leading and trailing colons), but the pattern is `[A-Za-z0-9_\-\+]{1,64}` |
| 4 | `Username` typed as `UsernameType` | Multi-hop / remote-tagged users rejected | Upstream uses `IsValidUsernameAllowRemote` for shared-channel users (regex `^[a-z0-9.\-_:]*$`); `UsernameType` excludes `:` |
| 5 | `UserProps/RemoteUsername` typed as `UsernameType` | Remote-username props with `@` (e.g. `alice@source`) rejected | The wire field carries an upstream-supplied remote identifier, not a Mattermost username (see `server/wire/user_test.go:22`) |

### P2: stricter than upstream, may reject valid records

| # | Element / type | Risk |
|---|---|---|
| 6 | `EmailType` regex | Rejects RFC 5321-valid local-parts with `!#$%&'*/=?^_\`{\|}~`, IDN/Unicode local-parts, and IP-literal hosts. Mattermost's own `IsValidEmail` is more permissive, so the schema would drop records the producer happily emitted |

### P3: cap probably too tight

| # | Element | Current cap | Reasoning |
|---|---|---|---|
| 7 | `MentionTransformsType.Transform` | 100 | Mention transforms are channel-scoped and ride with each envelope, including all distinct mentions ever observed in the channel. 100 is plausible to exceed in long-lived channels; raise to 1000 to match the other per-envelope caps |

### P4: fixture-only failures (not schema bugs)

The 21 fixtures use synthetic placeholders that were never meant to
look like real `mmModel.NewId()` output: `<Id>sm-NN</Id>` and
`<RemoteId>remote-low</RemoteId>`. These fail every `MattermostIdType`
check. They are cosmetic, but they must be regenerated for the
example suite to validate against the strict schema.

## Phase A: schema fixes

Edit `schema/crossguard-wiggin77-constrained.xsd` in a single commit.
Each change is small and the rationale is captured in inline XSD
comments so the security/compliance reviewer can audit the loosening.

1. **TestID format** (P1.1). Add a `TestIdType`:
   ```xml
   <xs:simpleType name="TestIdType">
     <xs:restriction base="xs:string">
       <xs:pattern value="test-[0-9]{1,30}"/>
       <xs:maxLength value="64"/>
     </xs:restriction>
   </xs:simpleType>
   ```
   Reference it from `TestID` in the envelope choice (replace the
   `MattermostIdType` reference). Comment the rationale: `TestID` is a
   plugin-internal probe identifier, not an `mmModel.NewId()` value.

2. **Empty TeamName/ChannelName on test envelopes** (P1.2). Loosen
   `SlugType` to accept zero-length values:
   ```xml
   <xs:pattern value="([a-z0-9][a-z0-9_\-]{0,63})?"/>
   ```
   Keep the `maxLength` cap. The producer ships empty strings for
   `test` envelopes because they have no channel scope. Comment the
   rationale next to the pattern.

3. **OverrideIconEmoji colon-wrapped values** (P1.3). Replace the
   reference to `EmojiNameType` with a dedicated `IconEmojiType`:
   ```xml
   <xs:simpleType name="IconEmojiType">
     <xs:restriction base="xs:string">
       <xs:pattern value=":?[A-Za-z0-9_\-\+]{1,62}:?"/>
       <xs:maxLength value="64"/>
     </xs:restriction>
   </xs:simpleType>
   ```
   Leave the existing `EmojiNameType` in place for `Reaction.EmojiName`
   (reactions store bare names without colons).

4. **Remote-tagged usernames** (P1.4). Add `:` to `UsernameType`:
   ```xml
   <xs:pattern value="[a-z0-9][a-z0-9_\-\.:]{0,63}"/>
   ```
   Cite upstream `validUsernameCharsForRemote` in the comment so the
   reviewer can see this matches `IsValidUsernameAllowRemote`.

5. **RemoteUsername prop** (P1.5). Decouple from `UsernameType`. The
   wire value is an upstream-supplied identifier that can contain `@`
   plus host-style characters. Introduce `RemoteIdentifierType` with
   `maxLength=256` and no pattern, and use it for
   `UserPropsType/RemoteUsername`.

6. **Email permissiveness** (P2.6). Drop the regex on `EmailType`,
   keep the `maxLength=254` cap. Justification in the comment: the
   schema must not reject what the producer accepts; producer-side
   email validation is the upstream Mattermost validator.

7. **Mention-transforms cap** (P3.7). Raise
   `MentionTransformsType.Transform/@maxOccurs` from 100 to 1000.
   Comment cites the matching cap on `Users`, `Reactions`, etc.

No changes to `crossguard.xsd`. The loose schema is the deployed
contract and must not move.

## Phase B: regenerate fixtures

The fixtures use hand-typed synthetic IDs. The roundtrip test already
has machinery to emit examples; reuse it.

1. Audit `server/examples_roundtrip_test.go` for the fixture-generation
   path (the `UPDATE_EXAMPLES=1` env var on `TestExampleFiles` in
   `server/`, per CLAUDE.md). Confirm the path produces real
   26-character IDs.
2. Replace synthetic seeds in the test fixtures' source with
   `mmModel.NewId()` calls (or equivalent fixed 26-char seeds). The
   ID values should be deterministic across runs so the fixtures are
   reproducible: use a seeded RNG or hard-code 26-char strings that
   match `[a-z0-9]{26}`.
3. Run `UPDATE_EXAMPLES=1 go test -run TestExampleFiles ./server/` and
   commit the regenerated XML files.

The bot username `release-summary-bot` in the existing fixture matches
the loosened `UsernameType` and needs no change.

## Phase C: CI gate

Add a Go test (or extend `TestExampleFiles`) that, when `xmllint` is
on `PATH`, validates every fixture against *both* schemas:

```go
for _, schema := range []string{
    "schema/crossguard.xsd",
    "schema/crossguard-wiggin77-constrained.xsd",
} {
    for _, fixture := range fixtures {
        // exec xmllint --noout --schema <schema> <fixture>
        // fail the test on non-zero exit
    }
}
```

The test is skipped when `xmllint` is absent so developer environments
without libxml2 stay green; CI installs libxml2-utils (already a
common base-image dependency) and exercises the gate. This prevents a
future fixture regeneration from re-introducing a synthetic ID that
passes the loose schema but fails the strict one.

## Validation gate

The change is complete when:

1. `make check-style` is clean.
2. `make test` is green.
3. `for f in schema/examples/*.xml; do xmllint --noout --schema
   schema/crossguard-wiggin77-constrained.xsd "$f"; done` reports zero
   failures.
4. The same loop against `schema/crossguard.xsd` still reports zero
   failures (loose schema must continue to admit what the strict
   schema admits).
5. The new Go test (Phase C) passes locally.

## Out of scope follow-ups

- Promote the constrained schema to the deployed contract. That is a
  product/compliance decision and a separate review cycle; this plan
  only makes the strict file accurate so the review can happen.
- Tighten `MattermostIdType` to require exactly 26 chars by also
  setting `minLength` (currently only `maxLength=26` plus the
  `[a-z0-9]{26}` pattern, which already enforces the bound).
- Document the schema-vs-producer contract in
  `implementation-plans/26-04-12-03-xml-wire-format-schema.md` so the
  next reviewer doesn't have to rediscover that the schema is
  generated from the wire structs, not the other way around.
