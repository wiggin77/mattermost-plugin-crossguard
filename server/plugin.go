package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	pluginapi "github.com/mattermost/mattermost/server/public/pluginapi"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

// outboundConn holds a provider connection used for relay publishing.
type outboundConn struct {
	provider      QueueProvider
	name          string
	healthy       bool
	lastCheckTime time.Time
}

type Plugin struct {
	plugin.MattermostPlugin

	client            *pluginapi.Client
	router            *mux.Router
	botUserID         string
	kvstore           store.KVStore
	configuration     *configuration
	configurationLock sync.RWMutex

	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	inboundCtx    context.Context
	inboundCancel context.CancelFunc
	outboundMu    sync.RWMutex
	outboundConns []outboundConn
	inboundMu     sync.RWMutex
	inboundConns  []inboundConn

	nodeID string

	// epoch identifies the sender process generation. Generated once at
	// OnActivate and stamped on every outbound envelope so receivers can
	// detect sender restart (cursor reset) versus normal in-stream
	// reordering. Same 26-char Mattermost ID format as every other ID
	// on the wire.
	epoch string

	// remoteIDs maps connection name (per direction) to the shared channels
	// remote ID assigned by the server. Inbound and outbound connections
	// that share the same SiteURL also share a single remoteID. The map is
	// rebuilt by OnActivate and reconcileRemotes; it is not protected by a
	// dedicated lock because writes happen only during configuration
	// reconciliation, which is serialized by the harness.
	remoteIDs map[string]string
}

func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)

	botUserID, err := p.client.Bot.EnsureBot(&model.Bot{
		Username:    "crossguard",
		DisplayName: "Cross Guard",
		Description: "Cross Guard bot for cross-domain message relay.",
	})
	if err != nil {
		return fmt.Errorf("failed to ensure crossguard bot: %w", err)
	}
	p.botUserID = botUserID

	bundlePath, err := p.API.GetBundlePath()
	if err != nil {
		return fmt.Errorf("failed to get bundle path: %w", err)
	}

	profileImage, err := os.ReadFile(filepath.Join(bundlePath, "assets", "crossguard.png"))
	if err != nil {
		return fmt.Errorf("failed to read bot profile image: %w", err)
	}

	if appErr := p.API.SetProfileImage(botUserID, profileImage); appErr != nil {
		return fmt.Errorf("failed to set bot profile image: %w", appErr)
	}

	inner := store.NewKVStore(p.client, manifest.Id)
	p.kvstore = store.NewCachingKVStore(inner, p.API)

	if err := p.registerCommand(); err != nil {
		return err
	}

	p.initAPI()

	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.nodeID = model.NewId()
	p.epoch = model.NewId()
	p.API.LogInfo("Sender epoch assigned",
		"error_code", errcode.PluginEpochAssigned,
		"epoch", p.epoch, "node_id", p.nodeID)

	if err := p.registerRemotes(); err != nil {
		return err
	}

	// One-shot upgrade migrations from the previous (hook-based) version
	// of the plugin. Each migration is gated on its own KV marker so it
	// runs exactly once per cluster.
	p.runUpgradeMigrations()

	p.connectOutbound()
	p.connectInbound()

	return nil
}

func (p *Plugin) OnDeactivate() error {
	if p.cancel != nil {
		p.cancel()
	}
	p.closeInbound()
	p.wg.Wait()
	p.closeOutbound()

	p.remoteIDs = nil
	return nil
}

func (p *Plugin) OnPluginClusterEvent(_ context.Context, ev model.PluginClusterEvent) {
	if caching, ok := p.kvstore.(*store.CachingKVStore); ok {
		caching.HandleClusterEvent(ev)
	}
}

// registerRemotes reads the current configuration and registers a shared
// channels remote per unique SiteURL across both directions. Inbound and
// outbound connections that share a SiteURL end up mapped to the same
// remoteID. Idempotent: calling RegisterPluginForSharedChannels with an
// existing SiteURL returns the existing remoteID.
func (p *Plugin) registerRemotes() error {
	cfg := p.getConfiguration()
	outbound, _ := cfg.GetOutboundConnections()
	inbound, _ := cfg.GetInboundConnections()

	siteURLToDisplayName := make(map[string]string)
	addConn := func(conn ConnectionConfig) {
		if conn.SiteURL == "" {
			return
		}
		if _, exists := siteURLToDisplayName[conn.SiteURL]; !exists {
			siteURLToDisplayName[conn.SiteURL] = conn.Name
		}
	}
	for _, c := range outbound {
		addConn(c)
	}
	for _, c := range inbound {
		addConn(c)
	}

	siteURLToRemoteID := make(map[string]string, len(siteURLToDisplayName))
	for siteURL, displayName := range siteURLToDisplayName {
		remoteID, err := p.API.RegisterPluginForSharedChannels(model.RegisterPluginOpts{
			Displayname:  fmt.Sprintf("Cross Guard (%s)", displayName),
			PluginID:     manifest.Id,
			CreatorID:    p.botUserID,
			AutoShareDMs: false,
			AutoInvited:  false,
			SiteURL:      siteURL,
		})
		if err != nil {
			p.API.LogError("Failed to register plugin remote for shared channels",
				"error_code", errcode.PluginRegisterFailed,
				"site_url", siteURL, "error", err.Error())
			return fmt.Errorf("failed to register remote for %s: %w", siteURL, err)
		}
		siteURLToRemoteID[siteURL] = remoteID
	}

	remoteIDs := make(map[string]string, len(outbound)+len(inbound))
	for _, c := range outbound {
		if id := siteURLToRemoteID[c.SiteURL]; id != "" {
			remoteIDs["outbound:"+c.Name] = id
		}
	}
	for _, c := range inbound {
		if id := siteURLToRemoteID[c.SiteURL]; id != "" {
			remoteIDs["inbound:"+c.Name] = id
		}
	}
	p.remoteIDs = remoteIDs
	return nil
}

