# Documentation Accuracy Plan: Post Shared-Channels Refactor

**Date:** 2026-05-22
**Status:** Approved 2026-05-22, revised 2026-05-22. PR per phase. JSON wire-format support will be re-implemented in a follow-up effort after this documentation work lands, so the docs describe both JSON and XML as the target state. Frontend `message_format` selector left untouched (it already collects the right value; the backend implementation is the follow-up). `error-codes.html` will be regenerated from a new generator program; see Phase 4.

## Background

Since the shared-channels refactor (commit `e37edd5`) and the cascade of changes that followed (plugin-owned wire types `cf9be6c`, Epoch/Sequence stamping `6a2b1fe`, single-active inbound elector `ba2b1fe`, four-provider transport, request-mode admin flows `2ad40fa`/`c070bab`, Azure Service Principal auth `3b3c680`), the documentation set linked from the `README.md` has fallen materially out of date.

Audit results (recorded in this conversation): the XSD, the 22 XML example fixtures, and the slash-command reference (`commands.html`) are accurate. **Every other linked document is wrong** at the architecture, wire-format, security-mechanism, or settings level. The whitepaper, threat model, transport-interface doc, OpenAPI `MessageEnvelope` schema, and several Help-Guide pages all anchor their narratives on code that no longer exists.

This plan brings the documentation back into alignment with the current code, without making any product changes.

## Scope

In scope:

- `README.md` wire-format section and `server/model/message.go` cross-reference.
- `public/help/help.html`, `admin.html`, `api.html`, `error-codes.html` (the Help Guide).
- `public/help/transport-interface.html` (the Transport Interface Reference).
- `public/help/whitepaper.html`.
- `public/help/threatmodel.html`.
- `schema/crossguard-api.yaml` (OpenAPI envelope, payload schemas, provider config blocks).
- `schema/crossguard.xsd` header comment only (the structural content is correct).
- `schema/examples/README.md` (factual fixes to fixture descriptions).
- The four PDFs in `public/help/` (regenerated via `make generate-pdfs`).

Out of scope:

- Implementing JSON wire-format support. That work is the immediate follow-up to this plan; the docs written here describe JSON and XML as the target state so that when the JSON code lands, the docs are already accurate.
- Touching the frontend `message_format` selector in `ConnectionSettings.tsx`. It already collects the correct value; the backend hookup is the follow-up.
- Changes to any production code path under `server/`, including `server/wire/*`, `server/transport.go`, `server/configuration.go`. The XSD and the wire types are the reference for XML; the JSON envelope shape is documented as a mirror of the XML envelope (see Phase 2).
- Changes to `commands.html` (already accurate) or the 22 XML example fixtures (all currently xmllint-clean against the XSD).

New tooling introduced by this plan:

- `scripts/generate-error-codes/` (Go program) — emits `public/help/error-codes.html` from `server/errcode/codes.go`. Wired into the Makefile as `make generate-error-codes`. See Phase 4 for the full design.

## Guiding principles

1. **Code is the source of truth.** Every claim in every doc must be re-greppable against `server/`.
2. **XSD is the structural reference.** Where the OpenAPI and HTML docs describe envelope or wire-type shape, copy from `schema/crossguard.xsd`.
3. **No new abstractions.** Same files, same section structure, same navigation. Rewrite only the wrong sections.
4. **No backward-compat shims.** Strike removed settings (`UsernameLookup`), removed mechanisms (`crossguard_relayed`, `Position=crossguard-sync`, `SetDeletingFlag`, `relaySem`), and the fictional typed-envelope vocabulary (`crossguard_post`/`update`/`delete`/`reaction_add`/`reaction_remove`) outright. The four real envelope types (`sync_msg`, `attachment`, `profile_image`, `test`) apply to both JSON and XML serializations.
5. **Document the target state, JSON and XML.** JSON wire-format support is the immediate follow-up to this plan. The docs in this plan describe both formats and the `message_format` selector as if both are wired through. When the JSON code lands, the docs are already accurate. Phase 1 (already merged) preserved existing JSON mentions in the README rather than stripping them.

