# Port Azure Service Principal Authentication

**Date:** 2026-05-22
**Branch:** `recv_shared_channels_apis`
**Status:** Ready to execute.

## Goal

Land upstream's Service Principal (SP) authentication feature in its final
post-cleanup state (after upstream commits `76cf2ec` and `0656890`), not the
original `e092df3` form. End state: each Azure provider (Queue, Blob, Service
Bus) accepts `auth_mode: "service-principal"` with `tenant_id`, `client_id`,
`client_secret`, `azure_cloud`, and (Service Bus only) `service_bus_namespace`.
Existing shared-key and connection-string configurations continue to work
unchanged via an empty-default `auth_mode`.

## Upstream commits in scope

End-state target = the head of upstream/main minus the smoke-test commits and
the already-converged uniqueness fix.

- `e092df3` (Phase 1: SP for the three Azure providers) -- foundation, modified
  by the two below before merge.
- `76cf2ec` (Simplify SP credential to inline `client_secret` only) -- drop
  env-var and file-path secret sources; rely on Mattermost's existing
  `MM_PLUGINSETTINGS_PLUGINS_CROSSGUARD_*` env-var substitution for injection.
- `0656890` (Tighten validation, sanitization, reconcile docs) -- unconditional
  blob-sidecar empty-fields rule in SP mode, four extra error-redaction
  patterns, FQDN-shape validation for `service_bus_namespace`, webapp clears
  `azure_servicebus` block on provider switch.
- `367c7eb` (Help docs) -- ported as-is in Phase 7.

Out of scope on this branch:

- `ede6910` (per-direction uniqueness) -- already converged in our
  `configuration.go:282-291`.
- `c271c6f`, `c6239c4` (Python smoke-test fixes) -- our Go-based integration
  tests already use correct configs (`build/devbaseline/devbaseline.go:111`
  uses `flush_interval_seconds: 5`).

## Constraints discovered during research

1. **WAL-based blob provider stays untouched.** Our
   `server/azure_blob_provider.go` is ~1259 lines of write-ahead-log plus
   distributed-locking machinery and has no upstream counterpart. SP only needs
   to land at the single point where the `container.Client` is constructed.
   The WAL recovery, lock acquisition, fencing-token, and processed-marker code
   is auth-agnostic and must not be perturbed.
2. **Constructor signatures change.** All three providers currently take
   `(cfg, api)`. Upstream's save-time `GetToken` probe needs a
   `context.Context`. We add a `ctx context.Context` parameter to each
   provider constructor; all call sites and tests update accordingly.
3. **`OnConfigurationChange` posture.** Our current code at
   `server/configuration.go:633-637` warns and proceeds on validation error.
   We will adopt upstream's hardening (return the error). That hardening is
   what triggered the smoke-test fallout commits we are skipping; our Go
   integration suite builds correct configs so the blast radius is small.
4. **Error-code slots line up.** Our highest used codes per range:
   `18048` / `19009` / `26006`. Upstream's SP codes (`18049` / `19010` /
   `26007`, plus the cross-provider audit code `12010`) all fit the
   next-available slot. No renumbering needed; `TestCodesUnique` will pass.
5. **Already converged.** `ede6910` (per-direction connection-name uniqueness)
   is independently present in our branch; skip.
6. **Service Bus blob sidecar in connection-string mode.** Both upstream and
   our branch already use independent `BlobAccountName` / `BlobAccountKey`
   shared-key credentials for the blob sidecar
   (`server/azure_servicebus_provider.go:138`). This stays unchanged. The
   open design choice is only for SP-mode sidecar behavior; see open
   question #1.

## Phases

### Phase 1 -- Lift credential and error helpers (clean port)

New files, mostly verbatim from upstream's final state:

- `server/azure_credentials.go` -- `resolveAzureCloud`,
  `buildClientSecretCredential`, `probeAzureSP`, `logAzureAuthAudit`,
  `secretHashPrefix`. Use the simplified `76cf2ec` signature (no env-var, no
  file-path; only inline `client_secret`). The `probeAzureSP` signature is
  the 7-arg form from `76cf2ec`, not the 9-arg `e092df3` form.
