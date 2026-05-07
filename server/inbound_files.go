package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

// fileWatcherInitialBackoff is the wait between the first WatchFiles return
// and the next attempt. Doubles up to fileWatcherMaxBackoff so a persistently
// broken provider does not log-spam. Declared as var so tests can shrink them.
var (
	fileWatcherInitialBackoff = time.Second
	fileWatcherMaxBackoff     = time.Minute
)

// startInboundFileWatchers runs WatchFiles on the connection's provider in
// its own goroutine and restarts it on return until the context is cancelled.
// WatchFiles is expected to block until ctx.Done; an early return (NATS
// JetStream consumer torn down by a reconnect, transport-level error, etc.)
// would otherwise leave the receiver permanently deaf to file payloads. The
// loop logs each restart so operators can correlate watcher gaps with
// upstream incidents.
func (p *Plugin) startInboundFileWatchers(ctx context.Context, ic inboundConn) {
	p.wg.Go(func() {
		backoff := fileWatcherInitialBackoff
		for {
			err := ic.provider.WatchFiles(ctx, p.handleInboundFile(ic.name))
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				p.API.LogWarn("Inbound file watcher exited with error, will restart",
					"error_code", errcode.InboundFileWatcherRestart,
					"conn_name", ic.name, "backoff", backoff.String(), "error", err.Error())
			} else {
				p.API.LogInfo("Inbound file watcher returned without error, will restart",
					"error_code", errcode.InboundFileWatcherRestart,
					"conn_name", ic.name, "backoff", backoff.String())
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > fileWatcherMaxBackoff {
				backoff = fileWatcherMaxBackoff
			}
		}
	})
}

// handleInboundFile returns a WatchFiles handler that dispatches by the
// X-Crossguard-Kind header. Returning nil tells the provider to clean up
// the file (delete blob/object). Returning an error keeps the file for
// the next watcher cycle.
func (p *Plugin) handleInboundFile(connName string) func(key string, data []byte, headers map[string]string) error {
	return func(key string, data []byte, headers map[string]string) error {
		switch headers[headerKind] {
		case kindAttachment:
			return p.handleInboundAttachment(connName, key, data, headers)
		case kindProfileImage:
			return p.handleInboundProfileImage(connName, key, data, headers)
		default:
			p.API.LogWarn("Inbound file: unknown kind",
				"error_code", errcode.InboundFileUnknownKind,
				"conn_name", connName, "key", key, "kind", headers[headerKind])
			// Permanent: malformed headers will not heal on retry.
			return nil
		}
	}
}

