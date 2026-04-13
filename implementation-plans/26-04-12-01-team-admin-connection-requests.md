# Team Admin Connection Requests

## Context

Currently, the `Restrict Connection Management to System Admins` setting is all-or-nothing: when enabled, team admins are completely blocked from linking/unlinking connections. This plan adds a new setting `Allow Team Admins to Request Connections` that lets team admins submit **link** requests for system admin approval via DM with interactive buttons. Unlink requests execute directly without requiring approval, since only establishing new connections is security-sensitive.

## Problem Statement

Team admins need a way to request new connections without having direct permission to execute them. Today they get a generic "You don't have permissions" error. We need an approval workflow where team admins can request links, system admins review via DM with approve/deny buttons, and the requester gets notified of the outcome. Unlink requests are allowed to execute directly since removing a connection is less security-sensitive than establishing one.

## Current State

- `RestrictToSystemAdmins` bool setting in `plugin.json` (line 52) and `configuration.go` (line 140)
- `isTeamAdminOrSystemAdmin()` in `command.go:470-490` gates all team link/unlink actions
- `handleInitTeam()` (`api.go:357`) and `handleTeardownTeam()` (`api.go:507`) are the API handlers
- `executeInitTeam()` (`command.go:264`) and `executeTeardownTeam()` (`command.go:592`) are slash command handlers
- Existing interactive post pattern in `prompt.go` with Accept/Block buttons and `ConnectionPrompt` struct in `store/store.go:8-12`
- Frontend toggle in `CrossguardTeamModal.tsx:143-172` calls init/teardown API, checks `response.ok`
- `ConnectionStatus` struct in `service.go:312-324` returned by the status endpoint

### Current Gaps
- No request/approval workflow exists
- No DM notification pattern (current prompts post to town-square)
- No way for team admins to participate when `RestrictToSystemAdmins` is enabled

## Design Principles

| Pattern | Our Approach | Avoid | Reference |
|---------|-------------|--------|-----------|
| Approval workflow | DMs to each sysadmin with buttons | Single post in shared channel | User requirement: DM-based |
| State management | New `ConnectionRequest` in KVStore | Extending `ConnectionPrompt` (different lifecycle) | `store/store.go:8-12` |
| Concurrency | CAS on KV for atomic state transitions | Read-then-write without CAS | `store/client.go` atomic patterns |
| Deny reason | `OpenInteractiveDialog` for text input | Inline text parsing | `command.go` dialog pattern |
| Frontend detection | `request_pending` per-connection + `request_mode` on team status response | Inferring request mode from API action responses | `service.go:312-324` |

## Requirements

- [ ] New `AllowTeamAdminRequests` bool setting in admin console
- [ ] Team admins can submit link requests when both `RestrictToSystemAdmins` and `AllowTeamAdminRequests` are enabled
- [ ] Team admins can execute unlink requests directly (no approval needed) when both settings are enabled
- [ ] Each system admin receives a DM with: who is requesting, what team, what connection, what action (link), plus Approve/Deny buttons
- [ ] On Approve: execute the action, DM requester "approved", update all admin posts to remove buttons
- [ ] On Deny: dialog for optional reason, DM requester "denied" with reason, update all admin posts
- [ ] Idempotent: duplicate requests for the same team+connection are rejected while one is pending
- [ ] Only system admins can approve/deny (team admins cannot self-approve)

## Out of Scope

- Channel-level requests (team-level only for now)
- Request expiration/TTL (can add later)
- Request history/audit log
- Batch approve/deny

## Technical Approach

### Step 1: Configuration Setting

**Files**: `plugin.json`, `server/configuration.go`

Add `AllowTeamAdminRequests` setting in `plugin.json` after `RestrictToSystemAdmins`:
```json
{
    "key": "AllowTeamAdminRequests",
    "display_name": "Allow Team Admins to Request Connections",
    "type": "bool",
    "default": false,
    "help_text": "When enabled (and 'Restrict Connection Management to System Admins' is also enabled), team admins can submit connection link requests for system admin approval via direct message and can unlink connections directly. When disabled, team admins are blocked entirely."
}
```