## Decisions

1. **PR strategy** (resolved 2026-05-22): one PR per phase. Phase 1 (low-risk patches) first; subsequent phases land independently as each completes review.
2. **JSON wire-format support** (revised 2026-05-22): JSON support will be re-implemented as a follow-up immediately after this documentation work lands. The docs describe both JSON and XML as the target state. The frontend `message_format` selector is left untouched (it already collects the right value). Phase 1's README rewrite was reverted to preserve the JSON+XML wire-format table; Phases 2-4 are revised to describe both formats throughout.
3. **`error-codes.html` regeneration** (resolved 2026-05-22): Phase 4 introduces a generator. A new Go program at `scripts/generate-error-codes/main.go` parses `server/errcode/codes.go` and emits `public/help/error-codes.html`. A new Makefile target `make generate-error-codes` invokes it. Hand-written supplementary content (descriptions, troubleshooting tips) lives in a sidecar `scripts/generate-error-codes/annotations.yaml` keyed by constant name; the generator merges those in at render time. This makes the page reproducible from `codes.go` and stops it drifting.

## JSON envelope shape (resolved 2026-05-22)

JSON will exactly mirror the XML structure. The wire structs in `server/transport.go` and `server/wire/*.go` will pick up `json:"..."` tags alongside their existing `xml:"..."` tags as part of the JSON follow-up; JSON field names match XML element / attribute names so the two serializations are field-for-field equivalent. Phase 2's OpenAPI yaml rewrite documents this exact-mirror shape (e.g. `ConnName`, not `conn_name`); Phase 2's transport-interface.html shows side-by-side examples in both serializations sharing identical field names.

## Phase 1 — Low-risk mechanical patches

Small, contained fixes that do not touch the wire-protocol story. Safe to land first; useful as a sanity-check on tooling.

### Files

- `schema/crossguard.xsd`
- `schema/examples/README.md`
- `schema/crossguard-api.yaml`
- `public/help/api.html`
- `README.md`

### Changes

- **`schema/crossguard.xsd:11-26`**: update the header comment so its quoted maxLength values match the actual XSD body (96 / 96 / 381 / 53, not 64 / 64 / 254 / 35). The body is correct; only the comment is stale.
- **`schema/examples/README.md`**:
  - L79-82: rewrite the fixture-12 paragraph. The fixture carries `<TeamName>nats-low-to-high</TeamName><ChannelName>nats-low-to-high</ChannelName>`, not empty elements.
  - L76-78: unmix the fixture-19 / fixture-20 reference. The bot user is fixture 20.
  - Files table (L41-63): add a row for fixture 22 (`22_system_add_to_channel.xml`).
  - Optionally swap the Python `xmlschema` snippet for an `xmllint` invocation to match the integration-test contract.
- **`schema/crossguard-api.yaml`**:
  - Add `site_url` to `ConnectionConfig` (matches `configuration.go:184`).
  - Add `poll_interval_seconds` and `blob_poll_interval_seconds` to `AzureQueueProviderConfig`.
  - Add `batch_poll_interval_seconds` to `AzureBlobProviderConfig`.
  - Reconcile the "azure_queue" prose with the "azure-queue" enum values on the `provider` field.
- **`public/help/api.html`**:
  - Add `GET /api/v1/autocomplete/connections/{action}` (`server/api.go:51`).
  - Correct the authentication note for `POST /dialogs/connection/select`: UserId comes from the dialog payload, not the `Mattermost-User-Id` header.
- **`README.md`**:
  - L73: strike the `server/model/message.go` cross-reference (no such file; no such function).
  - L64-82: rewrite "Wire Format: JSON vs XML" as "Wire Format" (XML only), pointing at `schema/crossguard.xsd` and `schema/examples/`. Strike the BOM-stripping paragraph and the auto-detect claim.

### Validation

