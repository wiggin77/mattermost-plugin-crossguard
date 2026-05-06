# Remove the inbound relay semaphore

## Context

`relaySem` (256-slot buffered channel) was added to apply backpressure to the inbound message processing path so a flood of messages could not spawn unbounded goroutines. The current shape:

- Field on `Plugin` (server/plugin.go:41), sized by `relaySemaphoreSize = 256` (server/connections.go:12).
- Acquired in `handleInboundMessage` (server/inbound.go:92) before calling `processInboundMessage`; released on return.
- `OnConfigurationChange` uses `p.relaySem != nil` as a sentinel for "plugin has been activated" before triggering reconnects (server/configuration.go:612).

## Why remove it

The premise is wrong. Each inbound provider's `Subscribe` callback is invoked from a single dispatcher goroutine and processes messages serially:

- **NATS** (`nats_provider.go:80-97`): uses `nc.Subscribe` / `nc.QueueSubscribe`. The nats.go library's async subscriber model has one internal dispatcher goroutine per subscription; messages are delivered in order, synchronously, to the callback. There is no goroutine-per-message.
- **Azure Queue** (`azure_provider.go:159-227`): single `pollQueue` goroutine, `for _, msg := range resp.Messages` loop processes each message before moving to the next.
- **Azure Blob batched** (`azure_blob_provider.go:812-845`): single `pollBlobs` goroutine, one blob at a time.
- **Azure Service Bus** (`azure_servicebus_provider.go:227-317`): single `pollQueue` goroutine, serial message loop.

So the realistic worst case across N inbound connections is N concurrent handler invocations — well under the 256 cap. The semaphore has never engaged in any production configuration and provides no observable backpressure.

Outbound is also unaffected: `relaySem` is not acquired by `OnSharedChannelsSyncMsg` or any outbound code path.

The framework itself serializes outbound calls into the plugin (`syncLoop` is a single goroutine on the cluster leader, calling each `On*` hook synchronously). So adding *any* semaphore to defend against framework-side concurrency would be solving a problem the framework's contract explicitly rules out.

## Files to change

1. **`server/inbound.go`** — remove the semaphore acquire/release block and update the comment on `handleInboundMessage`. The function shrinks to a closure that just captures `connName` and calls `processInboundMessage`. Keep the closure so the handler signature stays compatible with `provider.Subscribe`.

2. **`server/plugin.go`** — remove the `relaySem` field from `Plugin` and the `p.relaySem = make(...)` init in `OnActivate`.

3. **`server/connections.go`** — remove `relaySemaphoreSize = 256` from the const block.

4. **`server/configuration.go:612`** — replace the `p.relaySem != nil` activation-sentinel with `p.ctx != nil`. `p.ctx` is set on the same line block in `OnActivate` as `relaySem`, so the semantic is identical: "plugin has gone through activation, safe to reconnect on config change." Update the surrounding comment.

5. **Tests:**
   - `server/configuration_test.go:964-979` (`TestOnConfigurationChange_NoReconnectBeforeActivation`) — update the comment from "relaySem is nil" to "p.ctx is nil"; the test still asserts the same behavior (no reconnect before activation). The body needs no changes since it never set relaySem.
   - `server/configuration_test.go:1015` (`TestOnConfigurationChange_WithReconnect`) — replace `p.relaySem = make(chan struct{}, 50)` with `p.ctx, _ = context.WithCancel(context.Background())`.
   - `server/plugin_test.go:85` — remove the `p.relaySem = make(...)` line.
   - `server/test_helpers_test.go:156, 424` — remove both `p.relaySem = make(...)` lines. These helpers now only need to set `p.ctx`, which they already do (or should; verify).

## Risks

- **A future provider that does deliver messages concurrently per subscription**: a custom provider could break this assumption (e.g. a JetStream pull consumer with a worker pool, or someone wiring up `nats.ChanSubscribe` later). Mitigation: the semaphore is trivial to re-add in one place (`handleInboundMessage`) when there is a concrete reason. Don't pre-add for hypotheticals.
- **Activation-sentinel correctness**: using `p.ctx != nil` instead of `p.relaySem != nil` relies on `p.ctx` only being set in `OnActivate` and cleared on `OnDeactivate`. Verify `OnDeactivate` does not nil out `p.ctx`. Currently it calls `p.cancel()` but does not set `p.ctx = nil`, so the sentinel survives across deactivate/reactivate. Acceptable: deactivation already handles its own teardown, and a config change after deactivate-without-reactivate is a non-issue.

## Out of scope

- The proposed `fileSem` for the file-attachment plan. That is being dropped from that plan in a separate revision; this plan and that revision are independent and can land in either order.
- Any change to outbound publish concurrency, since it never used a semaphore.

## Verification

- `make check-style`
- `make test`
- `make docker-integration-test` (covers all four providers' inbound paths)
- Manual: post a burst of ~100 messages on Server A, observe Server B receives all of them without errors. Same on each of the four providers.

## Implementation order

Single commit. The change is small, mechanical, and the tests are co-located.

1. Edit the four production files plus the test files in one pass.
2. `make check-style` + `make test`.
3. Commit with a message that names the analysis (framework serializes outbound; provider subscribe callbacks deliver serially per subscription) so future readers don't re-discover the rationale.
