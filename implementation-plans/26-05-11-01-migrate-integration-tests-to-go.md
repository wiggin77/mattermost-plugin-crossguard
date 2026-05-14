# Migrate integration tests from Makefile to Go

## Context

`build/integration-tests.mk` is ~1250 lines of shell wrapped in make recipes.
Adding a single test costs 80 to 150 lines of boilerplate (login, channel
lookup, polling loop, JSON parsing) plus the cognitive tax of three nested
quoting levels (make `$$`, shell `"..."`, python `"..."`). Failures abort
silently inside `&& \` chains and the only debugging tool is "rerun and watch
output."

The work is fundamentally HTTP + JSON against two Mattermost servers in
docker, with occasional `mmctl --local` and `docker compose exec` calls.
The plugin already depends on `github.com/mattermost/mattermost/server/public/model`,
which ships a typed `Client4`. Re-using it for the integration suite gives
us real types, real errors, real stack traces, and reusable helpers.

This plan migrates the test bodies to Go while keeping Docker provisioning
(setup, deploy, plugin reset, container lifecycle) in make. The make
entry point `make docker-integration-test` is preserved.

## Out of scope

- Replacing `docker-setup`, `docker-deploy`, `docker-start`, `docker-stop`,
  `hosts-setup`, `docker-servicebus-up`, or `servicebus-probe-run`. These
  are infrastructure, not tests, and stay in make.
- Migrating the Go unit tests in `server/*_test.go`. They are fine as-is.
- Adding new test coverage. The migration is one-for-one; new gaps are
  separate work. Exception: tests that obviously parameterize over a
  `transport` axis (lifecycle being the clearest case) should be migrated
  as table-driven even though the table has only one row at migration
  time. The shape is in scope for the migration; populating additional
  rows is the follow-up coverage plan's job.
- Parallelizing tests. The current suite is serial because tests share the
  `low-to-high` channel and global plugin config; that constraint does not
  go away with the language change.

## Target structure

```
server/integration/
├── doc.go              package docs and build-tag note
├── harness.go          Harness struct, NewHarness, login, client setup
├── docker.go           shell-out helpers (mmctl, docker compose exec)
├── config.go           plugin config read/patch/restore
├── channel.go          channel/team/user/membership/init helpers
├── post.go             post create/edit/delete/react helpers
├── poll.go             generic Eventually-style polling
├── png.go              go:embed or stdlib image/png unique avatar generator
├── smoke_test.go       NATS smoke (replaces docker-smoke-test body)
├── lifecycle_test.go   Gap 1: edits, deletes, reactions
├── profile_image_test.go   Gap 2
├── prompt_test.go          Gap 3: channel accept/block
├── file_filter_test.go     Gap 4: sender/receiver deny
├── rewrite_test.go         rewrite-team
├── xml_test.go             xml-low-to-high
├── azure_queue_test.go     Azure Queue message + file
├── azure_blob_test.go      Azure Blob (batched) message + file
└── servicebus_test.go      Service Bus message + file
```

All files carry `//go:build integration` so `go test ./...` ignores them.
The make target invokes `go test -tags=integration -timeout=30m -count=1 ./server/integration/...`.

`-count=1` disables the test cache: these tests have side effects on docker
state, so cached PASS is meaningless.

## Harness sketch

```go
type Harness struct {
    A, B          Server          // host, port, admin token, plugin id
    AdminA, AdminB *model.Client4
    UserClients   map[string]*model.Client4 // login user -> client
    Compose       *DockerCompose
}

func NewHarness(t *testing.T) *Harness   // logs in admin on both, fails fast
func (h *Harness) ClientAs(t, server, username) *model.Client4
func (h *Harness) EnsureUser(t, server, username, email)         // mmctl
func (h *Harness) EnsureChannel(t, client, teamName, name) *Channel
func (h *Harness) AddMember(t, client, channelID, userID)
func (h *Harness) InitChannel(t, client, channelID, direction, conn) // slash
func (h *Harness) ResetPlugin(t, server)                          // mmctl
func (h *Harness) PatchPluginConfig(t, server, mutator func(*Cfg)) (restore func())
```

Polling primitive (returns the matched value, fails the test on timeout):

```go
func Eventually[T any](t *testing.T, timeout time.Duration, fn func() (T, bool)) T
```

Used as:

```go
bPost := Eventually(t, 20*time.Second, func() (*model.Post, bool) {
    posts, _, err := h.AdminB.GetPostsForChannel(ctx, lthB.Id, 0, 20, "", false, false)
    if err != nil { return nil, false }
    for _, p := range posts.Posts {
        if strings.Contains(p.Message, marker) { return p, true }
    }
    return nil, false
})
```

## Migration phases

### Phase 1: Foundation and proof point

Build the harness and port one test end-to-end. Goal: validate the pattern
before bulk migration.

1. Create `server/integration/` with build tag, harness, docker helper,
   poll primitive, and config patcher.
2. Add `make docker-integration-test-go` (parallel target; original
   target stays untouched). Wires `go test -tags=integration -count=1 -timeout=30m`.
3. Port `docker-post-lifecycle-test` to `TestPostLifecycle`. This test
   touches every primitive (post, edit, react, delete, poll, single-post-
   GET-by-id) without needing config patches, so it exercises the harness
   without the complexity of plugin resets. Structure the test body as a
   table-driven loop over a `transport` axis (channel name, connection
   name, optional plugin-config setup hook), and populate the table with
   a single NATS row at migration time (one-for-one with the shell test).
   This reserves the shape so the follow-up coverage plan closes the
   Azure lifecycle gap by adding Azure Queue, Azure Blob, and Service Bus
   rows rather than rewriting or refactoring the test. Same shape applies
   to other migrated tests whose underlying capability is transport-
   agnostic (profile image, file filter, prompt accept/block); keep them
   single-row at migration time.
4. Run both old and new versions back to back; confirm equivalence.

**Exit criterion:** `make docker-integration-test-go` runs the lifecycle
test green, and the Go test is shorter and clearer than the shell one.

### Phase 2: Migrate stateless tests

Tests that do not patch plugin config. Reuse the existing low-to-high
linkage from `make deploy`.

- Smoke test (`docker-smoke-test`)
- Rewrite-team test
- XML wire format test (uses dedicated user `userc`)
- File attachment relay (PDF and DOCX)
- Profile image sync (Gap 2; bring `gen-unique-png.py` over as `png.go`
  using stdlib `image/png` and `bytes.Buffer`)

**Exit criterion:** `go test -tags=integration -run 'TestSmoke|TestRewrite|TestXML|TestFileAttachment|TestProfileImage'` is green and these targets are removed from the shell makefile section that runs them.

### Phase 3: Migrate config-patching tests

Tests that need `PatchPluginConfig` + plugin reset. Harness helper returns
a `restore func()` that the test defers.

- File filter (Gap 4; sender deny, sender allow negative, receiver deny)
- Prompt accept/block (Gap 3; uses fresh channel names per run, so
  scope-limited cleanup works via t.Cleanup)

**Exit criterion:** PatchPluginConfig + ResetPlugin proven against the
non-Azure providers before we use them for the Azure suite.

### Phase 4: Migrate provider-specific tests

Each needs config patching plus provider-specific verification.

- Azure Queue: message relay + PDF file relay
- Azure Blob (batched): message relay + deferred PDF file relay
- Service Bus: message relay + PDF file relay
  - The `servicebus-probe-run` make dependency stays in make. The Go test
    only runs once make has confirmed the emulator is reachable.

**Exit criterion:** `go test -tags=integration ./server/integration/...`
covers every old shell test.

### Phase 5: Cutover and cleanup

1. Replace the body of `docker-integration-test` with a single
   `go test -tags=integration` invocation. Keep the smoke test target.
2. Remove the shell recipes that were ported. Keep targets that other
   parts of the build invoke (`docker-azure-smoke-test` etc.) as thin
   wrappers around the Go test by name (`-run TestAzureQueue`) so external
   callers and CI scripts do not break.
3. Delete `build/gen-unique-png.py` (subsumed by `png.go`).
4. Update CLAUDE.md and any developer docs that mention the shell targets.

**Exit criterion:** `git diff build/integration-tests.mk` shows only the
provisioning targets remaining. No shell test recipes left.

## Risks and mitigations

- **mmctl invocation:** `mmctl --local user create` and friends run inside
  the container. `exec.Command("docker", "compose", "-f", "...", "exec",
  "-T", "mattermost-a", "mmctl", "--local", ...)` is fine but verbose;
  wrap once in `docker.go`. Exit code 1 with "already exists" is normal,
  so the helper inspects stderr.
- **mmctl deprecation / version skew:** if mmctl is unavailable, fall
  back to admin Client4 calls (`CreateUser`, `CreateTeam`, `AddTeamMember`).
  Some operations like plugin enable/disable have Client4 equivalents.
- **Plugin reset timing:** the existing shell tests `sleep 3` after a
  reset. Replace with poll-for-readiness (GET /plugins/crossguard/api/v1/status
  with a short timeout) so the test waits exactly as long as needed.
- **Test ordering and shared state:** the smoke test must run first (it
  links low-to-high). Either use a `TestMain` that runs smoke setup once,
  or use `testing.M` ordering with `-run` and a wrapper test that calls
  `t.Run("Smoke", ...); t.Run("Lifecycle", ...)` in order.
- **CI script breakage:** any external caller of the named shell targets
  (azure smoke, etc.) keeps working because phase 5 leaves those targets
  as thin wrappers.
- **Test flakiness from short polling intervals:** the shell tests poll
  every 1s. Match that in Go (`Eventually` with 1s ticker, configurable
  timeout). Use `t.Helper()` on polling primitives so failures point at
  the call site.

## Effort estimate

Roughly:

- Phase 1: half a day. Harness design dominates.
- Phase 2: half a day. Five tests at ~30 min each once the pattern is set.
- Phase 3: half a day. Config patching helper + two tests.
- Phase 4: half a day. Three provider tests, mostly mechanical.
- Phase 5: an hour or two. Mostly deletion and target wrapping.

Total: about two focused days. Spread over a week with other work.

## Why not other options

- **Pytest:** adds a Python toolchain dependency the repo currently does
  not have. The plugin already builds Go; one toolchain is better than two.
- **Bats / shellspec:** still shell; same quoting tax, just with a thin
  framework. Does not solve the actual problem.
- **Separate `.sh` scripts called from make:** removes the make `$$`
  level only. Still leaves the shell + python quoting issue. Marginal
  improvement, not worth the churn.
- **Playwright:** designed for browser flows. The integration suite is
  pure HTTP. Playwright would be overkill and require a Node runtime
  for tests that have nothing to do with the webapp.

## Open questions for the implementer

1. Should `TestMain` provision the smoke test linkage (link low-to-high
   once) or should every test that uses it call a `RequireSmokeLinkage`
   helper? `TestMain` is faster but couples tests; the helper pattern
   is more flexible if we ever parallelize.
2. Should the harness assume containers are already up (current shell
   behaviour via `docker-check`) or spin them up? Probably assume-up;
   spin-up belongs in `make`.
3. For the Service Bus emulator readiness check, do we keep the existing
   `servicebus-probe` Go binary invoked from make, or fold it into the
   harness as a SkipUnlessReady check? Folding it in is cleaner but
   couples the test to the emulator's connection string.