Add to `configuration` struct (`configuration.go:136`):
```go
AllowTeamAdminRequests *bool `json:"AllowTeamAdminRequests"`
```

Add helpers:
```go
func (c *configuration) isTeamAdminRequestsAllowed() bool {
    return c.AllowTeamAdminRequests != nil && *c.AllowTeamAdminRequests
}

func (c *configuration) isRequestMode() bool {
    return c.isRestrictedToSystemAdmins() && c.isTeamAdminRequestsAllowed()
}
```

### Step 2: Store Layer

**Files**: `server/store/store.go`, `server/store/client.go`

Add `ConnectionRequest` struct to `store.go`:
```go
const (
    RequestStatePending = "pending"
)

type ConnectionRequest struct {
    RequesterID string   `json:"requester_id"`
    TeamID      string   `json:"team_id"`
    ConnKey     string   `json:"conn_key"`     // e.g. "outbound:my-conn"
    PostIDs     []string `json:"post_ids"`     // DM post IDs sent to each sysadmin
    CreatedAt   int64    `json:"created_at"`
}
// Note: No Action field needed. Requests are always for "link".
// Unlink operations execute directly without approval.
```

Add to `KVStore` interface:
```go
GetConnectionRequest(teamID, connKey string) (*ConnectionRequest, error)
CreateConnectionRequest(teamID, connKey string, req *ConnectionRequest) (bool, error)
DeleteConnectionRequest(teamID, connKey string) error
```

In `client.go`, add prefix `connRequestPrefix: pluginID + "-connreq-"` and implement using the same pattern as `ConnectionPrompt` methods (`client.go:292-329`). `CreateConnectionRequest` uses `pluginapi.SetAtomic(nil)` for CAS creation (prevents duplicates). No `Set` method needed; requests are created then deleted (no state transitions on the request itself).

**Concurrency on approval**: Since requests are simply created and deleted (not state-transitioned), the approval handler uses `GetConnectionRequest` then `DeleteConnectionRequest`. If two admins click Approve simultaneously, both may Get successfully before either Deletes. In the worst case both execute the link (which is idempotent via `AddTeamConnection`) and both delete the KV entry (second delete is a no-op). The requester may receive two notification DMs, which is a minor cosmetic issue. This is acceptable because the link action is idempotent and adding CAS delete complexity is not justified.

### Step 3: Error Codes

**File**: `server/errcode/codes.go`

Add new block for `request.go` (range 24000-24999):
```go
// request.go (24000-24999)
const (
    RequestGetFailed              = 24000
    RequestCreateFailed           = 24001
    RequestGetDMChannelFailed     = 24002
    RequestCreateDMPostFailed     = 24003
    RequestSaveFailed             = 24004
    RequestApproveGetFailed       = 24005
    RequestApproveExecFailed      = 24006
    RequestApproveDeleteFailed    = 24007
    RequestApproveNotifyFailed    = 24008
    RequestDenyDialogFailed       = 24009
    RequestDenyGetFailed          = 24010
    RequestDenyDeleteFailed       = 24011
    RequestDenyNotifyFailed       = 24012
    RequestUpdatePostFailed       = 24013
    RequestNoSystemAdmins         = 24014
    RequestConnRemovedFromConfig  = 24015
)
```

Add all to `AllCodes` slice.

### Step 4: Request Logic

**New file**: `server/request.go`

#### `isTeamAdminInRequestMode(userID, teamID string) bool`
- Returns false if user is sysadmin (they already have full permission)
- Returns false if `!cfg.isRequestMode()`
- Returns true if user is a team admin for the given team
- Note: `isTeamAdminOrSystemAdmin` already returns false for team admins when restricted, so this is checked as a fallback after the permission denial
- **Used by both link and unlink paths.** The caller decides what to do: link path creates a request, unlink path executes directly.

