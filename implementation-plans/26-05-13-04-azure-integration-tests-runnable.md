# Make Azure integration tests runnable locally and in CI

## Context

Plan 03 (`26-05-13-03-test-coverage-followups.md`) landed seven new
in-process integration tests under `server/integration_*_test.go`. The
four NATS tests run against an embedded `nats-server/v2` and pass on
every developer machine. The three Azure tests
(`integration_reorder_azure_queue_test.go`,
`integration_reorder_azure_blob_test.go`,
`integration_reorder_servicebus_test.go`) currently `t.Skip` because
Azurite and the Service Bus emulator can only run as separate
containers, not in-process.

The repo already has a working Docker stack for those emulators:

- `docker-compose.dev.yml` brings up `azurite` (Queue + Blob on
  10001/10000) and, behind the `servicebus` compose profile, the
  Service Bus emulator on 5672 with a SQL Server sidecar.
- `make docker-setup` starts the stack and `make docker-integration-test`
  drives the existing curl-based smoke tests (`docker-azure-smoke-test`,
  `docker-azure-blob-smoke-test`, `docker-servicebus-smoke-test`).
- CI workflow `.github/workflows/pr.yml` already has an
  `integration-test` job that brings the stack up and runs
  `docker-integration-test`.
- The Service Bus emulator config in `build/servicebus-emulator-config.json`
  pre-declares exactly one queue, `crossguard-relay`. The emulator does
  not support runtime queue creation; queues are static.

Three things prevent my new in-process tests from running against this
stack:

1. **Wrong Azurite account key.** My tests use the Microsoft canonical
   key, but `build/integration-tests.mk:402` uses a custom key that
   appears to be what the Azurite image bundled in this repo actually
   accepts. Need to align on one key.
2. **Timestamped resource names.** My tests create
   `crossguard-test-reorder-1778712...` queues/containers, which the
   SDK can do at runtime for Azurite (Queue, Blob) but not for Service
   Bus. Each run leaks resources to Azurite's bind-mounted data dir.
3. **No CI wiring.** Nothing invokes the new `TestIntegration*` tests
   with the emulators reachable; CI runs `docker-integration-test`
   only.

This plan addresses all three in four phases that can land
independently. Goal: `make integration-test` works on a developer
machine after `make docker-setup`, and a CI job catches regressions
on every PR.

## Scope

In scope:

- Phase 1: align test config (account key, endpoints, queue names) with
  the existing Docker stack, gated on environment variables so the
  hard-coded defaults are obvious.
- Phase 2: pre-declare a dedicated Service Bus queue for the in-process
  tests and add per-test cleanup so reruns are idempotent.
- Phase 3: a `make integration-test` Make target and any test-side
  changes (queue creation, purge-on-startup) needed for repeatable
  local runs.
- Phase 4: CI workflow update to run `make integration-test` in the
  existing `integration-test` job.

Out of scope:

- Cleaning up the existing `docker-integration-test` smoke tests (they
  work; they cover end-to-end through real plugins).
- Adding new test coverage. This is plumbing only.
- A standalone CI job for in-process integration tests; folding into
  the existing one is simpler.

## Phase 1: align Azure test config with the working Docker stack

### Account key

The repo's Azurite container in `docker-compose.dev.yml:46` starts with
default args (no `AZURITE_ACCOUNTS`, no `--account-key`). It therefore
uses the well-known Microsoft Azurite key. The makefile at
`build/integration-tests.mk:402` writes a different key into the plugin
config and the existing smoke tests pass in CI; either Azurite is more
permissive than expected, or the key string was committed with a
mistake. Either way, **the in-process tests should use the same key as
`integration-tests.mk` so a single source of truth governs all
emulator-facing code**.

Concrete change in `server/integration_azure_helpers_test.go`:

- Replace `azuriteAccountKey` with the value from
  `integration-tests.mk:402` verbatim.
