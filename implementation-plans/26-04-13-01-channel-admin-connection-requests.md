# Allow Channel Admins to Request Connections from Team Admins

## Context

The plugin already has "Allow Team Admins to Request Connections" where team admins submit team-level link requests routed to system admins. This feature adds the same pattern at the channel level: channel admins submit channel-level link requests routed to team admins for approval.

This creates a complete hierarchical delegation chain:
- System admins can do everything directly
- Team admins can request team connections from system admins (existing)
- Channel admins can request channel connections from team admins (new)

## Problem Statement

When `RestrictToSystemAdmins` is enabled, channel admins cannot link channel connections at all. With team admin requests enabled, team admins can at least request team links. But channel admins, who manage day-to-day channel operations, are completely locked out of channel connection management. This feature lets channel admins request channel connections with team admin oversight.

## Current State

- `isChannelAdminOrHigher()` (command.go:843) blocks channel admins when `RestrictToSystemAdmins` is on
- `executeInitChannel()` (command.go:527) and `handleInitChannel()` (api.go:422) both use this check
- No request-mode fallback exists for channel-level operations
- `CrossguardChannelModal.tsx` has no request pending or request mode UI

### Pre-existing Gap: Team Admin Channel Permissions

When `RestrictToSystemAdmins=ON + AllowTeamAdminRequests=ON`, team admins can request team-level connections, but once approved, they cannot link channels because `isChannelAdminOrHigher` delegates to `isTeamAdminOrSystemAdmin` which returns false for team admins when restricted. This feature must also fix this gap: team admins in request mode should be able to link/unlink channels directly (they already have team-level authority via the request workflow).

### Reference Patterns (existing team admin requests)
- `request.go` - full request lifecycle (create, approve, deny, cancel)
- `store/store.go:8-16` - `ConnectionRequest` struct
- `store/client.go:373-402` - KV operations with CAS
- `api.go:39-41` - API route registration
- `command.go:264-268` - request mode fallback in `executeInitTeam`
- `api.go:372-394` - request mode fallback in `handleInitTeam`
- `service.go:139-162` - DM message builders
- `CrossguardTeamModal.tsx:465-502` - request pending/request mode button rendering

## Design Principles

| Pattern | Our Approach | Avoid | Reference |
|---------|-------------|-------|-----------|
| File organization | New `channel_request.go` | Extending `request.go` | Keeps team/channel separation clean |
| Approver lookup | Team admins via `GetTeamMembers` + `SchemeAdmin` | System admin lookup | `getSystemAdmins()` as pattern |
| Permission hierarchy | Channel admins request, team admins + sysadmins approve | Channel admins self-approving | `request.go:210` sysadmin check |
| Approver permission | Direct team membership check (bypasses RestrictToSystemAdmins gate) | Using `isTeamAdminOrSystemAdmin` (blocked when restricted) | `command.go:515` restriction gate |
| Settings | Independent `AllowChannelAdminRequests` flag | Coupling to `AllowTeamAdminRequests` | `plugin.json:59-64` |
| Store reuse | Extend existing `ConnectionRequest` with optional `ChannelID` | Separate struct | Avoids duplication |
| KV key format | `{pluginID}-chanconnreq-{channelID}-{connKey}` | Reusing team request prefix | `client.go:33` |
| DM builders | Parameterize existing builders with optional channel info | Duplicating into separate functions | Keeps single source of truth, saves ~30-40 lines |

## Requirements

- [ ] New `AllowChannelAdminRequests` setting (bool, default false)
- [ ] Channel admins in request mode can submit channel link requests
- [ ] Requests are DM'd to team admins with Approve/Deny buttons
- [ ] Team admins AND system admins can approve/deny
- [ ] Channel admins in request mode can unlink directly (no approval needed)
- [ ] Team admins in request mode can link/unlink channels directly (fix pre-existing gap)
- [ ] Pending requests auto-cancel on channel teardown and team teardown
- [ ] Channel modal shows request pending/request mode state
- [ ] Error codes in 25000-25999 range for channel_request.go

## Out of Scope

