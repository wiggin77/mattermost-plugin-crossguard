//go:build integration

package integration

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// linkageOnce guards RequireSmokeLinkage so the setup only runs once per
// process even when multiple tests pull it in. The setup is idempotent at
// the API level (channel creation is GetOrCreate, init-team/init-channel
// are no-ops when already linked), but skipping the duplicate work keeps
// the suite fast.
var (
	linkageOnce sync.Once
	linkageErr  error
)

// LinkedChannels describes the low-to-high channel pair created by the
// smoke linkage. It is returned by RequireSmokeLinkage so tests can post
// without repeating channel lookups.
type LinkedChannels struct {
	LowToHighA string
	LowToHighB string
}

// RequireSmokeLinkage idempotently establishes the low-to-high relay used by
// every Phase 1/Phase 2 test: ensures the channel exists on both servers,
// adds usera/useraa/userb, then issues the four /crossguard init-team and
// two /crossguard init-channel commands that the original docker-smoke-test
// makefile target ran.
//
// Subsequent calls within the same `go test` process are no-ops. Callers
// should assume containers are up and the plugin is deployed with
// outboundconnections=low-to-high and inboundconnections=low-to-high on
// Server A and Server B respectively (this is what `make deploy` configures).
func RequireSmokeLinkage(t *testing.T, h *Harness) LinkedChannels {
	t.Helper()

	var result LinkedChannels
	linkageOnce.Do(func() {
		result, linkageErr = ensureSmokeLinkage(t, h)
	})

	if linkageErr != nil {
		t.Fatalf("RequireSmokeLinkage: %v", linkageErr)
	}

	// On second and subsequent calls, the sync.Once skipped the work, so
	// recompute the channel ids cheaply. The lookup is fast and tolerates
	// concurrent callers.
	if result.LowToHighA == "" {
		lthA := h.EnsureChannel(t, h.A, "test", "low-to-high", "Low To High")
		lthB := h.EnsureChannel(t, h.B, "test", "low-to-high", "Low To High")
		result.LowToHighA = lthA.Id
		result.LowToHighB = lthB.Id
	}
	return result
}

func ensureSmokeLinkage(t *testing.T, h *Harness) (LinkedChannels, error) {
	t.Helper()

	lthA := h.EnsureChannel(t, h.A, "test", "low-to-high", "Low To High")
	lthB := h.EnsureChannel(t, h.B, "test", "low-to-high", "Low To High")

	for _, u := range []string{"usera", "useraa"} {
		h.AddChannelMemberByUsername(t, h.A, lthA.Id, u)
	}
	h.AddChannelMemberByUsername(t, h.B, lthB.Id, "userb")

	cmds := []struct {
		s       Server
		channel string
		cmd     string
	}{
		{h.A, lthA.Id, "/crossguard init-team outbound:low-to-high"},
		{h.A, lthA.Id, "/crossguard init-team inbound:high-to-low"},
		{h.B, lthB.Id, "/crossguard init-team inbound:low-to-high"},
		{h.B, lthB.Id, "/crossguard init-team outbound:high-to-low"},
		{h.A, lthA.Id, "/crossguard init-channel outbound:low-to-high"},
		{h.B, lthB.Id, "/crossguard init-channel inbound:low-to-high"},
	}
	for _, c := range cmds {
		resp := h.ExecSlash(t, c.s, c.channel, c.cmd)
		// init-* commands return human-readable text. They are idempotent
		// when the channel/team is already linked, so we accept anything
		// that does not contain "error" in the response text.
		if resp != nil && resp.Text != "" && strings.Contains(strings.ToLower(resp.Text), "error") {
			t.Logf("RequireSmokeLinkage: %s on %s returned %q (continuing; may be benign)",
				c.cmd, c.s.Name, resp.Text)
		}
	}

	// Wait for both sides' frameworks to consider their counterpart remote
	// online for the smoke channel before returning. Without this, the
	// first post in a freshly deployed environment can hit the framework's
	// retry-exhaustion window (the plugin remote is offline until
	// pluginRemoteInitialPingDelay fires) and be dropped.
	h.WaitForChannelRemotesOnline(t, h.A, lthA.Id, 60*time.Second)
	h.WaitForChannelRemotesOnline(t, h.B, lthB.Id, 60*time.Second)

	return LinkedChannels{LowToHighA: lthA.Id, LowToHighB: lthB.Id}, nil
}
