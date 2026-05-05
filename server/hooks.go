package main

import (
	"fmt"

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

	env := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    connName,
		TeamName:    team.Name,
		ChannelName: channel.Name,
		SyncMsg:     msg,
	}

	if err := p.publishToOutboundConn(p.ctx, env, connName); err != nil {
		p.API.LogError("Failed to publish outbound sync envelope",
			"error_code", errcode.OutboundSyncMsgPublishFailed,
			"conn_name", connName, "channel_id", msg.ChannelId, "error", err.Error())
		return mmModel.SyncResponse{}, fmt.Errorf("publish failed for %s: %w", connName, err)
	}

	return buildSyncResponse(msg), nil
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

// OnSharedChannelsPing reports the health of the remote's outbound provider.
// Returning false would cause the server to mark the remote as offline and
// stop sync delivery, so inbound-only connections (which have no outbound
// provider to check) report healthy.
func (p *Plugin) OnSharedChannelsPing(rc *mmModel.RemoteCluster) bool {
	if rc == nil {
		return false
	}
	connName := p.connNameForRemote(rc.RemoteId)
	if connName == "" {
		return false
	}

	if !p.hasOutboundProvider(connName) {
		return true
	}

	p.outboundMu.RLock()
	defer p.outboundMu.RUnlock()
	for _, oc := range p.outboundConns {
		if oc.name == connName {
			return oc.healthy
		}
	}
	return false
}