- `xmllint --noout schema/crossguard.xsd` — schema still parses.
- For each example: `xmllint --noout --schema schema/crossguard.xsd schema/examples/*.xml` — all 22 still validate.
- `grep -rn` each named field/symbol in the updated docs against `server/` — every reference resolves.

## Phase 2 — Source-of-truth rewrite: Transport Interface + OpenAPI envelope

This phase rewrites the deepest technical references. Every other document cross-references these, so they get fixed first.

### Files

- `public/help/transport-interface.html`
- `schema/crossguard-api.yaml`

### Changes

Phase 2 documents JSON and XML as parallel serializations of one semantic envelope model. The XML serialization already exists in code (and in `schema/crossguard.xsd`); the JSON serialization is the immediate follow-up. Per the resolved JSON envelope shape (above), the JSON envelope mirrors the XML element / attribute names exactly. The same Go structs in `server/transport.go` and `server/wire/*.go` carry both XML and JSON tags; the OpenAPI yaml documents the JSON field names as the XML names (e.g. `ConnName`, `Timestamp`, `SyncMsg`).

- **`transport-interface.html` (roughly lines 36-1184)**: replace the entire fictional protocol description with the real envelope model, described in both serializations:
  - **Semantic envelope:** `Version`, `Type`, `ConnName`, `Timestamp`, `Epoch`, `Sequence`, `TeamName`, `ChannelName`, and either a `SyncMsg` payload (a `wire.SyncMsg`) or a `TestID`. Anchor the field semantics on the Go struct (`server/transport.go:35-47`); the two serializations differ only in encoding.
  - **`Type` enum:** `sync_msg`, `attachment`, `profile_image`, `test`. Strike the six fictional `crossguard_*` types entirely.
  - **XML serialization:** `<CrossGuardEnvelope version="1" type="...">` with child elements per the XSD. Anchored on `schema/crossguard.xsd`.
  - **JSON serialization:** `{"version": 1, "type": "...", "ConnName": "...", "Timestamp": "...", "Epoch": "...", "Sequence": N, "TeamName": "...", "ChannelName": "...", "SyncMsg": {...} | "TestID": "..."}`. JSON field names exactly match the XML element / attribute names on the same Go structs (added as `json:"..."` tags alongside the existing `xml:"..."` tags in the JSON follow-up). The OpenAPI yaml is the normative schema (see below).
  - **`SyncMsg` payload:** replace the per-type `PostMessage` / `DeleteMessage` / `ReactionMessage` / `TestMessage` payload tables with a single `SyncMsg` section sourced from the XSD, describing `Post`, `Users`, `Reactions`, `Statuses`, `MembershipChanges`, `Acknowledgements`, `MentionTransforms`. Show both XML and JSON examples for each.
  - **One-post-per-envelope contract:** document `buildOutboundEnvelopes` as a compliance content-inspection blast-radius control. The contract holds for both serializations.
  - **`message_format` setting:** document the per-connection `message_format` toggle (`json` | `xml`) and how outbound chooses the encoding. Document the inbound auto-detect rule: the first non-whitespace byte selects (`<` means XML, `{` means JSON). A leading UTF-8 BOM on inbound XML is stripped before detection. Inbound connections do not need to be told which format to expect.
  - **Forward references for the JSON follow-up:** where the doc names specific Go entities for JSON (e.g. a format-detection helper or marshaller), use the form "in the upcoming JSON support work" rather than pretending the symbols already exist. Once the follow-up code lands, those forward references get rewritten to point at the actual symbols.
  - Fix "12 schema-valid sample payloads" → 22.
- **`transport-interface.html` provider sections**: refresh the Azure Queue, Azure Blob, and Azure Service Bus config tables to include:
  - The Service Principal auth fields (`auth_mode`, `tenant_id`, `client_id`, `client_secret`; `service_bus_namespace` for Service Bus).
  - The configurable poll intervals (`poll_interval_seconds`, `blob_poll_interval_seconds`, `batch_poll_interval_seconds`).
