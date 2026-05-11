# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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

Cross Guard is a Mattermost Federal plugin that enables cross-domain message relay between Mattermost servers. Messages flow through pluggable transport providers (NATS, Azure Queue, Azure Blob).

- **Plugin ID**: `crossguard`
- **Min Mattermost Version**: 6.2.1

## Build and Test Commands

| Command | Description |
|---------|-------------|
| `make dist` | Build plugin bundle |
| `make test` | Run all backend and frontend tests |
| `make check-style` | Lint (golangci-lint + eslint + type-check) |
| `make coverage` | Backend + frontend coverage summaries |
| `make deploy` | Build, deploy to Docker, and run NATS smoke test |

### Running a Single Go Test

```bash
cd server && go test -run TestFunctionName -v ./...
# Or target a specific package:
cd server && go test -run TestFunctionName -v ./store/
```

### Running Frontend Tests

```bash
cd webapp && npm run test              # Unit tests
cd webapp && npm run test:pw-ct        # Playwright component tests
```

## Architecture

### Backend Message Flow

```
Outbound: MM Post Hook -> buildEnvelope() -> publishToOutbound() -> QueueProvider.Publish()
Inbound:  QueueProvider.Subscribe() -> handleInboundMessage() -> resolve team/channel -> create post
```

**Outbound path** (`hooks.go` -> `connections.go`): Mattermost hook methods (`MessageHasBeenPosted`, `MessageHasBeenUpdated`, `MessageHasBeenDeleted`, `ReactionHasBeenAdded/Removed`) check if the channel is relay-enabled, build an `Envelope`, and publish to all matching outbound providers.

**Inbound path** (`inbound.go`): Each inbound connection subscribes to its provider. Messages are unmarshaled from JSON or XML into `model.Envelope`, then dispatched by type to handlers that resolve team/channel, check idempotency via post mappings, resolve or create sync users, and create/update/delete local posts.

**Retry queue** (`retry_queue.go`, `retry_dispatch.go`): When inbound messages reference posts or users that don't exist yet (out-of-order delivery), they're enqueued for retry (max 3 attempts, 20s delay, 2min max age).

### QueueProvider Interface (`server/provider.go`)

All transport providers implement `QueueProvider`:

```go
type QueueProvider interface {
    Publish(ctx context.Context, data []byte) error
    Subscribe(ctx context.Context, handler func(data []byte) error) error
    UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error
    WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error
    MaxMessageSize() int
    Close() error
}
```

Three implementations:

- **NATS** (`nats_provider.go`): Push-based subscription, JetStream Object Store for files, no message size limit
- **Azure Queue** (`azure_provider.go`): Polling-based (5s queue, 15s blob), 48KB message limit, base64 encoding
- **Azure Blob** (`azure_blob_provider.go`): Batch-based via Write-Ahead Log (WAL). Messages accumulate locally in JSONL, flush to blob on interval (60s default). Distributed locking via KV store. Most complex provider.

### Store Layer (`server/store/`)

- `KVStore` interface (`store.go`): Team/channel connection management, post ID mappings (remote->local for idempotency), delete flags (loop prevention), connection prompts (accept/block state), team rewrite index (remote->local team name mapping)
- `CachingKVStore` (`caching.go`): Wraps KVStore with expirable LRU caches (15min TTL). Cluster cache invalidation via `OnPluginClusterEvent` with 4 event types (`cache_inv_teaminit`, `cache_inv_initteams`, `cache_inv_chaninit`, `cache_inv_rwindex`)
- `client.go`: Implementation using Mattermost's pluginapi.Client KV operations. Keys are prefixed (e.g. `team_conns:`, `post_mapping:`, `deleting_flag:`)

### Model Layer (`server/model/`)

- `Envelope`: Top-level message wrapper with `Type` field (e.g. `crossguard_post`, `crossguard_delete`) and format-agnostic serialization (JSON/XML auto-detected by first byte)
- `PostMessage`, `DeleteMessage`, `ReactionMessage`, `TestMessage`: Type-specific payloads

### Service Layer (`server/service.go`)

Business logic for init/teardown of team and channel connections. Called by both the REST API and slash commands. Manages the lifecycle: link connection, post announcement, update channel headers.

### Prompt System (`server/prompt.go`)

When an inbound message arrives for an unlinked team or channel, the plugin posts an interactive Accept/Block prompt to admins. Prompts are stored in KV with "pending" or "blocked" state. Reset via `/crossguard reset-prompt`.

### Sync Users (`server/sync_user.go`)

Creates synthetic local users (username format: `{remote}.{connName}`) to represent remote senders. When `UsernameLookup` is enabled, attempts to match a real local user first.

### Frontend (`webapp/src/`)

- `index.tsx`: Plugin entry point. Registers admin console custom settings, root components (modals), sidebar indicators, user popover attributes, channel header and main menu actions. Monitors Redux store for team/channel changes to update connection state.
- `ConnectionSettings.tsx`: Admin console UI for configuring inbound/outbound connections with provider-specific forms. Used for both InboundConnections and OutboundConnections settings.
- `CrossguardChannelModal.tsx` / `CrossguardTeamModal.tsx`: Modals for linking/unlinking connections at channel and team level. Opened via custom DOM events (`crossguard:open-modal`, `crossguard:open-team-modal`).
- `connection_state.ts`: Observer pattern for channel connection state. Global map with subscribe/notify, updated via API polling and WebSocket events.
- `CrossguardChannelIndicator.tsx`: Sidebar icon for channels with active connections.
- `CrossguardUserPopover.tsx`: Shows "Relayed from" info for sync users.

