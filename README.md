# Cross Guard - Mattermost Plugin

Cross Guard plugin for Mattermost Federal. Enables cross-domain message relay between Mattermost servers using pluggable transport providers (NATS, Azure Queue Storage, Azure Blob Storage, or Azure Service Bus) with a JSON or XML wire format.

For the architecture, threat model, and design rationale, see the [Cross Guard Whitepaper (PDF)](public/help/crossguard-whitepaper.pdf). GitHub renders the PDF inline when you open the link. The companion [Threat Model (PDF)](public/help/crossguard-threatmodel.pdf), [Transport Interface Reference (PDF)](public/help/crossguard-transport-interface.pdf), and [Help Guide (PDF)](public/help/crossguard-help.pdf) live in the same directory.

## Development

### Prerequisites

- Go 1.26+
- Node.js 20+
- Docker (for local development environment)

### Quick Start

```bash
# Set up /etc/hosts for dual-server hostnames
make hosts-setup

# Install dependencies and build
cd webapp && npm install && cd ..
make dist

# Docker development environment (dual Mattermost servers + NATS)
make docker-setup
make deploy
```

### Docker Development Environment (Dual-Server)

The dev environment runs two Mattermost servers (A and B) with a shared NATS bus, an Azurite instance (Azure Storage Emulator), and an Azure Service Bus emulator (opt-in) for cross-domain relay testing with any provider.

After `make docker-setup`:

- **Server A (Low)**: http://low.test:8075
  - Admin: `admin / password`
  - User: `usera / password`
  - Team: Test A
- **Server B (High)**: http://high.test:8076
  - Admin: `admin / password`
  - User: `userb / password`
  - Team: Test B