- Add a comment pointing at that file as the canonical source.
- Add env-var overrides for every constant so CI or a developer can
  point the tests at a different Azurite instance without code changes:
  - `CROSSGUARD_AZURITE_ACCOUNT_NAME` (default `devstoreaccount1`)
  - `CROSSGUARD_AZURITE_ACCOUNT_KEY` (default: the makefile value)
  - `CROSSGUARD_AZURITE_QUEUE_URL` (default `http://127.0.0.1:10001/devstoreaccount1`)
  - `CROSSGUARD_AZURITE_BLOB_URL` (default `http://127.0.0.1:10000/devstoreaccount1`)
  - `CROSSGUARD_SERVICEBUS_CONNSTR` (default the host conn-string from
    `integration-tests.mk:727`)

A small helper in the same file reads env-with-default so test code
stays clean:

```go
func envOr(name, fallback string) string {
    if v := os.Getenv(name); v != "" {
        return v
    }
    return fallback
}
```

### Queue and container names

The existing smoke tests own three Azurite resources:

- `crossguard-azure-test` (Azure Queue)
- `crossguard-azure-files` (Azure Blob, used for file transfer)
- `crossguard-azure-blob-batches` (Azure Blob, used by the blob batcher)

And one Service Bus queue:

- `crossguard-relay`

The in-process tests must not share these (the smoke test runs
concurrently with them in CI, and the in-process tests assert exact
dispatched envelope counts which would be polluted by smoke-test
traffic). Pick dedicated names:

- `crossguard-itest-queue` (in-process Azure Queue test)
- `crossguard-itest-blob` (in-process Azure Blob test)
- `crossguard-itest-sb` (in-process Service Bus test)

Queue and container resources can be created at runtime via the Azure
SDK; `newAzureProvider` already calls `CreateQueue` and
`CreateContainer` (idempotent, ignores `*AlreadyExists`). For the
in-process test, that creation succeeds on first run and is a no-op on
subsequent runs. **The test should also purge the queue/container at
setup** so a previous failed run does not leave stale messages that
the in-process test would treat as unexpected envelopes:

- Azure Queue: drain by calling `DequeueMessages` + `DeleteMessage` in
  a loop with a short visibility timeout until empty.
- Azure Blob: list blobs and delete each one. The blob provider's
  batch container is small, so this is fast.

A `purgeAzureResources(t, provider)` helper in
`integration_azure_helpers_test.go` does both, called from each test's
setup.

### Service Bus queue declaration

Service Bus emulator queues are static: they must be in
`build/servicebus-emulator-config.json` at container startup time.
Add a second queue entry:

```json
{
  "Name": "crossguard-itest-sb",
  "Properties": {
    "DeadLetteringOnMessageExpiration": false,
    "DefaultMessageTimeToLive": "PT1H",
    "DuplicateDetectionHistoryTimeWindow": "PT20S",
    "ForwardDeadLetteredMessagesTo": "",
    "ForwardTo": "",
    "LockDuration": "PT30S",
    "MaxDeliveryCount": 10,
    "RequiresDuplicateDetection": false,
    "RequiresSession": false
  }
}
```

Shorter `LockDuration` (30 s vs 1 m) because the in-process test's
abandon-and-retry path benefits from faster redelivery.

Purge: Service Bus does not expose a "drain queue" API in the Go SDK
without admin-level permissions. The test instead works around this by
**only counting envelopes received during the test's own time window**
(record `time.Now()` at the test's start; ignore messages whose
broker-side EnqueuedTime is earlier). That sidesteps the need to purge
stale messages from a previous run.

### Test-side changes per file

`integration_reorder_azure_queue_test.go`:

- Replace timestamped `queueName` with constant `crossguard-itest-queue`.
- Replace `azuriteQueueEndpoint` literal with `envOr(...)`.
- Add `purgeAzureQueue(provider)` at the top of the test body.
- Replace the pre-flight Publish skip-on-error with `provider.Publish`
  inside the main test body (no longer needed; we control the queue).

`integration_reorder_azure_blob_test.go`:

- Same shape: constant container name, env-var endpoint, purge call.
- Drop the `noopKV` workaround in favor of a tiny in-memory mock that
  actually persists Set/Get/Delete so the blob WAL recovery path is
  exercised correctly during the test. ~30 lines.

`integration_reorder_servicebus_test.go`:

- Constant queue name `crossguard-itest-sb`.
- Conn-string from env.
- Time-window filter on receive instead of purge.
- Drop the pre-flight Publish skip-on-error pattern: if the queue is
  pre-declared, the publish will succeed (or signal a real bug).