- **`transport-interface.html` interface section**: add the real `QueueProvider` interface (`server/provider.go`), including the `IsConnected() bool` method the prior doc omitted.
- **`schema/crossguard-api.yaml`**:
  - Rewrite the `MessageEnvelope` schema as the JSON serialization of the semantic envelope: `version`, `type`, `ConnName`, `Timestamp`, `Epoch`, `Sequence`, `TeamName`, `ChannelName`, plus a `SyncMsg` object or a `TestID` string. JSON field names exactly match the XML element / attribute names (`version` and `type` are attributes on the XML root, so they remain lowercase; child element names stay CamelCase).
  - Rewrite `MessageType` enum to the four real types.
  - Replace `PostMessage` / `DeleteMessage` / `ReactionMessage` / `TestMessage` schemas with `SyncMsg` and its components, modeled on the XSD types (`SyncMsgType`, `PostType`, `UserType`, `ReactionType`, `StatusType`, `MembershipChangeType`, `PostAcknowledgementType`). Field names match the XML element names on the corresponding Go structs in `server/wire/*.go`; the JSON follow-up will add `json:"..."` tags alongside the existing `xml:"..."` tags to produce exactly these names.
  - Keep `ConnectionConfig.message_format` as-is. The selector already exists in the UI and will be wired through in the JSON follow-up.

### Validation

- Every named XML field/type re-greppable in `server/transport.go`, `server/wire/*.go`, `server/provider.go`, `server/configuration.go`.
- JSON envelope fields documented in Phase 2 form the contract for the JSON follow-up. They are not yet greppable in code; that is expected and called out in the doc.
- XSD still parses; 22 fixtures still validate.
- OpenAPI: spot-check with `swagger-cli validate schema/crossguard-api.yaml` or an equivalent if available.

## Phase 3 — Whitepaper and Threat Model rewrite

The two largest narrative documents. They lean on the Phase 2 wire-format rewrite, so do them after Phase 2.

### Files

- `public/help/whitepaper.html`
- `public/help/threatmodel.html`

### Whitepaper changes

- **§1 Executive Summary**: XML-only wire; four providers; mention the shared-channels framework as the boundary on the Mattermost side.
- **§5 Outbound**: replace with the shared-channels hook flow (`OnSharedChannelsSyncMsg`, `OnSharedChannelsAttachmentSyncMsg`, `OnSharedChannelsProfileImageSyncMsg`, `OnSharedChannelsPing`); `buildOutboundEnvelopes` fanout (one envelope per post + optional metadata); `publishToOutboundConn` with Epoch/Sequence stamping via `KVStore.BumpSequenceCounter`. Strike every `MessageHasBeenPosted/Updated/Deleted/Reaction*` reference.
- **§6 Inbound**: replace with `processInboundMessage` → `inboundSequencer.Admit` → `rewriteChannelIDs` → `ReceiveSharedChannelSyncMsg`. Document the single-active KV-lease elector (`server/inbound_election.go`, 45s TTL / 15s renew), the gap-deadline ticker, and the cursor checkpoint ticker. Strike `handleInboundPost/Update/Delete/Reaction`. Document the inbound auto-detect rule (first non-whitespace byte: `<` means XML, `{` means JSON; BOM stripped for XML); name the format-detection symbol as a forward reference to the JSON follow-up rather than as code that exists today.
- **§7 Data Boundary**: replace with the plugin-owned wire-types story (`server/wire/`), the typed `PostProps`/`UserProps` whitelists, the XSD as the structural contract for XML, and the OpenAPI yaml as the contract for JSON. Strike the fictional `crossguard_post|update|delete|reaction_add|reaction_remove|test` typed-envelope vocabulary; keep the "two serializations of one semantic envelope" framing, since that is the target state JSON support will deliver.
- **§8 User Identity**: replace `UsernameLookup`, the "Model 1 / Model 2 / Hybrid" framing, and the `Position=crossguard-sync` sync-user convention with the shared-channels framework's remote-user model (every received User carries a non-empty `RemoteId`).
- **§9 Connection Management**: add the team-admin and channel-admin request-mode flows (`AllowTeamAdminRequests`, `AllowChannelAdminRequests`, the DM-based interactive approval prompts, the `/api/v1/channel-request/*` endpoints, `server/request.go`, `server/channel_request.go`).
- **§10 Loop Prevention**: strike the four-mechanism story (`crossguard_relayed`, bot filter, `Position=crossguard-sync` filter, `SetDeletingFlag`). Replace with: the framework's cursors and per-user `RemoteId` provide intrinsic loop prevention; the outbound hook additionally uses `connNameForRemote(rc.RemoteId)` (`server/hooks.go:47`) to suppress re-emission to the originating remote.
- **§11 Transport Security**: extend per-provider auth tables to include Service Principal for all three Azure providers; keep NATS auth_type entries.
- **§13 State Management**: replace the KV-schema table. Strike `pm-{connName}-{remotePostID}` and `crossguard-deleting-{localPostID}`. Add `crossguard-actinb-...` (inbound lease), `crossguard-seqcur-...` (sequencer cursor), `crossguard-connreq-...` (team-admin request), `crossguard-chanconnreq-...` (channel-admin request). Spot-verify the cache table against `server/store/caching.go`.
- **§14 Concurrency Model**: strike `relaySem` (256) and the file-transfer semaphore (32). Add: single-active inbound elector; sequencer dedup/reorder/gap-audit; deliberate serial inbound handler. Add Epoch/Sequence stamping on the outbound side.
- **§15 Monitoring**: replace the literal-log-message table with a pointer to `server/errcode/codes.go` and `error-codes.html`.
- **§17 CDS Deployment Checklist**: strike "Disable `UsernameLookup`" (step 2) and the `Position='crossguard-sync'` audit (step 9). Add: enabling `AllowChannelAdminRequests` if channel-admins should request links; verify the Epoch/Sequence audit codes are surfacing in operator log aggregation.

