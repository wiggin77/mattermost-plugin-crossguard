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
- **Service Bus emulator (AMQP)**: `sb://localhost:5672` (opt-in, started via `make docker-servicebus-smoke-test`)

`make deploy` automatically configures Server A with an outbound connection and Server B with an inbound connection, then runs a quick NATS smoke test. Use `make docker-integration-test` for the full test suite (loopback, file relay, XML, Azure Queue, Azure Blob, Azure Service Bus).

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
| `make docker-smoke-test` | Quick NATS relay smoke test (single low-to-high message) |
| `make docker-integration-test` | Full integration suite (loopback, files, XML, Azure Queue, Azure Blob, Azure Service Bus) |
| `make docker-azure-smoke-test` | Run Azure Queue/Blob relay smoke test via Azurite |
| `make docker-azure-blob-smoke-test` | Run Azure Blob batched (WAL + deferred file) smoke test via Azurite |
| `make docker-servicebus-smoke-test` | Run Azure Service Bus relay smoke test via the Service Bus emulator (starts the emulator + SQL Server sidecar on demand) |
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