// reconcileRemotes recomputes the desired remote registrations after a
// configuration change. New SiteURLs are registered; SiteURLs that no longer
// have any referring connection are unregistered.
func (p *Plugin) reconcileRemotes() {
	cfg := p.getConfiguration()
	outbound, _ := cfg.GetOutboundConnections()
	inbound, _ := cfg.GetInboundConnections()

	desired := make(map[string]string)
	addConn := func(conn ConnectionConfig) {
		if conn.SiteURL == "" {
			return
		}
		if _, exists := desired[conn.SiteURL]; !exists {
			desired[conn.SiteURL] = conn.Name
		}
	}
	for _, c := range outbound {
		addConn(c)
	}
	for _, c := range inbound {
		addConn(c)
	}

	siteURLToRemoteID := make(map[string]string, len(desired))
	desiredRemoteIDs := make(map[string]bool, len(desired))

	// Reuse known remoteIDs by reverse lookup; otherwise the server's
	// idempotent re-registration returns the same ID.
	for siteURL, displayName := range desired {
		remoteID, err := p.API.RegisterPluginForSharedChannels(model.RegisterPluginOpts{
			Displayname:  fmt.Sprintf("Cross Guard (%s)", displayName),
			PluginID:     manifest.Id,
			CreatorID:    p.botUserID,
			AutoShareDMs: false,
			AutoInvited:  false,
			SiteURL:      siteURL,
		})
		if err != nil {
			p.API.LogError("Failed to register plugin remote during reconcile",
				"error_code", errcode.PluginRegisterFailed,
				"site_url", siteURL, "error", err.Error())
			continue
		}
		siteURLToRemoteID[siteURL] = remoteID
		desiredRemoteIDs[remoteID] = true
	}

	// Remove remoteIDs that are no longer referenced.
	for _, oldID := range p.remoteIDs {
		if oldID == "" {
			continue
		}
		if !desiredRemoteIDs[oldID] {
			if err := p.API.UnregisterPluginRemoteForSharedChannels(oldID); err != nil {
				p.API.LogWarn("Failed to unregister stale plugin remote",
					"error_code", errcode.PluginUnregisterFailed,
					"remote_id", oldID, "error", err.Error())
			}
		}
	}

	remoteIDs := make(map[string]string, len(outbound)+len(inbound))
	for _, c := range outbound {
		if id := siteURLToRemoteID[c.SiteURL]; id != "" {
			remoteIDs["outbound:"+c.Name] = id
		}
	}
	for _, c := range inbound {
		if id := siteURLToRemoteID[c.SiteURL]; id != "" {
			remoteIDs["inbound:"+c.Name] = id
		}
	}
	p.remoteIDs = remoteIDs
}

// connNameForRemote scans the remoteIDs map for the first connection name
// matching the given remoteID. Either direction is acceptable since paired
// connections share a remoteID. Returns the direction-prefixed key
// (e.g. "outbound:high") or "" if not found.
func (p *Plugin) connNameForRemote(remoteID string) string {
	if remoteID == "" || p.remoteIDs == nil {
		return ""
	}
	// Prefer outbound mapping when both directions are configured, since
	// outbound is what the server-side hooks (OnSharedChannelsSyncMsg,
	// OnSharedChannelsPing) target.
	for _, dir := range []string{"outbound", "inbound"} {
		for key, id := range p.remoteIDs {
			if id != remoteID {
				continue
			}
			// key format: "<direction>:<name>"; check direction prefix.
			if len(key) > len(dir)+1 && key[:len(dir)+1] == dir+":" {
				return key[len(dir)+1:]
			}
		}
	}
	return ""
}

// hasOutboundProvider returns true when the named connection currently has a
// live outbound provider.
func (p *Plugin) hasOutboundProvider(connName string) bool {
	p.outboundMu.RLock()
	defer p.outboundMu.RUnlock()
	for _, oc := range p.outboundConns {
		if oc.name == connName {
			return true
		}
	}
	return false
}

// hasInboundProvider returns true when the named connection currently has a
// live inbound subscription.
func (p *Plugin) hasInboundProvider(connName string) bool {
	p.inboundMu.RLock()
	defer p.inboundMu.RUnlock()
	for _, ic := range p.inboundConns {
		if ic.name == connName {
			return true
		}
	}
	return false
}
