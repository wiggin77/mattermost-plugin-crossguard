//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"maps"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// PluginID is the Mattermost plugin id for crossguard. The shell tests
// hard-coded this; doing the same here is fine and keeps the harness
// independent of the manifest package (which lives outside the build tag).
const PluginID = "crossguard"

// PluginSettings is the strongly-typed view of the crossguard sub-map under
// PluginSettings.Plugins[PluginID]. The shape is the live plugin
// configuration (see server/configuration.go: type configuration), but
// served by the Mattermost API as map[string]any with string-valued JSON
// for the connection lists. We model only the fields the integration suite
// patches; round-tripping through json.RawMessage preserves anything else.
//
// Callers typically read the current value via PatchPluginConfig's mutator,
// edit the fields they care about, and let the harness write it back.
type PluginSettings = map[string]any

// PatchPluginConfig fetches the current Mattermost config on the given
// server, lets the caller mutate the crossguard plugin sub-map, writes it
// back, and returns a restore function the caller should defer. The
// restore function writes the original sub-map back. Both calls block on
// the plugin coming back to a "running" state.
//
// Note: the patch is server-scoped. Tests that need to patch both servers
// should call this twice.
func (h *Harness) PatchPluginConfig(t *testing.T, s Server, mutate func(PluginSettings)) (restore func()) {
	t.Helper()
	client := h.AdminFor(s)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg, _, err := client.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig on %s failed: %v", s.Name, err)
	}
	if cfg.PluginSettings.Plugins == nil {
		cfg.PluginSettings.Plugins = map[string]map[string]any{}
	}

	// Deep-copy the original via JSON so the restore func is decoupled
	// from later in-place edits by the mutator.
	original := deepCopyJSON(t, cfg.PluginSettings.Plugins[PluginID])

	current := cloneSettings(cfg.PluginSettings.Plugins[PluginID])
	mutate(current)
	cfg.PluginSettings.Plugins[PluginID] = current

	if _, _, err := client.UpdateConfig(ctx, cfg); err != nil {
		t.Fatalf("UpdateConfig on %s failed: %v", s.Name, err)
	}

	h.WaitPluginReady(t, s)

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cfg, _, err := client.GetConfig(ctx)
		if err != nil {
			t.Logf("restore PatchPluginConfig: GetConfig on %s failed: %v", s.Name, err)
			return
		}
		if cfg.PluginSettings.Plugins == nil {
			cfg.PluginSettings.Plugins = map[string]map[string]any{}
		}
		cfg.PluginSettings.Plugins[PluginID] = original
		if _, _, err := client.UpdateConfig(ctx, cfg); err != nil {
			t.Logf("restore PatchPluginConfig: UpdateConfig on %s failed: %v", s.Name, err)
		}
		h.WaitPluginReady(t, s)
	}
}

// ResetPlugin disables and re-enables the crossguard plugin via mmctl in the
// given server's container, then waits for it to return to running.
func (h *Harness) ResetPlugin(t *testing.T, s Server) {
	t.Helper()
	h.Compose.PluginDisable(t, s.Container, PluginID)
	h.Compose.PluginEnable(t, s.Container, PluginID)
	h.WaitPluginReady(t, s)
}

// WaitPluginReady polls GetPluginStatuses until crossguard is reported as
// running on the given server, or fails the test after 30s. After the
// plugin reports running we add a short settle window so the inbound
// elector has time to acquire its KV lease and (re)subscribe to the
// transport. Without this, a Subscribe race causes the first post after a
// reset to be lost. The shell tests had a 3s sleep here; we match that.
func (h *Harness) WaitPluginReady(t *testing.T, s Server) {
	t.Helper()
	client := h.AdminFor(s)
	Eventually(t, 30*time.Second, "plugin "+PluginID+" not running on "+s.Name, func() (struct{}, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		statuses, _, err := client.GetPluginStatuses(ctx)
		if err != nil {
			return struct{}{}, false
		}
		for _, ps := range statuses {
			if ps.PluginId == PluginID && ps.State == model.PluginStateRunning {
				return struct{}{}, true
			}
		}
		return struct{}{}, false
	})
	time.Sleep(pluginReadySettle)
}

// pluginReadySettle is the additional delay after the plugin state goes to
// "running" before we treat the plugin as fully ready. It covers the
// inbound elector's first lease acquisition and the framework's
// shared-channels remote-id handshake.
//
// 5 seconds is the empirical floor that keeps the suite stable when the
// Azure provider tests run their many plugin resets back-to-back. The
// original shell tests slept 3, but they also did fewer resets per run.
const pluginReadySettle = 5 * time.Second

func cloneSettings(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	maps.Copy(out, in)
	return out
}

func deepCopyJSON(t *testing.T, in map[string]any) map[string]any {
	t.Helper()
	if in == nil {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("deepCopyJSON marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("deepCopyJSON unmarshal: %v", err)
	}
	return out
}
