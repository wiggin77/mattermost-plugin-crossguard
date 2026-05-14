# Cross-transport test coverage parity

## Context

The integration test matrix today has uneven coverage across the four
supported transports (NATS, Azure Queue, Azure Blob, Service Bus). NATS
is exercised end-to-end against real Mattermost servers for posts,
edits, deletes, reactions, profile-image sync, file filtering, prompt
accept/block, team rewrite, and bi-directional channels; it is also
exercised at the sequencer-contract level for reorder with monotonicity
assertion, loss with gap audit, sender restart, and cross-channel
isolation. The three Azure transports have one in-process reorder test
each (no monotonicity assertion, weak fault model, no loss/restart/
isolation scenarios) and one e2e smoke test each (post relay plus PDF
attachment only).

Concretely, the gaps (see `server/integration_*_test.go` and
`build/integration-tests.mk`):

| Capability                              | NATS | AzureQueue | AzureBlob | ServiceBus |
|-----------------------------------------|------|------------|-----------|------------|
| In-process reorder + monotonicity       | yes  | partial    | partial   | partial    |
| In-process loss + gap audit             | yes  | no         | no        | no         |
| In-process sender restart               | yes  | no         | no        | no         |
| In-process cross-channel isolation      | yes  | no         | no        | no         |
| E2E post edit / delete / reaction       | yes  | no         | no        | no         |
| E2E profile image sync                  | yes  | no         | no        | no         |
| E2E file filter (deny mode)             | yes  | no         | no        | no         |
| E2E prompt accept / block               | yes  | no         | no        | no         |

This plan closes those gaps in two layers. The in-process layer runs
against emulators with the sender-side fault wrapper and pins the
sequencer's contract on every transport. The e2e layer runs the
lifecycle, filter, prompt, and profile-image suites against real
Mattermost servers parameterized by transport, so a regression in any
provider's behavior under realistic plugin code is caught for all four
backends.

Dependencies:

- Plan 04 (`26-05-13-04-azure-integration-tests-runnable.md`) must land
  first. Plan 04 makes the three existing in-process Azure tests
  actually run with stable resource names, purge helpers, and a Service
  Bus queue pre-declared. The in-process work here adds new tests on
  that scaffolding; without plan 04 the new tests would inherit plan
  04's known plumbing problems (timestamped-resource leak, no purge,
  Service Bus queue not pre-declared).
- Plan 01 (`26-05-11-01-migrate-integration-tests-to-go.md`) phases 1
  through 3 must land before this plan's e2e work begins. Plan 01 (as
  amended) designs `lifecycle_test.go`, `profile_image_test.go`,
  `file_filter_test.go`, and `prompt_test.go` as table-driven over a
  `transport` axis with one NATS row each. This plan populates the
  Azure rows.

Plan 03 (in-process tests) shipped and is not modified here; it is the
foundation that the in-process scenario runners refactor in Phase 1.

## Scope

In scope:

- Phase 1: Extract reusable scenario runners from the NATS in-process
  tests so loss / restart / cross-channel / reorder scenarios can be
  driven against any transport without duplicated bodies.
- Phase 2: Apply those scenario runners to Azure Queue, Azure Blob, and
  Service Bus. Three new tests per transport (nine total) plus a
  monotonicity assertion added to the existing reorder tests.
- Phase 3: Add Azure Queue, Azure Blob, and Service Bus rows to
  `lifecycle_test.go` so edit, delete, reaction-add, reaction-remove
  are asserted end-to-end on every transport.
- Phase 4: Same parameterization for `profile_image_test.go`,
  `file_filter_test.go`, and `prompt_test.go` where the underlying
  capability is transport-agnostic.
- Phase 5: CI wiring (timeout bump only; plan 01 and plan 04 do the
  heavy lifting).

Out of scope:

- New unit tests on `inboundSequencer` itself. Plan 03 covers the
  sequencer under direct test; this plan exercises it through every
  transport.
