# Integration test coverage gaps

## Context

`make docker-integration-test` now exercises all four QueueProvider
implementations end-to-end (NATS JSON, NATS XML, Azure Queue, Azure Blob,
Azure Service Bus) on a cross-server topology (Server A outbound to Server B
inbound). The smoke test plus the rewrite-team, file attachment, and per-
provider tests prove that the basic happy path works.

Several user-visible behaviors and recently-added defenses are not covered
by the docker integration suite. They are validated only by Go unit tests,
or in some cases only by manual verification against the poc1 / poc2
instances. This plan lists the gaps and proposes a sequenced approach to
close them.

The gaps below were identified from a transport coverage audit on
2026-05-09 after the cross-server rewrite of the Azure tests.

## Gap 1: Post lifecycle (edits, deletes, reactions)

### Why this matters

The framework calls `OnSharedChannelsSyncMsg` for any post change, not just
creation. If the plugin or framework regresses the edit, delete, or reaction
path, the receiver silently drifts out of sync. Today only post creation is
verified by the smoke test.

### Approach

Extend the NATS smoke test path with four follow-up operations on the post
created during the smoke test, each with its own assertion:

- **Edit**: PATCH the post on Server A with new message text. Poll Server B
  for the post by content match, then assert `update_at > create_at` and
  the message field reflects the new text.
- **Delete**: DELETE the post on Server A. Poll Server B and assert the
  post returns `delete_at != 0`.
- **Add reaction**: POST a reaction to the relayed post on Server A. Poll
  the reactions endpoint on Server B (`/api/v4/posts/{id}/reactions`) and
  assert the emoji is present.
- **Remove reaction**: DELETE the reaction on Server A. Poll and assert the
  reaction is gone.

### Test target

Add a new target `docker-post-lifecycle-test` invoked from
`docker-integration-test` immediately after `docker-smoke-test`. Keeping it
separate from the smoke test makes failures easier to localize.

### Caveats

The post on Server B is owned by a sync user and has a different post id
than the original. Resolve B's post id once on first match (by message
content), then use that id for the edit / delete / reaction assertions.

## Gap 2: Profile image sync

### Why this matters

Profile image sync is a separate code path:
`OnSharedChannelsProfileImageSyncMsg` -> `UploadFile` with
`kind=profile_image` -> file watcher -> `ReceiveSharedChannelProfileImageSyncMsg`.
It was validated manually on poc1 / poc2 but is not in CI. A regression
would not be caught.

### Approach

New target `docker-profile-image-test` run after the smoke test has wired
the `low-to-high` connection.

1. Create a fresh user (e.g. `userg`) on Server A and add to the smoke
   test channel so the framework sync's the user to Server B.
2. Verify Server B has the synthetic user `userg.low-to-high` with the
   default avatar. Capture the initial `last_picture_update`.
3. POST a new profile image via `/api/v4/users/{id}/image` on Server A.
4. Poll Server B for `userg.low-to-high`. Assert `last_picture_update`
   advances. Optionally GET the image bytes via
   `/api/v4/users/{id}/image` on Server B and assert the byte length
   matches what was uploaded.

### Caveats

Profile image sync only fires when the framework's sync engine notices the
change. The user must already be a member of a linked shared channel so
the framework knows where to propagate the update.

## Gap 3: Connection prompt accept / block

### Why this matters

When an inbound message arrives for an unlinked channel or team, the plugin
posts an interactive Accept / Block prompt to admins. This is the operator-
facing happy path for first-time-link scenarios, and it is not exercised
end-to-end today.

### Approach

New target `docker-prompt-test` with two sub-cases.

**Accept path:**

1. Create channel `prompt-test` on both servers. Init both teams. Do NOT
   init the channel on Server B.
2. Post on Server A's `prompt-test`.
3. Poll Server B's admin DM (or wherever the plugin posts prompts, see
   `prompt.go`) for an interactive prompt referencing `low-to-high`.
4. Hit the accept endpoint:
   `POST /plugins/crossguard/api/v1/prompt/channel/accept`.
5. Verify the channel is now linked on Server B (e.g. via
   `/plugins/crossguard/api/v1/channels/{id}/status`).
6. Post a follow-up message on Server A. Verify it relays normally.

**Block path:**

1. Same setup, fresh channel name (e.g. `prompt-test-blocked`).
2. Post on Server A.
3. Hit the block endpoint:
   `POST /plugins/crossguard/api/v1/prompt/channel/block`.
