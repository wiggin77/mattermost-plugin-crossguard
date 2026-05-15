//go:build integration

package integration

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestRewriteTeam verifies that the receiving side's `rewrite-team` rule
// remaps an inbound post from team `rewrite-src` (on Server A) into the
// `test` team on Server B. Mirrors the rewrite-team block of the shell
// `docker-integration-test` target.
//
// Reuses the smoke-test outbound/inbound transport (low-to-high). Adds:
//
//   - team `rewrite-src` on Server A (with admin and usera as members)
//   - channel `rewrite-test` in `rewrite-src` on A and in `test` on B
//   - /crossguard rewrite-team low-to-high rewrite-src on Server B
func TestRewriteTeam(t *testing.T) {
	h := NewHarness(t)
	RequireSmokeLinkage(t, h)

	// Source team on A. mmctl is required because admin signup may be off.
	h.EnsureTeam(t, h.A, "rewrite-src", "Rewrite Source", "admin", "usera")

	channelA := h.EnsureChannel(t, h.A, "rewrite-src", "rewrite-test", "Rewrite Test")
	channelB := h.EnsureChannel(t, h.B, "test", "rewrite-test", "Rewrite Test")

	h.AddChannelMemberByUsername(t, h.A, channelA.Id, "usera")
	h.AddChannelMemberByUsername(t, h.B, channelB.Id, "userb")

	// Linkage: outbound on A's source team; inbound on B's test team
	// (init-team inbound:low-to-high on B is idempotent; the existing
	// smoke linkage already covered it but the shell test repeats it).
	h.ExecSlash(t, h.A, channelA.Id, "/crossguard init-team outbound:low-to-high")
	h.ExecSlash(t, h.B, channelB.Id, "/crossguard init-team inbound:low-to-high")
	h.ExecSlash(t, h.A, channelA.Id, "/crossguard init-channel outbound:low-to-high")
	h.ExecSlash(t, h.B, channelB.Id, "/crossguard init-channel inbound:low-to-high")

	// The receiver rewrites incoming team "rewrite-src" to "test".
	h.ExecSlash(t, h.B, channelB.Id, "/crossguard rewrite-team low-to-high rewrite-src")

	useraClient := h.ClientAs(t, h.A, "usera", "password")
	marker := fmt.Sprintf("rewrite-test:%d-%d", time.Now().UnixNano(), os.Getpid())

	CreatePost(t, useraClient, channelA.Id, marker)
	h.FindRelayedPost(t, h.B, channelB.Id, marker, 20*time.Second)
}