- Nested request chains (channel admin request requiring team admin who also needs to request)
- Notification preferences for team admins
- Bulk approve/deny UI
- Channel admin requesting team-level connections

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| Who can approve? | Team admins + system admins | New `isTeamAdminDirect` helper that bypasses `RestrictToSystemAdmins` gate (unlike `isTeamAdminOrSystemAdmin` at command.go:515 which returns false for team admins when restricted) |
| Separate file? | Yes, `channel_request.go` | Keeps team/channel separation clean, avoids 1000-line request.go |
| Settings coupling? | Independent from `AllowTeamAdminRequests` | A deployment may want channel-level delegation without team-level |
| Team admin in channel request mode? | No, excluded | Team admins get direct channel permission when `AllowTeamAdminRequests` is ON |
| Deny dialog state format | `channelID\|teamID\|ck` (3 parts) | Need both IDs for lookup and permission check |
| Shared vs separate struct? | Extend `ConnectionRequest` with optional `ChannelID` | Avoids duplicate struct, same JSON shape with one extra field |
| Team teardown cascade? | Yes, cancel pending channel requests | Prevents orphaned channel requests when team connection is removed |
| Channel deleted while pending? | Approve handler verifies channel exists | Same pattern as connection-removed-from-config check |

## Technical Approach

### Task 1: Configuration (plugin.json + configuration.go)

**plugin.json** - Add after `AllowTeamAdminRequests` (line 64):
```json
{
    "key": "AllowChannelAdminRequests",
    "display_name": "Allow Channel Admins to Request Channel Connections",
    "type": "bool",
    "default": false,
    "help_text": "When enabled (and 'Restrict Connection Management to System Admins' is also enabled), channel admins can submit channel connection link requests for team admin approval via direct message and can unlink channel connections directly. When disabled, channel admins are blocked entirely."
}
```

**configuration.go** - Add to `configuration` struct (line 141):
- `AllowChannelAdminRequests *bool` field
- `isChannelAdminRequestsAllowed()` helper (mirrors `isTeamAdminRequestsAllowed`)
- `isChannelRequestMode()` helper - returns `isRestrictedToSystemAdmins() && isChannelAdminRequestsAllowed()`

### Task 2: Store Layer (store/store.go + store/client.go)

**store/store.go** - Extend `ConnectionRequest` (line 10) with optional `ChannelID`:
```go
type ConnectionRequest struct {
    RequesterID string   `json:"requester_id"`
    TeamID      string   `json:"team_id"`
    ChannelID   string   `json:"channel_id,omitempty"`  // NEW: empty = team-level request
    ConnKey     string   `json:"conn_key"`
    PostIDs     []string `json:"post_ids"`
    CreatedAt   int64    `json:"created_at"`
}
```

Add to `KVStore` interface (after line 73):
- `GetChannelConnectionRequest(channelID, connKey string) (*ConnectionRequest, error)`
- `CreateChannelConnectionRequest(channelID, connKey string, req *ConnectionRequest) (bool, error)`
- `DeleteChannelConnectionRequest(channelID, connKey string) error`

**store/client.go**:
- Add `chanConnRequestPrefix` field to `Client` struct
- Initialize: `chanConnRequestPrefix: pluginID + "-chanconnreq-"`
- Implement 3 methods following the existing `ConnectionRequest` KV pattern (client.go:373-402)
- Key: `chanConnRequestPrefix + channelID + "-" + connKey`

### Task 3: Error Codes (errcode/codes.go)

Add block for `channel_request.go` (range 25000-25999):
```go
ChanRequestGetFailed             = 25000
ChanRequestGetDMChannelFailed    = 25002
ChanRequestCreateDMPostFailed    = 25003
ChanRequestSaveFailed            = 25004
ChanRequestApproveGetFailed      = 25005
ChanRequestApproveExecFailed     = 25006
ChanRequestApproveDeleteFailed   = 25007
ChanRequestApproveNotifyFailed   = 25008
ChanRequestDenyDialogFailed      = 25009
ChanRequestDenyGetFailed         = 25010
ChanRequestDenyDeleteFailed      = 25011
ChanRequestDenyNotifyFailed      = 25012
ChanRequestUpdatePostFailed      = 25013
ChanRequestNoTeamAdmins          = 25014
ChanRequestConnRemovedFromConfig = 25015
ChanRequestConfirmDMFailed       = 25016
ChanRequestNoAdminsNotified      = 25017
ChanRequestGetTeamAdminsFailed   = 25018
```

Add all to `AllCodes` slice.

### Task 4: DM Message Builders (service.go)