### API Endpoints (`server/api.go`)

All under `/plugins/crossguard/api/v1/`:

- `POST /test-connection` -- Test provider connectivity
- `POST /teams/{id}/init|teardown` -- Link/unlink team connections
- `POST /channels/{id}/init|teardown` -- Link/unlink channel connections
- `GET /status`, `GET /teams/{id}/status`, `GET /channels/{id}/status` -- Connection status
- `GET /channels/connections?ids=...` -- Bulk channel connection lookup
- `POST /prompt/accept|block`, `POST /prompt/channel/accept|block` -- Prompt responses
- `POST /teams/{id}/rewrite`, `DELETE /teams/{id}/rewrite` -- Team name rewrite mapping

### Slash Commands (`server/command.go`)

`/crossguard <subcommand>`: `init-team`, `init-channel`, `teardown-team`, `teardown-channel`, `reset-prompt`, `reset-channel-prompt`, `rewrite-team`, `status`, `help`

## Auto-Generated Files

- `server/manifest.go` -- Generated from `plugin.json`. Do not edit manually.
- `webapp/src/manifest.ts` -- Generated from `plugin.json`. Do not edit manually.

## Go Files

After editing Go files, run `make check-style` to fix import formatting. The golangci-lint config (`.golangci.yml`) auto-rewrites `interface{}` to `any` and sorts imports with local prefix `github.com/MattermostFederal/mattermost-plugin-crossguard`.

## Logging

**Never log sensitive data.** Log calls at any level must not include message content (post bodies, file contents, attachment data), authentication material (tokens, passwords, API keys), or anything else that would compromise privacy or security if it appeared in operator log aggregation. Log identifiers (channel ID, user ID, post ID, remote ID), counts, sizes, and configuration names instead. This applies to error context too: when wrapping a downstream error, prefer the error type/code over its full message if the message could include user input.

## Log Error Codes

Every `p.API.LogInfo`, `LogWarn`, and `LogError` call in non-test code must include a unique numeric error code as the first key-value pair, sourced from `server/errcode/codes.go`. `LogDebug` calls are exempt: debug logs are development aids, not part of the operator-facing contract, and their content can change freely without breaking anything operators depend on.

```go
p.API.LogError("Failed to check channel connections",
    "error_code", errcode.HooksChannelConnCheckFailed,
    "channel_id", channelID, "error", err.Error())
```

When adding a new info/warn/error log call:

1. Open `server/errcode/codes.go` and find the block for the file you are editing (each file owns a 1000-range, e.g. `hooks.go` uses 10000-10999, `inbound.go` uses 15000-15999).
2. Append a new constant at the next unused integer in that block. Name it `<FilePrefix><CamelCaseSummary>` describing the event, not the log level.
3. Append the new constant to the `AllCodes` slice at the bottom of the file. `TestCodesUnique` enforces uniqueness.
4. Reference it from the log call as `"error_code", errcode.YourConstant` as the first K/V pair.
5. Never reuse or renumber existing codes. The integer value is the stable contract for log grep; the identifier can be renamed, but the number must not change once assigned.

Test-file log calls do not need error codes. Permissive test mocks should use the `registerLogMocks(api, "LogInfo", "LogWarn", ...)` helper in `server/prompt_test.go`, which registers `.Maybe()` expectations at arities 1-16.

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

### Docker Commands

| Command | Description |
|---------|-------------|
| `make docker-start` | Start containers (without user setup) |
| `make docker-stop` | Stop containers (preserves data) |
| `make docker-down` | Stop and remove containers |
| `make docker-clean` | Remove containers and all data |
| `make docker-logs` | Follow Server A logs |
| `make docker-logs-b` | Follow Server B logs |
| `make docker-reset` | Disable and re-enable plugin on both servers |
| `make docker-smoke-test` | Quick NATS relay smoke test |
| `make docker-integration-test` | Full integration suite (loopback, files, XML, Azure) |
| `make docker-azure-smoke-test` | Run Azure Queue/Blob relay smoke test via Azurite |
| `make docker-azure-blob-smoke-test` | Run Azure Blob batched (WAL + deferred file) smoke test via Azurite |
| `make docker-servicebus-smoke-test` | Run Azure Service Bus relay smoke test via the Service Bus emulator (+ SQL Server Linux sidecar). Readiness gated by `servicebus-probe`. |
| `make docker-disable` | Disable plugin on both servers |
| `make docker-enable` | Enable plugin on both servers |
| `make docker-plugin-list` | List installed plugins on both servers |
| `make docker-kill-orphans` | Kill orphaned containers on MM ports |

### Release

| Command | Description |
|---------|-------------|
| `make release` | Full release: checks, tests, SBOM audit, CodeQL, sign, checksum |
| `make release-tag` | Create git tag for the current version |

## Technology Stack

### Backend
- Go 1.26.1
- Mattermost Plugin API
- Gorilla Mux (routing)
- NATS (nats.go v1.49.0) for message relay
- Azure SDK (azqueue v1.0.1, azblob v1.6.4) for Azure Queue/Blob provider

### Frontend
- React 18.2, TypeScript 5.9, Redux 5.0, Webpack 5.105
- Mattermost Redux
- Node.js 20.11

### Testing
- Go: `stretchr/testify`
- Frontend: Playwright 1.59.1 (E2E and component testing)

## Reference Documentation

- [Mattermost Plugin Development](https://developers.mattermost.com/integrate/plugins/)
- [Webapp API Reference](https://developers.mattermost.com/integrate/reference/webapp/webapp-reference/)
- [Plugin Starter Template](https://github.com/mattermost/mattermost-plugin-starter-template)