4. Post a follow-up on Server A. Verify it does NOT relay.
5. Optional: verify the prompt's "blocked" state is visible via the
   status endpoint.

### Code touchpoints

- `prompt.go` for the prompt format and KV state machine.
- `api.go` for the accept / block endpoint signatures.

## Gap 4: File filter mode and size limits (sender + receiver)

### Why this matters

The receiver-side file filter / disable gate was added recently (defense in
depth: a misconfigured or compromised sender must not be able to push files
the receiver has disabled or denied). It has Go unit tests but no
integration coverage that proves the file watcher path actually drops the
filtered files.

### Approach

New target `docker-file-filter-test` with three sub-tests, each restoring
config at the end so the smoke test channel remains usable.

**Sub-test A: sender-side deny mode**

1. PATCH Server A's `low-to-high` config to add
   `file_filter_mode=deny`, `file_filter_types=["pdf"]`. Reset plugin on A.
2. Post a PDF on Server A's smoke test channel.
3. Poll Server B for the post (text always relays, so the post should
   arrive). Assert the post has zero `file_ids`.
4. Flip the filter to `["txt"]`. Post a PDF. Assert the PDF DOES relay.
5. Restore: drop the filter from Server A's config. Reset plugin on A.

**Sub-test B: receiver-side deny mode**

1. With sender filter cleared, PATCH Server B's `low-to-high` inbound
   config to set `file_filter_mode=deny`, `file_filter_types=["pdf"]`.
   Reset plugin on B.
2. Post a PDF on Server A.
3. Verify the post arrives on B but with zero `file_ids` (receiver
   dropped the file).
4. Verify Server B's plugin log contains an `errcode.InboundAttachmentFiltered`
   entry for the dropped file.
5. Restore: drop the filter from Server B's inbound config. Reset plugin
   on B.

**Sub-test C: sender-side size limit** -- NOT IMPLEMENTABLE AS SPECIFIED

The plan assumed a per-connection `max_file_size` field. The plugin actually
reads the server's global `FileSettings.MaxFileSize` (see `hooks.go`
`maxFileSize()` and the `fi.Size > maxSize` check). Lowering the global
setting after upload to verify the relay-time check is possible but is global
shared state, which makes integration coverage fragile. Size enforcement is
covered by Go unit tests in `hooks_test.go`; leaving it there.

### Caveats

Each sub-test requires a plugin reset on at least one side. Build a small
helper to patch + reset + sleep (it's the same pattern the Azure tests
already use). Restore at end-of-sub-test so failures do not poison
subsequent tests.

## Gap 5: Service Bus file relay

### Why this matters

Service Bus is the only transport in the integration suite where files are
not exercised. If a customer ever turns on `file_transfer_enabled=true` for
production Service Bus, today we would ship blind.

### Approach

Investigation result: `UploadFile` and `WatchFiles` in
`azure_servicebus_provider.go` are real implementations that mirror the
Azure Queue provider's Blob Storage sidecar pattern. The config exposes
`blob_service_url`, `blob_account_name`, `blob_account_key`, and
`blob_container_name` on `azure_servicebus`. Extended
`docker-servicebus-smoke-test` with a PDF file sub-test pointing at the
Azurite emulator (container `crossguard-servicebus-files`).

### Out-of-scope flag

If Service Bus does not support file transfer at all, document that fact
and skip this gap. It becomes a feature ticket only when a customer
requests it.

## Suggested ordering

1. **Gap 1: post lifecycle** -- biggest coverage gap relative to
   user-visible behavior, smallest implementation cost. Reuses the smoke
   test connection wiring.
2. **Gap 2: profile image** -- second-biggest gap, already manually
   verified, just needs to land in CI.
3. **Gap 4: file filter and size limits** -- moderate cost, exercises a
   recently-added defense-in-depth path.
4. **Gap 3: prompt accept / block** -- most state-machine-heavy, biggest
   test code, lowest risk of regression in current code.
5. **Gap 5: Service Bus file relay** -- lowest priority. Investigation
   first; may or may not turn into a test task.

## Out of scope for this plan

- Channel-name rewrite. The plugin only supports team-name rewrite via
  `rewrite-team`. There is no channel-name rewrite feature. If one is
  added later, it will need its own test.
- HA / multi-node coverage. The docker dual-server topology is two single-
  node servers, not an HA cluster. NATS queue groups (which the plugin
  uses for multi-node fan-out prevention) are not exercised here.
- Connection health tracking and reconnection (`updateOutboundHealth`,
  health recheck interval). Best covered by Go unit tests or a chaos test
  rig, not the docker integration suite.