### Threat Model changes

- **§3 Data Boundary**: rewrite envelope/payload tables to match the new `transport-interface.html`.
- **§5 Relay Loop Prevention**: rewrite around the framework-plus-`connNameForRemote` mechanism. Strike the four legacy mechanisms.
- **T-SPOOF-1**: replace `resolveInboundUser`/`UsernameLookup`/`sync_user.go` discussion with the shared-channels remote-user model. Discuss the trust placed in the framework's `RemoteId`-stamped User upserts.
- **T-DOS-1**: strike the fictional 256-slot `relaySem`. Replace with the deliberately serial inbound handler (`server/inbound.go`) and the back-pressure the active receiver applies by not advancing the provider cursor on failure.
- **T-DOS-2**: strike the fictional NATS exponential-backoff. Replace with the framework's hook-return retry semantics (a failed `OnSharedChannelsSyncMsg` leaves the cursor unadvanced, so the framework retries).
- **Provider tables**: add rows for Azure Blob and Azure Service Bus; extend auth rows to include Service Principal.
- **New STRIDE coverage**: add rows or matrix updates for:
  - Epoch/Sequence (anti-tamper, anti-replay, anti-reorder).
  - Single-active receiver lease (anti-split-brain, repudiation of duplicate processing).
  - One-post-per-envelope (information-disclosure blast radius for compliance content inspection).
  - Numeric error codes (`server/errcode/codes.go`) as the operator-facing audit contract.
- **Deployment recommendations**: strike "Disable `UsernameLookup`"; strike the sync-user audit step; add `AllowChannelAdminRequests` to the request-mode discussion.
- **Section 1 / sidebar / matrix consistency**: walk the matrix at the end and confirm each cross-reference still resolves to a body row.

### Validation

- Every named function, file, constant, KV key, log code, hook, setting in either document must grep-hit in `server/`.
- Read each document end-to-end in browser view to catch broken intra-doc anchors.

## Phase 4 — Help Guide pages

User-facing, smaller individual rewrites.

### Files

