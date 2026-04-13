---
name: add-slash-cmds
description: Use when Crossguard plugin API endpoints in server/api.go have grown out of sync with slash commands in server/command.go. Surveys both files, identifies API operations that lack a corresponding slash command, and suggests new subcommands to add. Trigger after adding new REST endpoints, during periodic audits, or before cutting a release.
---

# Add Slash Commands

Find API endpoints in `server/api.go` that have no corresponding slash command in `server/command.go`, then suggest and implement new `/crossguard` subcommands to close the gap. The plugin follows a dual-interface pattern where most operations should be accessible via both the REST API and slash commands.

## When to Use

- After adding new REST endpoints in `server/api.go` without adding matching slash commands
- During a periodic audit of API-vs-command parity
- Before cutting a release, to ensure all user-facing operations have a slash command
- When a user reports that an operation is only available via the API and not the command line

## When NOT to Use

- For endpoints that are purely internal (dialog callbacks, interactive message button handlers)
- For bulk/admin-only query endpoints that do not map to a single-team or single-channel action
- When the API endpoint is only called by the webapp and has no meaningful CLI equivalent

## Workflow

Run these steps in order. Survey first, build the gap analysis, write and confirm a plan with the user, then implement.

### Task tracking

This workflow is driven by the harness task list. Do not work without a task.

1. Start by calling `TaskList` to check for pre-existing tasks from a prior run. Reuse or delete stale tasks instead of duplicating.
2. After completing Step 1 (Survey) and Step 3 (Write and confirm plan), use `TaskCreate` to create one task per new slash command identified in the approved plan. Typical tasks:
   - `Add slash command: <subcommand-name>` (subject), `activeForm: Implementing <subcommand-name> slash command`
   - `Update autocomplete data for <subcommand-name>`, `activeForm: Updating autocomplete`
   - `Verify new slash commands`, `activeForm: Verifying commands`
   Every task MUST set `subject`, `description` (what specifically is changing and why), and `activeForm`.
3. Walk the task list top-to-bottom. For each task: call `TaskUpdate` with `status: "in_progress"` **before** editing any file, do the work, then call `TaskUpdate` with `status: "completed"` only after the step's checks pass.
4. Never hold more than one task in `in_progress` at a time.
5. If blocked, leave the task `in_progress`, create a new task via `TaskCreate` describing the blocker, and move on. Do not silently skip.
6. After Step 7 (Report), call `TaskList` one more time and confirm zero `pending` or `in_progress` tasks remain. Delete any stale ones via `TaskUpdate` with `status: "deleted"` before finishing.

### 1. Survey API endpoints and slash commands

Read both files and build two inventories:

**API endpoints** (from `server/api.go`):
```bash
grep -E 'router\.HandleFunc\(' server/api.go
```

**Slash command subcommands** (from `server/command.go`):
```bash
grep -E '^\s+case\s+"' server/command.go
grep -E '^\s+action\w+\s*=' server/command.go
```

For each API endpoint, note:
- Route path and HTTP method
- Handler function name
- What operation it performs (read the handler to understand its purpose)
- Permission model (system admin, team admin, channel admin, any member)

For each slash command subcommand, note:
- Subcommand name and arguments
- Which `execute*` function it calls
- Permission requirements
- Autocomplete description

Step 1 is read-only. Do not edit any file yet.

### 2. Build gap analysis

Create a mapping table of API endpoints to slash commands. Classify each endpoint as:

| Category | Description | Action |
|----------|-------------|--------|
| **Paired** | Has both an API endpoint and a slash command | No action needed |
| **API-only (user-facing)** | API endpoint exists but no slash command, and the operation is meaningful from the CLI | Suggest a new slash command |
| **API-only (internal)** | API endpoint exists but the operation is UI-driven (dialog callbacks, button handlers, bulk queries) | Skip, document why |
| **Command-only** | Slash command exists without a direct API equivalent | Note for awareness, no action |

Known internal/UI-driven endpoints to exclude (unless the user explicitly requests them):
- `/api/v1/dialog/select-connection` (interactive dialog callback)
- `/api/v1/prompt/accept`, `/api/v1/prompt/block` (interactive message button handlers)
- `/api/v1/prompt/channel/accept`, `/api/v1/prompt/channel/block` (channel prompt button handlers)
- `/api/v1/request/approve`, `/api/v1/request/deny`, `/api/v1/request/deny-submit` (request approval button handlers)
- `/api/v1/channels/connections` (bulk query for webapp sidebar)

