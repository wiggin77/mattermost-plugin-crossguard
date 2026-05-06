# Augment Outbound SyncMsg.Users Inline to Defeat the Optimistic-Cursor Skew

**Date**: 2026-05-06
**Repo**: MattermostFederal/mattermost-plugin-crossguard
**Branch**: recv_shared_channels_apis (or a child branch)
**Context**: Cross Guard is built on the shared channels framework but transports its sync messages over fire-and-forget channels (NATS today; Azure Queue/Blob/Service Bus in some configurations). The framework records "synced" against a per-user / per-channel / per-remote cursor (`SharedChannelUsers.LastSyncAt`) as soon as the plugin's `OnSharedChannelsSyncMsg` returns nil. With a fire-and-forget transport, the plugin returns nil once the publish lands in the broker; it has no way to know whether the receiving side processed the user upsert successfully. If the receiver fails (because of misconfigured share state, an in-flight deploy, or any transient error), the cursor advances anyway and the user is silently skipped on subsequent syncs. Posts then fail forever on the receiving side with `error fetching user for post sync: resource "User" not found`.

This plan removes the dependency on the user-sync cursor by always inlining the user records that any outgoing post or reaction references, regardless of what the framework decided to include in `msg.Users`.

## Goals and non-goals

**Goals:**
- A post-author user record is always present in the SyncMsg the plugin publishes, even when the framework's optimistic cursor would have suppressed it.
- The receiving side processes users before posts (already true in the framework) and creates/updates synthetic users idempotently.
- No transport-layer changes. No bidirectional connectivity requirement. No server-side coordination.

**Non-goals:**
- This does not fix optimistic cursor skew for non-user dimensions (post edits, reactions, etc.). Those keep the existing semantics. The complementary fix is the recovery API (planned separately) and/or the full end-to-end ack design (option 1, also planned separately).
- This does not change the channel/remote handshake or the invite flow.
- This does not attempt to send mention targets that aren't post authors. Mentions are encoded as text in `Post.Message`; the receiver handles transformation. Augmenting only post authors and reaction/acknowledgement actors is sufficient for the failure mode we observed.

## Root cause recap

`shouldUserSync` (`mattermost/server/platform/services/sharedchannel/sync_send.go:588`) decides whether a user goes into the outbound `SyncMsg.Users`. After the framework returns from the plugin hook successfully, `recordUsersSync` advances `SharedChannelUsers.LastSyncAt`. On the next iteration, `shouldUserSync` returns false (because `user.UpdateAt <= scu.LastSyncAt`) and the user is dropped from the outbound payload. If the receiver never created the user the first time around, every subsequent post by that user fails on the receiver in `upsertSyncPost` (`sync_recv.go:509`).

The plugin can't change `recordUsersSync`, but it can ensure `msg.Users` carries the post author every time the post is sent. The receiver's `upsertSyncUser` (`sync_recv.go:310`) is idempotent: it patches existing users (when `RemoteId` matches) and creates them when missing. So always-include is safe.

## What to change

### File: `server/hooks.go`

Add a helper before publishing that augments `msg.Users` to cover every locally-resident user referenced anywhere in the SyncMsg.