#### `createConnectionRequest(user *model.User, teamID string, conn store.TeamConnection) (string, error)`
1. Build `connKey` from conn (e.g. "outbound:my-conn")
2. Check for existing pending request via `kvstore.GetConnectionRequest(teamID, connKey)`. If exists, return error "A request is already pending for this connection."
3. Get team via `p.API.GetTeam(teamID)` for display name
4. Get system admins (see utility below). If zero admins, return error "No system admins available to review your request."
5. Build `ConnectionRequest` struct
6. For each sysadmin: get DM channel (`p.API.GetDirectChannel(p.botUserID, admin.Id)`), create interactive post with Approve/Deny buttons, collect post IDs
7. Atomically create via `kvstore.CreateConnectionRequest(teamID, connKey, &req)`. If false (race), delete the DM posts we just created
8. Return success message

#### DM Post Content
```
**Connection Link Request**

@username (Display Name) is requesting to **link** connection `outbound:my-conn` for team **TeamDisplayName**.

**Requesting user**: @username
**Team**: TeamDisplayName
**Connection**: outbound:my-conn
```

Buttons (same pattern as `prompt.go:46-76`):
- "Approve" (style: "good") -> `/plugins/{id}/api/v1/request/approve`
- "Deny" (style: "danger") -> `/plugins/{id}/api/v1/request/deny`
- Context: `team_id`, `conn_key`

#### `handleRequestApprove(w, r)`
1. Decode `PostActionIntegrationRequest`
2. Verify `user.IsSystemAdmin()` (not `isTeamAdminOrSystemAdmin`, prevents self-approval)
3. Get request via `kvstore.GetConnectionRequest(teamID, connKey)`. If nil, respond "This request is no longer active."
4. Validate the connection still exists in config (`getAllConnectionNames()`). If removed, delete the request, update posts with "Connection no longer exists", notify requester, return
5. Parse `connKey` back to `store.TeamConnection`
6. Execute: call `initTeamForCrossGuard` (requests are always link actions)
7. Delete request from KV
8. Update all admin DM posts: remove buttons, append "Approved by @admin" (use `updatePromptPost` pattern from `prompt.go:431`). Log and continue if individual updates fail
9. DM the requester: "Your request to **link** connection `outbound:my-conn` for team **TeamDisplayName** has been approved by @admin."

#### `handleRequestDeny(w, r)`
1. Decode `PostActionIntegrationRequest`
2. Verify `user.IsSystemAdmin()`
3. Verify request still exists (if not, respond "This request is no longer active.")
4. Open interactive dialog:
```go
p.API.OpenInteractiveDialog(model.OpenDialogRequest{
    TriggerId: req.TriggerId,
    URL:       fmt.Sprintf("/plugins/%s/api/v1/request/deny-submit", manifest.Id),
    Dialog: model.Dialog{
        Title: "Deny Connection Request",
        Elements: []model.DialogElement{{
            DisplayName: "Reason for denial",
            Name:        "reason",
            Type:        "textarea",
            Optional:    true,
            Placeholder: "Optional reason",
        }},
        State: teamID + "|" + connKey, // pipe delimiter avoids ambiguity since connKey contains ":"
    },
})
```
5. Return empty `PostActionIntegrationResponse`

#### `handleRequestDenySubmit(w, r)`
1. Decode `SubmitDialogRequest`
2. Verify `user.IsSystemAdmin()` (same check as approve/deny handlers; prevents non-admins from crafting a POST to this endpoint)
3. Handle `req.Cancelled` (user dismissed dialog, do nothing)
4. Parse state using `strings.SplitN(state, "|", 2)` to extract `teamID` and `connKey`
5. Get request from KV. If nil, return (already handled)
6. Extract reason from `req.Submission["reason"]`
7. Delete request from KV
8. Update all admin DM posts: remove buttons, append "Denied by @admin. Reason: ..."
9. DM requester: "Your request to **link** connection `outbound:my-conn` for team **TeamDisplayName** has been denied by @admin." + reason if provided

#### `getSystemAdmins() ([]*model.User, error)`
Paginate through `p.API.GetUsers(&model.UserGetOptions{Role: model.SystemAdminRoleId, Page: page, PerPage: 100})`. The plugin API's `GetUsers` with `Role` filter returns only users with that role, so this is efficient even on large servers.