- `public/help/help.html`
- `public/help/admin.html`
- `public/help/error-codes.html`
- (`commands.html` already accurate, no change)

### Changes

- **`help.html`**:
  - Overview prose (L42-46) and federation diagram (L62-73): four providers (NATS, Azure Queue, Azure Blob, Azure Service Bus); shared-channels framework, not post-lifecycle hooks.
  - "What Gets Relayed" table (L91-106): re-anchor on SyncMsg components, not per-event hooks. Strike the `crossguard_relayed` and delete-flag loop-prevention text.
  - Strike the **Username Lookup** section (L145-150) entirely.
  - Add an "Inbound" subsection that mentions the single-active elector and the sequencer at the same level of detail as the outbound discussion.
  - Update the "16 REST endpoints" claim to the current count (23 at audit time; re-count when editing).
- **`admin.html`**:
  - Provider Selection intro (L77-80) and provider table (L81-105): four providers; validation list `nats | azure-queue | azure-blob | azure-servicebus`.
  - Strike the `UsernameLookup` section (L541-560) entirely.
  - Testing Connections section (L731-736): add Azure Blob and Azure Service Bus test paths.
  - Troubleshooting (L869-940): add Azure Service Bus entries (DLQ behavior, redelivery, blob sidecar).
  - Add an Azure Service Bus connection example JSON alongside the existing NATS / Azure Queue / Azure Blob examples.
- **`error-codes.html` (regenerated by new tooling)**:
  - **New generator at `scripts/generate-error-codes/main.go`.** Standalone Go program (own `main` package, separate from the plugin server binary). Parses `server/errcode/codes.go` with `go/parser`/`go/ast` to extract:
    - The file-block comment immediately above each `const ( ... )` group (e.g. `// hooks.go (10000-10999)`) — used as the block header and to derive the 1000-range.
    - Each `ConstSpec` inside the group: constant name, integer value, and the immediately-preceding GoDoc-style comment (if any) as the per-code description.
    - The `AllCodes` slice at the bottom of the file is consumed only to assert that every parsed constant appears in `AllCodes` — drift detected here is a generator error, not a silent omission.
  - **Sidecar annotations file at `scripts/generate-error-codes/annotations.yaml`.** Top-level map keyed by constant name. Each value may carry `description:` (used when the Go file has no doc comment) and `troubleshooting:` (operator-facing remediation steps). When both a Go doc comment and a YAML `description` exist, the YAML wins (so doc edits can be made without touching code).
  - **HTML template** lives next to the generator (`scripts/generate-error-codes/error-codes.html.tmpl`, Go `html/template` syntax). Matches the existing `public/help/*.html` look: same `<aside class="sidebar">`, same `breadcrumb`, same range-quick-reference table at the top, same per-file blocks as `<section>` elements with a heading carrying the range, then a table of `code | name | description | troubleshooting`. Page nav anchor IDs are derived from the constant name (lowercased, hyphenated) so deep-links from other docs are stable.
  - **Output:** `public/help/error-codes.html`, overwritten in place.
  - **Makefile wiring:** add a phony target `generate-error-codes` (analogous to the existing `generate-pdfs`) that runs `go run ./scripts/generate-error-codes`. Document it in the README's "Common Commands" table.
  - **Test:** add `scripts/generate-error-codes/main_test.go` covering: (a) every constant in `errcode.AllCodes` appears in the rendered output exactly once, (b) every integer value is unique, (c) every annotation key in the YAML matches a real constant.
  - **Drift-free contract going forward:** every PR that adds an error code must rerun `make generate-error-codes`. CI can later add a guard that fails if the regenerated file differs from the committed one; out of scope for this plan but easy to add.
  - **Content the generated page must show** (sanity targets, since the prior hand-maintained page got these wrong):
    - The real `hooks.go` 10000-range block: `OutboundSyncMsg*`/`OutboundAttachment*`/`OutboundProfileImage*`/`Ping*` (`codes.go:105-151`). The fabricated `HooksChannelConnCheckFailed` family must be gone.
    - The `inbound.go` 15000-range block including `InboundActive*` (15200-range), `InboundSeq*` (15300-range), and checkpoint codes (15400-range).
    - The `plugin.go` (27000-range), `inbound_files.go` (28000-range), and `wire/*` (29000-range) blocks (`codes.go:355-401`).
    - No `retry_dispatch.go` block (file removed).
    - No `sync_user.go` block (file removed).
    - A Range Quick Reference table covering every block the generator actually emitted, with the highest range matching the file's actual ceiling.

