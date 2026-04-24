# Cross Guard - Mattermost Plugin

## Critical Workflow

**Research -> Plan -> Implement** -- never jump straight to coding.

1. **Research**: Explore the codebase, understand existing patterns
2. **Plan**: Create a detailed implementation plan and verify it with the user
3. **Implement**: Execute the plan with validation checkpoints

When asked to implement any feature, first say: "Let me research the codebase and create a plan before implementing."

## Use Multiple Agents

Leverage subagents aggressively for better results:

- Spawn agents to explore different parts of the codebase in parallel
- Use one agent to write tests while another implements features
- Delegate research tasks
- For complex refactors: one agent identifies changes, another implements them

## Working Memory Management

When context gets long:

- Re-read CLAUDE.md files to refresh context
- Summarize progress before major changes
- Document current state before large refactors

## Problem-Solving Approach

When stuck or confused:

1. **Stop** -- don't spiral into complex solutions
2. **Delegate** -- consider spawning agents for parallel investigation
3. **Step back** -- re-read the requirements
4. **Simplify** -- the simple solution is usually correct
5. **Ask** -- "I see two approaches: [A] vs [B]. Which do you prefer?"

## Formatting

- Never use em dashes in code, comments, strings, or commit messages. Use commas, periods, or parentheses instead.

## Important Development Notes

- Always run checks (`make check-style`, `make test`) before committing code
- When in doubt, choose clarity over cleverness
- Avoid complex abstractions or "clever" code
- Do not add verbose comments; only comment complex operations
- Never fabricate commands that don't exist
- Never add synthetic or mock data to fix failing tests

## Absolute Git Prohibitions

These commands destroy uncommitted work and are **forbidden**:

- **Never run `git checkout -- <path>`**
- **Never run `git checkout HEAD -- <path>`**
- **Never run `git restore <path>`**
- **Never run `git reset --hard`**

### What To Do Instead

```bash
# CORRECT WAY:
git stash                    # Save changes safely
# ... run tests ...
git stash pop                # Restore changes
```

## Code Analysis Standards

Before raising concerns, trace actual execution paths and construct specific failing scenarios. Focus on evidence-based claims with concrete code examples rather than theoretical issues.

## Project Overview

Cross Guard is a Mattermost Federal plugin built on the mattermost-plugin-starter-template. It provides a Go backend with a React/TypeScript frontend.

- **Plugin ID**: `crossguard`
- **Min Mattermost Version**: 6.2.1

## Reference Documentation