### Files

- `server/integration_azure_helpers_test.go`: env-var helpers, purge
  helpers, account key alignment.
- `server/integration_reorder_azure_queue_test.go`: constant queue
  name, env-var endpoint, purge call.
- `server/integration_reorder_azure_blob_test.go`: same shape, plus
  in-memory KV mock.
- `server/integration_reorder_servicebus_test.go`: constant queue
  name, env-var conn-string, time-window receive filter.
- `build/servicebus-emulator-config.json`: add
  `crossguard-itest-sb` queue.

Estimate: 200 lines net change across these five files plus the JSON.

## Phase 2: `make integration-test` target

The existing Makefile has an `integration-test` target I added in plan
03 that runs `gotestsum -- -v -run '^TestIntegration' ./server/...`.
That works for the NATS subset but does nothing about emulator
availability.

Two-layer target:

- `make integration-test` (existing): runs the NATS subset only with
  emulators-not-required behavior. Kept for the fast unit-only path.
- `make integration-test-azure` (new): depends on `docker-setup`
  (azurite up) and `docker-servicebus-up` (Service Bus profile up),
  then runs the Azure subset with `CROSSGUARD_AZURE_INTEGRATION=1` set
  so any skip becomes a fail. The env var is already wired in
  `integration_azure_helpers_test.go:23-25`.

```makefile
.PHONY: integration-test-azure
integration-test-azure: docker-setup docker-servicebus-up servicebus-probe-run
	@echo "Running Azure in-process integration tests..."
	CROSSGUARD_AZURE_INTEGRATION=1 \
	$(GOBIN)/gotestsum -- -v \
	  -run '^TestIntegrationReorderAzureQueue|^TestIntegrationReorderAzureBlob|^TestIntegrationReorderServiceBus' \
	  ./server/...
```

The `servicebus-probe-run` dependency is critical: the SB emulator
binds 5672 before its schema is ready, so without the probe the first
publish call hits an `amqp:not-found` even when the container is up.

The existing `docker-integration-test` target stays as-is (it runs the
curl-based smoke tests that drive real plugin code). The two suites
together give the full coverage promised by plan 03: smoke tests
exercise plugin-to-plugin paths, in-process tests exercise sequencer
reorder/loss behavior at the transport boundary.

A convenience wrapper `make integration-test-all` chains both:

```makefile
.PHONY: integration-test-all
integration-test-all: docker-integration-test integration-test-azure
```

### Files

- `Makefile` (or `build/integration-tests.mk`): add the two targets
  above. Prefer `build/integration-tests.mk` since the existing
  Azure smoke-test targets already live there.

## Phase 3: per-test isolation and idempotency

Even with dedicated queue/container names, two failure modes remain:

1. **Test reruns after a panic** leave queue/container with messages
   that the next run mistakes for "expected delivery".
2. **Parallel test execution** (`go test -p 2`) on the same emulator
   crosses streams.

Mitigations:

- `purgeAzureQueue`, `purgeAzureBlobContainer`: helpers in
  `integration_azure_helpers_test.go` that drain everything before the
  test publishes. Idempotent and cheap (~50 ms).
- Each integration test calls `t.Parallel()` only inside its own
  subgroup, never against another Azure test. The package-level
  `go test -p` default of `GOMAXPROCS` is fine for unit tests but the
  Azure tests should serialize. Achieve this by marking each Azure
  test with a shared mutex via a small `azureSerialize` helper:

```go
var azureMu sync.Mutex

func acquireAzure(t *testing.T) {
    azureMu.Lock()
    t.Cleanup(azureMu.Unlock)
}
```

This is package-internal so it adds zero ceremony per test (one line at
the top of each Azure test body).

### Files

- `server/integration_azure_helpers_test.go`: purge helpers + mutex.
- Each of the three Azure test files: one-line `acquireAzure(t)` call.

Estimate: 100 lines.

## Phase 4: CI integration

The existing `integration-test` job in
`.github/workflows/pr.yml` already runs the Docker stack and the
smoke tests. Two minimal edits land the in-process tests:

1. Before `Run integration tests`, bring up the Service Bus profile:

   ```yaml
   - name: Start Service Bus emulator
     run: make docker-servicebus-up
     timeout-minutes: 5
   ```

2. After `Run integration tests`, add:

   ```yaml
   - name: Run in-process Azure integration tests
     run: make integration-test-azure
     timeout-minutes: 10
     env:
       CROSSGUARD_AZURE_INTEGRATION: "1"
   ```

The `CROSSGUARD_AZURE_INTEGRATION=1` env converts any test-side skip
(emulator unreachable, key wrong, queue missing) into a fail. CI's
contract is "Azure tests must run for real."

The job step order becomes:

1. Start Docker environment (`make docker-setup`)
2. Deploy plugin (`make docker-deploy`)
3. Run smoke test (`make docker-smoke-test`)
4. Run integration tests (`make docker-integration-test`)
5. Start Service Bus emulator (`make docker-servicebus-up`)
6. **Run in-process Azure integration tests** (`make integration-test-azure`)

Total added CI time: roughly 30 s (the in-process tests run in 15-20 s
once the emulators are up).

### Files

- `.github/workflows/pr.yml`: two new steps in the existing
  `integration-test` job.

## Order of work

Each phase produces a working artifact independently:

1. **Phase 1 first** (align config). Pre-requisite for everything else.
   After this lands, a developer can run
   `CROSSGUARD_AZURE_INTEGRATION=1 go test -run TestIntegrationReorderAzureQueue -v ./server/`
   manually after `make docker-setup` and the test passes.
2. **Phase 2** (make targets). Removes the manual env-var dance.
3. **Phase 3** (isolation). Makes reruns idempotent.
4. **Phase 4** (CI). Pins regressions on every PR.

Phases 1-3 can land in a single PR. Phase 4 should be a separate PR so
the CI green-run is observable independent of test changes.

## Validation gates

Per phase:

- After Phase 1: manual run on developer machine passes all three
  Azure tests with `CROSSGUARD_AZURE_INTEGRATION=1`.
- After Phase 2: `make integration-test-azure` runs the three Azure
  tests end-to-end (depends on docker-setup having been run).
- After Phase 3: `make integration-test-azure` passes on two
  consecutive runs without `make docker-down` between them.
- After Phase 4: CI green on a PR that touches no test code.

Cross-phase, before final merge:

- `make check-style` clean.
- `make docker-integration-test` still green (no regression in
  existing smoke tests).
- `make integration-test-azure` green on CI for at least 3
  consecutive runs (flake detection).

## Risks

1. **Azurite account key mystery.** If the makefile's custom key
   actually does not work and the smoke tests succeed for some
   other reason (e.g., the smoke test doesn't actually exercise
   authenticated paths), Phase 1's alignment will fail. Mitigation:
   verify by running `make docker-azure-smoke-test` locally and
   confirming it exercises Publish + Receive. If it doesn't, fall
   back to the Microsoft canonical key and ignore the makefile.

2. **Service Bus emulator config reload.** Adding a queue to
   `servicebus-emulator-config.json` requires a container restart.
   `make docker-servicebus-up` is idempotent; `--force-recreate` may
   be needed to pick up config changes. Document this in the plan
   if it bites; in practice, `docker compose --profile servicebus up
   -d --force-recreate servicebus-emulator` handles it.

3. **CI timing.** The Service Bus probe target waits up to 120 s for
   the emulator to be ready. On a slow CI runner, this could push
   the integration-test job past its timeout. Mitigation: raise the
   job timeout from 10 minutes to 15.

4. **Test serialization defeats parallelism.** The Azure tests run
   serially via `azureMu`. With three tests at ~5 s each, that's 15 s
   sequentially. Faster than the NATS tests' parallel run, so this is
   acceptable. If a fourth Azure test is added, revisit isolation by
   resource (one mutex per provider type) rather than one global.

5. **Emulator data leak across CI runs.** The Azurite container uses
   a bind mount `./docker/azurite-data` that persists between
   `docker-setup` and `docker-down` but is wiped by `docker-clean`.
   The purge helpers prevent stale-message contamination, but a
   developer who runs the in-process tests after an aborted smoke
   test may still see odd behavior. Document `make docker-clean` as
   the reset hammer.