- `server/azure_errors.go` -- `sanitizeAzureError`, `sanitizeAzureString`.
  Include the four extra redaction patterns from `0656890` (`skoid` / `sktid`
  user-delegation SAS object/tenant identifiers, OAuth2 `client_assertion`
  form fields, OAuth2 `password` form fields, `id_token` JSON values).
- `server/azure_credentials_test.go` and `server/azure_errors_test.go` --
  clean port. Exclude the deleted `TestResolveAzureSecret_*` (9 tests for the
  removed env/file secret resolver) and the deleted
  `*_SPMode_NoSecretSource` and `*_MultipleSecretSourcesIsError` tests.
- Add `AzureSPAuditConstructed = 12010` to `server/errcode/codes.go` and to
  the `AllCodes` slice.

### Phase 2 -- Configuration: SP fields, validation, sentinel merge

Edit `server/configuration.go`:

- Add `AuthMode`, `AzureCloud`, `TenantID`, `ClientID`, `ClientSecret` to
  `AzureQueueProviderConfig`, `AzureBlobProviderConfig`, and
  `AzureServiceBusProviderConfig`. Add `ServiceBusNamespace` to the Service
  Bus struct (required only in SP mode; SAS connection strings encode the
  namespace, SP mode does not).
- Add constants: `SecretSentinel = "__CROSSGUARD_SECRET_UNCHANGED__"`,
  `AzureAuthSharedKey`, `AzureAuthConnectionString`,
  `AzureAuthServicePrincipal`, `AzureCloudPublic`, `AzureCloudUSGov`,
  `AzureCloudChina`.
- Add helpers: `normalizeAzureAuthMode`, `validateAzureTenantID`,
  `validateAzureCloud`, `validateServiceBusNamespace`,
  `validateAzureServicePrincipalFields`, `azureForbidSPFields`.
  `validateAzureServicePrincipalFields` shrinks per `76cf2ec` to a
  required-`client_secret` check (no "exactly one of three" path).
- Switch `validateAzureQueueConnection`, `validateAzureBlobConnection`, and
  `validateAzureServiceBusConnection` to branch on
  `normalizeAzureAuthMode(cfg.AuthMode)`. Enforce per-mode required and
  forbidden field sets including cross-mode leakage:
    - `account_key` empty in SP mode.
    - `connection_string` empty in SP mode.
    - SP fields (`tenant_id`, `client_id`, `client_secret`) empty in legacy
      mode.
    - `service_bus_namespace` empty in connection-string mode.
    - `blob_account_name` and `blob_account_key` (the Service Bus sidecar
      shared-key fields) empty unconditionally in SP mode, per `0656890`.
- Sentinel merge: in `OnConfigurationChange`, before `validate()`, walk each
  Azure connection and replace every `SECRET_SENTINEL` placeholder
  (`client_secret`, `account_key`, `connection_string`, sidecar
  `blob_account_key`) with the prior stored value, so cleartext secrets
  never round-trip through the browser on save. New test
  `server/sentinel_merge_test.go`.
- Harden `OnConfigurationChange` to return the error from `validate()`
  instead of logging a warning and proceeding (the current behaviour at
  `server/configuration.go:633-637`). Bad configs will now refuse to load;
  the prior config stays active. Update any tests that asserted the
  warn-and-proceed contract.
- Add error codes `AzureBlobSPCredentialFailed = 18049`,
  `AzureQueueSPCredentialFailed = 19010`,
  `ServiceBusSPCredentialFailed = 26007`. Append to `AllCodes`.

### Phase 3 -- Wire SP into the three providers

- `server/azure_provider.go`:
    - Change `newAzureProvider` signature to accept
      `ctx context.Context`. Update all call sites
      (`server/connections.go` and any tests).
    - Branch on `normalizeAzureAuthMode(cfg.AuthMode)`. SP path:
      `buildClientSecretCredential` then
      `azqueue.NewQueueClientWithTokenCredential`. Skip
      `CreateIfNotExist` in SP mode (operators pre-provision queues with
      data-plane-only RBAC, `Storage Queue Data Contributor`).
    - Blob sidecar: in SP mode, inherit parent credential
      (`container.NewClientWithTokenCredential`); ignore any
      `BlobAccountName` / `BlobAccountKey` (validation has already enforced
      they are empty).
    - Update `testAzureQueueConnection` (in `server/api.go` or its callee)
      to delegate field validation to `validateAzureQueueConnection` and
      call `probeAzureSP` in SP mode.
