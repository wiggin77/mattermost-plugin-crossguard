package main

import (
	"fmt"
	"maps"

	mmModel "github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

// OnSharedChannelsSyncMsg receives content changes from the server's Shared
// Channels Service and forwards them to the matching outbound provider as an
// XML TransportEnvelope.
//
// rc identifies which remote triggered this sync. Each outbound connection
// is registered with a distinct SiteURL and therefore a distinct remoteID,
// so this hook fires once per remote per batch. Returning a non-nil error
// keeps the server's per-remote cursor in place so the batch is retried;
// the SyncMsg payload is idempotent on the receiving side.
func (p *Plugin) OnSharedChannelsSyncMsg(
	msg *mmModel.SyncMsg,
	rc *mmModel.RemoteCluster,
) (mmModel.SyncResponse, error) {
	channelID := ""
	postCount := 0
	remoteID := ""
	if msg != nil {
		channelID = msg.ChannelId
		postCount = len(msg.Posts)
	}
	if rc != nil {
		remoteID = rc.RemoteId
	}
	p.API.LogDebug("OnSharedChannelsSyncMsg invoked",
		"channel_id", channelID, "post_count", postCount, "remote_id", remoteID)

	if msg == nil {
		return mmModel.SyncResponse{}, nil
	}
	if rc == nil {
		return mmModel.SyncResponse{}, nil
	}

	connName := p.connNameForRemote(rc.RemoteId)
	if connName == "" {
		p.API.LogWarn("No connection found for remote",
			"error_code", errcode.OutboundSyncMsgNoRemoteMatch,
			"remote_id", rc.RemoteId)
		return mmModel.SyncResponse{}, nil
	}

	// Inbound-only connections register a remote (so ReceiveSharedChannelSyncMsg
	// has a valid remoteID) but do not have an outbound provider. Acknowledge
	// the sync message so the cursor advances; do not attempt to publish.
	if !p.hasOutboundProvider(connName) {
		return buildSyncResponse(msg), nil
	}

	channel, appErr := p.API.GetChannel(msg.ChannelId)
	if appErr != nil {
		p.API.LogError("Failed to get channel for outbound sync",
			"error_code", errcode.OutboundSyncMsgChannelLookupFailed,
			"channel_id", msg.ChannelId, "error", appErr.Error())
		return mmModel.SyncResponse{}, fmt.Errorf("channel lookup failed: %s", appErr.Error())
	}

	team, appErr := p.API.GetTeam(channel.TeamId)
	if appErr != nil {
		return mmModel.SyncResponse{}, fmt.Errorf("team lookup failed: %s", appErr.Error())
	}

	augmented := p.augmentSyncMsgUsers(msg)

	env := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    connName,
		TeamName:    team.Name,
		ChannelName: channel.Name,
		SyncMsg:     augmented,
	}

	if err := p.publishToOutboundConn(p.ctx, env, connName); err != nil {
		p.API.LogError("Failed to publish outbound sync envelope",
			"error_code", errcode.OutboundSyncMsgPublishFailed,
			"conn_name", connName, "channel_id", msg.ChannelId, "error", err.Error())
		return mmModel.SyncResponse{}, fmt.Errorf("publish failed for %s: %w", connName, err)
	}

	return buildSyncResponse(msg), nil
}

// augmentSyncMsgUsers ensures every locally-resident user referenced by the
// SyncMsg's Posts, Reactions, Acknowledgements, MembershipChanges, or Statuses
// is present in msg.Users. The shared-channels framework records "synced"
// against an optimistic per-user cursor as soon as OnSharedChannelsSyncMsg
// returns nil; over a fire-and-forget transport (NATS, Azure Queue, etc.) the
// receiver may have silently dropped a previous user upsert, leaving the
// receiver without the user record while the sender's cursor has advanced.
// Inlining the user record on every referencing message defeats that skew.
//
// The receiver's upsertSyncUser is idempotent (patches existing users when
// RemoteId matches, creates them when missing), so always-include is safe.
//
// The framework's SyncMsg is never mutated in place; on augmentation a new
// SyncMsg is returned with a freshly allocated Users map.
func (p *Plugin) augmentSyncMsgUsers(msg *mmModel.SyncMsg) *mmModel.SyncMsg {
	if msg == nil {
		return msg
	}

	referenced := make(map[string]struct{})
	collect := func(uid string) {
		if uid == "" {
			return
		}
		referenced[uid] = struct{}{}
	}
	for _, post := range msg.Posts {
		if post != nil {
			collect(post.UserId)
		}
	}
	for _, r := range msg.Reactions {
		if r != nil {
			collect(r.UserId)
		}
	}
	for _, a := range msg.Acknowledgements {
		if a != nil {
			collect(a.UserId)
		}
	}
	for _, m := range msg.MembershipChanges {
		if m != nil {
			collect(m.UserId)
		}
	}
	for _, s := range msg.Statuses {
		if s != nil {
			collect(s.UserId)
		}
	}

	for uid := range msg.Users {
		delete(referenced, uid)
	}

	if len(referenced) == 0 {
		return msg
	}

	augmentedUsers := make(map[string]*mmModel.User, len(msg.Users)+len(referenced))
	maps.Copy(augmentedUsers, msg.Users)

	added := 0
	for uid := range referenced {
		user, appErr := p.API.GetUser(uid)
		if appErr != nil || user == nil {
			p.API.LogWarn("Cannot fetch local user for outbound sync augmentation",
				"error_code", errcode.OutboundSyncMsgUserLookupFailed,
				"user_id", uid)
			continue
		}
		// Skip users that originated from a remote (synthetic remote users
		// created by a previous inbound sync). The framework's shouldUserSync
		// applies the same filter; we re-apply it defensively because we
		// bypass the cursor.
		if user.RemoteId != nil && *user.RemoteId != "" {
			continue
		}
		augmentedUsers[uid] = user
		added++
	}

	if added == 0 {
		return msg
	}

	p.API.LogInfo("Augmented outbound SyncMsg with referenced users",
		"error_code", errcode.OutboundSyncMsgUsersAugmented,
		"channel_id", msg.ChannelId, "added", added,
		"total_users", len(augmentedUsers))

	augmented := *msg
	augmented.Users = augmentedUsers
	return &augmented
}

