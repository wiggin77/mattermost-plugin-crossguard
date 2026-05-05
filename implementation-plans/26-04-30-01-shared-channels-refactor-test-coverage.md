# Cross Guard: Test Coverage for Shared Channels Refactor

## Context

The shared channels API refactor (`26-04-12-02-shared-channels-api-refactor.md`)
landed with the production code rewritten and the existing test suite kept
passing. However, the deep behavior tests for the new code paths were deferred:

- `server/hooks_test.go` was deleted with the old hook implementations and
  not replaced.
- `server/inbound_test.go` was deleted with the old inbound handlers and not
  replaced.
- `server/connections_test.go` was deleted (its tests covered the old
  envelope builders and the `publishToOutbound` fan-out) and not replaced.
- A handful of existing tests (`plugin_test.go`, `configuration_test.go`,
  `service_test.go`, `command_test.go`, `prompt_test.go`, `api_test.go`)
  passed only because they happened to exercise paths that did not invoke
  the new shared channel APIs. They do not assert that
  `RegisterPluginForSharedChannels`, `ShareChannel`,
  `InviteRemoteToChannel`, `UninviteRemoteFromChannel`, `UnshareChannel`,
  or `ReceiveSharedChannelSyncMsg` are called with the right arguments.

The goal of this plan is to bring test coverage for the refactor up to the
spec laid out in `26-04-12-02-shared-channels-api-refactor.md` (sections
"Testing Plan" and "Acceptance Criteria"), so that future regressions in the
rewritten paths are caught.

## Current State

`make test` passes. `go build`, `go vet`, and `gofmt -l` are clean. The
production code matches the refactor plan, but the tests below are missing
or shallow.

## Design Principles

| Approach | Avoid |
|---|---|
| Use `plugintest.API` mocks with explicit expectations on the new shared channel methods | Wide `Maybe()` matchers that pass even if the call site never fires |
| Drive each test through the public entry point (hook, handler, command, slash command) | Calling private helpers in isolation when the wiring is the thing we want to verify |
| Use the existing `flexibleKVStore` / `setupTestPluginWithRouter` harness | Inventing a parallel scaffold |
| For multi-remote tests, set `p.remoteIDs` directly to skip the registration dance | Calling `OnActivate` end-to-end when only the routing logic is under test |
| Keep XML round-trip assertions narrow (one or two structural checks) | Snapshot-comparing the entire serialized payload |

## Out of Scope

- File attachment / profile image sync tests (still stubbed in production).
- E2E tests against the Docker dev environment.
- Frontend tests.
- Any changes to production code beyond minor test-seam exports if needed.

## Tasks

### 1. `server/hooks_test.go` (new file)

Cover `OnSharedChannelsSyncMsg`, `OnSharedChannelsPing`, and the two stubs:

- [ ] `TestOnSharedChannelsSyncMsg_Success`: rc.RemoteId resolves to a configured
      outbound connection; the envelope reaches the mock provider's `Publish`;
      `SyncResponse.PostsLastUpdateAt` reflects the max post UpdateAt.
- [ ] `TestOnSharedChannelsSyncMsg_NoRemoteMatch`: unknown rc.RemoteId returns an
      empty `SyncResponse{}` and a nil error; logs a warning with
      `OutboundSyncMsgNoRemoteMatch`.
- [ ] `TestOnSharedChannelsSyncMsg_PublishError`: provider's `Publish` returns
      an error; the hook returns a non-nil error so the server cursor does
      not advance; outbound health is marked unhealthy.
- [ ] `TestOnSharedChannelsSyncMsg_NilMsg` and `_NilRC`: both early-return with
      empty `SyncResponse{}` and nil error.
- [ ] `TestOnSharedChannelsSyncMsg_InboundOnly`: `hasOutboundProvider` returns
      false; the hook returns a successful `SyncResponse` without invoking
      Publish (cursor advances).
- [ ] `TestOnSharedChannelsPing_Healthy` / `_Unhealthy` / `_UnknownRemote` /
      `_InboundOnly`.
- [ ] `TestOnSharedChannelsAttachmentStub` and `_ProfileImageStub`: assert nil
      return; assert exactly one Debug log fires.

Helper: a small `setupHookTestPlugin(t, conns []outboundConn, remoteIDs map[string]string)`
that wires `p.remoteIDs`, `p.outboundConns`, and a `mockQueueProvider`
recording the published bytes.