#### `updateRequestPosts(postIDs []string, newMessage string)`
Iterate through stored post IDs, call `p.API.GetPost(id)` then `p.API.UpdatePost(post)` with buttons removed and updated message. Log and continue on individual failures (admin may have deleted the post).

### Step 5: API Routes

**File**: `server/api.go` - add in `initAPI()`:
```go
router.HandleFunc("/api/v1/request/approve", p.handleRequestApprove).Methods(http.MethodPost)
router.HandleFunc("/api/v1/request/deny", p.handleRequestDeny).Methods(http.MethodPost)
router.HandleFunc("/api/v1/request/deny-submit", p.handleRequestDenySubmit).Methods(http.MethodPost)
```

### Step 6: Modify Authorization Flow

**File**: `server/api.go`

In `handleInitTeam()` (line 357), after the existing `isTeamAdminOrSystemAdmin` check that returns 403:
```go
if !p.isTeamAdminOrSystemAdmin(user.Id, teamID) {
    if p.isTeamAdminInRequestMode(user.Id, teamID) {
        // resolve connection name, create request
        // return 200 with {"status": "request_submitted", "message": "..."}
        return
    }
    writeJSONError(w, "insufficient permissions", http.StatusForbidden)
    return
}
```

In `handleTeardownTeam()` (line 507), team admins execute unlinking directly:
```go
if !p.isTeamAdminOrSystemAdmin(user.Id, teamID) {
    if p.isTeamAdminInRequestMode(user.Id, teamID) {
        // execute teardown directly, same as sysadmin flow
        // fall through to normal teardown logic
    } else {
        writeJSONError(w, "insufficient permissions", http.StatusForbidden)
        return
    }
}
```

After a successful teardown (for any user), cancel any pending link request for the same connection:
```go
// After successful teardown, auto-cancel any pending link request
if req, _ := p.kvstore.GetConnectionRequest(teamID, connKey); req != nil {
    p.kvstore.DeleteConnectionRequest(teamID, connKey)
    p.updateRequestPosts(req.PostIDs, "Request cancelled (connection was unlinked).")
    // Optionally DM requester that their pending request was cancelled
}
```

Return **200 OK** (not 202) with `{"status": "request_submitted", "message": "Your request has been submitted for system admin approval.", "team_id": "...", "connection_name": "..."}`. Using 200 avoids breaking the frontend's `response.ok` check (`CrossguardTeamModal.tsx:159`). The frontend distinguishes via the `status` field in the response body.

**File**: `server/command.go`

In `executeInitTeam()` (line 264), after the permission check:
```go
if !p.isTeamAdminOrSystemAdmin(args.UserId, args.TeamId) {
    if p.isTeamAdminInRequestMode(args.UserId, args.TeamId) {
        // resolve connection, create request
        return respondEphemeral("Your request to link connection `%s` for this team has been submitted for system admin approval.", connKey)
    }
    return respondEphemeral("You must be a team admin or system admin.")
}
```

In `executeTeardownTeam()` (line 592), team admins execute directly:
```go
if !p.isTeamAdminOrSystemAdmin(args.UserId, args.TeamId) {
    if p.isTeamAdminInRequestMode(args.UserId, args.TeamId) {
        // fall through to normal teardown logic
    } else {
        return respondEphemeral("You must be a team admin or system admin.")
    }
}
```

After successful teardown, auto-cancel any pending link request (same as API handler above).

### Step 7: Update Status Endpoint

**File**: `server/service.go`

Add `RequestPending` field to `ConnectionStatus` struct:
```go
type ConnectionStatus struct {
    // ... existing fields
    RequestPending  bool   `json:"request_pending,omitempty"`  // true if a link request is pending approval
}
```

Add `RequestMode` field to the top-level `TeamStatusResponse` so the frontend knows button labels before the user clicks:
```go
type TeamStatusResponse struct {
    // ... existing fields
    RequestMode bool `json:"request_mode,omitempty"` // true when team admins must request links (for non-sysadmin callers)
}
```