**New helper** (add before `buildRequestDMMessage`):
- `channelLink(channel *model.Channel, team *model.Team) string` - returns markdown link like `[**#channel-name**](/team/channels/channel-name)`

**Parameterize existing builders** instead of creating separate functions:

Update `buildRequestDMMessage` signature:
```go
func buildRequestDMMessage(userLabel, teamLink, ck string, connMap map[string]ConnectionConfig, channelLink string) string
```
- If `channelLink != ""`, use header `#### :link: Channel Connection Link Request` (instead of `#### :link: Connection Link Request`)
- After calling `writeConnDetailRows`, add `| **Channel** | channelLink |` row when non-empty
- Existing team-level callers pass `""` for `channelLink`

Update `buildRequestConfirmationMessage` signature:
```go
func buildRequestConfirmationMessage(teamLink, ck string, connMap map[string]ConnectionConfig, channelLink, approverLabel string) string
```
- Use `approverLabel` in the text: "Your request has been sent to {approverLabel} for approval."
- Existing team-level callers pass `""` for `channelLink` and `"system admins"` for `approverLabel`
- Channel-level callers pass the channel link and `"team admins"`
- Add `| **Channel** |` row when `channelLink` is non-empty

**Update existing callers in request.go**:
- `request.go:79` - pass `""` for channelLink
- `request.go:172` - pass `""` for channelLink and `"system admins"` for approverLabel

**Update existing tests in service_test.go** (lines 149, 164, 175, 180, 196, 216, 224) - add `""` channelLink / `"system admins"` approverLabel args

### Task 5: Core Logic (new file: channel_request.go)

1. **`isChannelAdminInRequestMode(userID, channelID, teamID) bool`**
   - Get user; return false for sysadmins
   - Return false if `!isChannelRequestMode()`
   - Get team member; return false if team admin (`SchemeAdmin`) since team admins get direct channel permission via Task 7
   - Get channel member; return `member.SchemeAdmin`

2. **`isTeamAdminDirect(userID, teamID string) bool`** (new helper)
   - Get user; if `user.IsSystemAdmin()` return true
   - Get team member via `GetTeamMember(teamID, userID)`; return `member.SchemeAdmin`
   - **Does NOT check `isRestrictedToSystemAdmins()`** (unlike `isTeamAdminOrSystemAdmin` at command.go:515 which returns false for team admins when restricted)
   - This is needed because channel request mode requires `RestrictToSystemAdmins=ON`, but the approve/deny handlers must still allow team admins to act

3. **`getTeamAdmins(teamID string) ([]*model.User, error)`**
   - Paginate `GetTeamMembers(teamID, page, 200)`
   - Filter `SchemeAdmin == true`, call `GetUser` for each
   - Include all team admins (sysadmins who are also team admins included)

4. **`createChannelConnectionRequest(user, channelID, teamID string, conn store.TeamConnection) (string, error)`**
   - Check for existing via `GetChannelConnectionRequest`
   - Get channel via `GetChannel(channelID)` and team via `GetTeam(teamID)` for display
   - Verify team has the connection linked (prerequisite check)
   - Call `getTeamAdmins(teamID)` for approvers
   - Build DM with `buildRequestDMMessage(userLabel, teamLink, ck, connMap, channelLink)` (parameterized, not separate function)
   - Buttons: `/plugins/{id}/api/v1/channel-request/approve` and `deny`
   - Context: `channel_id`, `team_id`, `conn_key`
   - Store via `CreateChannelConnectionRequest(channelID, ck, req)` where req has ChannelID set
   - Send confirmation via `buildRequestConfirmationMessage(teamLink, ck, connMap, channelLink, "team admins")`

5. **`handleChannelRequestApprove(w, r)`**
   - Extract `channel_id`, `team_id`, `conn_key` from context
   - Permission: **`isTeamAdminDirect(user.Id, teamID)`** (NOT `isTeamAdminOrSystemAdmin`, which is blocked when restricted)
   - Validate connection still in config and team has it linked
   - Verify channel still exists via `GetChannel(channelID)`
   - Execute: `initChannelForCrossGuard(user, channelID, conn)`
   - Delete request, update posts, notify requester

6. **`handleChannelRequestDeny(w, r)`**
   - Permission: **`isTeamAdminDirect`**
   - Open dialog, state: `channelID|teamID|ck`
   - URL: `/plugins/{id}/api/v1/channel-request/deny-submit`

