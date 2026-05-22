# Azure Service Principal integration test coverage

**Date:** 2026-05-22
**Branch:** TBD (follow-up to `recv_shared_channels_apis`)
**Status:** Draft, pending approval.

## Goal

Add automated end-to-end integration coverage for the Service Principal
(SP) auth path on Azure Queue and Azure Blob providers, using Azurite's
`--oauth basic` mode. Validates that our SP wiring (`TokenCredential`
flowing into the Azure SDK clients, the data-plane operations under
token auth) works at runtime, not just at compile time.

Out of scope: Service Bus SP. The Microsoft Service Bus emulator
(`mcr.microsoft.com/azure-messaging/servicebus-emulator`) has no OAuth
token endpoint and is not expected to grow one; SP Service Bus is only
testable against a real Azure namespace.

## Why this is its own plan

The main SP port plan (`26-05-22-01-azure-service-principal-port.md`)
delivered the runtime feature with strong unit-test coverage
(`azure_credentials_test.go`, `azure_errors_test.go`,
`sentinel_merge_test.go`, validator tests) plus the existing shared-key
integration coverage that the providers' data-plane paths share. What is
missing is a test that combines "SP credential" with "real broker on
the wire" so a future refactor cannot regress the wiring path between
those two without CI catching it.

Upstream shipped SP without this layer of coverage. We can do better.

## What this test would and would not validate

| Scenario | Covered today | Adds with this plan |
|---|---|---|
| `buildClientSecretCredential` constructs an `azidentity` credential | Unit | -- |
| Error sanitization on OAuth2 form bodies and JWTs | Unit | -- |
| `OnConfigurationChange` SP probe path runs | Unit (against stub credential) | Integration (against working OAuth endpoint) |
| `azqueue.NewQueueClient` accepts `TokenCredential` and SDK can Publish | -- | Integration |
| `container.NewClient` accepts `TokenCredential` and SDK can UploadBlob | -- | Integration |
| `azureCloudConfig` plumbing reaches the SDK without breaking it | -- | Integration |
| Sovereign-cloud authority host routing | -- | -- (needs real AAD) |
| Real AAD policy / conditional access enforcement | -- | -- (needs real AAD) |
| Real Azure RBAC role enforcement | -- | -- (needs real AAD) |

The realistic deliverable is "exercises our provider code under a
`TokenCredential`," not "proves our SP setup works against AAD." The
release-gate manual smoke against a real tenant is still required.

## Constraints discovered during scoping

1. **Azurite OAuth mode is real but fiddly.** `--oauth basic` adds an
   internal token endpoint to Azurite and accepts bearer tokens on the
   data-plane requests. The token validation is shape-only (no
   signature, no audience check), but the exact format requirements are
   undocumented and have shifted across Azurite versions.
2. **A second Azurite instance is the safe path.** Enabling `--oauth
   basic` on the existing Azurite container would force every shared-key
   integration test to also pass token-auth checks (or be reconfigured).
   Less risky to stand up a parallel container with OAuth enabled on
   different ports.
3. **Production constructors have no credential injection point.** Our
   `newAzureProvider` / `newAzureBlobProvider` call
   `azidentity.NewClientSecretCredential` internally. Either:
   (a) add a test-only constructor variant that accepts a pre-built
       `azcore.TokenCredential`, or
   (b) point `azidentity` at Azurite's mock token endpoint via a
       custom `cloud.Configuration.ActiveDirectoryAuthorityHost`.
   Option (a) is more controllable; option (b) tests more of our code
   path but introduces more failure modes. Decision captured below.
4. **Service Bus emulator gap is permanent for Phase 1.** Document it
   and gate the SP Service Bus test on a real-tenant credential env var
   (skip in CI, runnable locally by operators).

## Approach (recommended)

**Choose option (b)** -- point `azidentity` at Azurite's mock token
endpoint -- so the test exercises the full production code path
including `azidentity.NewClientSecretCredential` and the cloud
configuration plumbing. This is the higher-fidelity test and the bug
classes we are trying to catch (SDK wiring regressions, scope-string
typos, cloud-config plumbing) live in exactly that path.

If option (b) hits a wall we cannot work around in 1-2 days, fall back
to option (a) and document the limitation in the test file.

### Mechanics

1. Add a fourth `azure_cloud` value `"azurite"` (test-only, gated by a
   build tag or by `ConnectionConfig` only being constructed in tests)
   whose `cloud.Configuration` points `ActiveDirectoryAuthorityHost` at
   the Azurite OAuth endpoint. Alternatively, the test directly
   constructs a connection that bypasses `resolveAzureCloud` and feeds
   the SDK clients a custom `cloud.Configuration`. The latter avoids
   leaking test-only state into production code.
2. Stand up a second Azurite container in `docker-compose.dev.yml`
   named e.g. `azurite-oauth`, started with `--oauth basic` on ports
   distinct from the existing Azurite (e.g., 11000/11001).
