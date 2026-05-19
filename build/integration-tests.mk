# Integration and smoke test targets for the dual-server Docker dev environment.
# Included from the root Makefile. Relies on variables defined there
# (DOCKER_COMPOSE, MM_HOST, MM_PORT_A, MM_PORT_B, PLUGIN_ID) and the
# docker-check target.
#
# The test bodies live in Go at server/integration/ (build-tagged
# `integration`). See implementation-plans/26-05-11-01-migrate-integration-tests-to-go.md
# for the migration history. The targets below are either:
#
#   - The full-suite entry point (`docker-integration-test`), which
#     auto-deploys the plugin, brings up the Service Bus emulator, and
#     runs every test in the package.
#   - Single-test wrappers (`docker-smoke-test`, `docker-azure-smoke-test`,
#     etc.) that filter the suite to one Go test. These exist so CI
#     scripts and the legacy `make deploy` chain keep working. They
#     assume the plugin is already deployed (run `make deploy` first).
#   - Service Bus emulator infrastructure (`servicebus-probe-*`,
#     `docker-servicebus-up`), kept here because the emulator is
#     profile-gated (off unless explicitly brought up) and needs an
#     AMQP readiness probe before tests run.

# Service Bus emulator configuration. Consumed by servicebus-probe-run
# (the probe binary runs on the host, hence the localhost connection
# string). The dev plugin's own connection string lives in
# build/devbaseline/devbaseline.go.
SERVICEBUS_PORT_DEFAULT := 5672
SERVICEBUS_QUEUE := crossguard-relay
SERVICEBUS_HOST_CONNSTR := Endpoint=sb://localhost:$(SERVICEBUS_PORT_DEFAULT);SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true

## Build the host-side servicebus-probe binary that reports whether
## the emulator's AMQP listener is responsive.
.PHONY: servicebus-probe-build
servicebus-probe-build:
	@echo "Building servicebus-probe..."
	@go build -o ./build/bin/servicebus-probe ./build/servicebus-probe

## Bring up the Service Bus emulator + SQL sidecar (servicebus compose
## profile). The servicebus-* services are profiled so a plain
## `docker compose up` skips them; this target opts in.
.PHONY: docker-servicebus-up
docker-servicebus-up:
	@echo "Starting Service Bus emulator and SQL sidecar (profile: servicebus)..."
	@$(DOCKER_COMPOSE) --profile servicebus up -d servicebus-emulator

## Probe the Service Bus emulator until it answers AMQP PeekMessages
## or the deadline elapses. Used as a dependency by
## docker-integration-test and docker-servicebus-smoke-test so tests
## don't run against a half-started emulator.
.PHONY: servicebus-probe-run
servicebus-probe-run: servicebus-probe-build docker-servicebus-up
	@echo "Probing Service Bus emulator readiness..."
	@./build/bin/servicebus-probe -connstr '$(SERVICEBUS_HOST_CONNSTR)' -queue '$(SERVICEBUS_QUEUE)' -deadline 120s

# Shared go-test invocation used by every wrapper below. Tests live in
# server/integration/, gated by the `integration` build tag.
#
# -count=1 disables the test cache: these tests have side effects on
# docker state, so cached PASS is meaningless.
GO_TEST_INTEGRATION = MM_HOST=$(MM_HOST) MM_PORT_A=$(MM_PORT_A) MM_PORT_B=$(MM_PORT_B) \
	DOCKER_COMPOSE_FILE=docker-compose.dev.yml \
	CROSSGUARD_WIRE_VALIDATE=$(CROSSGUARD_WIRE_VALIDATE) \
	go test -tags=integration -timeout=30m -count=1 -v ./server/integration/...

## Full Go integration suite. Self-contained: builds & deploys the
## plugin, brings up the Service Bus emulator, then runs every test in
## the package (including the TEMPORARY warmup test that absorbs the
## upstream RegisterPluginForSharedChannels/PingNow race; remove when
## that fix lands).
##
## Dependencies (in order):
##   - docker-check: containers must already be up (`make docker-setup`)
##   - servicebus-probe-run: emulator up before plugin starts so its
##     first AMQP attempt succeeds rather than logging warnings
##   - docker-deploy: ensure the deployed plugin matches local source
##     and the baseline connection set is configured
##
## We depend on docker-deploy (not the `deploy` alias) to avoid
## chaining the legacy docker-smoke-test, which is now a single-test
## wrapper that runs inside this full suite anyway.
.PHONY: docker-integration-test
docker-integration-test: docker-check servicebus-probe-run docker-deploy
	@echo ""
	@echo "Running integration test suite (server/integration/)..."
	@$(GO_TEST_INTEGRATION)

## Same as docker-integration-test but with the wire-validation gate
## enabled: every outbound envelope the plugin emits during the suite
## is captured to /mattermost/wire-archive in each container, copied
## back via `docker compose exec cat`, and validated against
## schema/crossguard.xsd with xmllint. Adds one tmpfs write per
## envelope plus an xmllint exec per envelope at teardown; otherwise
## identical to docker-integration-test.
.PHONY: docker-integration-test-validate-wire
docker-integration-test-validate-wire:
	@$(MAKE) CROSSGUARD_WIRE_VALIDATE=1 docker-integration-test

# ---------------------------------------------------------------------
# Single-test wrappers.
#
# Each wrapper invokes the Go test package with a `-run` filter matching
# exactly one Test function. They exist so CI scripts that historically
# called these targets keep working; the bodies are no-ops in the
# makefile and live in Go.
#
# These wrappers do NOT run the warmup test (the warmup only runs when
# the full suite runs unfiltered). Run them only after the dev plugin
# has been warm for a minute or two, or invoke the full suite via
# `make docker-integration-test`. The legacy `make deploy` chain still
# calls docker-smoke-test directly after docker-deploy; that one was
# historically flaky and remains so until the upstream race fix lands.
# ---------------------------------------------------------------------

.PHONY: docker-smoke-test
docker-smoke-test: docker-check
	@$(GO_TEST_INTEGRATION) -run '^TestSmoke$$'

.PHONY: docker-post-lifecycle-test
docker-post-lifecycle-test: docker-check
	@$(GO_TEST_INTEGRATION) -run '^TestPostLifecycle$$'

.PHONY: docker-profile-image-test
docker-profile-image-test: docker-check
	@$(GO_TEST_INTEGRATION) -run '^TestProfileImage$$'

.PHONY: docker-file-filter-test
docker-file-filter-test: docker-check
	@$(GO_TEST_INTEGRATION) -run '^TestFileFilter$$'

.PHONY: docker-prompt-test
docker-prompt-test: docker-check
	@$(GO_TEST_INTEGRATION) -run '^TestPromptChannel$$'

.PHONY: docker-azure-smoke-test
docker-azure-smoke-test: docker-check
	@$(GO_TEST_INTEGRATION) -run '^TestAzureQueue$$'

.PHONY: docker-azure-blob-smoke-test
docker-azure-blob-smoke-test: docker-check
	@$(GO_TEST_INTEGRATION) -run '^TestAzureBlob$$'

.PHONY: docker-servicebus-smoke-test
docker-servicebus-smoke-test: docker-check servicebus-probe-run
	@$(GO_TEST_INTEGRATION) -run '^TestServiceBus$$'