```go
// augmentSyncMsgUsers walks msg.Posts/Reactions/Acknowledgements/Memberships
// /Statuses and ensures every locally-resident user referenced is present in
// msg.Users. Defeats the framework's optimistic-cursor skew over fire-and-
// forget transports: if the receiver dropped a user upsert silently, the
// next message that references them re-supplies the User record so the
// receiver can recreate them.
func (p *Plugin) augmentSyncMsgUsers(msg *mmModel.SyncMsg) *mmModel.SyncMsg {
    if msg == nil {
        return msg
    }

    // Collect user IDs referenced in the message.
    referenced := make(map[string]struct{})
    for _, post := range msg.Posts {
        if post == nil || post.UserId == "" {
            continue
        }
        referenced[post.UserId] = struct{}{}
    }
    for _, r := range msg.Reactions {
        if r == nil || r.UserId == "" {
            continue
        }
        referenced[r.UserId] = struct{}{}
    }
    for _, a := range msg.Acknowledgements {
        if a == nil || a.UserId == "" {
            continue
        }
        referenced[a.UserId] = struct{}{}
    }
    for _, m := range msg.MembershipChanges {
        if m == nil || m.UserId == "" {
            continue
        }
        referenced[m.UserId] = struct{}{}
    }
    for _, s := range msg.Statuses {
        if s == nil || s.UserId == "" {
            continue
        }
        referenced[s.UserId] = struct{}{}
    }

    // Drop the ones the framework already included.
    existing := make(map[string]struct{}, len(msg.Users))
    for _, u := range msg.Users {
        if u != nil {
            existing[u.Id] = struct{}{}
            delete(referenced, u.Id)
        }
    }

    if len(referenced) == 0 {
        return msg
    }

    // Defensive copy: never mutate the framework's SyncMsg directly. Shallow
    // copy is sufficient because only the Users slice is being extended.
    augmented := *msg
    augmented.Users = append([]*mmModel.User{}, msg.Users...)

    added := 0
    for uid := range referenced {
        user, appErr := p.API.GetUser(uid)
        if appErr != nil || user == nil {
            // User no longer exists locally (e.g. deactivated and purged).
            // Skip; the receiver will fail this single post but the rest
            // of the message proceeds. Log so operators can spot the
            // pattern.
            p.API.LogWarn("Cannot fetch local user for outbound sync augmentation",
                "error_code", errcode.OutboundSyncMsgUserLookupFailed,
                "user_id", uid)
            continue
        }
        // Don't sync users that originated from the remote (already remote
        // synthetic users from a previous inbound sync). The framework
        // already filters these out via shouldUserSync (line 590-592 in
        // sync_send.go), but we re-apply the rule defensively because we
        // bypass the cursor.
        if user.RemoteId != nil && *user.RemoteId != "" {
            continue
        }
        augmented.Users = append(augmented.Users, user)
        added++
    }

    if added > 0 {
        p.API.LogInfo("Augmented outbound SyncMsg with referenced users",
            "error_code", errcode.OutboundSyncMsgUsersAugmented,
            "channel_id", msg.ChannelId, "added", added,
            "total_users", len(augmented.Users))
    }

    return &augmented
}
```

In `OnSharedChannelsSyncMsg`, call the helper between the existing nil/connection checks and the envelope construction:

```go
func (p *Plugin) OnSharedChannelsSyncMsg(
    msg *mmModel.SyncMsg,
    rc *mmModel.RemoteCluster,
) (mmModel.SyncResponse, error) {
    if msg == nil { ... }
    if rc == nil { ... }

    connName := p.connNameForRemote(rc.RemoteId)
    if connName == "" { ... return ... }

    if !p.hasOutboundProvider(connName) {
        return buildSyncResponse(msg), nil
    }

    // ... existing channel/team lookup ...

    // INSERT HERE: augment users before publishing.
    augmented := p.augmentSyncMsgUsers(msg)

    env := &TransportEnvelope{
        Version:     1,
        Type:        TransportTypeSyncMsg,
        ConnName:    connName,
        TeamName:    team.Name,
        ChannelName: channel.Name,
        SyncMsg:     augmented,  // was: msg
    }
    // ... rest unchanged ...
}
```

### File: `server/errcode/codes.go`

Add two new error codes in the `hooks.go` block (10000-10999):

```go
OutboundSyncMsgUserLookupFailed = 10108 // GetUser failed during user augmentation
OutboundSyncMsgUsersAugmented   = 10109 // info: augmented msg.Users with N additions
```

Append both to `AllCodes`.

### Tests: `server/hooks_test.go` (or new file `server/hooks_augment_test.go`)

Add a test suite for `augmentSyncMsgUsers`:

1. **No referenced users**: empty `msg.Posts/Reactions/etc.` → returns the same SyncMsg pointer (or an unchanged copy), no `GetUser` calls.
2. **Post author already in msg.Users**: framework included the user → no augmentation, no `GetUser` calls for that ID.
3. **Post author not in msg.Users**: helper looks up the user, appends to `augmented.Users`, returns a new SyncMsg with the added user.
4. **Multiple referenced users across posts/reactions/memberships**: dedup against each other and against `msg.Users`; expect each unique missing ID to result in exactly one `GetUser` call.
5. **Local user has `RemoteId` set (synthetic remote user)**: helper skips them; they're not added to `augmented.Users`.
6. **`GetUser` returns error**: helper logs warn, skips that user, continues with others.
7. **Defensive copy**: mutations to `augmented.Users` don't show up in `msg.Users`.

Existing `OnSharedChannelsSyncMsg` integration tests should be updated to expect `GetUser` calls for the post author when the user isn't already in the SyncMsg.