In `getTeamStatus()` (line 171):
- Set `response.RequestMode = cfg.isRequestMode() && !user.IsSystemAdmin()` (sysadmins never see request mode)
- When building connection statuses, if `cfg.isRequestMode()`, check for a pending request on each connection via `kvstore.GetConnectionRequest(teamID, connKey)` and set `RequestPending`

### Step 8: Frontend Changes

**File**: `webapp/src/components/CrossguardTeamModal.tsx`

1. Update `ConnectionStatus` interface to include `request_pending`.

2. Update button rendering (lines 594-610):
```tsx
{conn.linked ? (
    <button style={s.btnUnlink} onClick={() => handleToggle(conn, true)} disabled={actionInProgress !== null}>
        {isActioning ? 'Unlinking...' : 'Unlink'}
    </button>
) : (
    conn.request_pending ? (
        <button style={s.btnDisabled} disabled={true}>Request Pending</button>
    ) : (
        <button style={s.btnLink} onClick={() => handleToggle(conn, false)} disabled={actionInProgress !== null}>
            {isActioning ? 'Linking...' : (requestMode ? 'Request Link' : 'Link')}
        </button>
    )
)}
```

Note: Unlink button always shows "Unlink" (never "Request Unlink") since unlinking executes directly.

3. In `handleToggle` (line 143): Check response body for `status === "request_submitted"` to show the appropriate success message: "Your request has been submitted for approval."

4. Detect `requestMode`: Read the `request_mode` boolean from the `TeamStatusResponse` returned by the status endpoint. This is set server-side based on config and user role (sysadmins never see request mode). The frontend uses this to render "Request Link" vs "Link" on initial page load without requiring a prior action.

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| New struct vs extend ConnectionPrompt? | New `ConnectionRequest` struct | Different lifecycle (user-initiated vs auto-detected), different fields (PostIDs, RequesterID) |
| Link vs unlink approval? | Link requires approval, unlink executes directly | Only establishing new connections is security-sensitive; removing connections is safe to allow |
| DMs vs shared channel post? | DM each system admin | User requirement; ensures each admin gets a personal notification |
| Deny reason required? | Optional textarea via dialog | User wants deny reasons but making it required would be friction |
| HTTP status for request submitted? | 200 OK with status field | Avoids breaking frontend `response.ok` check |
| Channel-level requests? | Deferred | Adds complexity without clear demand |
| Stale request cleanup? | Deferred | Requests deleted on approve/deny; orphans are rare and low-impact |
| Team admin demoted while pending? | Still approve (sysadmin explicitly approved) | The sysadmin's approval is the authority, not the requester's current role |

## Files to Modify

| File | Change |
|------|--------|
| `plugin.json` | Add `AllowTeamAdminRequests` setting |
| `server/configuration.go` | Add field + helpers |
| `server/store/store.go` | Add `ConnectionRequest` struct + interface methods |
| `server/store/client.go` | Implement KV methods + add prefix |
| `server/errcode/codes.go` | Add 24000-range codes |
| `server/request.go` | **New file**: `isTeamAdminInRequestMode()`, request creation, approve/deny/deny-submit handlers, DM logic |
| `server/api.go` | Register 3 routes, modify `handleInitTeam` (request mode), `handleTeardownTeam` (direct unlink) |
| `server/command.go` | Modify `executeInitTeam` (request mode), `executeTeardownTeam` (direct unlink) |
| `server/service.go` | Add `RequestPending` to `ConnectionStatus`, `RequestMode` to `TeamStatusResponse`, query in `getTeamStatus` |
| `webapp/src/components/CrossguardTeamModal.tsx` | Request mode button text, pending state, success message |

## Tasks