7. **`handleChannelRequestDenySubmit(w, r)`**
   - Parse 3-part state via `SplitN(state, "|", 3)`
   - Permission: **`isTeamAdminDirect(user.Id, teamID)`**
   - Delete, update posts, notify requester

8. **`cancelPendingChannelConnectionRequest(channelID, ck string)`**
   - Same pattern as `cancelPendingConnectionRequest` using channel KV methods

**Reuse from request.go** (do NOT duplicate):
- `updateRequestPosts(postIDs, extraRows)` (request.go:459) - already generic, works for both team and channel requests
- `notifyRequester(requesterID, message)` (request.go:479) - already generic

### Task 6: API Routes (api.go)

Register after existing request routes (after line 41):
```go
router.HandleFunc("/api/v1/channel-request/approve", p.handleChannelRequestApprove).Methods(http.MethodPost)
router.HandleFunc("/api/v1/channel-request/deny", p.handleChannelRequestDeny).Methods(http.MethodPost)
router.HandleFunc("/api/v1/channel-request/deny-submit", p.handleChannelRequestDenySubmit).Methods(http.MethodPost)
```

Modify `handleInitChannel()` (api.go:440) - add fallback chain after permission check:
```go
if !p.isChannelAdminOrHigher(user.Id, channelID, channel.TeamId) {
    if p.isChannelAdminInRequestMode(user.Id, channelID, channel.TeamId) {
        // verify team has connection, resolve name, create channel request
        // return {status: "request_submitted", message: ...}
    }
    writeJSONError(w, "insufficient permissions", http.StatusForbidden)
    return
}
```

Modify `handleTeardownChannel()` (api.go:495) - allow channel admins in request mode + cancel pending:
```go
if !p.isChannelAdminOrHigher(user.Id, channelID, channel.TeamId) {
    if !p.isChannelAdminInRequestMode(user.Id, channelID, channel.TeamId) {
        writeJSONError(w, "insufficient permissions", http.StatusForbidden)
        return
    }
}
// ... after successful teardown:
p.cancelPendingChannelConnectionRequest(channelID, connKey(connName))
```

### Task 7: Command Changes (command.go)

**Fix pre-existing gap: team admins in request mode need channel permission.**

Modify `executeInitChannel()` (command.go:527-528) - add two-level fallback:
```go
if !p.isChannelAdminOrHigher(args.UserId, args.ChannelId, args.TeamId) {
    // Level 1: Team admins in request mode get direct channel permission
    if p.isTeamAdminInRequestMode(args.UserId, args.TeamId) {
        // Fall through to normal init-channel flow (no request needed)
    } else if p.isChannelAdminInRequestMode(args.UserId, args.ChannelId, args.TeamId) {
        // Level 2: Channel admins submit a request
        return p.executeInitChannelRequest(args)
    } else {
        return respondEphemeral("You must be a member of this channel and a channel admin, team admin, or system admin.")
    }
}
```

Add `executeInitChannelRequest(args)` - mirrors `executeInitTeamRequest` (command.go:307-337):
- Gets user, verifies team has connection linked (prerequisite)
- Resolves connection from team connections
- Calls `createChannelConnectionRequest(user, args.ChannelId, args.TeamId, conn)`

Modify `executeTeardownChannel()` (command.go:587-588):
- Allow team admins in request mode AND channel admins in request mode to teardown:
```go
if !p.isChannelAdminOrHigher(args.UserId, args.ChannelId, args.TeamId) {
    if !p.isTeamAdminInRequestMode(args.UserId, args.TeamId) &&
       !p.isChannelAdminInRequestMode(args.UserId, args.ChannelId, args.TeamId) {
        return respondEphemeral(...)
    }
}
```
- After successful teardown, call `cancelPendingChannelConnectionRequest(args.ChannelId, connKey(connName))`

**Team teardown cascade**: Modify `executeTeardownTeam()` (command.go:662-668). After the team connection is unlinked and `cancelPendingConnectionRequest` is called, also cancel pending channel requests for that connection across channels. This requires iterating channels, but since we do not have a channel list index, we can skip the cascade for now and handle it in the approve handler (validate team still has the connection). Add a comment noting this is handled at approval time.

### Task 7.5: Self-Healing on Direct Channel Link (service.go)

When a team admin or sysadmin links a channel directly (bypassing the request flow), any pending channel request for that channel+connection should auto-cancel. Add to `initChannelForCrossGuard` (after successful link, before returning):