### 2. `server/inbound_test.go` (new file)

Cover `processInboundMessage`, `handleInboundSyncMsg`, and `rewriteChannelIDs`:

- [ ] `TestProcessInboundMessage_SyncMsg`: well-formed XML envelope is
      deserialized; `ChannelId` is rewritten on the SyncMsg, every Post,
      Reaction, MembershipChange, and Acknowledgement to the local channel
      ID; `ReceiveSharedChannelSyncMsg` is called once with the right
      `remoteID`.
- [ ] `TestProcessInboundMessage_Test`: `Type: "test"` envelope logs Info
      with `InboundTestReceived` and returns nil.
- [ ] `TestProcessInboundMessage_UnknownType`: unrecognized `Type` returns nil
      and logs a Warn with `InboundUnknownType`.
- [ ] `TestProcessInboundMessage_InvalidXML`: malformed bytes return nil
      (permanent failure, no redelivery) and log Error with
      `InboundUnmarshalFailed`.
- [ ] `TestProcessInboundMessage_AttachmentAndProfileImage`: each stub
      returns nil and logs Debug.
- [ ] `TestHandleInboundSyncMsg_UnlinkedTeam`: `GetTeamByName` returns
      `not found`; returns nil; no `ReceiveSharedChannelSyncMsg` call. The
      prompt flow is exercised via the existing prompt tests.
- [ ] `TestHandleInboundSyncMsg_UnlinkedChannel`: team resolves but
      channel-level connection is missing; triggers
      `handleUnlinkedInboundChannel`; returns nil.
- [ ] `TestHandleInboundSyncMsg_RewriteIndex`: `GetTeamByName` fails but
      `GetTeamRewriteIndex` returns a local team ID; resolution succeeds.
- [ ] `TestHandleInboundSyncMsg_PartialErrors`: `ReceiveSharedChannelSyncMsg`
      returns a `SyncResponse` with non-empty `PostErrors`; logs a Warn
      with `InboundPostSyncErrors`; hook still returns nil.
- [ ] `TestHandleInboundSyncMsg_ReceiveFails_ReturnsError`: API call returns
      an error; hook returns the error so ack-based providers redeliver.
- [ ] `TestHandleInboundSyncMsg_NoRemoteForConn`: `p.remoteIDs[connName]`
      missing; logs Error with `InboundNoRemoteForConn`; returns nil.
- [ ] `TestHandleInboundSyncMsg_OutboundOnlyConn`: `hasInboundProvider`
      returns false; logs Warn with `InboundNotConfigured`; returns nil.
- [ ] `TestHandleInboundMessage_SemaphoreBackpressure`: fill `p.relaySem`,
      then call the handler in a goroutine; assert it does not return
      until a slot is released; cancel `p.ctx` and assert it returns
      `context.Canceled`.

Note: the production code uses `inbound:<connName>` as the
`p.remoteIDs` key. Tests must populate it using that exact form.

### 3. `server/connections_test.go` (new file)

Cover `publishToOutboundConn` and friends:

- [ ] `TestPublishToOutboundConn_Success`: serializes the envelope to XML
      and publishes once via the named provider; outbound health stays
      healthy.
- [ ] `TestPublishToOutboundConn_UnknownConn`: unknown connection name
      returns an error.
- [ ] `TestPublishToOutboundConn_Unhealthy_RecentlyChecked`: connection
      flagged unhealthy within `healthRecheckInterval`; returns an error
      without invoking Publish.
- [ ] `TestPublishToOutboundConn_Splits`: provider has small
      `MaxMessageSize`; envelope splits into N parts; Publish is called N
      times, each part round-trips through `UnmarshalEnvelope` to a valid
      `TransportEnvelope`.
- [ ] `TestPublishToOutboundConn_PublishFails`: Publish returns an error
      mid-batch; the outer call returns the error; outbound is marked
      unhealthy.
- [ ] `TestUpdateOutboundHealth`: setting healthy=true after a failure
      clears the flag and refreshes `lastCheckTime`.

### 4. `server/plugin_test.go` (additions)

Cover the multi-remote registration logic:

- [ ] `TestOnActivate_RegistersMultipleRemotes`: configure two outbound and
      one inbound, all with distinct `SiteURL` values; assert
      `RegisterPluginForSharedChannels` is called three times with the
      right `SiteURL` arguments; assert `p.remoteIDs` has three entries
      keyed by `"outbound:<name>"` / `"inbound:<name>"` mapped to distinct
      remote IDs.
