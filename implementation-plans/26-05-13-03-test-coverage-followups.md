# Test coverage follow-ups

## Context

The poc1/poc2 sender-restart bug landed in production despite a 20-test
unit suite and full lint/test gating. The root cause was an interaction
between sender counter persistence and receiver epoch-change handling
that neither side's unit tests covered. Six tier-1 regression tests
were added in the previous PR
(`server/inbound_sequencer_test.go::TestSequencerCheckpointLoadThenEpochChange`
and the five tests in `server/outbound_seq_test.go`) to pin the
specific bug shape, but several broader coverage gaps remain:

1. The transport-level reorder/loss integration tests listed in the
   Phase 4 validation gate of
   `implementation-plans/26-05-13-01-envelope-sequence-and-epoch.md`
   were deferred and never implemented.
2. The single-active-receiver elector
   (`server/inbound_election.go`) has zero unit tests.
3. The fanout builder
   (`server/transport.go::buildOutboundEnvelopes`) has five happy-path
   tests but no coverage for several known edge cases.
4. The property-based invariant test
   (`TestSequencerInvariants`) listed as "nice-to-have" was never
   written.

This plan addresses all four in three phases that can land
independently. The goal is defense in depth: bugs of the
sender-receiver-interaction shape, the cluster-handoff shape, and the
property-violation shape should each have a test that fails before the
fix lands and remains green afterward.

## Scope

In scope:

- Tier 2: per-transport integration tests with deterministic reorder
  and loss injection.
- Tier 3a: elector unit tests (two-node race, crash failover,
  graceful step-down, partition).
- Tier 3b: fanout builder edge cases.
- Tier 3c: property-based invariant test for the sequencer state
  machine.

Out of scope:

- New product features. This is pure test coverage.
- Docker harness changes beyond a new `make` target for the
  fault-injection suite.
- Performance benchmarks (separate work).

## Phase A: transport reorder and loss integration tests

The unit test suite covers the sequencer state machine but not the
behavior of each transport provider under the reordering vectors that
production actually exhibits. The Phase 4 validation gate of plan 01
listed six integration tests:

| Test | Provider | Failure mode injected |
|---|---|---|
| `TestIntegrationReorderNATS` | NATS queue group | adjacent-swap with probability 0.5 |
| `TestIntegrationReorderAzureQueue` | Azure Queue | handler-error-then-visibility-timeout retry on 10% of messages |
| `TestIntegrationReorderServiceBus` | Service Bus | `AbandonMessage` on 10% of messages |
| `TestIntegrationReorderAzureBlob` | Azure Blob | concurrent senders producing out-of-order blob list arrival |
| `TestIntegrationLossNATS` | NATS core | drop 5% of messages |
| `TestIntegrationCrossChannelIsolation` | NATS | reorder on channel A only, assert channel B keeps up |

A seventh test now joins the list to cover the production-observed bug
shape end-to-end against a real transport:

| Test | Provider | Failure mode |
|---|---|---|
| `TestIntegrationSenderRestartNATS` | NATS | publish, restart sender plugin, publish, assert receive without 30 s gap wait |

### Reorder-injection harness

A new helper wraps any `QueueProvider` and intercepts `Publish` calls
on the sender side. The wrapper has two configurable modes:

- `swap-adjacent`: with probability `p`, hold the incoming publish and
  swap it with the previous one in a small buffer.
- `delay-one-in-N`: hold every Nth publish for K ticks before
  releasing.

The wrapper is for test use only and lives in a new file
`server/testreorder/wrapper.go` so production code is unaffected. The
wrapper implements `QueueProvider` and delegates everything except
`Publish` to the underlying provider.

The `loss` mode for `TestIntegrationLossNATS` is a third behavior on
the same wrapper: drop with probability `p`, no buffer.

### Test scaffolding

Each integration test:

1. Spins up the test provider (NATS via embedded `nats-server/v2`,
   Azure providers via Azurite, Service Bus via the emulator).