- `server/azure_blob_provider.go`:
    - Same branching at the single client-construction site. The WAL,
      locking, recovery, and marker code stays untouched.
    - Skip container `Create` in SP mode.
- `server/azure_servicebus_provider.go`:
    - SP path constructs `azservicebus.NewClient` from
      `ServiceBusNamespace + TokenCredential`. Admin client similarly for any
      control-plane operations.
    - Blob sidecar in SP mode inherits the parent SP `azcore.TokenCredential`
      (same instance reused; not constructed twice). Validation in Phase 2
      already forbids `BlobAccountName` and `BlobAccountKey` in SP mode, so
      the sidecar branch can assume those fields are empty.
    - Skip queue/topic/container auto-provision in SP mode.

### Phase 4 -- REST API and audit

- `server/api.go`: `/api/v1/test-connection` for the three Azure providers
  delegates required-field validation to the per-provider validators (drop
  any hardcoded `account_key` / `connection_string` checks). In SP mode,
  invoke `probeAzureSP`.
- Emit one `AzureSPAuditConstructed` INFO log line per successful SP
  credential construction with K/V `tenant_id`, `client_id`, `azure_cloud`,
  `secret_sha256_prefix` (the first 8 hex chars of SHA-256 of the resolved
  secret). The secret itself is never logged.

### Phase 5 -- Webapp

Edit `webapp/src/components/ConnectionSettings.tsx`:

- Add `ServicePrincipalFieldsComponent`, reused across all three Azure
  providers. Per `76cf2ec`, this component renders one `client_secret`
  password input plus `tenant_id`, `client_id`, and the Azure Cloud select.
- Auth Mode select on each Azure provider form (legacy default + SP). Azure
  Cloud select beside it.
- Conditional rendering: hide `account_key` / `connection_string` /
  `blob_account_name` / `blob_account_key` in SP mode; show
  `service_bus_namespace` only in SP mode.
- Move "Blob Service URL" for Azure Queue into the file-transfer block,
  alongside "Blob Container Name" (per `76cf2ec`).
- `validateAzureAuthFields`:
    - Trim and lowercase `auth_mode` the same way the server's
      `normalizeAzureAuthMode` does, so hand-edited configs with mixed-case
      values do not fall through to "Invalid auth_mode" (per `0656890`).
    - `isValidAzureTenantID` (GUID or FQDN).
    - For Service Bus SP mode, FQDN-shape check on `service_bus_namespace`
      mirroring server's `validateServiceBusNamespace` (strip `sb://` and
      trailing slash; require a dot; no whitespace, no path, no other
      scheme) per `0656890`.
- Provider switch handler clears `azure_queue`, `azure_blob`, and
  `azure_servicebus` blocks when switching providers (the `0656890` fix
  added `azure_servicebus` to the clear set).
- `loadConnectionForEdit` hydrates every secret field with `SECRET_SENTINEL`
  so the form never shows cleartext on edit; the server-side sentinel-merge
  in Phase 2 restores the prior value on save.
- Playwright CT coverage for: auth-mode toggle visibility, validation paths,
  cross-mode leakage detection, sentinel round-trip on save-then-reopen.

### Phase 6 -- Schema

Edit `schema/crossguard-api.yaml`:

- New `AzureServicePrincipalFields` component with properties `auth_mode`,
  `azure_cloud`, `tenant_id`, `client_id`, `client_secret`.
- Each of `AzureQueueProvider`, `AzureBlobProvider`, `AzureServiceBusProvider`
  references it via `allOf`. Service Bus also adds `service_bus_namespace`.
- Document per-auth-mode required and forbidden field sets in each provider
  description.