## Implementation order

1. Add `errcode` constants and update `AllCodes`. (Trivial, isolated.)
2. Implement `augmentSyncMsgUsers` with unit tests. (Pure function, easy to TDD.)
3. Wire it into `OnSharedChannelsSyncMsg`. Update integration tests for the hook to mock `GetUser`.
4. `make check-style && make test` clean.
5. Deploy to a single-server test environment and verify with the existing DIAG logs that augmented messages carry the user count we expect.
6. Smoke-test the scenario that prompted this fix: post a fresh message from a user whose `SharedChannelUsers.LastSyncAt` is "ahead" of reality on the receiver, confirm receiver creates the user and accepts the post.

## Acceptance criteria

- A post sync message published by Cross Guard always contains the post author's User record in `msg.Users`, regardless of whether the framework included them.
- Reaction, acknowledgement, status, and membership-change actors are also carried in `msg.Users` when they're locally-resident (i.e., not already remote synthetic users).
- Receiver upserts the inlined users idempotently and proceeds with post processing without "User not found" errors.
- A locally-deleted user (`GetUser` returns NotFound) is logged and skipped; other entries in the same SyncMsg are still published.
- The framework's SyncMsg is not mutated in place; the augmentation produces a copy.
- Test suite covers the seven scenarios listed above.

## Testing notes

- Unit tests use the existing `plugintest.API` mock to assert `GetUser` calls.
- Integration with the embedded NATS server should remain green; the augmentation is invisible at the transport layer.
- Manual recovery test: on a server pair where the user-sync cursor is known to be ahead of reality (e.g., simulate by setting `SharedChannelUsers.LastSyncAt` to current time without ever delivering the user record), post a fresh message from that user, verify the post lands on the remote.
- Regression check: confirm an existing real-user already on both sides does not get re-created or have its `RemoteId` flipped on every post (the receiver's `upsertSyncUser` should patch in place when `RemoteId` matches).

## Risks and limitations

- **Bigger payloads.** Every post sync now carries at minimum one User record (the author), even when the receiver already has it. For typical usage (1-3 users in a SyncMsg), this is at most a few KB extra per message. Negligible against the existing post payload size.
- **Mention transformation isn't covered.** Posts can mention users who are neither the author nor a reactor. The framework currently sends mention transforms (`MentionTransforms`) via the SyncMsg, and the receiver re-renders mentions accordingly. We do not augment for mention targets in this plan because they require parsing the post body and looking up by username, extra cost with no observed benefit because the receiver's mention transforms are independent of `msg.Users`. If we later see the same cursor-skew failure pattern affect mentioned users (e.g., `@mentioned-user` rendered as text rather than a link on the receiver), revisit and add mention augmentation.
- **Remote-origin users (`User.RemoteId != ""`) are deliberately skipped.** This matches the framework's own `shouldUserSync` filter (line 590-592) and avoids accidentally trying to "ship back" a synthetic user to its origin. If a real local user somehow has a non-empty `RemoteId` set (shouldn't happen on poc1's side under normal operation), they would be dropped from the augmentation. Worth a one-line warn log if we detect this edge case.
- **Optimistic cursor skew still applies to other dimensions.** Edits to existing posts, reactions, profile-image syncs, and channel-membership syncs can still suffer the same fundamental issue. This plan only fixes the user dimension because that's the failure path that bites posts and produces the most visible breakage. Full coverage requires option 1 (end-to-end ack) or option 2 (server-side cursor callback).
- **Receiver's `RemoteId` mismatch.** If the receiver already has a user with the same Id but a different `RemoteId` (because the user was created from a different remote earlier), `upsertSyncUser` returns `ErrRemoteIDMismatch`. The framework currently logs and skips that user but proceeds with the rest of the message. We should confirm the receiver's behavior in this edge case is acceptable; the plan doesn't change it.

## Follow-on work

After this lands:
- Document the new behavior in the plugin's contract section (the README's architecture section already describes `OnSharedChannelsSyncMsg`).
- File a parallel ticket for option 1 (end-to-end ack) to address the residual cursor-skew dimensions.
- File a parallel ticket for option 3 (recovery API: `/crossguard resync-channel`) for cases where state has already diverged before this fix is deployed (existing customers upgrading).
- Strip the temporary `DIAG:` logs once we've confirmed the fix in production.
