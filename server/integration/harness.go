//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// Server describes one of the two dockerized Mattermost instances.
type Server struct {
	// Name is a short label used in test output ("A" or "B").
	Name string
	// Host is the host that curl/Client4 uses to reach the instance
	// (defaults to 127.0.0.1, overridable via MM_HOST).
	Host string
	// Port is the HTTP port.
	Port int
	// Container is the docker compose service name (used by mmctl shell-outs).
	Container string
}

// URL returns the base URL used to construct a Client4 instance.
func (s Server) URL() string {
	return fmt.Sprintf("http://%s:%d", s.Host, s.Port)
}

// Harness bundles per-suite state: the two servers, admin clients, a cache
// of per-user logged-in clients, and a docker compose helper.
//
// One Harness is created per top-level test via NewHarness(t). It does not
// touch global state; callers that need shared linkage (e.g. low-to-high
// channel + team init) should call RequireSmokeLinkage(t, h) explicitly.
type Harness struct {
	A, B           Server
	AdminA, AdminB *model.Client4
	Compose        *DockerCompose

	mu          sync.Mutex
	userClients map[string]*model.Client4 // key: serverName + "/" + username

	// preWireArchive captures the set of envelope filenames already
	// present in each server's archive directory at NewHarness time.
	// validateWireArchive uses it to filter out envelopes from
	// previous tests in the same `go test` run so each test's gate
	// only inspects what it actually emitted. Keyed by container name.
	preWireArchive map[string]map[string]bool
}

// NewHarness logs in admin on both servers and returns a Harness. It fails
// fast if either login fails so tests do not have to repeat the check.
//
// The harness assumes the containers are running and the plugin is enabled.
// It does not attempt to start anything.
func NewHarness(t *testing.T) *Harness {
	t.Helper()

	host := getenv("MM_HOST", "127.0.0.1")
	portA := getenvInt(t, "MM_PORT_A", 8075)
	portB := getenvInt(t, "MM_PORT_B", 8076)

	// docker compose -f resolves the path relative to its cwd, which for
	// `go test` is the test package directory. Anchor to the repo root so
	// the compose file is found regardless of where the test is invoked.
	composeFile := getenv("DOCKER_COMPOSE_FILE", "docker-compose.dev.yml")
	if !filepath.IsAbs(composeFile) {
		composeFile = RepoPath(t, composeFile)
	}

	h := &Harness{
		A:           Server{Name: "A", Host: host, Port: portA, Container: "mattermost-a"},
		B:           Server{Name: "B", Host: host, Port: portB, Container: "mattermost-b"},
		Compose:     &DockerCompose{File: composeFile},
		userClients: make(map[string]*model.Client4),
	}

	h.AdminA = h.loginOrFatal(t, h.A, "admin", "password")
	h.AdminB = h.loginOrFatal(t, h.B, "admin", "password")

	// Wire-validation mode: when CROSSGUARD_WIRE_VALIDATE=1 is set on
	// the test runner, take a snapshot of each server's envelope
	// archive now and register a cleanup that validates every envelope
	// added since the snapshot against schema/crossguard.xsd. The
	// plugin writes envelopes to the archive whenever
	// CROSSGUARD_ENVELOPE_ARCHIVE_DIR is set in the container's env
	// (docker-compose.dev.yml does that unconditionally; the dev
	// overhead is one tmpfs file per outbound envelope).
	//
	// Snapshot-rather-than-clear is used because the dev container
	// images are distroless and have no rm in the container; docker
	// cp is the only image-agnostic way to interact with files inside.
	if WireValidateEnabled() {
		h.snapshotWireArchive(t)
		t.Cleanup(func() { h.validateWireArchive(t) })
	}
	return h
}

// AdminFor returns the admin client for the given server.
func (h *Harness) AdminFor(s Server) *model.Client4 {
	if s.Name == h.A.Name {
		return h.AdminA
	}
	return h.AdminB
}

// ClientAs returns a Client4 logged in as the given user on the given server.
// Repeated calls for the same (server, username) return the cached client.
func (h *Harness) ClientAs(t *testing.T, s Server, username, password string) *model.Client4 {
	t.Helper()
	key := s.Name + "/" + username
	h.mu.Lock()
	if c, ok := h.userClients[key]; ok {
		h.mu.Unlock()
		return c
	}
	h.mu.Unlock()

	c := h.loginOrFatal(t, s, username, password)

	h.mu.Lock()
	h.userClients[key] = c
	h.mu.Unlock()
	return c
}

func (h *Harness) loginOrFatal(t *testing.T, s Server, username, password string) *model.Client4 {
	t.Helper()
	c := model.NewAPIv4Client(s.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, err := c.Login(ctx, username, password); err != nil {
		t.Fatalf("login %s@%s failed: %v", username, s.Name, err)
	}
	return c
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		t.Fatalf("invalid integer for %s=%q: %v", key, v, err)
	}
	return n
}