- WAL recovery for Azure Blob across a sender restart. Flagged as a
  gap in plan 04 risk #1; needs a dedicated test that simulates
  mid-batch crash and is out of scope for parity.
- Single-active-receiver elector and KV lease tests. The elector is a
  separate concern with its own contracts and deserves its own plan.
- Status sync, DOCX-on-Azure, channel header sync, team rewrite on
  Azure, bi-directional on Azure, and other minor per-payload-type
  gaps. These are not parity gaps in the sequencer-contract sense and
  become cheap once the table-driven shape exists.
- `splitTransportEnvelope` oversize tests for Azure Queue (48 KB
  limit). Separate code path, separate test.

## Phase 1: extract in-process scenario runners

The current NATS tests in `server/integration_*_nats_test.go` repeat
the same shape three times: build sender + receiver + wrapper, drive N
publishes, drain gap timeouts, assert dispatch set, assert audit. Lift
that shape into runners that take a provider factory.

Target shape, `server/integration_runners_test.go` (new):

```go
// providerFactory builds a real provider plus a teardown. Returns
// (sender, receiver) because NATS uses two nats.Conns while Azure
// providers can serve as both. cleanup is registered on t.Cleanup
// inside the factory.
type providerFactory func(t *testing.T) (sender, receiver QueueProvider)

func runReorderScenario(t *testing.T, pf providerFactory, gapTimeout time.Duration, n int)
func runLossScenario(t *testing.T, pf providerFactory, gapTimeout time.Duration, n int)
func runSenderRestartScenario(t *testing.T, pf providerFactory)
func runCrossChannelIsolationScenario(t *testing.T, pf providerFactory, gapTimeout time.Duration)
```

Each runner contains the full test body (wrapper construction, sender
and receiver wiring, publish loop, drain, assertions). Critically,
`runReorderScenario` asserts both set membership AND dispatched-seq
monotonicity (the latter is currently only present in the NATS reorder
test, see `integration_reorder_nats_test.go:54-59`). After Phase 1 the
monotonicity contract is exercised through any provider the factory
returns.

Existing NATS tests reduce to one-liners:

```go
func TestIntegrationReorderNATS(t *testing.T) {
    runReorderScenario(t, natsProviderFactory, 30*time.Second, 20)
}
func TestIntegrationLossNATS(t *testing.T) {
    runLossScenario(t, natsProviderFactory, 200*time.Millisecond, 60)
}
func TestIntegrationSenderRestartNATS(t *testing.T) {
    runSenderRestartScenario(t, natsProviderFactory)
}
func TestIntegrationCrossChannelIsolation(t *testing.T) {
    runCrossChannelIsolationScenario(t, natsProviderFactory, 200*time.Millisecond)
}
```

Pure refactor, no behavior change. Run all four NATS tests before and
after to confirm green.

### Files

- `server/integration_runners_test.go` (new, ~200 lines)
- `server/integration_helpers_test.go` (extract `natsProviderFactory`
  from `newIntegrationHarness`)
- `server/integration_reorder_nats_test.go` (reduce to one-liner)
- `server/integration_loss_nats_test.go` (reduce)
- `server/integration_sender_restart_nats_test.go` (reduce)
- `server/integration_cross_channel_isolation_test.go` (reduce)

Estimate: 250 lines net change (mostly subtraction in the per-test
files).

## Phase 2: apply scenario runners to Azure transports

Three factories, four new scenario tests per transport. Each factory
honors `CROSSGUARD_AZURE_INTEGRATION` (plan 04 wiring), calls
`requireEmulatorReachable`, constructs the production provider with
the constants from `integration_azure_helpers_test.go` (plan 04
aligned), purges resources where applicable, and returns a (sender,
receiver) pair.

### Azure Queue

`server/integration_azure_queue_factory_test.go`:

```go
func azureQueueProviderFactory(t *testing.T) (sender, receiver QueueProvider) {
    requireEmulatorReachable(t, "Azurite Queue", azuriteQueuePort)
    api := &plugintest.API{}
    registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
    // poll interval override for fast test runs
    origPoll := azureQueuePollInterval
    azureQueuePollInterval = 50 * time.Millisecond
    t.Cleanup(func() { azureQueuePollInterval = origPoll })

    cfg := AzureQueueProviderConfig{
        QueueServiceURL: azuriteQueueEndpoint,
        AccountName:     azuriteAccountName,
        AccountKey:      azuriteAccountKey,
        QueueName:       "crossguard-itest-queue", // plan 04 constant
    }
    p, err := newAzureProvider(cfg, api)
    require.NoError(t, err)
    t.Cleanup(func() { _ = p.Close() })
    purgeAzureQueue(t, p) // plan 04 helper
    return p, p
}
```

New tests in `server/integration_azure_queue_scenarios_test.go`:

- `TestIntegrationReorderAzureQueue` (replaces existing inline body;
  now also asserts monotonicity via the shared runner).
- `TestIntegrationLossAzureQueue` (new): 5% loss, gap timeout, audit
  assertion.
- `TestIntegrationSenderRestartAzureQueue` (new): pin the
  poc1 to poc2 regression on Azure Queue.
- `TestIntegrationCrossChannelIsolationAzureQueue` (new):
  per-(connName, channelID) cursor independence.

### Azure Blob

`server/integration_azure_blob_factory_test.go`: builds outbound and
inbound providers against the same container (see
`integration_reorder_azure_blob_test.go:44-56`), returns them as
sender and receiver. Inherits the real in-memory KV mock that plan 04
phase 1 introduces in place of `noopKV`.

New tests (`server/integration_azure_blob_scenarios_test.go`):

- `TestIntegrationReorderAzureBlob` (replaces existing body; switches
  the wrapper from `ModePassthrough` to `ModeSwapAdjacent` so reorder
  is actually exercised, and adds monotonicity via the runner).
- `TestIntegrationLossAzureBlob`
- `TestIntegrationSenderRestartAzureBlob`
- `TestIntegrationCrossChannelIsolationAzureBlob`

### Service Bus

`server/integration_servicebus_factory_test.go`: uses the
`crossguard-itest-sb` queue declared by plan 04. Applies the
time-window filter on receive (plan 04 phase 1) instead of purge.

New tests (`server/integration_servicebus_scenarios_test.go`):

- `TestIntegrationReorderServiceBus` (replaces existing body; adds
  monotonicity via the runner; keeps the abandon-every-10th
  handler-side retry that the existing test uses, since that is the
  Service Bus reorder vector).
- `TestIntegrationLossServiceBus`
- `TestIntegrationSenderRestartServiceBus`
- `TestIntegrationCrossChannelIsolationServiceBus`

### Files

- `server/integration_azure_queue_factory_test.go` (new, ~80 lines)
- `server/integration_azure_queue_scenarios_test.go` (new, ~80 lines)
- `server/integration_azure_blob_factory_test.go` (new, ~80 lines)
- `server/integration_azure_blob_scenarios_test.go` (new, ~80 lines)
- `server/integration_servicebus_factory_test.go` (new, ~80 lines)
- `server/integration_servicebus_scenarios_test.go` (new, ~80 lines)
- Delete the existing single-file inline-body tests for the three
  transports.

Estimate: 500 lines net add, 200 lines deleted.

## Phase 3: e2e lifecycle parity

Depends on plan 01 phase 1 through 3 (the `lifecycle_test.go`
table-driven shape with one NATS row).

For each Azure transport, add a row to the lifecycle test table.
Schema for a row:

```go
type lifecycleRow struct {
    transport     string                  // "azure-queue", "azure-blob", "servicebus"
    channelNameA  string                  // e.g. "azure-test"
    channelNameB  string                  // mirror on Server B
    connName      string                  // e.g. "azure-low-to-high"
    setup         func(*testing.T, *Harness)  // patches plugin config, resets plugin, init team + channel
    relayTimeout  time.Duration           // first-relay wait (Service Bus needs 60s)
    opTimeout     time.Duration           // per-operation wait after channel is warm
}
```

Each row drives the canonical lifecycle sequence: post create, post
edit, reaction add, reaction remove, post delete. Assertions are
identical across rows (same Mattermost API calls on server B). The
setup hook for Azure transports patches outbound and inbound
connection config, calls `ResetPlugin`, runs the slash commands to
init the team and channel; see `build/integration-tests.mk:456-478`
for the canonical shell sequence to translate.

### Files

- `server/integration/lifecycle_test.go` (add three Azure rows)
- `server/integration/config.go` (add Azure connection config helpers
  if plan 01 has not already; verify before duplicating)

Estimate: 100 lines net add.

## Phase 4: e2e ancillary parity

Same shape as Phase 3 for the remaining gap files. Each test gains
Azure Queue, Azure Blob, and Service Bus rows.

- `profile_image_test.go`: upload unique PNG, assert
  `last_picture_update` advances on the receiver across all four
  transports' linked channels. Use a dedicated user per transport row
  (e.g., userg-aq, userg-ab, userg-sb) to avoid the framework's
  RemoteID conflict that the existing NATS test sidesteps with userg.
- `file_filter_test.go`: sender-side deny plus receiver-side deny
  across all four transports. Each provider's file-transfer code path
  differs (NATS uses JetStream Object Store, Azure Queue uses Azure
  Blob for file storage, Azure Blob uses its own batched WAL, Service
  Bus uses Azure Blob for file storage), so the filter test exercises
  a different downstream code path per row.
- `prompt_test.go`: accept and block flow on a fresh channel per row.

### Files

- `server/integration/profile_image_test.go` (Azure rows)
- `server/integration/file_filter_test.go` (Azure rows)
- `server/integration/prompt_test.go` (Azure rows)

Estimate: 200 lines net add.

## Phase 5: CI wiring

Phases 1 and 2 add tests that run with `CROSSGUARD_AZURE_INTEGRATION=1`.
Plan 04 already added `make integration-test-azure` and a CI step that
runs it. The new Phase 2 tests inherit that wiring because they live
in the same package and match the same `^TestIntegration*` pattern.

Phases 3 and 4 add tests under `server/integration/`. Plan 01 phase 5
already replaces the body of `docker-integration-test` with a
`go test -tags=integration` invocation; the new rows inherit that.

Only CI change in this plan: raise the `integration-test` job timeout.
Phase 2 adds nine new in-process Azure tests at roughly 5 to 15 seconds
each; Phases 3 and 4 add nine new e2e rows at roughly 10 to 60 seconds
each (Service Bus dominates). Estimated added wall time: 5 minutes.
Bump the existing 10 to 15 minute timeout to 25 minutes to absorb both
the new tests and emulator-readiness margin.

### Files

- `.github/workflows/pr.yml` (timeout bump on the integration-test job)

Estimate: trivial.

## Order of work

Phases 1 and 2 are independent of plan 01 and can land first, after
plan 04. Phases 3 and 4 depend on plan 01 phases 1 through 3.

1. **Phase 1** (scenario runner extraction): pure refactor of NATS
   tests, no new behavior. PR boundary.
2. **Phase 2** (Azure scenario coverage): nine new in-process tests
   plus monotonicity on three updated reorder tests. PR boundary.
3. **Phase 3** (e2e lifecycle parity): waits on plan 01 phase 1 to 3.
   Closes the biggest e2e gap. PR boundary.
4. **Phase 4** (e2e ancillary parity): one PR per file or one PR for
   all three; author's call.
