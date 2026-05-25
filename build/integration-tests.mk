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
## enabled: every outbound envelope is captured to
## /mattermost/wire-archive in each container and validated against
## schema/crossguard.xsd at the end of the run. JSON envelopes are
## converted to XML through the wire types before validation (see
## build/wire-archive-validate).
##
## WIRE_FORMAT (optional) forces every outbound connection's
## message_format to "xml" or "json" before the test pass runs. Empty
## (default) uses each connection's declared message_format from
## build/devbaseline.
##
## For full XML + JSON coverage, run this target twice:
##
##   WIRE_FORMAT=xml  make docker-integration-test-validate-wire
##   WIRE_FORMAT=json make docker-integration-test-validate-wire
##
## Each invocation runs the suite once and validates the archive
## additions from its own run. The plugin picks up the wire-format
## change without a rebuild or redeploy because message_format only
## affects encoding at marshal time, not transport plumbing.
WIRE_FORMAT ?=

.PHONY: docker-integration-test-validate-wire
docker-integration-test-validate-wire: docker-check servicebus-probe-run docker-deploy
	@command -v xmllint >/dev/null 2>&1 || { \
		echo >&2 "ERROR: xmllint not found on PATH (required for CROSSGUARD_WIRE_VALIDATE=1)."; \
		echo >&2 "Install: sudo apt install libxml2-utils  (Debian/Ubuntu)"; \
		echo >&2 "         brew install libxml2            (macOS)"; \
		exit 1; \
	}
	@case '$(WIRE_FORMAT)' in \
		'') echo "WIRE_FORMAT not set: using each connection's declared message_format from build/devbaseline" ;; \
		xml|json) \
			echo "WIRE_FORMAT=$(WIRE_FORMAT): forcing every outbound connection's message_format to '$(WIRE_FORMAT)'" ;; \
		*) \
			echo >&2 "ERROR: WIRE_FORMAT must be empty, \"xml\", or \"json\" (got '$(WIRE_FORMAT)')"; \
			exit 2 ;; \
	esac
	@TOKEN_A=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_A)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	PATCH_A=$$(go run ./build/configure-baseline -side a -format '$(WIRE_FORMAT)') && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_A)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_A" \
		-H "Content-Type: application/json" \
		-d "$$PATCH_A" >/dev/null && \
	echo "Server A: outbound message_format set" && \
	TOKEN_B=$$(curl -sf -X POST http://$(MM_HOST):$(MM_PORT_B)/api/v4/users/login \
		-d '{"login_id":"admin","password":"password"}' -i 2>/dev/null \
		| grep -i '^Token:' | awk '{print $$2}' | tr -d '\r') && \
	PATCH_B=$$(go run ./build/configure-baseline -side b -format '$(WIRE_FORMAT)') && \
	curl -sf -X PUT http://$(MM_HOST):$(MM_PORT_B)/api/v4/config/patch \
		-H "Authorization: Bearer $$TOKEN_B" \
		-H "Content-Type: application/json" \
		-d "$$PATCH_B" >/dev/null && \
	echo "Server B: outbound message_format set"
	@CROSSGUARD_WIRE_VALIDATE=1 $(GO_TEST_INTEGRATION)

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
