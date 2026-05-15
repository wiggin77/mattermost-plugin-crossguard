//go:build integration

package integration

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestSmoke is the minimal end-to-end check: relay one message from
// Server A low-to-high to Server B low-to-high. Mirrors the body of the
// shell `docker-smoke-test` target. Slow tests should depend on this
// passing first (RequireSmokeLinkage memoizes the setup).
func TestSmoke(t *testing.T) {
	h := NewHarness(t)
	linkage := RequireSmokeLinkage(t, h)

	useraClient := h.ClientAs(t, h.A, "usera", "password")
	marker := fmt.Sprintf("smoke-test:%d-%d", time.Now().UnixNano(), os.Getpid())

	CreatePost(t, useraClient, linkage.LowToHighA, marker)
	h.FindRelayedPost(t, h.B, linkage.LowToHighB, marker, 20*time.Second)
}