### Phase 7 -- Docs

Port into `public/help/` (HTML largely verbatim from upstream's final state):

- `admin.html` -- new "Azure Service Principal Authentication" section,
  Validation Rules Summary updates, blob-sidecar SP-mode-empty rule stated
  as unconditional (per `0656890`).
- `api.html` -- per-auth-mode validation rules for
  `/api/v1/test-connection`.
- `error-codes.html` -- rows for `12010`, `18049`, `19010`, `26007` (final
  state -- the eight reserved codes `0656890` removed are not added here).
- `transport-interface.html` -- update Authentication and Auto-Provisioning
  rows in the provider comparison table; add SP cross-references in each
  Azure provider section.
- `whitepaper.html` -- security-properties paragraph acknowledges SP as an
  alternative to shared-key/SAS for the three Azure providers.
- `threatmodel.html` -- rewrite T-SPOOF-AZ1 to cover the SP credential path,
  recommended SP-over-shared-key posture, and the Phase 1B secret-in-config
  exposure mitigated by the Mattermost env-var substitution mechanism.
- `make generate-pdfs`. Verify error-code parity (Go constants in
  `codes.go` = `AllCodes` slice = HTML rows).

### Phase 8 -- Test fill-in

Folded into earlier phases, but tracked here for completeness:

- `server/configuration_test.go` -- SP validation cases (each per-mode
  required and forbidden combination), `normalizeAzureAuthMode` cases,
  FQDN-shape `service_bus_namespace` cases, cross-mode leakage cases.
- `server/azure_provider_test.go`, `server/azure_blob_provider_test.go`,
  `server/azure_servicebus_provider_test.go` -- add SP construction and
  probe scenarios using token-credential mocks. Update existing call sites
  for the new ctx parameter.
- `server/azure_credentials_test.go`, `server/azure_errors_test.go`,
  `server/sentinel_merge_test.go` -- new files (Phases 1 and 2).
- Webapp Playwright CT (Phase 5).

### Phase 9 -- Dev and integration sanity

- Confirm `make deploy` and the Go integration suite still pass with empty
  `auth_mode` (legacy default). This is the contract for back-compat.
- Investigate whether Azurite (Queue and Blob) and the Service Bus emulator
  support AAD token authentication well enough for an SP-mode integration
  test. Azurite has AAD support since ~3.x; the SB emulator's support is
  less clear. If a real SP integration test is not feasible, document the
  gap rather than fake it.

## Rough effort

- Phases 1-3 (server core): ~1.5 day
- Phase 4 (REST + audit): ~0.5 day
- Phase 5 (webapp + CT tests): ~1.5 day
- Phases 6-7 (schema + docs + PDFs): ~1 day
- Phase 9 (integration check): ~0.5 day

Total: ~5 dev-days.

## Decisions (resolved)

- **Service Bus blob sidecar in SP mode**: match upstream -- the sidecar
  inherits the parent SP credential, and the validator unconditionally
  forbids `BlobAccountName` and `BlobAccountKey` on the Service Bus config
  when `auth_mode == "service-principal"` (even if file transfer is
  disabled). One AAD identity holds both `Azure Service Bus Data
  Sender/Receiver` and `Storage Blob Data Contributor` roles. Reflected
  in Phase 2 (validation) and Phase 3 (Service Bus provider).
- **`OnConfigurationChange` posture**: harden -- return the error from
  `validate()`. Reflected in Phase 2.
- **Save-time AAD probe**: keep upstream's synchronous `GetToken` with the
  10-second timeout. Operators get fast feedback on bad credentials.
  Reflected in Phase 4.
- **PR shape**: one PR.

## Open questions

None remaining. Plan is ready to execute.

## Reference

- Upstream final state: `git diff 3f51684 upstream/main -- server/ webapp/
  schema/ public/help/`
- Upstream implementation plan:
  `git show upstream/main:implementation-plans/26-05-14-01-azure-service-principal-auth.md`
- Our refactored providers: `server/azure_provider.go`,
  `server/azure_blob_provider.go`, `server/azure_servicebus_provider.go`,
  `server/configuration.go`.