### Validation

- Every error code name in `error-codes.html` must exist in `server/errcode/codes.go`; every code in `codes.go` must appear in the page.
- Every endpoint in `api.html` registers in `server/api.go`; every endpoint in `api.go` appears in `api.html`.
- Every command in `commands.html` (no edits, just confirm) matches `server/command.go`.

## Phase 5 — Regenerate PDFs and final pass

### Steps

- Run `make generate-error-codes`. This refreshes `public/help/error-codes.html` from `server/errcode/codes.go` and `scripts/generate-error-codes/annotations.yaml`. (Doing this at the start of Phase 5 catches any code-level error-code drift that landed during the Phase 1-4 PR cycle.)
- Run `make generate-pdfs`. This invokes `scripts/generate-pdfs.js` to render the four HTML pages to PDF via Playwright/Chromium.
- Confirm the four PDFs (`crossguard-whitepaper.pdf`, `crossguard-threatmodel.pdf`, `crossguard-transport-interface.pdf`, `crossguard-help.pdf`) regenerated cleanly. Spot-read page-by-page in a viewer for layout artifacts (overflow, broken anchors, missing fonts).
- Final pass on `README.md`: re-verify each linked URL still resolves to an extant file and section after the rewrites.

### Validation

- PDFs open in a viewer.
- Every link in the README opens.
- Every internal `#anchor` link in the Help Guide navigation resolves.
- `make check-style` and `make test` still pass (no code paths touched, but worth confirming).

## Out of scope but tracked as immediate follow-ups

- **JSON wire-format implementation.** The committed work after this doc plan. Implements:
  - `json:"..."` tags added alongside the existing `xml:"..."` tags on `server/transport.go` `TransportEnvelope` and on every struct under `server/wire/`. JSON field names mirror the XML element / attribute names exactly (`ConnName`, `Timestamp`, etc.), so a single struct serializes to either format with no field renames.
  - A JSON encoder branch in `MarshalEnvelope` selected by a new `MessageFormat` field on `ConnectionConfig` (read from the existing `message_format` JSON key the frontend already saves).
  - Inbound auto-detection in `UnmarshalEnvelope`: peek the first non-whitespace byte (`<` → `xml.Unmarshal`, `{` → `json.Unmarshal`), strip a leading UTF-8 BOM for the XML path.
  - Wire-up of the frontend selector (it already collects the value, just needs to be persisted into the new `MessageFormat` field).
  - Validation: round-trip tests in both formats; integration tests with `CROSSGUARD_WIRE_VALIDATE=1` for XML and a JSON Schema equivalent for JSON.
- **CI guard on `error-codes.html`.** The generator added in Phase 4 makes the page reproducible from `codes.go`, but nothing currently prevents a PR from forgetting to rerun it. A small CI job that runs `make generate-error-codes` and diffs the working tree would close the loop. Out of scope here.
- **Historical plan-document drift.** Several documents under `implementation-plans/` reference functions and KV keys that no longer exist (e.g. the original post-mapping plans). They are intentionally not in scope here, but worth a sweep at some point.

## Effort estimate

- Phase 1: roughly half a day.
- Phase 2: one to one-and-a-half days.
- Phase 3: two to three days. Whitepaper alone is the biggest single piece.
- Phase 4: one day.
- Phase 5: an hour.

Total roughly five to seven working days for a single engineer doing the rewrites; less if Phase 3 is parallelized across two people (whitepaper vs threat model).
