//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// WaitForChannelRemotesOnline polls the upstream remote-cluster REST API on
// server s until at least one online remote is associated with channelID, or
// fails the test on timeout.
//
// Once the framework reports an online remote for the channel, its sync
// queue will not drop subsequent posts due to the cold-start race where
// the plugin remote was offline (LastPingAt = 0) during the first sync
// attempts. This is the deterministic gate that replaces the older
// timing-based warmup sleep.
//
// The endpoint exercised is
// GET /api/v4/remotecluster?in_channel=<id>&exclude_offline=true, which
// requires admin auth. h.AdminFor(s) supplies the admin client.
func (h *Harness) WaitForChannelRemotesOnline(t *testing.T, s Server, channelID string, timeout time.Duration) {
	t.Helper()
	client := h.AdminFor(s)
	Eventually(t, timeout, "online remote for channel "+channelID+" on "+s.Name, func() (struct{}, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rcs, _, err := client.GetRemoteClusters(ctx, 0, 100, model.RemoteClusterQueryFilter{
			InChannel:      channelID,
			ExcludeOffline: true,
		})
		if err != nil {
			return struct{}{}, false
		}
		return struct{}{}, len(rcs) > 0
	})
}