- [ ] `TestOnActivate_SharedSiteURLRegistersOnce`: outbound `nats-low` and
      inbound `nats-low` defaulting to the same `SiteURL` only register
      once; both names map to the same remoteID.
- [ ] `TestOnDeactivate_UnregistersAllRemotes`: assert exactly one
      `UnregisterPluginForSharedChannels(manifest.Id)` call (bulk).
- [ ] `TestConnNameForRemote`: tests the reverse lookup including the
      "outbound preferred over inbound" behavior when both directions
      share a remoteID.
- [ ] `TestRegisterRemotes_RegistrationError`: server returns an error for
      one SiteURL; `OnActivate` returns the error; nothing is left in
      `p.remoteIDs`.

### 5. `server/configuration_test.go` (additions)

Cover the SiteURL lifecycle and per-direction uniqueness:

- [ ] `TestValidate_SameNameAcrossDirections`: inbound `high` and outbound
      `high` both pass; both default `SiteURL` to `"crossguard:high"`.
- [ ] `TestValidate_DuplicateNameSameDirection`: two inbound `high` connections
      fail with "duplicate name".
- [ ] `TestValidate_SiteURLDefaultedOnFirstSave`: empty `SiteURL` is set to
      `"crossguard:" + Name` after `validate()` completes.
- [ ] `TestValidate_SiteURLImmutableOnRename`: a connection saved with
      `SiteURL: "crossguard:high"` and later renamed to `low` keeps
      `SiteURL: "crossguard:high"`.
- [ ] `TestOnConfigurationChange_RegistersNewSiteURL`: harness add of a
      new connection invokes `RegisterPluginForSharedChannels` for its
      SiteURL.
- [ ] `TestOnConfigurationChange_UnregistersRemovedSiteURL`: removing a
      connection whose SiteURL is no longer referenced invokes
      `UnregisterPluginRemoteForSharedChannels(remoteID)`.
- [ ] `TestOnConfigurationChange_KeepsSharedSiteURL`: removing one side of
      a paired pair (the SiteURL is still referenced by the other side)
      does NOT unregister the remote.

### 6. `server/service_test.go` (additions)

Cover the share/invite/uninvite/unshare wiring:

- [ ] `TestInitChannel_SharesChannel`: linking the first connection to a
      channel calls `ShareChannel` and `InviteRemoteToChannel(channelID,
      remoteID, userID, true)` with the remoteID from `p.remoteIDs`.
- [ ] `TestInitChannel_AlreadyShared_StillInvites`: `ShareChannel` returns
      "already shared" error; the call is logged at Debug; init succeeds
      and `InviteRemoteToChannel` is still invoked.
- [ ] `TestInitChannel_NoRemoteIDsConfigured`: no `p.remoteIDs` entry for
      the connection (e.g. asymmetric outbound-only that has not yet
      registered); `ShareChannel` is NOT called; init still succeeds.
- [ ] `TestTeardownChannel_LastConn_UninvitesAndUnshares`: removing the
      last connection invokes `UninviteRemoteFromChannel` then
      `UnshareChannel`.
- [ ] `TestTeardownChannel_RemainingConnections_OnlyUninvites`: removing
      one of two connections invokes `UninviteRemoteFromChannel` for the
      removed remote only; `UnshareChannel` is NOT called.
- [ ] `TestTeardownChannel_NoRemotesRegistered_NoUnshare`: `p.remoteIDs`
      empty (test-mode plugin); the guard added in service.go skips
      `UnshareChannel`. (Documents the current production behavior so a
      future change does not regress it silently.)

### 7. `server/command_test.go` (additions)

- [ ] `TestExecuteCommand_InitChannel_CallsShareAndInvite`: when the slash
      command links a channel, the mock API records the
      `ShareChannel`/`InviteRemoteToChannel` calls.
- [ ] `TestExecuteCommand_TeardownChannel_CallsUninvite`: removing a
      channel connection records the `UninviteRemoteFromChannel` call.

### 8. `server/prompt_test.go` (additions)

- [ ] `TestHandlePromptAccept_ChannelLevel_CallsShareAndInvite`: accepting
      an unlinked-channel prompt (which calls
      `initChannelForCrossGuard`) records the `ShareChannel` and
      `InviteRemoteToChannel` mock calls.