```go
// Auto-cancel any pending channel request for this connection
if p.getConfiguration().isChannelRequestMode() {
    p.cancelPendingChannelConnectionRequest(channelID, connKey(conn))
}
```

This prevents "ghost" DM buttons from lingering after a direct link. Callers: `api.go:463`, `api.go:704`, `prompt.go:347`.

### Task 8: Channel Status Updates (service.go)

Add `ChannelRequestMode bool` field to `ChannelStatusResponse` (service.go:406):
```go
ChannelRequestMode bool `json:"channel_request_mode,omitempty"`
```

Update `getChannelStatus()` (service.go:431) signature to accept `callingUser *model.User`:
- If `isChannelRequestMode()` is on and caller is not team admin/sysadmin for this team, set `ChannelRequestMode: true`
- For unlinked connections when `isChannelRequestMode()`, check `GetChannelConnectionRequest(channelID, key)` and set `RequestPending: true`

**Callers to update** (only one production caller):
- `handleChannelStatus()` (api.go:781) - pass user to `getChannelStatus`
- Tests in `service_test.go` (lines 1410, 1425, 1441, 1468, 1503, 1526, 1542, 1985) - update to pass a test user

### Task 9: Frontend (CrossguardChannelModal.tsx)

Add `request_pending?: boolean` to `ConnectionStatus` interface (line 6-16).
Add `channel_request_mode?: boolean` to `ChannelStatusResponse` interface (line 18-24).

Add state:
```typescript
const [requestMode, setRequestMode] = React.useState(false);
```

In `fetchStatus`, set `requestMode` from `data.channel_request_mode`.

Update button rendering (lines 403-419) to match team modal pattern (CrossguardTeamModal.tsx:465-502):
- If `conn.request_pending`: disabled "Request Pending" button with title "A connection request is awaiting team admin approval"
- If `requestMode` and not linked: "Request Link" / "Requesting..." labels
- Otherwise: existing Link/Unlink buttons

In `handleToggle`, check for `data.status === 'request_submitted'` to show approval success message.

### Task 10: Tests

**New file: `server/channel_request_test.go`** - mirrors `request_test.go`:
- `TestIsChannelAdminInRequestMode` - sysadmin false, team admin false, channel admin with mode on true, mode off false
- `TestIsTeamAdminDirect` - team admin returns true even when restricted, sysadmin true, regular user false
- `TestCreateChannelConnectionRequest` - happy path, duplicate, no team admins, team missing connection
- `TestGetTeamAdmins` - happy path, empty, mixed with sysadmins
- `TestHandleChannelRequestApprove` - happy path, team admin approved when restricted, non-team-admin rejected, not found, connection removed, channel deleted
- `TestHandleChannelRequestDeny` / `TestHandleChannelRequestDenySubmit`
- `TestCancelPendingChannelConnectionRequest`

**Update: `server/configuration_test.go`** - tests for `isChannelAdminRequestsAllowed()` and `isChannelRequestMode()`

**Update: `server/store/client_test.go`** - tests for channel connection request KV operations

**Update: `server/command_test.go`** - test team admin request mode fallback for init-channel and teardown-channel

**Update: `webapp/src/components/CrossguardChannelModal.pw.tsx`** - test request mode rendering

## Files to Modify

| File | Change |
|------|--------|
| `plugin.json` | Add `AllowChannelAdminRequests` setting |
| `server/configuration.go` | Add field + 2 helper methods |
| `server/store/store.go` | Add `ChannelID` field to `ConnectionRequest` + 3 interface methods |
| `server/store/client.go` | Add prefix + 3 KV method implementations |
| `server/errcode/codes.go` | Add 25000-range constants + update `AllCodes` |
| `server/service.go` | Add `channelLink` helper, parameterize existing `buildRequestDMMessage` and `buildRequestConfirmationMessage` with optional channel info, update `ChannelStatusResponse`, update `getChannelStatus` signature, add self-healing cancel in `initChannelForCrossGuard` |
| `server/channel_request.go` | **New file** - `isChannelAdminInRequestMode`, `isTeamAdminDirect`, `getTeamAdmins`, `createChannelConnectionRequest`, approve/deny/denySubmit handlers, `cancelPendingChannelConnectionRequest` |
| `server/api.go` | 3 new routes + modify `handleInitChannel`, `handleTeardownChannel`, `handleChannelStatus` |
| `server/command.go` | Modify `executeInitChannel`, `executeTeardownChannel`, add `executeInitChannelRequest`, fix team admin channel permission gap |
| `webapp/src/components/CrossguardChannelModal.tsx` | Add request mode/pending UI |

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Team admin lookup performance | Paginate with 200 per page, same as `getSystemAdmins` |
| Team has no team admins | Return clear error "no team admins available to review your request" |
| Team admin demoted between DM and click | Approve/deny handlers verify `isTeamAdminDirect` at execution time |
| Direct link while request pending | `initChannelForCrossGuard` auto-cancels pending channel request (self-healing) |
| Connection removed while pending | Approve handler validates config + auto-cancels (same as existing) |
| Channel deleted while pending | Approve handler verifies channel exists via `GetChannel` |
| Team connection unlinked while channel request pending | Approve handler validates team has connection; auto-cancels with notification |
| Channel admin promoted to team admin while pending | Request still valid, a different team admin approves |

