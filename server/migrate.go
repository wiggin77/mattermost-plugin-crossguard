package main

import (
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

// One-shot upgrade markers. These keys live in the plugin's KV namespace and
// gate the migrations so they run exactly once per cluster, regardless of
// how many times the plugin is reactivated. Bump the version suffix when a
// new migration is added so it runs on existing installations.
const (
	kvCleanupMarker        = "crossguard-migrate-kv-cleanup-v1"
	channelShareMarker     = "crossguard-migrate-channel-share-v1"
	migrateListKeysPerPage = 1000
)

// Stale KV prefixes left behind by versions of the plugin that tracked post
// ID mappings and delete-flags in KV. The shared channels server now owns
// both responsibilities, so these rows are dead weight.
var staleKVPrefixes = []string{
	"pm-",
	"crossguard-deleting-",
}

// runUpgradeMigrations is invoked from OnActivate after the kvstore, bot
// user, and remote registration are in place. Each migration is gated on
// its own KV marker, so a partially applied upgrade resumes where it left
// off on the next activation.
//
// Migrations are best-effort: failures are logged and the run continues so
// a single broken channel does not prevent the rest of the cluster from
// upgrading. The marker is only set when the run completes without error
// for that migration; otherwise it retries on the next activation.
func (p *Plugin) runUpgradeMigrations() {
	if p.cleanupOrphanedKV() {
		if _, err := p.client.KV.Set(kvCleanupMarker, true); err != nil {
			p.API.LogWarn("Failed to record KV cleanup marker",
				"error_code", errcode.PluginMigrateRecordKVFailed,
				"marker", kvCleanupMarker, "error", err.Error())
		}
	}

	if p.shareExistingChannels() {
		if _, err := p.client.KV.Set(channelShareMarker, true); err != nil {
			p.API.LogWarn("Failed to record channel share migration marker",
				"error_code", errcode.PluginMigrateRecordKVFailed,
				"marker", channelShareMarker, "error", err.Error())
		}
	}
}

// cleanupOrphanedKV deletes KV rows from previous plugin versions that no
// longer have any reader: post mappings (pm-*) and delete flags
// (crossguard-deleting-*). Returns true when the scan completed without a
// listing error so the caller can record the migration marker.
func (p *Plugin) cleanupOrphanedKV() bool {
	var done bool
	if err := p.client.KV.Get(kvCleanupMarker, &done); err == nil && done {
		return false
	}

	deleted := 0
	for page := 0; ; page++ {
		keys, err := p.client.KV.ListKeys(page, migrateListKeysPerPage,
			pluginapi.WithChecker(func(key string) (bool, error) {
				for _, prefix := range staleKVPrefixes {
					if strings.HasPrefix(key, prefix) {
						return true, nil
					}
				}
				return false, nil
			}),
		)
		if err != nil {
			p.API.LogWarn("KV cleanup migration: list keys failed",
				"error_code", errcode.PluginMigrateListKVFailed,
				"page", page, "error", err.Error())
			return false
		}

		for _, key := range keys {
			if err := p.client.KV.Delete(key); err != nil {
				p.API.LogWarn("KV cleanup migration: delete failed",
					"error_code", errcode.PluginMigrateDeleteKVFailed,
					"key", key, "error", err.Error())
				continue
			}
			deleted++
		}

		if len(keys) < migrateListKeysPerPage {
			break
		}
	}

	if deleted > 0 {
		p.API.LogInfo("KV cleanup migration completed",
			"error_code", errcode.PluginMigrateSummary,
			"deleted", deleted)
	}
	return true
}

// shareExistingChannels walks every channel that has Cross Guard
// connection rows in KV and runs ShareChannel + InviteRemoteToChannel for
// each linked outbound or paired connection. This brings channels that
// were linked under the previous (hook-based) version into the
// shared-channels regime so OnSharedChannelsSyncMsg starts firing for them
// without an admin having to teardown and re-link each channel by hand.
//
// Requires p.remoteIDs to be populated, so callers must invoke this only
// after registerRemotes has run.
func (p *Plugin) shareExistingChannels() bool {
	var done bool
	if err := p.client.KV.Get(channelShareMarker, &done); err == nil && done {
		return false
	}
	if len(p.remoteIDs) == 0 {
		// Nothing to invite. Marker is still set so we do not rescan on
		// every activation while the cluster has no connections configured.
		return true
	}

	channelInitPrefix := manifest.Id + "-channelinit-"

	shared := 0
	invited := 0
	for page := 0; ; page++ {
		keys, err := p.client.KV.ListKeys(page, migrateListKeysPerPage,
			pluginapi.WithPrefix(channelInitPrefix),
		)
		if err != nil {
			p.API.LogWarn("Channel share migration: list keys failed",
				"error_code", errcode.PluginMigrateListKVFailed,
				"page", page, "error", err.Error())
			return false
		}

		for _, key := range keys {
			channelID := strings.TrimPrefix(key, channelInitPrefix)
			if !model.IsValidId(channelID) {
				continue
			}
			s, i := p.shareChannelOnUpgrade(channelID)
			shared += s
			invited += i
		}

		if len(keys) < migrateListKeysPerPage {
			break
		}
	}

	p.API.LogInfo("Channel share migration completed",
		"error_code", errcode.PluginMigrateSummary,
		"channels_shared", shared, "remotes_invited", invited)
	return true
}

// shareChannelOnUpgrade runs ShareChannel and InviteRemoteToChannel for a
// single channel with existing Cross Guard connections. Returns
// (sharedDelta, invitedDelta) for the migration summary log. Errors are
// swallowed (logged) so a single bad channel does not abort the run.
func (p *Plugin) shareChannelOnUpgrade(channelID string) (int, int) {
	conns, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil || len(conns) == 0 {
		return 0, 0
	}

	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		p.API.LogWarn("Channel share migration: GetChannel failed; skipping",
			"error_code", errcode.PluginMigrateGetChannelFailed,
			"channel_id", channelID, "error", appErr.Error())
		return 0, 0
	}

	// ShareChannel is idempotent on the server. Treat any error as a
	// "may already be shared" signal and continue with the invites.
	sharedDelta := 0
	if _, err := p.API.ShareChannel(&model.SharedChannel{
		ChannelId:        channel.Id,
		TeamId:           channel.TeamId,
		Home:             true,
		ShareName:        channel.Name,
		ShareDisplayName: channel.DisplayName,
		CreatorId:        p.botUserID,
	}); err != nil {
		p.API.LogDebug("Channel share migration: ShareChannel returned error (may already be shared)",
			"error_code", errcode.PluginMigrateShareFailed,
			"channel_id", channelID, "error", err.Error())
	} else {
		sharedDelta = 1
	}

	invitedDelta := 0
	seenRemotes := make(map[string]bool, len(conns))
	for _, conn := range conns {
		remoteID := p.remoteIDs[connKey(conn)]
		if remoteID == "" || seenRemotes[remoteID] {
			continue
		}
		seenRemotes[remoteID] = true
		if err := p.API.InviteRemoteToChannel(channel.Id, remoteID, p.botUserID, true); err != nil {
			p.API.LogWarn("Channel share migration: InviteRemoteToChannel failed",
				"error_code", errcode.PluginMigrateInviteFailed,
				"channel_id", channelID, "remote_id", remoteID, "error", err.Error())
			continue
		}
		invitedDelta++
	}

	return sharedDelta, invitedDelta
}
