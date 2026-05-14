package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

// The Azure integration tests require external emulators (Azurite for
// Queue/Blob and the Service Bus emulator for SB). Unlike NATS, these
// emulators cannot be embedded in-process; they run as separate
// processes started via `make docker-setup`. The tests skip cleanly
// when the emulators aren't reachable so the wider `go test` run
// stays green on developer machines without Docker up.
//
// Set CROSSGUARD_AZURE_INTEGRATION=1 to fail the tests (rather than
// skip) when an emulator is unreachable. Useful in CI environments
// that are supposed to have the emulators running and want to catch
// regressions where the emulators didn't start.
const azureIntegrationEnv = "CROSSGUARD_AZURE_INTEGRATION"

// azuriteQueueEndpoint is the default Azurite Queue endpoint used by
// `make docker-setup`. Tests can override via env var if Azurite is
// bound elsewhere.
const (
	azuriteAccountName = "devstoreaccount1"
	// Azurite's well-known account key. Microsoft's documentation
	// frequently appends `==` padding to this value, but that makes
	// the literal 90 chars (not a multiple of 4) and Go's base64
	// decoder rejects it. The 88-char form below is what Azurite
	// itself expects and what the Azure SDK accepts.
	azuriteAccountKey    = "Eby8vdM02xNOcqFlqUwJPLlmEEHck/dpcKlNzh+kdmzNllNN0/JfHQfHo5dq4cwbnPwHNwDqksFp6XmHCXJ5KOSg"
	azuriteQueueEndpoint = "http://127.0.0.1:10001/devstoreaccount1"
	azuriteBlobEndpoint  = "http://127.0.0.1:10000/devstoreaccount1"
	azuriteQueuePort     = "127.0.0.1:10001"
	azuriteBlobPort      = "127.0.0.1:10000"
	servicebusAMQPPort   = "127.0.0.1:5672"
)

// requireEmulatorReachable returns when host:port accepts a TCP
// connection within timeout. If unreachable, the test is skipped
// (or failed when CROSSGUARD_AZURE_INTEGRATION=1).
func requireEmulatorReachable(t *testing.T, name, hostPort string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", hostPort, 300*time.Millisecond)
	if err != nil {
		msg := fmt.Sprintf("%s emulator not reachable at %s: %v", name, hostPort, err)
		if os.Getenv(azureIntegrationEnv) == "1" {
			t.Fatal(msg)
		}
		t.Skip(msg + " (set " + azureIntegrationEnv + "=1 to fail instead of skip)")
	}
	_ = conn.Close()
}

// withTestContext returns a context bounded by a test deadline. The
// integration tests use these so a hung emulator doesn't hang the
// whole `go test` invocation.
func withTestContext(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), d)
}