## Edge Cases

1. **Duplicate requests** - CAS prevents, returns "request already pending"
2. **Zero team admins** - Returns error "no team admins available to review your request"
3. **Two team admins click Approve** - Both may execute (initChannelForCrossGuard is idempotent)
4. **Connection removed from config** - Approve validates, auto-cancels with notification
5. **Channel admin unlinks while pending** - Teardown executes, pending request auto-cancelled
6. **AllowChannelAdminRequests toggled off** - Existing DM buttons still work (same behavior as team requests)
7. **Team not initialized** - `createChannelConnectionRequest` verifies team has the connection
8. **Channel deleted while pending** - Approve handler checks channel exists, auto-cancels if not
9. **Team admin is also channel admin** - Gets team admin behavior (direct permission), not channel request mode
10. **AllowTeamAdminRequests ON but AllowChannelAdminRequests OFF** - Team admins in request mode can still link channels directly (gap fix), channel admins cannot
11. **Both settings ON simultaneously** - Team admins approve channel requests via `isTeamAdminDirect` (bypasses restriction gate), regardless of their own team-level request mode status
12. **AllowChannelAdminRequests ON but AllowTeamAdminRequests OFF** - Channel admins can request from team admins. Team admins can approve via `isTeamAdminDirect`. System admins can also approve. (Team admins cannot request team connections in this configuration, but can still approve channel requests.)

## Testing Plan

**Unit**: Configuration helpers, `isChannelAdminInRequestMode`, `getTeamAdmins`, store operations
**Integration**: Full request lifecycle (create -> approve/deny -> notify), cancellation, permission checks, team admin channel permission gap fix
**E2E**: Channel modal request mode rendering, button states
**Docker**: Manual test with dual-server setup

## Verification

1. `make check-style` - lint passes
2. `make test` - all tests pass
3. `make deploy` - deploy to docker
4. Manual test flows:
   - Enable `RestrictToSystemAdmins` + `AllowChannelAdminRequests`
   - As channel admin, run `/crossguard init-channel` -> should submit request
   - As team admin, check DM -> should have Approve/Deny buttons
   - Click Approve -> channel linked, requester notified
   - Test deny flow with reason
   - Test teardown cancellation
   - Verify channel modal shows request pending state
   - Enable `AllowTeamAdminRequests` + `AllowChannelAdminRequests`
   - As team admin in request mode, run `/crossguard init-channel` -> should work directly (no request needed)

## Acceptance Criteria

- [ ] Channel admins can submit channel link requests when both settings enabled
- [ ] Team admins receive DM with Approve/Deny buttons
- [ ] System admins can also approve/deny
- [ ] Approval executes the channel link and notifies requester
- [ ] Denial with optional reason notifies requester
- [ ] Channel admins in request mode can unlink directly
- [ ] Team admins in request mode can link/unlink channels directly (gap fix)
- [ ] Pending requests auto-cancel on teardown
- [ ] Channel modal shows "Request Pending" / "Request Link" states
- [ ] Slash command and API both support request mode
- [ ] All new code has error codes in 25000 range
- [ ] Tests cover happy path, permission checks, edge cases

## Checklist

- [ ] **Diagnostics**: Channel request creation and approval/denial should post to diagnostics channel (follow existing request pattern)
- [ ] **Slash command**: No new subcommand needed; `init-channel` falls back to request mode automatically
