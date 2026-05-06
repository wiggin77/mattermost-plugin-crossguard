package main

import (
	"context"
	"fmt"
	"strings"

	mmModel "github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

type inboundConn struct {
	provider QueueProvider
	name     string
}

func (p *Plugin) connectInbound() {
	p.inboundCtx, p.inboundCancel = context.WithCancel(p.ctx)
	ctx := p.inboundCtx

	cfg := p.getConfiguration()
	conns, err := cfg.GetInboundConnections()
	if err != nil {
		p.API.LogError("Failed to parse inbound connections",
			"error_code", errcode.InboundParseConnsFailed,
			"error", err.Error())
		return
	}

	var pool []inboundConn
	for _, conn := range conns {
		provider, err := p.createProvider(conn, "Inbound")
		if err != nil {
			p.API.LogError("Failed to connect inbound",
				"error_code", errcode.InboundConnectFailed,
				"name", conn.Name, "provider", conn.Provider, "error", err.Error())
			continue
		}

		handler := p.handleInboundMessage(conn.Name)
		if err := provider.Subscribe(ctx, handler); err != nil {
			p.API.LogError("Failed to subscribe inbound",
				"error_code", errcode.InboundSubscribeFailed,
				"name", conn.Name, "error", err.Error())
			_ = provider.Close()
			continue
		}

		pool = append(pool, inboundConn{
			provider: provider,
			name:     conn.Name,
		})
		p.API.LogInfo("Inbound subscription established",
			"error_code", errcode.InboundSubscriptionEstablished,
			"name", conn.Name, "provider", conn.Provider)
	}

	p.inboundMu.Lock()
	p.inboundConns = pool
	p.inboundMu.Unlock()
}

func (p *Plugin) closeInbound() {
	if p.inboundCancel != nil {
		p.inboundCancel()
	}

	p.inboundMu.Lock()
	conns := p.inboundConns
	p.inboundConns = nil
	p.inboundMu.Unlock()

	for _, ic := range conns {
		_ = ic.provider.Close()
	}
}

func (p *Plugin) reconnectInbound() {
	p.closeInbound()
	p.connectInbound()
}

// handleInboundMessage returns a synchronous Subscribe handler. The handler
// blocks on the relay semaphore (rather than dropping when full) so we
// apply backpressure to ack-based providers (Azure Queue, Service Bus,
// future NATS JetStream) instead of acknowledging messages we never
// processed. The handler's return value is the delivery acknowledgement
// signal: nil means "processed", non-nil means "not processed, redeliver
// if you can".
func (p *Plugin) handleInboundMessage(connName string) func(data []byte) error {
	return func(data []byte) error {
		select {
		case p.relaySem <- struct{}{}:
			defer func() { <-p.relaySem }()
		case <-p.ctx.Done():
			return p.ctx.Err()
		}
		return p.processInboundMessage(connName, data)
	}
}

func (p *Plugin) processInboundMessage(connName string, data []byte) error {
	p.API.LogDebug("Inbound message received from provider",
		"conn_name", connName, "bytes", len(data))

	env, err := UnmarshalEnvelope(data)
	if err != nil {
		p.API.LogError("Failed to unmarshal transport envelope",
			"error_code", errcode.InboundUnmarshalFailed,
			"conn_name", connName, "error", err.Error())
		// Permanent failure: a malformed message will never succeed on retry.
		return nil
	}

	switch env.Type {
	case TransportTypeSyncMsg:
		return p.handleInboundSyncMsg(connName, env)
	case TransportTypeTest:
		p.API.LogInfo("Received test message",
			"error_code", errcode.InboundTestReceived,
			"conn_name", connName, "test_id", env.TestID)
		return nil
	case TransportTypeAttachment:
		p.API.LogDebug("Attachment sync not yet implemented",
			"error_code", errcode.InboundAttachmentStubbed,
			"conn_name", connName)
		return nil
	case TransportTypeProfileImage:
		p.API.LogDebug("Profile image sync not yet implemented",
			"error_code", errcode.InboundProfileImageStubbed,
			"conn_name", connName)
		return nil
	default:
		p.API.LogWarn("Unknown transport message type",
			"error_code", errcode.InboundUnknownType,
			"conn_name", connName, "type", env.Type)
		return nil
	}
}

func (p *Plugin) handleInboundSyncMsg(connName string, env *TransportEnvelope) error {
	if env.SyncMsg == nil {
		return nil
	}

	// Defense in depth: data arrives from external transport, so reject
	// sync messages targeting a connection that has no inbound subscription
	// configured.
	if !p.hasInboundProvider(connName) {
		p.API.LogWarn("Received sync message for non-inbound connection",
			"error_code", errcode.InboundNotConfigured,
			"conn_name", connName)
		return nil
	}

	team, channel, err := p.resolveTeamAndChannel(connName, env.TeamName, env.ChannelName)
	if err != nil {
		// resolveTeamAndChannel logs and triggers prompts when appropriate;
		// the message is dropped (returning nil) because retrying would not
		// help until an admin links the channel.
		_ = team
		_ = err
		return nil
	}

	conns, kvErr := p.kvstore.GetChannelConnections(channel.Id)
	if kvErr != nil || !hasInboundConnection(conns, connName) {
		p.handleUnlinkedInboundChannel(team, channel, connName)
		return nil
	}

	remoteID := p.remoteIDs["inbound:"+connName]
	if remoteID == "" {
		p.API.LogError("No registered remote for inbound connection",
			"error_code", errcode.InboundNoRemoteForConn,
			"conn_name", connName)
		return nil
	}

	rewriteChannelIDs(env.SyncMsg, channel.Id)

	resp, err := p.API.ReceiveSharedChannelSyncMsg(remoteID, env.SyncMsg)
	if err != nil {
		p.API.LogError("Failed to receive sync message",
			"error_code", errcode.InboundReceiveSyncFailed,
			"conn_name", connName, "channel_id", channel.Id, "error", err.Error())
		return fmt.Errorf("ReceiveSharedChannelSyncMsg failed: %w", err)
	}

	postCount := 0
	if env.SyncMsg != nil {
		postCount = len(env.SyncMsg.Posts)
	}
	p.API.LogDebug("ReceiveSharedChannelSyncMsg accepted",
		"conn_name", connName, "channel_id", channel.Id, "post_count", postCount)

	if errs := joinSyncErrors(resp); errs != "" {
		p.API.LogWarn("Some entities failed to sync",
			"error_code", errcode.InboundPostSyncErrors,
			"conn_name", connName, "errors", errs)
	}
	return nil
}

func joinSyncErrors(resp mmModel.SyncResponse) string {
	var all []string
	all = append(all, resp.UserErrors...)
	all = append(all, resp.PostErrors...)
	all = append(all, resp.ReactionErrors...)
	all = append(all, resp.AcknowledgementErrors...)
	all = append(all, resp.StatusErrors...)
	all = append(all, resp.MembershipErrors...)
	return strings.Join(all, ", ")
}

// rewriteChannelIDs updates ChannelId references in nested SyncMsg entities
// to the local channel ID. The remote sender's view of ChannelId is not
// valid on the receiving side; the server's ReceiveSharedChannelSyncMsg
// uses these IDs to route content.
func rewriteChannelIDs(msg *mmModel.SyncMsg, localChannelID string) {
	msg.ChannelId = localChannelID
	for _, post := range msg.Posts {
		if post != nil {
			post.ChannelId = localChannelID
		}
	}
	for _, reaction := range msg.Reactions {
		if reaction != nil {
			reaction.ChannelId = localChannelID
		}
	}
	for _, mc := range msg.MembershipChanges {
		if mc != nil {
			mc.ChannelId = localChannelID
		}
	}
	for _, ack := range msg.Acknowledgements {
		if ack != nil {
			ack.ChannelId = localChannelID
		}
	}
}

// resolveTeamAndChannel looks up the local team and channel by their remote
// names. If the team is unlinked (no team-level inbound prompt accepted),
// triggers the team-level prompt. If the team is found via the rewrite
// index, uses that.
func (p *Plugin) resolveTeamAndChannel(connName, teamName, channelName string) (*mmModel.Team, *mmModel.Channel, error) {
	team, err := p.findTeamByRewrite(connName, teamName)
	if err != nil {
		return nil, nil, err
	}
	if team == nil {
		var appErr *mmModel.AppError
		team, appErr = p.API.GetTeamByName(teamName)
		if appErr != nil {
			// Unknown team. Cannot post a prompt without a team context.
			return nil, nil, fmt.Errorf("team %q not found: %w", teamName, appErr)
		}
	}

	teamConns, kvErr := p.kvstore.GetTeamConnections(team.Id)
	if kvErr != nil {
		return nil, nil, fmt.Errorf("failed to check team connections: %w", kvErr)
	}
	if !hasInboundConnection(teamConns, connName) {
		p.handleUnlinkedInbound(team, connName)
		return nil, nil, fmt.Errorf("inbound connection %q is not linked to team %q", connName, teamName)
	}

	channel, appErr := p.API.GetChannelByName(team.Id, channelName, false)
	if appErr != nil {
		return nil, nil, fmt.Errorf("channel %q not found in team %q: %w", channelName, teamName, appErr)
	}
	return team, channel, nil
}

func (p *Plugin) findTeamByRewrite(connName, remoteTeamName string) (*mmModel.Team, error) {
	localTeamID, err := p.kvstore.GetTeamRewriteIndex(connName, remoteTeamName)
	if err != nil {
		return nil, fmt.Errorf("rewrite index lookup for %s/%s: %w", connName, remoteTeamName, err)
	}
	if localTeamID == "" {
		return nil, nil
	}
	team, appErr := p.API.GetTeam(localTeamID)
	if appErr != nil {
		return nil, fmt.Errorf("rewrite target team %s not found: %w", localTeamID, appErr)
	}
	return team, nil
}

func hasInboundConnection(conns []store.TeamConnection, connName string) bool {
	for _, tc := range conns {
		if tc.Direction == "inbound" && tc.Connection == connName {
			return true
		}
	}
	return false
}
