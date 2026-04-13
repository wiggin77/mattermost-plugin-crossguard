---
name: add-backend-tests
description: Systematically find Go backend test coverage gaps and add exhaustive unit/integration tests. Use when you want to improve Go test coverage, add missing tests, or harden existing test suites.
---

# Add Backend Tests

Systematically analyze Go backend coverage, identify gaps, and add exhaustive tests that follow project patterns. You are a Go testing expert who writes thorough, maintainable tests.

**This skill runs in two phases: Plan first, then Execute.**

---

## Phase A: Plan (Read-Only)

Enter plan mode, analyze coverage, build a todo list, then exit plan mode for user approval.

### Step 1: Enter Plan Mode

Call `EnterPlanMode` to ensure no edits are made during analysis.

### Step 2: Measure Current Coverage

Run `make coverage-backend` and capture the output. This runs `go test -coverprofile` then `go tool cover -func` to show per-function coverage.

```bash
make coverage-backend 2>&1
```

**Apply a two-gate check before exiting early.** Only stop here if BOTH gates pass:

1. **Overall gate**: total coverage is ≥ 90%.
2. **Per-file floor gate**: no individual source file (excluding `*_test.go` files) is below 80%.

If both gates pass, report the overall number plus the lowest per-file coverage, congratulate the user, and exit plan mode. No additional tests are needed.

If the overall gate passes but one or more files are under the 80% per-file floor, do NOT exit early. Treat those under-floor files as the priority targets for this run, even though overall coverage looks healthy. A high average can hide a single poorly-tested file.

Quick way to list files below the 80% per-file floor (skipping test files):
```bash
make coverage-backend 2>&1 | awk '/\.go:/ && $NF ~ /%$/ {sub("%","",$NF); pkg=$1; sub(":.*","",pkg); cov[pkg]+=$NF; n[pkg]++} END {for (f in cov) if (cov[f]/n[f] < 80 && f !~ /_test\.go$/) printf "%-60s %.1f%%\n", f, cov[f]/n[f]}' | sort -k2 -n
```

If either gate fails, parse the output to build a prioritized list:
- **Tier 1**: Functions at 0% coverage (completely untested)
- **Tier 2**: Functions below 60% coverage (significant gaps)
- **Tier 3**: Functions below 80% coverage (moderate gaps)
- **Tier 4**: Complex functions above 80% that have untested edge cases

**Target large zero-coverage files first.** Within Tier 1, rank files by the number of zero-coverage functions they contain (and by file size for ties). A single file with 20 untested functions is a much bigger coverage win than 20 scattered functions across different files, and shared test setup (mocks, fixtures, helpers) can be reused across all functions in the same file. Group the plan so the largest zero-coverage file is fully addressed before moving to the next.

Quick way to rank files by zero-coverage count:
```bash
make coverage-backend 2>&1 | awk '$NF == "0.0%"' | awk -F: '{print $1}' | sort | uniq -c | sort -rn
```

Skip trivial functions (main, manifest constants, simple getters under 5 lines).

### Step 3: Understand What Needs Testing

For each function in your priority list:

1. **Read the source file** to understand the function's logic, branches, and error paths
2. **Read the existing test file** (if one exists) to see what's already covered
3. **Identify untested paths**: error returns, edge cases, branch conditions, concurrent scenarios
4. **Note dependencies**: what needs mocking (API calls, KV store, providers)

### Step 4: Build Task List

Before creating anything, call `TaskList` to check for pre-existing tasks from a prior run of this skill. Reuse, update, or delete stale tasks instead of creating duplicates.

Then use `TaskCreate` to create one task per function or logical group of functions needing tests. Every task MUST set all three fields:

- **subject**: `Test <FunctionName> in <file.go>` (imperative form)
- **description**: Current coverage %, what specifically needs testing (list the untested branches, error paths, edge cases), and the tier (1-4)
- **activeForm**: Present-continuous form shown in the spinner, e.g. `Testing HandleInbound semaphore paths`

Group related functions into a single task when they share setup (e.g., all methods on the same receiver that need the same mock). Order tasks by tier (Tier 1 first), and within Tier 1 order by largest zero-coverage file first so shared fixtures and mocks can be built once and reused across the whole file.

**The task list created here is the source of truth for Phase B.** Do not execute any test-writing work in Phase B that is not represented by a task. If you discover new work mid-execution, create a new task for it before doing the work.

Example tasks:
- `Test HandleInbound semaphore-full and context-cancel paths in inbound.go` (Tier 1, 0% coverage, needs mock provider + semaphore fill)
- `Test splitMessage UTF-8 boundary handling in service.go` (Tier 3, 65% coverage, missing multi-byte split edge cases)