- [Mattermost Plugin Development](https://developers.mattermost.com/integrate/plugins/)
- [Webapp API Reference](https://developers.mattermost.com/integrate/reference/webapp/webapp-reference/)
- [Plugin Starter Template](https://github.com/mattermost/mattermost-plugin-starter-template)
- [Plugin Overview](https://developers.mattermost.com/integrate/plugins/overview/)
- [Demo Plugin](https://github.com/mattermost/mattermost-plugin-demo)

## Auto-Generated Files

- `server/manifest.go` -- Generated from `plugin.json`. Do not edit manually.
- `webapp/src/manifest.ts` -- Generated from `plugin.json`. Do not edit manually.

## Key Files

- `plugin.json` - Plugin manifest (ID, version, min server version)
- `go.mod` - Go dependencies
- `Makefile` - Build commands
- `.golangci.yml` - Go linter configuration
- `docker-compose.dev.yml` - Docker development environment
- `.nvmrc` - Node.js version (20.11)
- `CHANGELOG.md` - Version history

## Project Structure

- `server/` - Go backend (plugin API, providers, store, model)
- `webapp/` - React/TypeScript frontend (admin console components)
- `build/` - Build tooling (manifest generation, deployment scripts)
- `docker/` - Docker compose config and plugin volumes
- `schema/` - Data schemas (XSD)
- `implementation-plans/` - Feature planning documents
- `assets/` - Plugin icon assets
- `public/` - Static assets

## Docker Development Environment (Dual-Server)

The dev environment runs two Mattermost servers with a shared NATS bus and an Azurite (Azure Storage Emulator) instance for Azure Queue/Blob testing.

### Getting Started

```bash
make hosts-setup    # Add low.test and high.test to /etc/hosts (one-time, requires sudo)
make docker-setup   # Start containers, create users and teams
make deploy         # Build, deploy, and run quick NATS smoke test
```

After setup:

- **Server A (Low)**: http://low.test:8075 (admin/password, usera/password, useraa/password, Team: Test A)
- **Server B (High)**: http://high.test:8076 (admin/password, userb/password, Team: Test B)
- **NATS**: nats://localhost:4222 (monitor: http://localhost:8222)
- **NATS (from plugins)**: nats://nats:4222
- **Azurite Queue**: http://localhost:10001
- **Azurite Blob**: http://localhost:10000
- **Service Bus Emulator (AMQP)**: sb://localhost:5672 (from plugins: `sb://servicebus-emulator:5672`). Default SAS connection string: `Endpoint=sb://servicebus-emulator;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true`

### Common Commands

| Command | Description |
|---------|-------------|
| `make hosts-setup` | Add low.test/high.test to /etc/hosts (requires sudo) |
| `make docker-setup` | First-time setup: start containers, create users and teams |
| `make deploy` | Build, deploy plugin, and run quick NATS smoke test |
| `make dist` | Build plugin bundle only |
| `make test` | Run all tests |
| `make coverage` | Run Go tests and print code coverage summary |
| `make check-style` | Lint code |
| `make nuke` | Remove everything: containers, data, build artifacts |

### Docker Management Commands

| Command | Description |
|---------|-------------|
| `make docker-start` | Start containers (without user setup) |
| `make docker-stop` | Stop containers (preserves data) |
| `make docker-down` | Stop and remove containers |
| `make docker-clean` | Remove containers and all data |
| `make docker-logs` | Follow Server A logs |
| `make docker-logs-b` | Follow Server B logs |
| `make docker-reset` | Disable and re-enable plugin on both servers |
| `make docker-smoke-test` | Quick NATS relay smoke test (single low-to-high message) |
| `make docker-integration-test` | Full integration suite (loopback, files, XML, Azure) |
| `make docker-azure-smoke-test` | Run Azure Queue/Blob relay smoke test via Azurite |
| `make docker-azure-blob-smoke-test` | Run Azure Blob batched (WAL + deferred file) smoke test via Azurite |
| `make docker-servicebus-smoke-test` | Run Azure Service Bus relay smoke test via the Service Bus emulator (+ SQL Server Linux sidecar). Readiness gated by `servicebus-probe`. |
| `make docker-disable` | Disable plugin on both servers |
| `make docker-enable` | Enable plugin on both servers |
| `make docker-plugin-list` | List installed plugins on both servers |
| `make docker-kill-orphans` | Kill orphaned containers on MM ports |

### Release and Security

| Command | Description |
|---------|-------------|
| `make release` | Full release: checks, tests, SBOM audit, CodeQL, sign, checksum |
| `make release-tag` | Create git tag for the current version |
| `make sbom-audit` | Generate SBOMs and scan for vulnerabilities |
| `make codeql-analyze` | Run CodeQL security analysis on all code |
| `make security-gate` | Check scan results for critical/high issues |
| `make virus-scan` | Run ClamAV virus scan on built artifacts |
| `make generate-pdfs` | Generate PDF documentation |

## Technology Stack

### Backend
- Go 1.26.1
- Mattermost Plugin API
- Gorilla Mux (routing)
- NATS (nats.go v1.49.0) for message relay
- Azure SDK (azqueue v1.0.1, azblob v1.6.4) for Azure Queue/Blob provider

### Frontend
- React 18.2
- TypeScript 5.9
- Redux 5.0
- Webpack 5.105
- Mattermost Redux
- Node.js 20.11

### Testing
- Go: `stretchr/testify`
- Frontend: Playwright 1.59.1 (E2E and component testing)

## Go Files

After editing Go files, run `make check-style` to fix import formatting.

## Log Error Codes

Every `p.API.Log*` call (`LogDebug`, `LogInfo`, `LogWarn`, `LogError`) in non-test code must include a unique numeric error code as the first key-value pair, sourced from `server/errcode/codes.go`.

```go
p.API.LogError("Failed to check channel connections",
    "error_code", errcode.HooksChannelConnCheckFailed,
    "channel_id", channelID, "error", err.Error())
```

When adding a new log call:

1. Open `server/errcode/codes.go` and find the block for the file you are editing (each file owns a 1000-range, e.g. `hooks.go` uses 10000-10999, `inbound.go` uses 15000-15999).
2. Append a new constant at the next unused integer in that block. Name it `<FilePrefix><CamelCaseSummary>` describing the event, not the log level.
3. Append the new constant to the `AllCodes` slice at the bottom of the file. `TestCodesUnique` enforces uniqueness.
4. Reference it from the log call as `"error_code", errcode.YourConstant` as the first K/V pair.
5. Never reuse or renumber existing codes. The integer value is the stable contract for log grep; the identifier can be renamed, but the number must not change once assigned.

Test-file log calls do not need error codes. Permissive test mocks should use the `registerLogMocks(api, "LogInfo", "LogWarn", ...)` helper in `server/prompt_test.go`, which registers `.Maybe()` expectations at arities 1-16.