5. **Phase 5** (CI timeout): rolls into the Phase 2 PR (the one that
   first adds wall-time cost).

## Validation gates

Per phase:

- After Phase 1: existing NATS tests pass with no diff in assertion
  behavior. Side-by-side comparison of pre and post output recommended.
- After Phase 2: nine new tests plus three updated reorder tests pass
  with `CROSSGUARD_AZURE_INTEGRATION=1` on a developer machine after
  `make docker-setup` and `make docker-servicebus-up`. Monotonicity
  asserted on all four transports.
- After Phase 3: `TestPostLifecycle` runs all four rows green in CI.
- After Phase 4: each of `TestProfileImage`, `TestFileFilter`,
  `TestPrompt` runs all four rows green in CI.

Cross-phase, before final merge of Phases 2 and 4:

- `make check-style` clean.
- `make integration-test-azure` green on CI for 3 consecutive runs
  (flake detection).
- `make docker-integration-test` green on CI for 3 consecutive runs.

## Risks

1. **Provider factory abstraction leaks transport-specific quirks.**
   Azure Blob's outbound/inbound split, Service Bus's static queue,
   and Azure Queue's poll-interval tuning are all transport-specific.
   If the factory tries to hide them all the factory grows hair; if
   it exposes them the runners grow conditionals. Mitigation: the
   factory returns a `(sender, receiver)` pair and any
   transport-specific tuning lives inside the factory's setup. The
   runner stays transport-agnostic. The poll-interval override at
   `integration_reorder_azure_queue_test.go:31-33` becomes part of the
   Azure Queue factory.

2. **Service Bus emulator throughput limit.** The emulator's
   documented throughput is well below cloud Service Bus. The
   cross-channel isolation test publishes on two channels in rapid
   succession; if the emulator stalls the test could flake.
   Mitigation: keep per-channel N small (about 5 envelopes each), use
   generous `require.Eventually` timeouts, and document the throughput
   ceiling if flake persists.

3. **Azure Blob fault injection limited to wrapper.** The Azure Blob
   provider batches at the WAL layer, so the testreorder wrapper sees
   per-envelope identity at the sender side but the receiver gets
   batches. Reorder via `ModeSwapAdjacent` still works (the wrapper
   swaps envelopes before they enter the batch); loss works
   (envelopes dropped at the wrapper never enter the batch); restart
   works (epoch is per-envelope, batch-agnostic); cross-channel
   isolation works (sequencer keys on (connName, channelID), wrapper
   drops by index). The one scenario this does NOT cover is in-batch
   reordering by the receiver, which would require a different test
   shape and is out of scope.

4. **E2E timeouts for Service Bus.** The Service Bus smoke test polls
   up to 60s on first-init relay (`integration-tests.mk:851`).
   Lifecycle is five sequential operations; at 60s each that is 5
   minutes. Mitigation: after the initial relay establishes the
   channel link, subsequent operations on the same channel are much
   faster (under 5s in practice). The lifecycle row should wait for
   initial relay once (relayTimeout = 60s) then run the
   edit / delete / react sequence with opTimeout = 10s.

5. **Plan 01 amendment scope.** Phases 3 and 4 assume plan 01 produces
   table-driven test files. If plan 01's implementation diverges and
   the tests end up single-row hardcoded despite the amendment, Phase
   3 and 4 become refactor-plus-add instead of just add. Mitigation:
   review plan 01's first migrated test (lifecycle) for the table
   shape before this plan starts Phase 3. If the shape is missing,
   bounce it back to plan 01 rather than absorbing the refactor here.

6. **Factory cleanup on test failure.** The factories register
   `t.Cleanup` for provider close and resource purge. If a runner
   panics before publishing the cleanup still fires, but if the
   emulator itself hangs the cleanup can block. Mitigation: every
   provider Close call has a context timeout (existing behavior in
   the providers); the cleanup wraps Close with an additional
   `context.WithTimeout(5*time.Second)`.
