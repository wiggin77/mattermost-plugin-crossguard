package main

import (
	"encoding/base64"
	"encoding/json"
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

	// Fan out into one envelope per post plus an optional metadata
	// envelope so a compliance rejection of one post does not lose the
	// rest of the sync cycle. Each envelope is published independently
	// and gets its own per-channel Sequence in publishToOutboundConn.
	template := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    connName,
		TeamName:    team.Name,
		ChannelName: channel.Name,
	}
	wireLog := newPluginWireLogger(p.API,
		"conn_name", connName,
		"channel_id", msg.ChannelId,
		"team_id", channel.TeamId,
	)
	envs := buildOutboundEnvelopes(wireLog, template, augmented)
	for _, env := range envs {
		if err := p.publishToOutboundConn(p.ctx, env, connName); err != nil {
			p.API.LogError("Failed to publish outbound sync envelope",
				"error_code", errcode.OutboundSyncMsgPublishFailed,
				"conn_name", connName, "channel_id", msg.ChannelId, "error", err.Error())
			return mmModel.SyncResponse{}, fmt.Errorf("publish failed for %s: %w", connName, err)
		}
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
		user, appErr := p.getUser(uid)
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

// OnSharedChannelsAttachmentSyncMsg uploads a file attachment to the matching
// outbound connection's file path. Returning a non-nil error causes the
// framework to skip saveSharedAttachment, so the next sync cycle will retry
// (shouldSyncAttachment returns true while no record exists).
func (p *Plugin) OnSharedChannelsAttachmentSyncMsg(
	fi *mmModel.FileInfo,
	post *mmModel.Post,
	rc *mmModel.RemoteCluster,
) error {
	if fi == nil || post == nil || rc == nil {
		return nil
	}

	connName := p.connNameForRemote(rc.RemoteId)
	if connName == "" {
		p.API.LogWarn("No connection found for remote",
			"error_code", errcode.OutboundSyncMsgNoRemoteMatch,
			"remote_id", rc.RemoteId)
		return nil
	}

	if !p.hasOutboundProvider(connName) {
		p.API.LogDebug("Outbound attachment: connection has no outbound provider",
			"error_code", errcode.OutboundAttachmentNoOutboundProvider,
			"conn_name", connName)
		return nil
	}

	conn, ok := p.outboundConnConfigByName(connName)
	if !ok {
		p.API.LogWarn("Outbound attachment: connection config not found",
			"error_code", errcode.OutboundAttachmentConfigParseFailed,
			"conn_name", connName)
		return nil
	}

	if !conn.FileTransferEnabled {
		p.API.LogDebug("Outbound attachment: file transfer disabled",
			"error_code", errcode.OutboundAttachmentDisabled,
			"conn_name", connName)
		return nil
	}

	if !isFileAllowed(fi.Name, conn.FileFilterMode, conn.FileFilterTypes) {
		p.API.LogInfo("Outbound attachment: file rejected by filter",
			"error_code", errcode.OutboundAttachmentFiltered,
			"conn_name", connName, "post_id", post.Id, "file_id", fi.Id)
		return nil
	}

	if maxSize := p.maxFileSize(); maxSize > 0 && fi.Size > maxSize {
		p.API.LogWarn("Outbound attachment: file exceeds MaxFileSize",
			"error_code", errcode.OutboundAttachmentSizeExceeded,
			"conn_name", connName, "file_id", fi.Id,
			"size", fi.Size, "max_size", maxSize)
		// No retry: the file will not get smaller. Returning nil lets the
		// framework save the attachment record so it stops re-attempting.
		return nil
	}

	data, appErr := p.API.GetFile(fi.Id)
	if appErr != nil {
		p.API.LogError("Outbound attachment: GetFile failed",
			"error_code", errcode.OutboundAttachmentFileFetchFailed,
			"conn_name", connName, "file_id", fi.Id, "error", appErr.Error())
		return fmt.Errorf("get file %s: %w", fi.Id, appErr)
	}

	channel, appErr := p.API.GetChannel(post.ChannelId)
	if appErr != nil {
		p.API.LogError("Outbound attachment: channel lookup failed",
			"error_code", errcode.OutboundAttachmentChannelLookupFailed,
			"conn_name", connName, "channel_id", post.ChannelId, "error", appErr.Error())
		return fmt.Errorf("channel lookup: %w", appErr)
	}

	team, appErr := p.API.GetTeam(channel.TeamId)
	if appErr != nil {
		p.API.LogError("Outbound attachment: team lookup failed",
			"error_code", errcode.OutboundAttachmentTeamLookupFailed,
			"conn_name", connName, "team_id", channel.TeamId, "error", appErr.Error())
		return fmt.Errorf("team lookup: %w", appErr)
	}

	fileInfoJSON, err := json.Marshal(fi)
	if err != nil {
		p.API.LogError("Outbound attachment: FileInfo marshal failed",
			"error_code", errcode.OutboundAttachmentFileInfoEncodeFail,
			"conn_name", connName, "file_id", fi.Id, "error", err.Error())
		return fmt.Errorf("marshal FileInfo: %w", err)
	}

	headers := map[string]string{
		headerKind:     kindAttachment,
		headerConnName: connName,
		headerTeamName: team.Name,
		headerChanName: channel.Name,
		headerPostID:   post.Id,
		headerFilename: fi.Name,
		headerFileInfo: base64.StdEncoding.EncodeToString(fileInfoJSON),
	}

	key := "attachment/" + post.Id + "/" + fi.Id
	if err := p.uploadToOutboundConn(p.ctx, connName, key, data, headers); err != nil {
		p.API.LogError("Outbound attachment: upload failed",
			"error_code", errcode.OutboundAttachmentUploadFailed,
			"conn_name", connName, "post_id", post.Id, "file_id", fi.Id,
			"size", len(data), "error", err.Error())
		return fmt.Errorf("upload attachment: %w", err)
	}

	p.API.LogDebug("Outbound attachment: upload succeeded",
		"conn_name", connName, "post_id", post.Id, "file_id", fi.Id, "size", len(data))
	return nil
}

// OnSharedChannelsProfileImageSyncMsg uploads a user's profile image to the
// matching outbound connection's file path. The framework's
// sendProfileImageToPlugin advances the per-user LastSyncAt cursor
// unconditionally after this returns, so a non-nil error here is purely
// diagnostic. The next opportunity to retry is the user's next image change.
func (p *Plugin) OnSharedChannelsProfileImageSyncMsg(
	user *mmModel.User,
	rc *mmModel.RemoteCluster,
) error {
	if user == nil || rc == nil {
		return nil
	}

	connName := p.connNameForRemote(rc.RemoteId)
	if connName == "" {
		p.API.LogWarn("No connection found for remote",
			"error_code", errcode.OutboundSyncMsgNoRemoteMatch,
			"remote_id", rc.RemoteId)
		return nil
	}

	if !p.hasOutboundProvider(connName) {
		p.API.LogDebug("Outbound profile image: connection has no outbound provider",
			"error_code", errcode.OutboundProfileImageNoOutboundProvider,
			"conn_name", connName)
		return nil
	}

	conn, ok := p.outboundConnConfigByName(connName)
	if !ok {
		p.API.LogWarn("Outbound profile image: connection config not found",
			"error_code", errcode.OutboundProfileImageConfigParseFailed,
			"conn_name", connName)
		return nil
	}

	if !conn.FileTransferEnabled {
		p.API.LogDebug("Outbound profile image: file transfer disabled",
			"error_code", errcode.OutboundProfileImageDisabled,
			"conn_name", connName)
		return nil
	}

	data, appErr := p.API.GetProfileImage(user.Id)
	if appErr != nil {
		p.API.LogError("Outbound profile image: GetProfileImage failed",
			"error_code", errcode.OutboundProfileImageFetchFailed,
			"conn_name", connName, "user_id", user.Id, "error", appErr.Error())
		return fmt.Errorf("get profile image: %w", appErr)
	}

	headers := map[string]string{
		headerKind:     kindProfileImage,
		headerConnName: connName,
		headerUserID:   user.Id,
		headerFilename: "profile.png",
	}

	key := "profile_image/" + user.Id
	if err := p.uploadToOutboundConn(p.ctx, connName, key, data, headers); err != nil {
		p.API.LogError("Outbound profile image: upload failed",
			"error_code", errcode.OutboundProfileImageUploadFailed,
			"conn_name", connName, "user_id", user.Id,
			"size", len(data), "error", err.Error())
		return fmt.Errorf("upload profile image: %w", err)
	}

	p.API.LogDebug("Outbound profile image: upload succeeded",
		"conn_name", connName, "user_id", user.Id, "size", len(data))
	return nil
}

// maxFileSize returns the server's configured FileSettings.MaxFileSize, or
// 0 when the setting is unavailable (skip the size check in that case).
func (p *Plugin) maxFileSize() int64 {
	cfg := p.API.GetConfig()
	if cfg == nil || cfg.FileSettings.MaxFileSize == nil {
		return 0
	}
	return *cfg.FileSettings.MaxFileSize
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
