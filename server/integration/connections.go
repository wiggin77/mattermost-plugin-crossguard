//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// configChangeSettle is the wait after a successful UpdateConfig before
// callers may rely on the plugin having picked up the new settings. The
// platform calls OnConfigurationChange asynchronously; the plugin then
// runs reconnectOutbound, reconnectInbound, and reconcileRemotes. In
// practice this is sub-second on a healthy server; we leave generous
// headroom because the cost of guessing too short is a flaky test.
const configChangeSettle = 2 * time.Second

func waitForConfigChangeSettle() {
	time.Sleep(configChangeSettle)
}

// ConnectionDirection identifies which list of connections a mutator
// targets in the plugin's settings sub-map.
type ConnectionDirection string

const (
	Outbound ConnectionDirection = "outbound"
	Inbound  ConnectionDirection = "inbound"
)

func (d ConnectionDirection) configKey() string {
	if d == Outbound {
		return "outboundconnections"
	}
	return "inboundconnections"
}

// PatchConnection finds the connection by name in the
// outboundconnections/inboundconnections JSON-string field of the plugin
// config on the given server, runs the mutator on that entry, writes the
// config back, and resets the plugin so the new settings take effect.
// Returns a restore function that reverts to the original value and resets
// the plugin again. Test callers should defer the restore.
//
// The plugin stores connection lists as a JSON-encoded string inside the
// generic map[string]any plugin settings, so this helper handles the
// unmarshal/marshal cycle for the caller.
func (h *Harness) PatchConnection(t *testing.T, s Server, dir ConnectionDirection, connName string, mutate func(conn map[string]any)) (restore func()) {
	t.Helper()
	key := dir.configKey()
	client := h.AdminFor(s)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg, _, err := client.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig on %s: %v", s.Name, err)
	}
	if cfg.PluginSettings.Plugins == nil {
		cfg.PluginSettings.Plugins = map[string]map[string]any{}
	}
	settings := cfg.PluginSettings.Plugins[PluginID]
	if settings == nil {
		t.Fatalf("plugin %s has no settings on %s", PluginID, s.Name)
	}

	originalRaw, _ := settings[key].(string)
	if originalRaw == "" {
		t.Fatalf("plugin settings %s on %s is empty (expected JSON array)", key, s.Name)
	}

	patched, err := mutateConnectionList(originalRaw, connName, mutate)
	if err != nil {
		t.Fatalf("mutate %s on %s: %v", key, s.Name, err)
	}

	settings[key] = patched
	cfg.PluginSettings.Plugins[PluginID] = settings
	if _, _, err := client.UpdateConfig(ctx, cfg); err != nil {
		t.Fatalf("UpdateConfig on %s: %v", s.Name, err)
	}
	waitForConfigChangeSettle()

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cfg, _, err := client.GetConfig(ctx)
		if err != nil {
			t.Logf("restore PatchConnection: GetConfig on %s: %v", s.Name, err)
			return
		}
		if cfg.PluginSettings.Plugins == nil {
			cfg.PluginSettings.Plugins = map[string]map[string]any{}
		}
		settings := cfg.PluginSettings.Plugins[PluginID]
		if settings == nil {
			settings = map[string]any{}
		}
		settings[key] = originalRaw
		cfg.PluginSettings.Plugins[PluginID] = settings
		if _, _, err := client.UpdateConfig(ctx, cfg); err != nil {
			t.Logf("restore PatchConnection: UpdateConfig on %s: %v", s.Name, err)
			return
		}
		waitForConfigChangeSettle()
	}
}

func mutateConnectionList(raw, connName string, mutate func(map[string]any)) (string, error) {
	var entries []map[string]any
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	matched := 0
	for _, e := range entries {
		if name, _ := e["name"].(string); name == connName {
			mutate(e)
			matched++
		}
	}
	if matched == 0 {
		return "", fmt.Errorf("connection %q not found", connName)
	}
	out, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return string(out), nil
}