2. Wraps the sender-side provider in the reorder wrapper.
3. Constructs a sender Plugin and a receiver Plugin in-process,
   with mocked `ReceiveSharedChannelSyncMsg` capturing dispatched
   content.
4. Publishes a known sequence of posts plus reactions.
5. Asserts every post and every reaction was dispatched to the
   receiver. Audit logs are checked for the appropriate
   `InboundSeq*` codes (gap-filled events expected, gap-timeout
   only on the explicit loss test).

The existing dual-server docker harness (`make
docker-integration-test`) stays as-is; these new tests run as
in-process Go tests against embedded transports (NATS server, Azurite,
Service Bus emulator). That makes them fast and deterministic, and
they run on every PR without needing the full docker stack. A new
`make` target `make integration-test` runs only this suite, and it
gets folded into `make test` for full coverage on CI.

### Files

- `server/testreorder/wrapper.go`: the wrapper plus its three modes.
- `server/testreorder/wrapper_test.go`: smoke tests of the wrapper.
- `server/integration_reorder_nats_test.go`
- `server/integration_reorder_azure_queue_test.go`
- `server/integration_reorder_servicebus_test.go`
- `server/integration_reorder_azure_blob_test.go`
- `server/integration_loss_nats_test.go`
- `server/integration_cross_channel_isolation_test.go`
- `server/integration_sender_restart_nats_test.go`
- `Makefile`: new `integration-test` target.

Each integration test is roughly 100 lines; the wrapper is roughly 80
lines. Estimate 800-1000 lines plus a Makefile entry.

## Phase B: elector unit tests

`server/inbound_election.go` runs in production untested. The election
state machine has a finite set of transitions that lend themselves to
unit testing if the KV store is mocked.

### Tests

| Test | Scenario |
|---|---|
| `TestElectorAcquireOnEmptyLease` | Lease key absent, single elector ticks, lease acquired, `Subscribe` called once. |
| `TestElectorRenewWhenHolder` | Lease key contains this node's ID, tick renews, no transition. |
| `TestElectorObservesSiblingHolder` | Lease key contains a different node's ID, no acquire attempt, no transition. |
| `TestElectorAcquiresAfterTTLExpiry` | Lease key absent (sibling crashed), tick acquires, `Subscribe` called. |
| `TestElectorTwoNodeRace` | Two electors race on an empty lease, only one acquires, no flapping over 100 ticks. |
| `TestElectorStepsDownOnLost` | This node held the lease, KV read returns a different holder (partition), elector calls `Unsubscribe` and clears state. |
| `TestElectorGracefulStepdownPoke` | This node holds the lease; `poke()` from cluster event triggers an immediate tick that releases the lease cleanly. |
| `TestElectorPublishStepdownOnShutdown` | `shutdown()` releases the lease via KV and publishes the `active_inbound_stepdown` cluster event. |

The mock KV store records calls so the tests can assert on:

- The exact CAS sequence (read, conditional write).
- The TTL value passed to `SetExpiry`.
- The number of `Subscribe` / `Unsubscribe` calls (exactly one per
  acquire / release in steady state).

A mock `QueueProvider` with channel-based stubs for `Subscribe` and
`Close` lets tests assert what the elector did to the provider.

### Files

- `server/inbound_election_test.go`: the eight tests plus a mock
  helper.

Estimate 300-400 lines.

## Phase C: fanout builder edge cases

`server/transport.go::buildOutboundEnvelopes` has five tests that
cover the happy paths. The following edge cases are unchecked:

| Test | Scenario |
|---|---|
| `TestFanoutPostsOnlyDistinctUsers` | Multiple posts by different authors. Each post envelope inlines only its post's author, not the full Users map. |
| `TestFanoutReactionWithoutPostUser` | Orphan reaction whose user is not in `Users`. Metadata envelope is built but the user lookup misses gracefully (no nil deref). |
| `TestFanoutMembershipPlusOrphanReactionSameUser` | Membership change and orphan reaction for the same user. Metadata envelope deduplicates the user. |
| `TestFanoutNilEntries` | `Posts` slice contains nil entries (defensive); fanout skips them and produces the correct envelope count. |
| `TestFanoutMentionTransformsDuplicatedPerPostEnvelope` | Each post envelope carries the full MentionTransforms map (acceptable duplication trade-off documented in plan 02). |
| `TestFanoutSyncMsgIDAndChannelIDPreserved` | Every emitted envelope inherits `Id` and `ChannelId` from the source `SyncMsg`. |
| `TestFanoutEmptyButHasChannelId` | Sync msg with only `Id` and `ChannelId` (no posts, no metadata) still produces one bare envelope so the upstream cursor advances. (Already covered by `TestFanoutEmptySyncMsgEmitsBare`; this row documents the existing coverage.) |

### Files

- Extends `server/transport_test.go`.

Estimate 200-300 lines.

## Phase D: property-based invariant test

A property-based test asserts the two correctness statements that the
sequencer's design guarantees, against randomly generated reorder
sequences:

1. **No loss without audit.** Every input sequence number either
   appears in the dispatched output or appears inside an
   `InboundSeqGapTimeout` or `InboundSeqBufferOverflow` audit range.
   The union of dispatched seqs and audited-as-lost seqs equals the
   input seq set.
2. **Dispatched seqs are monotonic per channel.** The receiver sees
   strictly increasing sequence numbers per channel after the
   sequencer drains, ignoring duplicates that the duplicate-suppression
   logic correctly drops.

The test uses `testing/quick` to generate random permutations of seqs
1..N for varying N, optionally dropping a fraction to simulate loss,
and runs each permutation through the sequencer with a short
gap-timeout to force the timeout path. Each invariant violation
becomes a failing seed that can be replayed deterministically.

This is the strongest correctness statement we can make about the
sequencer without a back-channel. If a future refactor introduces a
subtle invariant violation (e.g., a corner case where an envelope is
neither dispatched nor audited), the property test catches it.

### Files

- `server/inbound_sequencer_property_test.go`: the property test plus
  generators.

Estimate 200-300 lines.

## Order of work

The three phases are independent and can land in any order. Recommended
ordering by value-per-effort:

1. **Phase A first** (transport integration). Highest coverage value:
   would have caught the production bug end-to-end, plus catches a
   whole class of provider-specific reordering issues. Single PR.
2. **Phase B second** (elector tests). Covers production code that
   currently has no tests at all. Localized PR.
3. **Phase C third** (fanout edges). Defensive coverage; lower
   probability of catching live bugs but worth having.
4. **Phase D fourth** (property test). The most theoretical of the
   four; useful for confidence but not load-bearing.

Phases A and B can land in parallel since they touch different files.

## Validation gates

Per phase:

- `make check-style` clean.
- `go test ./server/...` green, including the new tests.
- For Phase A only: `make integration-test` green (new target).

Cross-phase, before final merge:

- All tests in the existing suite remain green.
- `golangci-lint run ./...` clean.
- `python3 -m xmlschema` validates fixtures (no schema changes
  expected; this is a regression check).

## Risks

1. **Integration test flakiness.** The transport emulators
   (Azurite, Service Bus emulator) and the embedded NATS server are
   the same ones the existing docker tests use, so flakiness here
   is bounded by what we already accept. The reorder injection is
   deterministic per seed, so individual test runs are stable; CI
   can fix the seed.

2. **Test runtime.** Six new integration tests plus the existing
   suite might push `go test ./server/...` past the 60-second mark
   we currently see. If it grows past 2 minutes, the integration
   tests should be split into a separate target (`make
   integration-test`) that runs alongside but not blocking the
   fast unit-only path. The plan already names this target.

3. **Property test seeds.** A property-based test that fails only
   on certain seeds can be hard to reproduce. The test must log
   the failing seed and provide a replay mode. `testing/quick`
   handles this with the `-quickchecks` and `-test.seed` flags.

4. **Maintenance burden.** Six new integration tests roughly double
   the test count for the `server` package. Each one is one of the
   production paths customers actually hit, so the burden is
   justified, but be alert to any test that flakes more than once
   per month and either fix or remove it.