### Step 5: Exit Plan Mode

Call `ExitPlanMode` to present the plan and todo list to the user for approval.

---

## Phase B: Execute

After the user approves the plan, work through the todo list writing tests.

### Step 6: Write Tests Using Project Patterns

Phase B is a strict loop driven by the task list. Never skip a step, and never hold more than one task in `in_progress` at a time.

1. Call `TaskList` and pick the next `pending` task (respect tier ordering from Step 4).
2. Call `TaskUpdate` with `status: "in_progress"` **before** reading any source file or writing any test code for that task.
3. Do the work: read source, read existing tests, write subtests, run `make test`, run `make check-style`.
4. Only after `make test` passes and `make check-style` is clean, call `TaskUpdate` with `status: "completed"`.
5. If blocked, leave the task `in_progress`, create a new task via `TaskCreate` describing the blocker, and move on. Do not silently skip.
6. Loop back to step 1. Stop when `TaskList` shows no `pending` tasks.

### Test Infrastructure Available

The project has established test utilities in `server/test_helpers_test.go`:

**Plugin setup with router and mock KV store:**
```go
p, kvs := setupTestPluginWithRouter(api)
// kvs is a *flexibleKVStore - override any KV method via function pointers:
kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
    return nil, errors.New("store error")
}
```

**HTTP request builder for API tests:**
```go
req := makeAuthRequest(t, "POST", "/api/v1/endpoint", bodyStruct, "user-id")
w := httptest.NewRecorder()
p.ServeHTTP(w, req)
assert.Equal(t, http.StatusOK, w.Code)
```

**Response decoder:**
```go
result := decodeJSONResponse(t, w)
```

**Mock queue provider (implements QueueProvider interface):**
```go
provider := &mockQueueProvider{
    publishFn: func(ctx context.Context, data []byte) error {
        return errors.New("publish failed")
    },
    maxMsgSize: 1024,
}
```

**Embedded NATS server for integration tests:**
```go
addr := startEmbeddedNATS(t) // returns "nats://127.0.0.1:<port>"
np := connectToEmbeddedNATS(t, addr, "test-subject")
```

**Mattermost plugin API mock:**
```go
api := &plugintest.API{}
api.On("LogError", mock.Anything, mock.Anything, mock.Anything).Return()
api.On("GetTeamByName", "team-a").Return(&model.Team{Id: "team-id"}, nil)
defer api.AssertExpectations(t)
```

### Testing Patterns to Follow

**Subtests for related scenarios:**
```go
func TestFunctionName(t *testing.T) {
    t.Run("success case", func(t *testing.T) { ... })
    t.Run("error from dependency", func(t *testing.T) { ... })
    t.Run("edge case description", func(t *testing.T) { ... })
}
```

**Table-driven tests for input variations:**
```go
tests := []struct {
    name    string
    input   string
    want    string
    wantErr bool
}{
    {"valid input", "hello", "HELLO", false},
    {"empty input", "", "", true},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        got, err := Transform(tt.input)
        if tt.wantErr {
            require.Error(t, err)
        } else {
            require.NoError(t, err)
            assert.Equal(t, tt.want, got)
        }
    })
}
```

**Testing concurrent/semaphore behavior:**
```go
// Fill the semaphore to test "full" path
for i := 0; i < cap(p.relaySem); i++ {
    p.relaySem <- struct{}{}
}
defer func() {
    for i := 0; i < cap(p.relaySem); i++ {
        <-p.relaySem
    }
}()
// Now call the function - it should hit the "semaphore full" branch
```

**Testing context cancellation:**
```go
ctx, cancel := context.WithCancel(context.Background())
cancel() // cancel immediately
err := functionUnderTest(ctx)
assert.ErrorIs(t, err, context.Canceled)
```

### What Makes a Good Test

- **Tests behavior, not implementation**: assert on observable outcomes (HTTP status, return values, mock expectations), not internal state
- **One logical assertion per subtest**: test one path/branch per t.Run
- **Descriptive names**: `TestHandleInbound_SemaphoreFull_DropsMessage` not `TestHandleInbound3`
- **Isolated**: each subtest sets up its own state, no shared mutable state between subtests
- **Fast**: prefer mocks over real I/O. Use embedded NATS only for provider integration tests
- **No synthetic/mock data to fix failures**: if a test fails, fix the code or the test logic, never fabricate data

### What to Test in Each Function

For every function, aim to cover:

1. **Happy path**: normal successful execution
2. **Each error return**: every `return err` or `return nil, err` branch
3. **Nil/empty inputs**: nil pointers, empty strings, empty slices
4. **Boundary conditions**: exact limits, off-by-one, size thresholds
5. **Concurrent access**: semaphore full, context cancelled, CAS retry
6. **Permission checks**: unauthorized user, wrong role, missing header

### File Organization

Add tests to existing `*_test.go` files. The mapping:
- `server/api.go` -> `server/api_test.go`
- `server/connections.go` -> `server/connections_test.go`
- `server/inbound.go` -> `server/inbound_test.go`
- `server/nats_provider.go` -> `server/nats_test.go`
- `server/azure_provider.go` -> `server/azure_provider_test.go`
- `server/command.go` -> `server/command_test.go`
- `server/service.go` -> `server/service_test.go`
- `server/plugin.go` -> `server/plugin_test.go`
- `server/prompt.go` -> `server/prompt_test.go`
- `server/sync_user.go` -> `server/sync_user_test.go`
- `server/hooks.go` -> `server/hooks_test.go`
- `server/configuration.go` -> `server/configuration_test.go`
- `server/store/caching.go` -> `server/store/caching_test.go`
- `server/store/client.go` -> `server/store/client_test.go`
- `server/model/message.go` -> `server/model/message_test.go`
- `server/model/post_message.go` -> `server/model/post_message_test.go`
- `server/model/test_message.go` -> `server/model/test_message_test.go`

## Step 7: Tier Reference (Task Ordering)

You do not run a separate "phases" workflow. You walk the task list from Step 4 top-to-bottom using the loop in Step 6. The tiers below are only a reference for how tasks should already be ordered, and for what kind of work each tier tends to involve:

**Tier 1a - Quick wins (0% coverage, simple functions):**
Functions with straightforward logic that just need basic test coverage. Often small utility functions, error type methods, or simple delegating wrappers. Easy to knock out first and raise the baseline.

**Tier 1b - Integration tests (0% coverage, require embedded NATS):**
Provider functions that need a real NATS server. Use `startEmbeddedNATS(t)` and `connectToEmbeddedNATS(t, addr, subject)`.

**Tier 2 - Branch coverage (low coverage functions):**
Functions that have tests but miss important branches. Read existing tests carefully to avoid duplication, then add subtests for uncovered paths.

**Tier 3 - Edge cases and complex logic:**
Message splitting (UTF-8 boundaries), file handling (retry exhaustion, filter policies), health check logic (recheck windows), concurrent scenarios (semaphore full, CAS retry).

**Tier 4 - Interface extraction for testability (if needed):**
If a dependency uses a concrete SDK type that cannot be mocked (e.g., Azure blob client), extract an interface to enable unit testing. Follow the existing `azureQueuer` pattern in `azure_provider.go`. Create an explicit task for the extraction itself before creating tasks for the tests that depend on it.

## Step 8: Validate After Each Task

Validation runs per task, not per batch. For the currently `in_progress` task, run:

```bash
# Tests compile and pass
make test

# No lint issues (fixes import formatting)
make check-style

# Coverage improved
make coverage-backend 2>&1
```

Compare coverage numbers against the baseline from Step 2. If a function you targeted is still at 0%, your test isn't exercising the right code path. Re-read the source and fix before marking the task complete.

Only after all three commands succeed, call `TaskUpdate` with `status: "completed"` for that task. Then return to Step 6 and pick up the next `pending` task.

## Step 9: Final Verification

After the Step 6 loop drains all pending tasks:

```bash
# Full test suite passes
make test

# Style checks pass
make check-style

# Print final coverage summary
make coverage-backend 2>&1
```

Then call `TaskList` one more time and confirm there are zero `pending` or `in_progress` tasks left. If any remain, either finish them or delete them (via `TaskUpdate` with `status: "deleted"`) before reporting completion. Never leave stale tasks hanging.

Report the before/after coverage delta per package and overall.

## Common Pitfalls

- **Testing the mock, not the code**: ensure your mock setup actually forces the code path you intend. If a mock returns nil where the real code would return data, you may be testing a different branch.
- **Forgetting `defer api.AssertExpectations(t)`**: without this, unmet mock expectations silently pass.
- **Not reading existing tests first**: you'll write duplicates or miss established patterns.
- **Over-mocking**: if a function is pure logic with no dependencies, test it directly without mocks.
- **Ignoring goroutine cleanup**: any test that spawns goroutines (via Subscribe, WatchFiles, etc.) must cancel the context and wait for completion to avoid test pollution.
- **Flaky time-dependent tests**: use short, generous timeouts in tests. Prefer channels and synchronization over `time.Sleep`.