3. Add `server/integration/azure_queue_sp_test.go` and
   `server/integration/azure_blob_sp_test.go`. Each test:
     - Spins up the provider via `connections.go` against the new
       Azurite-OAuth instance with `auth_mode: "service-principal"`,
       a synthetic tenant ID, a synthetic client ID, and a synthetic
       client secret.
     - Publishes a message / uploads a blob.
     - Subscribes / lists and verifies arrival.
     - Tears down.
4. Add `server/integration/azure_servicebus_sp_test.go` that skips with
   a clear message: SB emulator has no OAuth endpoint; run this against
   a real namespace by setting `CROSSGUARD_AZURE_SB_SP_TEST=1` and the
   associated credential env vars.
5. Extend `build/integration-tests.mk` with
   `docker-azure-queue-sp-test` and `docker-azure-blob-sp-test` single-
   test wrappers (mirroring the existing `docker-azure-smoke-test`
   pattern). Wire them into the full-suite target.

## Phases

### Phase 1 -- Spike: verify Azurite OAuth mode actually works for our SDK

Time-boxed half-day. Goals:

- Bring up `azurite:latest --oauth basic` locally.
- Write a minimal Go program that uses
  `azidentity.NewClientSecretCredential` with a custom
  `ActiveDirectoryAuthorityHost` pointing at Azurite, then uses the
  resulting token to call `azqueue.NewQueueClient` and enqueue a
  message.
- Confirm the message lands.

If this works: proceed to Phase 2. If it fails after the time-box:
escalate to option (a) (test-only constructor variants + stub
`TokenCredential`) and document why in the eventual test file.

### Phase 2 -- Compose + harness

- Add the `azurite-oauth` service to `docker-compose.dev.yml`.
- Extend `server/integration/docker.go` (or equivalent) to expose the
  new container's URLs to tests via env vars / config defaults.
- Add a helper in `server/integration/` that builds an SP-mode
  `ConnectionConfig` pointed at Azurite-OAuth.

### Phase 3 -- Queue + Blob SP tests

- `azure_queue_sp_test.go`: publish/subscribe round-trip.
- `azure_blob_sp_test.go`: upload/list/download round-trip and one
  WAL-recovery scenario under SP.
- Both tests assert that the audit log line
  `AzureSPAuditConstructed=12010` was emitted once per provider
  construction.

### Phase 4 -- Service Bus skip + real-tenant gate

- `azure_servicebus_sp_test.go` with `t.Skip` unless
  `CROSSGUARD_AZURE_SB_SP_TEST=1` and required env vars set.
- Document the env vars in a `// Setup:` comment.
- Add the same gating pattern to an optional `azure_queue_sp_real_test.go`
  / `azure_blob_sp_real_test.go` so operators with a real tenant can
  validate against AAD on demand without changing CI behavior.

### Phase 5 -- Makefile wiring + docs

- Add `docker-azure-queue-sp-test`, `docker-azure-blob-sp-test` to
  `build/integration-tests.mk`.
- Update `CLAUDE.md`'s Docker Commands table to list them.
- Update `26-05-22-01-azure-service-principal-port.md` to mark the
  Phase 9 gap closed.

## Risks and unknowns

1. **Azurite OAuth shape may not satisfy `azidentity` strictly.** If
   `azidentity` rejects Azurite's token endpoint response shape (wrong
   `token_type`, missing `expires_in`, etc.), option (b) fails and we
   fall back to option (a). The Phase 1 spike is specifically to
   discover this.
2. **CI startup time.** Adding a second Azurite container costs ~5
   seconds at every integration test run. Acceptable.
3. **Port allocation.** Need to pick ports for the second Azurite that
   don't collide on dev machines. Use `11000/11001` (10000 + 1000
   pattern) and gate via `AZURITE_OAUTH_QUEUE_PORT` /
   `AZURITE_OAUTH_BLOB_PORT` env vars.
4. **Discovery of an Azurite-version-specific OAuth quirk.** We pin
   `azurite:latest` today. The test should set a specific tag (e.g.,
   the version that worked during Phase 1 spike) so a future Azurite
   release does not silently break the test.

## What this plan does NOT replace

A manual smoke test against a real Azure tenant before each release.
That smoke covers real AAD policy enforcement, real RBAC role
checking, and sovereign-cloud routing, none of which any emulator
reproduces. The CI integration test catches "wiring broke" earlier and
more cheaply; the release smoke catches "Azure-specific assumptions
broke."

## Effort estimate

- Phase 1 (spike): 0.5 day
- Phase 2 (compose + harness): 0.5 day
- Phase 3 (Queue + Blob tests): 0.5 day
- Phase 4 (Service Bus skip + real-tenant gate): 0.25 day
- Phase 5 (Makefile + docs): 0.25 day

Total: ~2 dev-days. Discovery risk in Phase 1 could extend Phase 2-3
by another half-day if option (a) is needed.

## Decision points

1. **Option (a) vs option (b) credential path.** Recommended (b) for
   fidelity; fall back to (a) if Phase 1 spike fails.
2. **Pin Azurite version.** Recommended: pin to a specific tag once
   Phase 1 has verified one works, rather than `:latest`.
3. **Run the SP tests on every PR or only on a dedicated workflow?**
   Recommended: every PR, because the cost is low and the regressions
   they catch are real. Revisit if CI time becomes a problem.