For each "API-only (user-facing)" endpoint, draft a slash command suggestion:
- **Subcommand name**: follow existing naming conventions (verb-noun, e.g., `init-team`, `teardown-channel`)
- **Arguments**: match the API endpoint's required parameters
- **Description**: one line for autocomplete
- **Permission level**: match the API handler's permission checks
- **Handler pattern**: reference similar existing commands to follow

### 3. Write and confirm plan

Present the gap analysis and suggested new commands to the user. The plan must cover:

- **Gap analysis table**: showing all endpoints and their classification
- **New commands**: for each suggested command, include the subcommand name, arguments, description, permission level, and which existing command to use as a pattern
- **Intentionally skipped**: any API-only endpoints that were excluded and why
- **Impact on existing code**: what constants, autocomplete entries, and switch cases need to change

Wait for user confirmation before proceeding. If the user wants to skip or modify any suggestions, revise the plan.

### 4. Implement new slash commands

For each approved new command, follow this pattern from existing commands:

1. **Add a constant** in the `const` block at the top of `command.go`:
   ```go
   actionNewCommand = "new-command"
   ```

2. **Add a case** to the `ExecuteCommand` switch statement:
   ```go
   case actionNewCommand:
       return p.executeNewCommand(args), nil
   ```

3. **Implement the handler** function following the pattern of the most similar existing command:
   - Extract arguments from `args.Command` using `strings.Fields`
   - Check permissions using the same helpers (`p.isTeamAdminOrSystemAdmin`, `p.isChannelAdminOrHigher`, etc.)
   - Use ephemeral responses via `respondEphemeral()` for errors and confirmations
   - If the operation needs a connection name and none is provided, use the dialog-based selection pattern
   - Call the same shared business logic that the API handler uses
   - Include error codes from `server/errcode/codes.go` for any `p.API.Log*` calls

4. **Add error codes** to `server/errcode/codes.go` for any new log calls, following the conventions in CLAUDE.md.

### 5. Update autocomplete data

In the `getAutocompleteData()` function in `command.go`:

1. Create a new `model.NewAutocompleteData` entry for each new subcommand
2. Set the hint string (arguments) and description to match the approved plan
3. Add it to the parent command with `cmd.AddCommand()`
4. Update the `AutoCompleteHint` string in `registerCommand()` to include the new subcommand name

### 6. Verify

- Run `make check-style` to ensure Go formatting and lint pass
- Run `make test` to confirm existing tests still pass
- Verify the new command constants, switch cases, and autocomplete entries are consistent:
  ```bash
  grep -c 'action[A-Z]' server/command.go       # constant count
  grep -c '^\s*case ' server/command.go          # switch case count
  grep -c 'AddCommand' server/command.go         # autocomplete entry count
  ```
  All three counts should be equal (or differ by exactly the number of non-constant cases like string literals).
- Read through each new handler to confirm it calls the same shared logic as the corresponding API handler

### 7. Report

Summarize in one short block:
- Which new slash commands were added and what they do
- Which API endpoints remain API-only and why
- Any follow-up work needed (tests, help docs, etc.)

Remind the user to run `/add-help-docs` if new commands were added, since `public/help/commands.html` will need updating.

## Critical Files

- `server/command.go` -- slash command registration, autocomplete, dispatcher, and handlers
- `server/api.go` -- REST endpoint registration and handlers (source of truth for operations)
- `server/errcode/codes.go` -- error code constants for new log calls
- `server/plugin.go` -- Plugin struct and shared helpers used by both API and command handlers

## Common Mistakes

- Adding a switch case but forgetting to add the autocomplete entry (or vice versa). All three must stay in sync: constant, case, autocomplete.
- Not following the existing permission model. Check what the API handler requires and match it.
- Implementing business logic directly in the command handler instead of calling the shared function that the API handler already uses.
- Forgetting to update the `AutoCompleteHint` string in `registerCommand()`.
- Classifying an internal/UI-driven endpoint as needing a slash command. Dialog callbacks and interactive button handlers should not become commands.
- Using em dashes in descriptions or comments. The repo convention forbids them.
- Adding log calls without error codes from `server/errcode/codes.go`.
- Not running `make check-style` after editing Go files.
