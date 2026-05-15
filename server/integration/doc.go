//go:build integration

// Package integration contains end-to-end integration tests that drive the
// dual-server Docker environment (low.test and high.test) over HTTP, using
// the upstream Mattermost Client4 typed API.
//
// All test files in this package carry the //go:build integration build tag,
// so `go test ./...` ignores them. Run the suite via:
//
//	make docker-integration-test-go
//
// or directly:
//
//	go test -tags=integration -timeout=30m -count=1 ./server/integration/...
//
// The harness assumes the Docker environment is already up and the plugin is
// deployed (typically via `make docker-setup` followed by `make deploy`).
// Spinning up containers is the responsibility of the make layer, not these
// tests.
package integration
