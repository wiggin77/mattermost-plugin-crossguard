# Azure Service Principal Authentication

## Overview

Add Azure AD **Service Principal (Client Secret)** authentication as an alternative to shared keys / SAS connection strings for all three Azure transport providers (Queue Storage, Blob Storage, Service Bus). Selection is per-connection via a new `auth_mode` field. Phase 1 covers Client Secret only; certificate auth, Managed Identity, and federated/workload identity are tracked as later phases.

## Problem Statement

- Federal and regulated customers cannot use long-lived storage account keys: their compliance baseline requires Azure AD identities backed by central rotation and RBAC.
- Shared keys grant full account access; Service Principal + RBAC enables least-privilege per queue/container/namespace.
- Service Bus SAS connection strings embed access keys whose rotation requires re-saving plugin config (a config-mgmt anti-pattern).
- Azure Government and other sovereign clouds require explicit AAD authority configuration that the current code does not support; adding SP auth surfaces this gap and must close it.

## Phase Strategy

| Phase | Focus | Status |
|-------|-------|--------|
| **Phase 1 (THIS PLAN)** | Client Secret SP for all 3 providers; Azure cloud env config; centralized secret-error sanitization; non-destructive test-connection; sentinel round-trip for stored secrets | In scope |
| **Phase 1B (follow-up)** | Refactor secret storage so `GET /api/v4/config` returns refs (not values) for ALL secrets (account_key, connection_string, client_secret) | Deferred |
| Phase 2 | Client Certificate auth (`azidentity.NewClientCertificateCredential`); independent SP for SB blob sidecar | Deferred |
| Phase 3 | Managed Identity | Deferred |
| Phase 4 | Federated/Workload Identity | Deferred |

### Phase 1 deployment guidance (documented in admin.html)

Phase 1 is **acceptable for development, staging, and production environments paired with strict secret-rotation policy** (90-day max). It is **not the recommended posture** for high-side ATO environments where Phase 2 (cert auth via Azure Key Vault + CSI driver) should be used. Phase 1 introduces `client_secret_env` / `client_secret_file` indirection so customers using Azure Key Vault / CSI volume mounts can avoid storing the secret in plugin config at all.

## Current State

Three Azure providers, each constructing SDK clients from shared keys / connection strings:

- `server/azure_provider.go:73-77` — Azure Queue Storage with `azqueue.NewSharedKeyCredential` + `NewQueueClientWithSharedKeyCredential`. Optional blob sidecar at `:96-100`.
- `server/azure_blob_provider.go:304-308` — Azure Blob Storage with `container.NewSharedKeyCredential` + `NewClientWithSharedKeyCredential`.
- `server/azure_servicebus_provider.go:116` — Azure Service Bus with `azservicebus.NewClientFromConnectionString`. Optional blob sidecar at `:138-145` uses independent shared key.

Config structs in `server/configuration.go:82-126`. Validators at `:361` (Queue), `:427` (Blob), `:490` (Service Bus). Redaction in `server/service.go:910-938`. Test-connection seams in `server/api.go:23-25`.

`azcore v1.20.0` and `azidentity v1.13.1` are already in `go.sum` as transitive dependencies. Promoting to direct requires is one-line `go.mod` change.

### Current Gaps

- Shared-key/SAS only; no AAD identity support.
- No Azure Government / sovereign-cloud configuration; SDK defaults to commercial AAD/storage endpoints.
- `sanitizeServiceBusError` (azure_servicebus_provider.go:90-113) is the only secret-scrubbing helper; Queue and Blob log raw `err.Error()`.
- Provider constructors unconditionally call control-plane `Create` on queues/containers (azure_provider.go:85, :109; azure_blob_provider.go:339; azure_servicebus_provider.go:157). Works for shared-key (full control plane) but fails 403 under SP with data-plane-only RBAC.
- `testAzure*Connection` paths (api.go:265, :315, :355) use destructive operations (Create + Enqueue + Delete for queue; Create + upload/delete for blob; Peek for SB only) — SP with least-privilege RBAC fails these even when runtime would work.
- No save-time credential probe; bad creds surface only on first message flow.
- Webapp `ConnectionSettings.tsx` hydrates `type='password'` fields from cleartext config values returned by the System Console, so existing secrets are already exposed to admin browsers on page load. This plan adds a sentinel round-trip for the SAVE path (server-side merge); a full READ-path fix is deferred to Phase 1B.

## Design Principles