// OnSharedChannelsAttachmentSyncMsg is a stub. File attachment sync is a
// follow-up plan; here we acknowledge the call so the server advances its
// cursor and does not retry indefinitely.
func (p *Plugin) OnSharedChannelsAttachmentSyncMsg(
	fi *mmModel.FileInfo,
	post *mmModel.Post,
	_ *mmModel.RemoteCluster,
) error {
	fileID, postID := "", ""
	if fi != nil {
		fileID = fi.Id
	}
	if post != nil {
		postID = post.Id
	}
	p.API.LogDebug("Outbound attachment sync not yet implemented",
		"error_code", errcode.OutboundAttachmentStubbed,
		"file_id", fileID, "post_id", postID)
	return nil
}

// OnSharedChannelsProfileImageSyncMsg is a stub. Profile image sync is a
// follow-up plan.
func (p *Plugin) OnSharedChannelsProfileImageSyncMsg(
	user *mmModel.User,
	_ *mmModel.RemoteCluster,
) error {
	userID := ""
	if user != nil {
		userID = user.Id
	}
	p.API.LogDebug("Outbound profile image sync not yet implemented",
		"error_code", errcode.OutboundProfileImageStubbed,
		"user_id", userID)
	return nil
}

// OnSharedChannelsPing reports whether the plugin can handle messages for the
// given remote. Checks both directions: outbound provider connectivity for
// publish, inbound provider connectivity for receive. Providers that do not
// expose connection state report connected unconditionally; see
// QueueProvider.IsConnected. Returning false causes the server to mark the
// remote offline and stop sync.
func (p *Plugin) OnSharedChannelsPing(rc *mmModel.RemoteCluster) bool {
	if rc == nil {
		return false
	}
	connName := p.connNameForRemote(rc.RemoteId)
	if connName == "" {
		p.API.LogWarn("Ping received for unknown remote",
			"error_code", errcode.PingNoRemoteMatch,
			"remote_id", rc.RemoteId, "display_name", rc.DisplayName)
		return false
	}

	if !p.outboundConnected(connName) {
		p.API.LogWarn("Ping: outbound provider not connected",
			"error_code", errcode.PingOutboundUnhealthy,
			"connection", connName,
			"remote_id", rc.RemoteId, "display_name", rc.DisplayName)
		return false
	}
	if !p.inboundConnected(connName) {
		p.API.LogWarn("Ping: inbound provider not connected",
			"error_code", errcode.PingInboundUnhealthy,
			"connection", connName,
			"remote_id", rc.RemoteId, "display_name", rc.DisplayName)
		return false
	}
	return true
}

// outboundConnected reports whether the outbound provider for connName is
// connected. Returns true if no outbound provider exists for connName, since
// inbound-only configurations should not fail the ping on the outbound check.
func (p *Plugin) outboundConnected(connName string) bool {
	p.outboundMu.RLock()
	defer p.outboundMu.RUnlock()
	for _, oc := range p.outboundConns {
		if oc.name == connName {
			return oc.provider.IsConnected()
		}
	}
	return true
}

// inboundConnected reports whether the inbound provider for connName is
// connected. Returns true if no inbound provider exists for connName.
func (p *Plugin) inboundConnected(connName string) bool {
	p.inboundMu.RLock()
	defer p.inboundMu.RUnlock()
	for _, ic := range p.inboundConns {
		if ic.name == connName {
			return ic.provider.IsConnected()
		}
	}
	return true
}