1. [ ] Add `AllowTeamAdminRequests` setting to `plugin.json` and `configuration.go`
2. [ ] Add `ConnectionRequest` struct and KVStore methods to `store/store.go` and `store/client.go`
3. [ ] Add error codes to `errcode/codes.go`
4. [ ] Create `server/request.go` with request creation, notification, approve/deny handlers
5. [ ] Register new API routes in `api.go`
6. [ ] Modify `handleInitTeam` in `api.go` for link request mode branching
7. [ ] Modify `handleTeardownTeam` in `api.go` for direct unlink by team admins
8. [ ] Modify `executeInitTeam` in `command.go` for link request mode branching
9. [ ] Modify `executeTeardownTeam` in `command.go` for direct unlink by team admins
10. [ ] Update `ConnectionStatus` and `getTeamStatus()` in `service.go`
11. [ ] Update `CrossguardTeamModal.tsx` for request mode UI
12. [ ] Write backend tests for request creation, approve, deny, and direct unlink flows
13. [ ] Write frontend tests for request mode button rendering
14. [ ] Run `make check-style` and `make test`

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Two admins click Approve simultaneously | Both may execute (idempotent link, no harm). Requester may get two DMs. Acceptable trade-off vs CAS delete complexity |
| Connection removed from config while request pending | Approve handler validates connection still exists; auto-cancels if removed |
| Zero system admins when request submitted | Return error to team admin: "No system admins available" |
| Updating N admin DM posts on action | Log and continue on individual failures; N is typically small (<10) |
| KV key length | Key is `crossguard-connreq-{26-char-teamID}-{connKey}` which fits within Mattermost's KV key limits |

## Edge Cases

| Scenario | Behavior |
|----------|----------|
| Team admin submits duplicate request | "A request is already pending for this connection." |
| Admin deletes the DM post then another admin approves | `UpdatePost` fails silently, approval still succeeds |
| Plugin disabled while requests pending | Requests persist in KV; buttons become non-functional. Re-enable resumes functionality |
| Team admin requests link, then sysadmin links directly | Direct link succeeds. Later approval of the request will no-op (AddTeamConnection is idempotent for existing connections) |
| Team admin unlinks while a link request is pending | Unlink executes directly. The pending link request is auto-cancelled: KV entry deleted, admin DM posts updated with "Request cancelled (connection was unlinked)", requester notified |
| Request created then `AllowTeamAdminRequests` turned off | Existing DM buttons still work; admin can still approve/deny. No new requests can be created |

## Testing Plan

**Unit (Go)**:
- `isTeamAdminInRequestMode()` returns correct values for sysadmin, team admin, regular user, various config combos
- `createConnectionRequest` creates KV entry and DM posts
- `handleRequestApprove` executes link action, deletes request, updates posts, notifies requester
- `handleRequestDeny` opens dialog
- `handleRequestDenySubmit` verifies IsSystemAdmin, deletes request, updates posts, notifies requester with reason
- Duplicate request rejection
- Unlink auto-cancels pending link request (KV deleted, posts updated, requester notified)
- Zero sysadmins error
- Connection removed from config during pending request
- Team admin can unlink directly without creating a request

**Frontend**:
- Button shows "Request Link" for unlinked connections when request mode is active
- Button shows "Unlink" (not "Request Unlink") for linked connections when request mode is active
- Button shows "Request Pending" (disabled) when a link request is pending
- Success message shows "request submitted" text for link requests
- Unlink operates normally (no request flow) when request mode is active
- Normal link/unlink behavior unchanged when not in request mode

**Integration (Docker)**:
- Enable both settings, log in as team admin, request a link
- Verify sysadmin receives DM with buttons
- Click Approve, verify connection is linked and requester gets DM
- Repeat with Deny + reason, verify requester gets denial DM
- As team admin, unlink a connection and verify it executes immediately without approval

## Acceptance Criteria

- [ ] New `Allow Team Admins to Request Connections` toggle visible in admin console
- [ ] Team admin clicking Link creates a request (not a direct action) when both settings enabled
- [ ] Team admin clicking Unlink executes directly (no approval needed) when both settings enabled
- [ ] All system admins receive DMs describing who, what team, what connection, what action
- [ ] Approve button executes the action and notifies the requester
- [ ] Deny button opens a dialog for optional reason and notifies the requester
- [ ] Duplicate requests are prevented
- [ ] System admins can still link/unlink directly (not affected by request mode)
- [ ] Frontend shows appropriate button text and pending state

## Checklist

- [ ] **Slash command**: `/crossguard init-team` supports link request mode, `/crossguard teardown-team` allows direct unlink (covered in Step 6)