| Pattern | Our Approach | Avoid | Reference |
|---------|-------------|--------|-----------|
| Auth discriminator | `auth_mode` string field per provider, mirroring NATS `auth_type` | Boolean flags, sum types, implicit detection (e.g., "tenant_id != ''") | NATS `AuthType` constants at configuration.go:23-25, validation switch at :332-337 |
| Field shape | **Flat** fields on each provider config (`tenant_id`, `client_id`, `client_secret`, `azure_cloud`, etc.) | Nested `service_principal` sub-struct | NATS pattern at configuration.go:67-79 (auth_type/token/username/password all flat) |
| Credential construction | Inline `azidentity.NewClientSecretCredential(...)` at each provider's auth-mode switch arm | Helper file with `buildAzureTokenCredential` | Existing shared-key constructors are also inlined six times across providers; matches established style |
| Exclusive validation | Per-mode switch rejects BOTH missing-required fields AND populated-from-other-mode fields | "Switch on mode; ignore the other mode's fields" | new — closes a silent-leftover-secret trap |
| Error sanitization | Centralized `sanitizeAzureError` used by all three providers, test-connection paths, and the new save-time probe | Per-provider regexes | Promote existing servicebus regex set (azure_servicebus_provider.go:94-113) |
| Auto-create resources | Skip in SP mode; pre-provisioned resources are required. Distinguish `AuthorizationPermissionMismatch` from `AlreadyExists` in shared-key mode too. | Always call Create | Least-privilege Federal RBAC posture |
| Test-connection probes | Non-destructive only (GetProperties / ReceiveAndPeek / list-with-zero-results) for BOTH legacy and SP modes | Create + Enqueue + Delete | api.go:265, :315, :355 currently destructive |
| Cloud environment | Per-provider `azure_cloud` field (`public`/`usgov`/`china`); propagated to BOTH `azidentity` options and SDK client options | Hardcoded commercial endpoints | new |
| Secret indirection | Optional `client_secret_env` / `client_secret_file`; resolved at provider construction; mutually exclusive with inline `client_secret` | Sole reliance on inline secret | Federal Key Vault + CSI driver workflow |
| Service Bus blob sidecar | When SB is in SP mode, sidecar inherits parent SP credential; `blob_account_name`/`blob_account_key` must be empty. Independent SP for sidecar is Phase 2. | "Sometimes inherit, sometimes not" silent rules | Documents the decision explicitly |
| Token credential lifecycle | Pipe plugin-lifetime ctx into all three provider constructors so credential goroutines die on reload | Untied background refresh | azure_blob_provider.go already takes ctx (:303); azure_provider/azure_servicebus_provider must be updated |
| Secret hydration (SAVE path) | OnConfigurationChange merges: if inbound secret == `"********"` sentinel, preserve stored value; if empty when stored is non-empty, also preserve | Always overwrite on save | new — fixes "accidentally clear secret on form save" + future-proofs Phase 1B |
| Audit logging | At provider construction, log `auth_mode`, `tenant_id`, `client_id`, and SHA-256(secret) (NOT the secret itself) for incident-window reconstruction | No record of which credential authenticated | new |

## Reference Patterns

- NATS auth_type dropdown: `server/configuration.go:23-25, 72, 332-337` (string discriminator + switch validation + default to legacy)
- Conditional field validation: `server/configuration.go:514-529` (file-transfer-enabled fields under SB)
- Redaction strips secrets, surfaces safe fields: `server/service.go:910-938`
- Servicebus error sanitization regexes: `server/azure_servicebus_provider.go:94-113` (template to extend and lift)
- Test-connection injection seam: `server/api.go:23-25` (function-var pattern for mocking in tests)
- Errcode file-block convention: `server/errcode/codes.go` lines 175-202 (azure-blob 18000-18999), 228-237 (azure-queue 19000-19999), 325-334 (servicebus 26000-26999)

## Requirements

### Authentication and credential handling
- [ ] Add `auth_mode` string field to all three Azure provider config structs
- [ ] Define constants `AzureAuthSharedKey = "shared-key"`, `AzureAuthConnectionString = "connection-string"` (Service Bus only), `AzureAuthServicePrincipal = "service-principal"`
- [ ] Add flat SP fields: `tenant_id`, `client_id`, `client_secret`, `client_secret_env` (optional), `client_secret_file` (optional)
- [ ] Add `azure_cloud` field per provider (`public` / `usgov` / `china`, default `public`)
- [ ] Add `service_bus_namespace` field (Service Bus only; required in SP mode since SAS connection string used to provide it)
- [ ] Promote `azidentity` and `azcore` from indirect to direct in `go.mod`