func (p *Plugin) handleInboundAttachment(connName, key string, data []byte, headers map[string]string) error {
	teamName := headers[headerTeamName]
	chanName := headers[headerChanName]
	postID := headers[headerPostID]
	fileInfoB64 := headers[headerFileInfo]

	if teamName == "" || chanName == "" || postID == "" || fileInfoB64 == "" {
		p.API.LogWarn("Inbound attachment: missing required header",
			"error_code", errcode.InboundAttachmentMissingHeader,
			"conn_name", connName, "key", key,
			"have_team", teamName != "", "have_channel", chanName != "",
			"have_post", postID != "", "have_fileinfo", fileInfoB64 != "")
		return nil
	}

	fi, err := decodeFileInfoHeader(fileInfoB64)
	if err != nil {
		p.API.LogWarn("Inbound attachment: FileInfo decode failed",
			"error_code", errcode.InboundAttachmentFileInfoDecodeFailed,
			"conn_name", connName, "key", key, "error", err.Error())
		return nil
	}

	team, err := p.findTeamByRewrite(connName, teamName)
	if err != nil {
		p.API.LogWarn("Inbound attachment: team rewrite lookup failed",
			"error_code", errcode.InboundAttachmentTeamLookupFailed,
			"conn_name", connName, "team_name", teamName, "error", err.Error())
		return fmt.Errorf("team rewrite lookup: %w", err)
	}
	if team == nil {
		var appErr *mmModel.AppError
		team, appErr = p.API.GetTeamByName(teamName)
		if appErr != nil {
			p.API.LogInfo("Inbound attachment: team not found, will retry",
				"error_code", errcode.InboundAttachmentTeamLookupFailed,
				"conn_name", connName, "team_name", teamName)
			return fmt.Errorf("team %q not found: %w", teamName, appErr)
		}
	}

	channel, appErr := p.API.GetChannelByName(team.Id, chanName, false)
	if appErr != nil {
		p.API.LogInfo("Inbound attachment: channel not found, will retry",
			"error_code", errcode.InboundAttachmentChannelLookupFailed,
			"conn_name", connName, "team_id", team.Id, "channel_name", chanName)
		return fmt.Errorf("channel %q in team %q not found: %w", chanName, teamName, appErr)
	}

	conns, kvErr := p.kvstore.GetChannelConnections(channel.Id)
	if kvErr != nil || !hasInboundConnection(conns, connName) {
		// Channel exists but is not linked for this inbound. Drop the file:
		// retrying will not help until an admin links the channel, and the
		// sender's framework already saved the SharedChannelAttachment record
		// when its outbound hook returned nil. Subsequent attachments after
		// acceptance arrive normally; this individual file is lost.
		p.API.LogInfo("Inbound attachment: channel not linked for connection, dropping",
			"error_code", errcode.InboundAttachmentChannelUnlinked,
			"conn_name", connName, "channel_id", channel.Id)
		return nil
	}

	remoteID := p.remoteIDs["inbound:"+connName]
	if remoteID == "" {
		p.API.LogError("Inbound attachment: no registered remote for connection",
			"error_code", errcode.InboundAttachmentNoRemoteForConn,
			"conn_name", connName)
		return nil
	}

	if _, err := p.API.ReceiveSharedChannelAttachmentSyncMsg(remoteID, channel.Id, fi, bytes.NewReader(data)); err != nil {
		p.API.LogError("Inbound attachment: ReceiveSharedChannelAttachmentSyncMsg failed",
			"error_code", errcode.InboundAttachmentReceiveFailed,
			"conn_name", connName, "channel_id", channel.Id,
			"size", len(data), "error", err.Error())
		return fmt.Errorf("ReceiveSharedChannelAttachmentSyncMsg: %w", err)
	}

	p.API.LogDebug("Inbound attachment: delivered",
		"conn_name", connName, "channel_id", channel.Id, "post_id", postID, "size", len(data))
	return nil
}

func (p *Plugin) handleInboundProfileImage(connName, _ string, data []byte, headers map[string]string) error {
	userID := headers[headerUserID]
	if userID == "" {
		p.API.LogWarn("Inbound profile image: missing user_id header",
			"error_code", errcode.InboundProfileImageMissingHeader,
			"conn_name", connName)
		return nil
	}

	remoteID := p.remoteIDs["inbound:"+connName]
	if remoteID == "" {
		p.API.LogError("Inbound profile image: no registered remote for connection",
			"error_code", errcode.InboundProfileImageNoRemoteForConn,
			"conn_name", connName)
		return nil
	}

	if err := p.API.ReceiveSharedChannelProfileImageSyncMsg(remoteID, userID, data); err != nil {
		p.API.LogError("Inbound profile image: ReceiveSharedChannelProfileImageSyncMsg failed",
			"error_code", errcode.InboundProfileImageReceiveFailed,
			"conn_name", connName, "user_id", userID,
			"size", len(data), "error", err.Error())
		return fmt.Errorf("ReceiveSharedChannelProfileImageSyncMsg: %w", err)
	}

	p.API.LogDebug("Inbound profile image: delivered",
		"conn_name", connName, "user_id", userID, "size", len(data))
	return nil
}

func decodeFileInfoHeader(b64 string) (*mmModel.FileInfo, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	var fi mmModel.FileInfo
	if err := json.Unmarshal(raw, &fi); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}
	return &fi, nil
}