- **NATS**: `nats://localhost:4222` (monitor: http://localhost:8222)
- **Azurite Queue**: `http://localhost:10001`
- **Azurite Blob**: `http://localhost:10000`
- **Service Bus emulator (AMQP)**: `sb://localhost:5672` (opt-in, started via `make docker-servicebus-smoke-test` or as a dependency of `make docker-integration-test`)

`make deploy` builds the plugin, deploys it to both servers, configures the baseline connection set (NATS + Azure Queue + Azure Blob + Service Bus), then runs a quick smoke test. Use `make docker-integration-test` for the full Go test suite (smoke, post lifecycle, profile image, file filter, prompt accept/block, rewrite team, XML format, and all four transport providers).

### Transport Providers

Cross Guard supports four pluggable transport providers, selectable per connection in the admin console. All four carry the same wire format and the same file-transfer semantics; they differ in delivery model, durability, and infrastructure requirements.

| Provider | Messaging | File Transfer | Auth |
|----------|-----------|---------------|------|
| **NATS** | Core pub/sub (fire-and-forget) | JetStream Object Store | Token, credentials, or mTLS |
| **Azure Queue Storage** | Azure Queue (5 s polling, at-least-once) | Azure Blob Storage (paired container, 15 s poll) | Storage account shared key |
| **Azure Blob Storage** | Batched WAL blobs (flush interval configurable, at-least-once) | Same Blob container (deferred file blobs) | Storage account shared key |
| **Azure Service Bus** | Service Bus queue (PeekLock / Complete / Abandon, at-least-once, native DLQ) | Azure Blob Storage (separate sidecar container, 15 s poll) | SAS connection string |

The server-side reference is at [`public/help/transport-interface.html`](public/help/transport-interface.html) (also shipped as a generated PDF) and the OpenAPI/JSON schema for every provider's configuration block is at [`schema/crossguard-api.yaml`](schema/crossguard-api.yaml).

### Wire Format: JSON vs XML

Each connection's `message_format` setting chooses how Cross Guard serializes the message envelope before handing it to the transport. Both formats carry identical semantic content; the wire bytes differ only in representation.

| Aspect | JSON | XML |
|--------|------|-----|
| When to use | Default. Fast, compact, the natural choice when both sides are Cross Guard plugins. | Integrations with cross-domain solutions (CDS), content filters, or data-diode shuttles that speak XML natively. |
| Schema | `schema/crossguard-api.yaml` (OpenAPI 3.1) models the JSON envelope and payloads. | [`schema/crossguard.xsd`](schema/crossguard.xsd) is the normative XML Schema; every outbound payload is schema-valid by construction. |
| Examples | [REST API reference](public/help/api.html) shows full request/response shapes. | [`schema/examples/`](schema/examples/) contains one schema-valid `.xml` file per message type (post, update, delete, reaction add/remove, test) plus a [README](schema/examples/README.md) walking through them. |
| Inbound detection | Auto-detected by [`model.DetectFormat`](server/model/message.go): the first non-whitespace byte decides (`<` means XML, anything else means JSON). | Same. Inbound connections do not need to be told which format to expect; outbound connections do. |
| BOM | N/A. | A leading UTF-8 BOM on inbound XML is stripped automatically before format detection. |

To validate the example XML payloads against the schema locally:

```sh
for f in schema/examples/*.xml; do
  xmllint --noout --schema schema/crossguard.xsd "$f"
done
```

#### Wire-format validation in integration tests

The example fixtures in `schema/examples/` are static. To verify that the **running** plugin also emits schema-conformant XML on every outbound path (sync_msg via `publishToOutboundConn`, test envelopes via each provider's test-connection handler), the integration suite has an opt-in validation mode.

Run the full suite with the mode on:

```sh
make docker-integration-test-validate-wire
# equivalent to:  CROSSGUARD_WIRE_VALIDATE=1 make docker-integration-test
```

How it works:

- The dev compose file sets `CROSSGUARD_ENVELOPE_ARCHIVE_DIR=/mattermost/wire-archive` on both `mattermost-a` and `mattermost-b` and **host-bind-mounts** `./docker/wire-archive-a` and `./docker/wire-archive-b` to that path. The bind mount is load-bearing: a tmpfs at the same path is invisible to the host docker daemon (mount-namespace isolation between the plugin process and the container's main namespace), so the test runner cannot read the files back. The plugin reads the env var once at start and, when set, writes every successfully marshalled envelope to that directory as `<unix_nano>_<seq>_<conn>_<type>.xml`. When the env var is unset (production), the archiver is a no-op.
- When the integration test runner sees `CROSSGUARD_WIRE_VALIDATE=1`, `NewHarness` clears each server's archive at test start and registers a `t.Cleanup` that, after the test finishes, lists every archived envelope on each container via `docker compose exec`, pipes each through `xmllint --schema schema/crossguard.xsd`, and fails the test on any non-conforming output (or on a zero count, which would indicate broken plumbing).

Failures print the offending envelope's filename, the xmllint diagnostic, and the envelope contents so the producer can be traced. `xmllint` is required on `PATH`; install `libxml2-utils` on Linux (it ships pre-installed on macOS).

### Slash Commands

Once the plugin is deployed, use `/crossguard` to manage cross-domain relay:

| Command | Description |
|---------|-------------|
| `/crossguard init-team [connection-name]` | Link a NATS connection to this team (requires team admin or system admin) |
| `/crossguard init-channel [connection-name]` | Link a NATS connection to this channel (requires channel admin or higher) |
| `/crossguard teardown-team [connection-name]` | Unlink a NATS connection from this team (requires team admin or system admin) |
| `/crossguard teardown-channel [connection-name]` | Unlink a NATS connection from this channel (requires channel admin or higher) |
| `/crossguard reset-prompt <connection-name>` | Clear a pending team connection prompt (requires team admin) |
| `/crossguard reset-channel-prompt <connection-name>` | Clear a pending channel connection prompt (requires team admin) |
| `/crossguard rewrite-team [name] [team]` | Set or clear a remote team name rewrite for an inbound connection (requires team admin) |
| `/crossguard status` | Show Cross Guard status for this team |
| `/crossguard help` | Show detailed help for all Cross Guard commands |

Typical workflow: `init-team <connection-name>` first, then `init-channel <connection-name>` on each channel you want relayed.

### Common Commands

| Command | Description |
|---------|-------------|
| `make hosts-setup` | Add `low.test` and `high.test` to /etc/hosts (requires sudo) |
| `make docker-setup` | First-time setup: start containers, create users and teams |
| `make deploy` | Build, deploy plugin, and run quick NATS smoke test |
| `make dist` | Build plugin bundle only |
| `make test` | Run all tests |
| `make coverage` | Run Go tests and print code coverage summary |
| `make check-style` | Lint code |
| `make clean` | Remove build artifacts |
| `make nuke` | Remove everything: containers, data, build artifacts |

### Docker Management Commands

| Command | Description |
|---------|-------------|
| `make docker-start` | Start containers (without setup) |
| `make docker-stop` | Stop containers (preserves data) |
| `make docker-down` | Stop and remove containers |
| `make docker-clean` | Remove containers and all data |
| `make docker-logs` | Follow Server A logs |
| `make docker-logs-b` | Follow Server B logs |
| `make docker-reset` | Disable and re-enable plugin on both servers |
| `make docker-disable` | Disable plugin on both servers |
| `make docker-enable` | Enable plugin on both servers |
| `make docker-plugin-list` | List installed plugins on both servers |
| `make docker-integration-test` | Full Go integration suite (smoke, post lifecycle, profile image, file filter, prompt accept/block, rewrite team, XML, Azure Queue, Azure Blob, Azure Service Bus). Self-contained: builds + deploys plugin and brings up the SB emulator. |
| `make docker-integration-test-validate-wire` | Same as `docker-integration-test`, but with `CROSSGUARD_WIRE_VALIDATE=1`. Captures every outbound envelope the plugin emits and validates each against [`schema/crossguard.xsd`](schema/crossguard.xsd) with `xmllint`. See [Wire-format validation in integration tests](#wire-format-validation-in-integration-tests) below. |
| `make docker-smoke-test` | Single-test wrapper: `go test -run TestSmoke` |
| `make docker-post-lifecycle-test` | Single-test wrapper: `go test -run TestPostLifecycle` |
| `make docker-profile-image-test` | Single-test wrapper: `go test -run TestProfileImage` |
| `make docker-file-filter-test` | Single-test wrapper: `go test -run TestFileFilter` |
| `make docker-prompt-test` | Single-test wrapper: `go test -run TestPromptChannel` |
| `make docker-azure-smoke-test` | Single-test wrapper: `go test -run TestAzureQueue` |
| `make docker-azure-blob-smoke-test` | Single-test wrapper: `go test -run TestAzureBlob` |
| `make docker-servicebus-smoke-test` | Single-test wrapper: `go test -run TestServiceBus` (also brings up the Service Bus emulator) |
| `make docker-kill-orphans` | Kill orphaned containers on MM ports |

### Release

```bash
make release        # Full build: checks, tests, SBOM audit, CodeQL, sign, checksum
make release-tag    # Create git tag for the version
git push origin v$(PLUGIN_VERSION)
```

### Security Scanning

| Command | Description |
|---------|-------------|
| `make sbom` | Generate SBOMs (CycloneDX) for Go and Node.js |
| `make sbom-audit` | Generate SBOMs and scan for vulnerabilities |
| `make codeql-analyze` | Run CodeQL on Go and JavaScript/TypeScript |
| `make security-gate` | Check scan results for critical/high issues |