### 9. `server/api_test.go` (additions / updates)

- [ ] `TestHandleTestNATSOutbound_RoundTrip`: replace the deleted "default
      format" assertion with a stricter check that the bytes published
      decode into a `TransportEnvelope` with `Type: "test"` and
      `TestID == response.id`.
- [ ] `TestHandleTestNATSInbound`: subscribe and verify a `Type: "test"`
      `TransportEnvelope` is delivered and decoded.

### 10. Update `server/test_helpers_test.go`

- [ ] Add a `mockQueueProvider` helper for capturing published bytes
      (currently each test rolls its own).
- [ ] Document, with a code comment, the convention that `p.remoteIDs`
      keys are direction-prefixed (`"outbound:<name>"` / `"inbound:<name>"`)
      so future test authors do not regress on this. (No production
      behavior change, just a comment.)

## Files to Modify / Create

| File | Action | Notes |
|---|---|---|
| `server/hooks_test.go` | Create | New tests for the four shared-channel hooks |
| `server/inbound_test.go` | Create | New tests for processInboundMessage / handleInboundSyncMsg |
| `server/connections_test.go` | Create | Tests for publishToOutboundConn |
| `server/plugin_test.go` | Modify | Multi-remote registration tests |
| `server/configuration_test.go` | Modify | SiteURL lifecycle and reconciliation tests |
| `server/service_test.go` | Modify | Share/invite/uninvite/unshare assertions |
| `server/command_test.go` | Modify | Init/teardown command exercises shared channel APIs |
| `server/prompt_test.go` | Modify | Accept-prompt path exercises ShareChannel/Invite |
| `server/api_test.go` | Modify | Test-connection round trip uses TransportEnvelope |
| `server/test_helpers_test.go` | Modify | Optional helper plus a clarifying comment |

No production code changes are expected. If a test reveals a real bug in the
refactor, fix the bug in the relevant source file and call it out in the PR
description (do not silently update the test to match buggy behavior).

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Tests assert against private helper signatures and break easily | Drive tests through the hook/handler entry points; treat the helpers as implementation detail. Use `setupTestPluginWithRouter` everywhere. |
| `plugintest.API` mock calls drift as the Mattermost server evolves | Use `mock.MatchedBy` for opaque struct args (e.g. `*model.SharedChannel`) so a new optional field on the server side does not break tests. |
| Concurrency in the inbound handler is hard to test deterministically | For the semaphore backpressure test, use `time.After(...)` + a buffered channel signaling "handler entered"; cancel the context to unblock. Avoid `time.Sleep` polling. |
| Multi-remote tests need a real `OnActivate` to thread through bot setup, bundle path, etc. | Where the wiring under test is just `registerRemotes`, call it directly with a hand-crafted `Plugin` (botUserID, configuration set). Reserve the full `OnActivate` path for one or two end-to-end happy/error tests. |

## Acceptance Criteria

- [ ] Every bullet under "Tasks" lands as a passing test.
- [ ] `make test` passes.
- [ ] `gofmt -l server/` is empty.
- [ ] `go vet ./...` is clean.
- [ ] No production code changes other than incidental comment additions
      and any genuine bug fixes uncovered by the new tests (called out in
      the PR description).
- [ ] Each new test asserts against a specific `errcode` constant where
      the behavior is supposed to log a specific code, so renames/renumbers
      of error codes show up as test failures.

## Decisions

| Question | Decision | Rationale |
|---|---|---|
| Re-import the deleted test files from git history vs. write fresh tests | Write fresh | The deleted files tested the old envelope-builder code paths; reusing them would mean grafting old assertions onto new behavior. |
| Use a shared `setupHookTestPlugin` helper or inline scaffolding per test | Shared helper | Reduces noise; `setupTestPluginWithRouter` already exists and we extend it for hook tests. |
| Drive multi-remote registration tests through `OnActivate` or `registerRemotes` directly | Both | One end-to-end test for `OnActivate` to lock in the wiring; the rest target `registerRemotes` and `reconcileRemotes` for speed and clarity. |
| Treat the lack of an `UnshareChannel` call when `p.remoteIDs` is empty as a feature | Yes (document with a test) | This guard exists in production; without it, every test that exercises teardown panics. The test pins the contract so a future refactor does not silently regress it. |