### Validation
- [ ] Per-mode validation switch with **exclusive** semantics: reject empty required-fields for the active mode AND reject populated fields from the inactive mode
- [ ] Normalize `auth_mode` value: trim whitespace, lowercase, then exact-match against constants. Empty → defaults to legacy mode for that provider (`shared-key` for Queue/Blob; `connection-string` for Service Bus)
- [ ] Validate `tenant_id` is either a GUID (`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`) or an FQDN-shaped string (contains a dot, no scheme, no path, e.g. `contoso.onmicrosoft.com`). `azidentity.NewClientSecretCredential` accepts both forms; GUID-only rejects legitimate domain-style tenant identifiers.
- [ ] Validate `azure_cloud` value is in the allowed set
- [ ] Validate exactly one of `client_secret`, `client_secret_env`, `client_secret_file` is set in SP mode
- [ ] For Service Bus in SP mode: validate `service_bus_namespace` looks like an FQDN (no `sb://` scheme — strip if user typed it; reject if not strippable to a valid host)
- [ ] For Service Bus in SP mode with file transfer enabled: `blob_account_name` and `blob_account_key` MUST be empty (sidecar inherits parent SP); `blob_service_url` and `blob_container_name` still required
- [ ] Save-time credential probe: call `cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: <data-plane scope per provider>})` with a 10s timeout, route errors through `sanitizeAzureError`, fail the save with a clear sanitized message. **Required scope strings (verified against pinned SDKs):**
  - Queue: `https://storage.azure.com/.default` (single scope; covers all storage data planes for the configured cloud)
  - Blob: `https://storage.azure.com/.default`
  - Service Bus: `https://servicebus.azure.net//.default` (note the SDK's intentional double-slash convention in `azservicebus/internal/sbauth/token_provider.go`)
  - For sovereign clouds, the SDK's `azcore/cloud.Configuration.Services` resolves these automatically when `Cloud` is set on the credential options; no per-cloud scope override needed.
- [ ] **Probe failure must block the save**: today `OnConfigurationChange` (`server/configuration.go:585`) logs `cfg.validate()` errors as warnings and **still applies the new config**. Plan must change `OnConfigurationChange` to return an `error` for probe failures so Mattermost rejects the save; do NOT only update `validate()` — the hook itself must propagate.

### Provider construction
- [ ] Each constructor switches on `auth_mode` and uses the SDK's non-shared-key constructors when in SP mode:
  - Queue: `azqueue.NewQueueClient(queueURL, tokenCred, &azqueue.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: <cfg cloud>}})`
  - Blob: `container.NewClient(containerURL, tokenCred, &container.ClientOptions{ClientOptions: azcore.ClientOptions{Cloud: <cfg cloud>}})`
  - Service Bus: `azservicebus.NewClient(namespace, tokenCred, nil)` — **NOTE**: `azservicebus.ClientOptions` (v1.10.0, `client.go:43-61`) does NOT embed `azcore.ClientOptions` and has no `Cloud` field. Service Bus routing is determined by the fully-qualified namespace FQDN itself (`*.servicebus.windows.net` for public, `*.servicebus.usgovcloudapi.net` for usgov, etc.); only the credential needs the `Cloud` option.
- [ ] Pass `Cloud` option to `azidentity.NewClientSecretCredential` (via `ClientSecretCredentialOptions.ClientOptions.Cloud`) for **all three** providers. Additionally, pass `Cloud` to the data-plane SDK client options for **Queue and Blob** (their `ClientOptions` types DO embed `azcore.ClientOptions`). For **Service Bus**, the credential option is sufficient.
- [ ] In SP mode, **skip** the `Create` calls on queues/containers (azure_provider.go:85, :109; azure_blob_provider.go:339; azure_servicebus_provider.go:157) — require pre-provisioned resources. In shared-key mode, the existing behavior is preserved
- [ ] **Missing-resource error handling after skipping auto-Create**: when the first runtime Enqueue/Upload/Send hits a non-existent queue/container, the SDK returns a generic 404. Wrap and surface as a sanitized actionable message: `"resource <name> not found; pre-provision via Azure portal or `az` CLI — auto-create is disabled in service-principal mode for least-privilege RBAC"`. Apply at all first-use call sites in each provider.
- [ ] In shared-key mode, distinguish `AuthorizationPermissionMismatch` from `AlreadyExists` and log distinct messages with separate error codes so least-privilege misconfiguration is actionable
- [ ] Pipe a plugin-lifetime `context.Context` (`p.ctx`, already set up at `server/plugin.go:94` and threaded through `connections.go:395` for the blob provider) into `newAzureProvider` and `newAzureServiceBusProvider`. **Goroutine model**: `azidentity` credentials have NO background goroutines; token refresh happens inline on `GetToken` calls. The ctx is therefore used only for `GetToken` cancellation, not for tearing down a refresh worker. Plugin-lifetime is the correct scope; no derived-ctx machinery is needed.
- [ ] Resolve `client_secret_env` / `client_secret_file` at construction time (not at save time, so rotation works without DB writes); reject if the referenced env var is unset or the file is unreadable

### Test-connection (api.go testAzure*Connection paths)
- [ ] Refactor all three to use **non-destructive** probes that work under data-plane-only RBAC:
  - Queue: `queueClient.GetProperties` (replaces Create + Enqueue + Delete at azure_provider.go:319+)
  - Blob: `containerClient.GetProperties` (replaces Create + upload/delete at azure_blob_provider.go:1459+)
  - Service Bus: keep existing `PeekMessages` (already non-destructive at azure_servicebus_provider.go:449-488), but route errors through the new shared sanitizer
- [ ] **Rewrite required-field gates in `handleTestAzureQueueConnection` / `handleTestAzureBlobConnection` / `handleTestAzureServiceBusConnection`** (api.go around :265, :292-300, :337-340) to switch on `auth_mode` BEFORE invoking the probe function. Today these handlers hardcode `account_key` / `connection_string` as required and return HTTP 400 before the inner `testAzure*ConnectionFn` runs — so SP-mode configs would be rejected at the HTTP layer no matter what the probe does. The plan's `server/api.go` row in Files to Modify covers this; the requirement is called out explicitly here so the implementer does not miss the handler-level gates.
- [ ] Each test-connection path acquires its credential via the same auth_mode switch as runtime construction; covers both modes
- [ ] All test-connection errors flow through `sanitizeAzureError` before reaching the API response or logs

### Centralized error sanitization
- [ ] Promote `sanitizeServiceBusError` to a shared `sanitizeAzureError` (likely a new `server/azure_errors.go` file or move into an existing shared helper)
- [ ] Scrub patterns: `SharedAccessKey=...`, `SharedAccessSignature=...`, `[?&]sig=...`, `client_secret=...` (URL-form encoded), `assertion=...`, `(?i)bearer\s+[A-Za-z0-9._\-]+`, `"access_token"\s*:\s*"[^"]*"`, `"refresh_token"\s*:\s*"[^"]*"`
- [ ] Wire all three providers' constructor error returns and runtime log lines through `sanitizeAzureError`. Auditable list:
  - azure_provider.go: lines 75, 79, 88, 98, 102, 111, 141, 177, 204, 213, 221, 248, 270, 281, 288, 295, 322, 326, 335, 342
  - azure_blob_provider.go: lines 306, 310, 339, 342, 351, 354, 453, 471, 481, 533, 541, 622, 632, 638, 662, 666, 685, 695, 714, 726, 821, 869, 882, 893, 905, 925, 941
  - api.go test-connection sites: 265, 315, 355
- [ ] Unit test asserting a fake `*url.Error` wrapping a request with `client_secret=hunter2` in the body produces output containing `client_secret=REDACTED` and not `hunter2`

### Redaction (service.go:redactConnection)
- [ ] Surface (safe to expose): `auth_mode`, `tenant_id`, `client_id`, `azure_cloud`, `service_bus_namespace` (Service Bus only)
- [ ] Never expose: `client_secret`, `client_secret_env` and `client_secret_file` (operationally sensitive even if not the secret itself — could hint at filesystem layout)
- [ ] Add unit test that marshals a redacted connection populated with SP fields and asserts the secret value does not appear in JSON output

### Secret hydration sentinel (SAVE path; READ path deferred to Phase 1B)
- [ ] In `OnConfigurationChange`, merge inbound connection list with stored list using these rules per secret field (`client_secret`, `account_key`, `connection_string`, `blob_account_key`):
  - Inbound value == `"********"` sentinel → preserve stored value
  - Inbound value empty AND stored value non-empty → preserve stored value (so accidental form save does not clear secret)
  - Inbound value non-empty AND != sentinel → accept inbound (real edit)
- [ ] **Data source for the merge**: `p.API.LoadPluginConfiguration(cfg)` returns only the inbound NEW config. The OLD config is available as `p.getConfiguration()` (returns the currently-active `*configuration`) BEFORE the new one is committed via `p.setConfiguration(cfg)`. Read OLD via `p.getConfiguration()`, parse its `InboundConnections`/`OutboundConnections` JSON strings, build a name-keyed map of stored secrets, then merge into the inbound parsed connections before validation runs. See `server/configuration.go:559` (`getConfiguration`) and `:598` (`setConfiguration`).
- [ ] Webapp ConnectionSettings.tsx: on form load, display empty string for password fields when stored value is non-empty (no "********" display — placeholder text only). On save, send `"********"` sentinel for unchanged fields and the new value for edited ones
- [ ] Document in admin.html that `GET /api/v4/config` still returns cleartext (Phase 1B follow-up); recommend env-var/file indirection for ATO environments

### Audit logging
- [ ] At provider construction (after a successful credential build), `LogInfo` with `auth_mode`, `tenant_id`, `client_id`, `azure_cloud`, and `SHA-256(secret)[:12]` (hex prefix only, never the full hash to make brute-force harder). NOT for shared-key mode (account_name suffices)
- [ ] New error code per provider for failed SP probe / construction

### Webapp (ConnectionSettings.tsx)
- [ ] Add `auth_mode` dropdown to each of three Azure forms (Queue, Blob, Service Bus)
- [ ] Conditional rendering: shared-key fields visible only in `shared-key` mode; SP fields visible only in `service-principal` mode; for Service Bus, `connection-string` mode shows the legacy field, `service-principal` mode shows `service_bus_namespace` + SP fields
- [ ] Add `azure_cloud` dropdown (Public / US Government / China)
- [ ] Add fields: `tenant_id`, `client_id`, `client_secret` (type='password'), `client_secret_env`, `client_secret_file`
- [ ] Client-side validation: required fields per mode; mutually-exclusive `client_secret*` triple; GUID format for `tenant_id`
- [ ] Form-load behavior for secret fields: empty when stored value is non-empty (placeholder "(saved)"); send `"********"` sentinel on save if unchanged

### Documentation
- [ ] `public/help/admin.html`: new SP auth section per provider with RBAC role guidance (Storage Queue Data Contributor / Storage Blob Data Contributor / Azure Service Bus Data Sender + Receiver; explicit note that Owner is NOT required because Phase 1 skips auto-create)
- [ ] Document Azure Government cloud configuration with the `azure_cloud` field
- [ ] Document the `client_secret_env` / `client_secret_file` indirection options with a Key Vault + CSI volume example
- [ ] Document the Phase 1 deployment-environment guidance (acceptable for dev/staging/regulated-with-rotation; cert auth recommended for high-side)
- [ ] Document that `GET /api/v4/config` still returns cleartext today and that Phase 1B will address this
- [ ] Update `schema/crossguard-api.yaml` with new fields
- [ ] Regenerate PDFs via `scripts/generate-pdfs.js`

### Testing infrastructure
- [ ] Go unit tests for validation: each provider × each `auth_mode` value × valid / missing-required / populated-other-mode combinations
- [ ] Go unit tests for sanitizer: enumerated leak patterns each scrubbed
- [ ] Go unit tests for redaction: secret fields absent from JSON output
- [ ] Go unit tests for sentinel merge: empty inbound preserves stored; sentinel preserves stored; new value overwrites
- [ ] Go unit tests for constructors: inject stub `azcore.TokenCredential` (it is already an interface — no mocking framework needed) and assert the correct SDK constructor was used
- [ ] Webapp Playwright component tests: dropdown switches modes, conditional fields show/hide, validation surfaces errors
- [ ] Manual smoke against real Azure SP (gated; not CI): one queue + one blob + one Service Bus namespace per cloud (public + government)

## Out of Scope

- Client certificate authentication (Phase 2)
- Managed Identity / Workload Identity (Phase 3, 4)
- Independent SP credential for Service Bus blob sidecar (Phase 2)
- Refactoring `GET /api/v4/config` to return secret refs instead of cleartext (Phase 1B)
- Credential rotation alerting / expiry monitoring
- FIPS 140-2 mode handling (Go runtime concern, not plugin)
- Caching/sharing the token credential across connections (each connection owns its credential; SDK token cache is internal)
- Cross-tenant credential validation (azidentity surfaces AADSTS errors clearly — sanitizer handles the rest)

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| Discriminator field name | `auth_mode` (string, kebab-case values, exact-match after trim+ToLower) | Mirrors NATS `auth_type` precedent; future-proof for cert/MI as new constant values |
| SP field shape | **Flat** fields per provider config | Matches NATS auth pattern (`auth_type` + flat `token`/`username`/`password`); simpler webapp form |
| Credential helper function | **No helper**; inline at each constructor's switch arm | Codebase already inlines shared-key construction six times; SDK call is one line |
| Default auth_mode when missing | Legacy mode for each provider (`shared-key` for Queue/Blob; `connection-string` for Service Bus) | Zero-config upgrade for existing connections |
| Coexisting legacy + SP fields | **Reject at validation** (exclusive semantics) | Prevents silent stale-secret retention; forces clean migration |
| Auto-create queues/containers | **Skip in SP mode**; require pre-provisioned resources | Data-plane-only RBAC is the Federal default; Owner-level RBAC just for create is over-privileged |
| Test-connection probes | **Non-destructive** in BOTH modes | Required for SP-data-plane-only; benefits shared-key mode too |
| Service Bus blob sidecar in SP mode | **Inherit parent SP**; `blob_account_name`/`blob_account_key` must be empty | Most-common deployment; mixed-mode deferred to Phase 2 |
| Azure cloud environment | **Per-provider `azure_cloud` field**; passed to credential options for all three providers and to data-plane client options for Queue + Blob only (Service Bus SDK has no `Cloud` option; uses FQDN namespace) | Different connections may target different clouds (dev cluster on public, prod on usgov) |
| Save-time probe scopes | Queue/Blob = `https://storage.azure.com/.default`; Service Bus = `https://servicebus.azure.net//.default` (double-slash is the SDK convention) | Verified against pinned SDK source; sovereign-cloud variants resolved automatically when `Cloud` is set on credential |
| Probe failure handling | `OnConfigurationChange` must return an `error` (not just log warning) when probe fails | Today's hook applies config even on validation failure (configuration.go:585); the probe gate has to live at the hook level |
| `tenant_id` validation | Accept GUID **or** FQDN-shaped string | `azidentity` accepts both; GUID-only rejects domain-style identifiers like `contoso.onmicrosoft.com` |
| Sentinel-merge data source | Read OLD config via `p.getConfiguration()` before `setConfiguration(new)`; build name-keyed map of stored secrets; merge into inbound | `LoadPluginConfiguration` only returns the new config; OLD is available in-memory until commit |
| Secret hydration | Sentinel `"********"` round-trip on SAVE path (server merges); empty stays empty | Closes accidental-clear bug; partial fix for cleartext-on-load (full fix in Phase 1B) |
| Inline-secret alternatives | `client_secret_env` and `client_secret_file` (mutually exclusive with inline and each other) | Federal Key Vault / CSI volume pattern without architecture overhaul |
| Save-time credential probe | **Yes** — `GetToken` with 10s timeout, sanitized errors | Catches revoked/typo'd creds at save instead of mid-relay log spam |
| Error sanitization scope | Centralized helper `sanitizeAzureError`, applied to all three providers | Existing per-SB scope is insufficient when SP errors flow through Queue/Blob paths |
| Token credential lifecycle | Plugin-lifetime ctx (`p.ctx`) threaded into all three provider constructors | `azidentity` credentials have NO background goroutines (refresh is inline in `GetToken`); ctx is used only for `GetToken` cancellation. Plugin-lifetime scope is sufficient; no derived-ctx machinery needed |
| `azidentity` dependency | Promote from transitive (`go.sum` already has v1.13.1) to direct in `go.mod` | One-line `require` change; no new dependency surface |
| Error codes | New per-provider codes following the file-block convention | Matches CLAUDE.md errcode policy |
| Stub `TokenCredential` for tests | Use `azcore.TokenCredential` interface directly with a tiny test struct | No mocking framework; ~5 LOC test double |

## Files to Modify

| File | Change |
|------|--------|
| `go.mod` | Promote `azidentity` and `azcore` to direct requires |
| `server/configuration.go` | Add `auth_mode` constants; add SP/cloud/namespace flat fields to each Azure config; per-mode exclusive validation; GUID/FQDN format checks |
| `server/azure_errors.go` (new) | Lift `sanitizeServiceBusError` to a package-wide `sanitizeAzureError`; expand regex set |
| `server/azure_provider.go` | Auth-mode switch in `newAzureProvider`; skip Create in SP mode; thread ctx through; route errors through `sanitizeAzureError`; refactor `testAzureQueueConnection` to non-destructive |
| `server/azure_blob_provider.go` | Same patterns; refactor `testAzureBlobConnection` to non-destructive; route errors through sanitizer |
| `server/azure_servicebus_provider.go` | Auth-mode switch in `newAzureServiceBusProvider`; SP credential inheritance for blob sidecar; thread ctx; route errors through sanitizer; remove now-redundant local `sanitizeServiceBusError` (delegates to shared) |
| `server/api.go` | Test-connection paths exercise both auth modes; sanitized error responses |
| `server/service.go` | `redactConnection` includes `auth_mode`, `tenant_id`, `client_id`, `azure_cloud`, `service_bus_namespace`; NEVER `client_secret*` |
| `server/service.go` or `server/configuration.go` | `OnConfigurationChange` sentinel merge for secret fields |
| `server/errcode/codes.go` | New error codes (see allocation below) + AllCodes entries |
| `webapp/src/components/ConnectionSettings.tsx` | Auth-mode dropdown, cloud dropdown, conditional fields, GUID validation, sentinel display behavior |
| `webapp/src/components/ConnectionSettings.pw.tsx` | Component tests per provider × per auth mode |
| `public/help/admin.html` | SP setup section per provider, RBAC guidance, cloud env, env/file indirection, Phase 1 deployment guidance |
| `schema/crossguard-api.yaml` | New fields in OpenAPI schema |
| `scripts/generate-pdfs.js` invocation | Regenerate after admin.html update |
| (smoke test) `Makefile` | Optional: add `make docker-azure-sp-smoke-test` target for an SP-auth smoke against a real Azure SP (gated, not CI) |

### Error code allocation

Following the file-block convention in `server/errcode/codes.go`:

- Azure Queue (block 19000-19999):
  - `AzureQueueSPCredentialFailed = 19010`
  - `AzureQueueSPProbeFailed = 19011`
  - `AzureQueueAuthzMismatchOnCreate = 19012` (distinguish 403 from 409 in shared-key auto-create path)
- Azure Blob (block 18000-18999):
  - `AzureBlobSPCredentialFailed = 18040`
  - `AzureBlobSPProbeFailed = 18041`
  - `AzureBlobAuthzMismatchOnCreate = 18042`
- Service Bus (block 26000-26999):
  - `ServiceBusSPCredentialFailed = 26007`
  - `ServiceBusSPProbeFailed = 26008`
- Configuration (block 10000-10999 already covers config; check next free number for):
  - `ConfigAzureAuthModeUnknown` (whichever next slot is free)
  - `ConfigAzureSecretSourceConflict` (next slot)

## Tasks

1. [ ] Promote `azidentity` + `azcore` to direct requires in `go.mod`; `go mod tidy`; `make check-style`
2. [ ] Add `AzureAuthSharedKey`/`AzureAuthConnectionString`/`AzureAuthServicePrincipal` constants and allowed `azure_cloud` values to `configuration.go`
3. [ ] Extend each Azure config struct with flat SP fields, `auth_mode`, `azure_cloud`, and (Service Bus only) `service_bus_namespace`
4. [ ] Rewrite three `validateAzure*Connection` functions with exclusive per-mode validation, GUID/FQDN format checks, secret-source-mutual-exclusion, and `auth_mode` normalization
5. [ ] Create `server/azure_errors.go` with `sanitizeAzureError`; port and expand `sanitizeServiceBusError` regexes; replace local helper references in `azure_servicebus_provider.go`
6. [ ] Route all SDK error log lines and constructor error returns in Queue and Blob providers through `sanitizeAzureError`
7. [ ] Thread plugin-lifetime ctx into `newAzureProvider` and `newAzureServiceBusProvider`; verify Close paths cancel correctly
8. [ ] Implement secret-source resolution helper (inline / env / file) for `client_secret*` fields
9. [ ] Implement save-time `GetToken` probe with 10s timeout and the **scope strings specified above** (`https://storage.azure.com/.default` for Queue/Blob; `https://servicebus.azure.net//.default` for SB); route errors through `sanitizeAzureError`
9a. [ ] **Modify `OnConfigurationChange` to return an `error` on probe/validation failure** so Mattermost rejects the save (current hook at `configuration.go:585` logs warnings and applies anyway)
10. [ ] Update each provider constructor with auth-mode switch: SP path uses `azidentity.NewClientSecretCredential` + token-credential SDK client; skip auto-Create in SP mode; pass `Cloud` to credential options for all three; pass `Cloud` to client options for Queue + Blob only (SB has no client-options `Cloud` field — routing via FQDN namespace)
10a. [ ] **Refactor `handleTestAzureQueueConnection` / `handleTestAzureBlobConnection` / `handleTestAzureServiceBusConnection` required-field gates** (api.go around :265, :292-300, :337-340) to switch on `auth_mode` before invoking the probe function
10b. [ ] Add sanitized actionable "resource not found; pre-provision in SP mode" wrappers at first-use call sites in each provider for the post-skip-Create runtime 404 path
11. [ ] Distinguish `AuthorizationPermissionMismatch` from `AlreadyExists` in shared-key auto-create paths; use new error codes
12. [ ] Refactor all three `testAzure*Connection` paths to non-destructive probes covering both auth modes
13. [ ] Update `redactConnection` to surface safe SP fields and explicitly omit secret fields; add unit test asserting no secret in JSON output
14. [ ] Implement sentinel-merge in `OnConfigurationChange` for `client_secret`, `account_key`, `connection_string`, `blob_account_key`
15. [ ] Add audit-log `LogInfo` at successful SP construction (`auth_mode`, `tenant_id`, `client_id`, `azure_cloud`, hash prefix of secret material)
16. [ ] Allocate new error codes; add to `AllCodes` slice; run `TestCodesUnique`
17. [ ] Webapp: extend TypeScript interfaces, add auth-mode/cloud dropdowns, conditional rendering, validation, sentinel display
18. [ ] Webapp: Playwright component tests for each provider × each auth mode
19. [ ] Go unit tests: validation matrix, sanitizer matrix, sentinel-merge matrix, constructor selection (with stub TokenCredential)
20. [ ] Docs: admin.html sections per provider, RBAC guidance, cloud env, secret indirection, deployment guidance, Phase 1B note about config-API exposure
21. [ ] Update OpenAPI schema; regenerate PDFs
22. [ ] Manual smoke against real Azure SP (public + government if available); document smoke procedure

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| `auth_mode` typo (e.g., `service_principal` snake_case) falls through to legacy mode silently | Reject unknown values explicitly; only empty defaults to legacy; document constants in admin.html |
| `azservicebus.ClientOptions` has no `Cloud` field; passing it would not compile | For SB, pass `Cloud` only to `ClientSecretCredentialOptions`; routing comes from FQDN namespace (`*.servicebus.usgovcloudapi.net` etc.) |
| Save-time probe uses wrong scope and rejects all valid creds | Hard-code scope strings per provider in the plan; cite SDK source files; sovereign-cloud variants resolved automatically by `azcore/cloud.Configuration.Services` |
| Probe runs in `validate()` but config is applied anyway | Change `OnConfigurationChange` to return error on probe failure; do not rely on warning-level logging |
| Required-field gates in `handleTestAzure*Connection` reject SP configs at HTTP 400 before the probe runs | Refactor each handler's required-field check as an `auth_mode` switch |
| Strict GUID validation rejects legitimate domain-style `tenant_id` (e.g., `contoso.onmicrosoft.com`) | Validate as GUID OR FQDN-shaped |
| First runtime op against non-existent queue/container returns generic 404 | Wrap as sanitized actionable message: "resource not found; pre-provision in SP mode" at each first-use call site |
| SP secret leaks via Queue/Blob error logs that previously only handled shared-key (which rarely appears in errors) | Centralize `sanitizeAzureError`; expand regex set to OAuth2 form bodies and bearer tokens; unit test enumerates expected leak shapes |
| SDK token-refresh goroutines leak across plugin reloads | Thread plugin-lifetime ctx into all three providers; verify with `pprof goroutine` before merge |
| Azure Government customers hit AAD authority mismatch with default | `azure_cloud` field required for non-public; passed to both credential and client options |
| SP with data-plane-only RBAC fails Create-on-startup | Skip Create in SP mode; require pre-provisioned resources; document the RBAC roles needed |
| Test-connection rejects valid SP that runtime would accept | Non-destructive probes (GetProperties / Peek) in both modes; same auth path as runtime |
| Bad SP creds discovered only on first message flow, spamming logs | Save-time `GetToken` probe with timeout; sanitized errors surface in admin UI |
| `client_secret` returned to admin browser via `GET /api/v4/config` (pre-existing) | Document the gap; offer `client_secret_env` / `client_secret_file` indirection; full fix in Phase 1B |
| Cleartext secrets accidentally cleared by saving form with empty password field | Sentinel-merge in `OnConfigurationChange`: empty inbound + non-empty stored = preserve |
| Operator confuses SB blob sidecar credential model (inherit vs independent) | Plan explicitly forbids `blob_account_name`/`blob_account_key` when SB is in SP mode; validation rejects; doc explicit |
| Phase 1 client-secret-only is insufficient for some Federal high-side ATOs | Document deployment guidance; offer file/env indirection; Phase 2 cert auth is the production path |
| Mismatch between SDK versions and `azcore.ClientOptions.Cloud` shape | Verified at SDK v1.20.0 (azcore) + v1.0.1 (azqueue) + v1.6.4 (azblob) + v1.10.0 (azservicebus) — all accept `ClientOptions{Cloud: cloud.AzureGovernment}` |

## UX Summary

| Scenario | Behavior |
|----------|----------|
| Admin opens existing shared-key connection | Form pre-fills shared-key fields; `auth_mode = shared-key` (default); password field shows placeholder `(saved)` not asterisks |
| Admin switches dropdown to Service Principal | Shared-key fields hide and are cleared; SP fields appear empty; `azure_cloud` defaults to Public; for SB, `service_bus_namespace` field shows |
| Admin saves with missing required SP field | Inline error rendered by form validation; save blocked |
| Admin saves with both legacy and SP fields populated | Server-side validation rejects; admin sees sanitized error: "auth_mode is service-principal; account_key must not be set" |
| Admin clicks Save without editing password field | Webapp sends sentinel `"********"`; server preserves stored value |
| Admin clicks Save with explicit empty password field | Webapp sends empty string; server preserves stored value (treats empty + non-empty stored as "no change") |
| Admin clicks Save with bad SP creds | Save-time probe fails; admin sees sanitized AADSTS error; save rejected; logs show error_code + sanitized message |
| Admin clicks Save with valid SP creds | Save-time probe succeeds; audit log line emitted; save succeeds |
| Test Connection in SP mode | Token acquired via SP creds + cloud env; non-destructive probe (GetProperties / Peek) executes; succeeds without needing management-plane RBAC |
| Plugin starts with SP that has data-plane-only RBAC | Constructor skips Create; provider starts cleanly; no 403 warnings |
| Plugin starts with shared-key that has data-plane-only RBAC | Create returns 403; logged at `LogWarn` with `AzureXxxAuthzMismatchOnCreate` error code (NOT confused with AlreadyExists) — admin gets actionable signal |
| Operator uses `client_secret_file: /run/secrets/azure-sp` | At construction, plugin reads file; never persists secret to plugin config; rotation = update mount + plugin reload |
| Operator on Azure Government with `azure_cloud: usgov` | All AAD + data-plane calls route to `.us` endpoints; commercial endpoints unused |

## Testing Plan

### Unit (Go)
- `validateAzureQueueConnection` / `Blob` / `ServiceBus`: matrix of `auth_mode` × valid/missing-required/populated-other-mode/bad-format inputs
- `sanitizeAzureError`: each leak pattern (shared key, SAS sig, OAuth2 form body, bearer token, JSON tokens) → scrubbed output
- `redactConnection`: SP fields populated → safe fields visible, secret absent (regex assertion on JSON marshal)
- Sentinel-merge in `OnConfigurationChange`: matrix of (inbound, stored) → (kept, replaced)
- Constructor selection: stub `azcore.TokenCredential` injected; assert provider uses SP path when `auth_mode == "service-principal"`
- `client_secret_env` / `client_secret_file` resolution: env unset → error; file missing → error; both inline and indirect → error
- Save-time `GetToken` probe: returns within timeout; error path produces sanitized message

### Integration / Smoke
- Existing NATS smoke and shared-key Azure smokes unchanged (regression gate)
- New `make docker-azure-sp-smoke-test` (optional Phase 1 deliverable) using Azurite + a stub OIDC identity provider, OR gated behind real-Azure env vars in CI

### E2E / Playwright (webapp)
- Auth-mode dropdown: switching modes shows/hides correct fields
- Cloud dropdown: switching changes form labels (e.g., service URL placeholders for usgov)
- Validation errors render on save with missing required SP fields
- Test-connection button uses the selected mode and surfaces sanitized errors

### Manual
- Real Azure SP against a public-cloud test namespace/account: queue send/receive, blob upload/download, SB send/receive
- Same on Azure Government (if access available) to validate cloud-env wiring
- Verify `mattermost.log` and `mmctl config show` outputs: no `client_secret`, no bearer tokens
- Verify browser DevTools network tab: `GET /api/v4/config` still returns cleartext (pre-existing exposure; documented gap until Phase 1B); webapp form load with `client_secret_file` shows no secret value at all (indirection working as designed)

## Acceptance Criteria

- [ ] All three providers operate end-to-end with SP credentials on both Azure Public and (if accessible) Azure Government
- [ ] Existing shared-key / connection-string configs continue working without any config edit
- [ ] No log output (any level) includes raw `client_secret`, SAS keys, bearer tokens, or OAuth2 form bodies — enforced by CI grep check on test fixtures
- [ ] Validation rejects missing-required-for-mode fields AND populated-from-other-mode fields with clear, sanitized messages
- [ ] Admin UI cleanly switches between auth modes; secret fields use sentinel hydration on save; no accidental secret clearing
- [ ] Save-time SP probe fails the save with a sanitized error message for bad credentials
- [ ] SP mode does not call control-plane Create; plugin starts cleanly with data-plane-only RBAC
- [ ] `client_secret_env` and `client_secret_file` work end-to-end for at least one provider
- [ ] Docs updated: admin.html SP sections, OpenAPI schema, regenerated PDFs, Phase 1B exposure note
- [ ] `make check-style && make test` pass
- [ ] Audit log line emitted at successful SP construction
- [ ] `TestCodesUnique` passes; new error codes added to `AllCodes`

## Checklist

- [ ] **Diagnostics**: Auth probe failures, sanitized credential errors, and audit-log success entries should post to the diagnostics channel (see `server/CLAUDE.md` → Diagnostics Channel)
- [ ] **Slash command**: Not applicable — admin-console-only feature. (Optional follow-up: `/crossguard auth-test <connection-name>` to re-run the save-time probe on demand, but defer)
