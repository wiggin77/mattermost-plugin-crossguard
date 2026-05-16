package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

// handleUnlinkedInbound posts a one-time interactive prompt to town-square
// asking a team/system admin to accept or block the inbound connection.
func (p *Plugin) handleUnlinkedInbound(team *model.Team, connName string) {
	prompt, err := p.kvstore.GetConnectionPrompt(team.Id, connName)
	if err != nil {
		p.API.LogError("Failed to get connection prompt",
			"error_code", errcode.PromptGetConnPromptFailed,
			"team_id", team.Id, "conn", connName, "error", err.Error())
		return
	}
	if prompt != nil {
		return
	}

	channel, appErr := p.API.GetChannelByName(team.Id, model.DefaultChannelName, false)
	if appErr != nil {
		p.API.LogError("Failed to get town-square for prompt",
			"error_code", errcode.PromptGetTownSquareFailed,
			"team_id", team.Id, "error", appErr.Error())
		return
	}

	message := fmt.Sprintf(
		"An inbound Cross Guard connection `%s` is trying to relay messages to this team (**%s**). A team admin or system admin must accept or block this connection.",
		connName, team.DisplayName,
	)

	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channel.Id,
		Message:   message,
	}
	model.ParseMessageAttachment(post, []*model.MessageAttachment{
		{
			Actions: []*model.PostAction{
				{
					Id:    "accept",
					Name:  "Accept",
					Style: "good",
					Type:  model.PostActionTypeButton,
					Integration: &model.PostActionIntegration{
						URL: fmt.Sprintf("/plugins/%s/api/v1/prompt/accept", manifest.Id),
						Context: map[string]any{
							"team_id":   team.Id,
							"conn_name": connName,
						},
					},
				},
				{
					Id:    "block",
					Name:  "Block",
					Style: "danger",
					Type:  model.PostActionTypeButton,
					Integration: &model.PostActionIntegration{
						URL: fmt.Sprintf("/plugins/%s/api/v1/prompt/block", manifest.Id),
						Context: map[string]any{
							"team_id":   team.Id,
							"conn_name": connName,
						},
					},
				},
			},
		},
	})

	created, appErr := p.API.CreatePost(post)
	if appErr != nil {
		p.API.LogError("Failed to create connection prompt post",
			"error_code", errcode.PromptCreatePostFailed,
			"team_id", team.Id, "conn", connName, "error", appErr.Error())
		return
	}

	saved, err := p.kvstore.CreateConnectionPrompt(team.Id, connName, &store.ConnectionPrompt{
		State:  store.PromptStatePending,
		PostID: created.Id,
	})
	if err != nil {
		p.API.LogError("Failed to save connection prompt",
			"error_code", errcode.PromptSavePromptFailed,
			"team_id", team.Id, "conn", connName, "error", err.Error())
		_ = p.API.DeletePost(created.Id)
		return
	}
	if !saved {
		_ = p.API.DeletePost(created.Id)
	}
}

func (p *Plugin) handlePromptAccept(w http.ResponseWriter, r *http.Request) {
	var req model.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePostActionResponse(w, "Invalid request.")
		return
	}

	teamID, _ := req.Context["team_id"].(string)
	connName, _ := req.Context["conn_name"].(string)
	if teamID == "" || connName == "" {
		writePostActionResponse(w, "Missing context.")
		return
	}

	if !p.isTeamAdminOrSystemAdmin(req.UserId, teamID) {
		writePostActionResponse(w, "You must be a team admin or system admin to accept connections.")
		return
	}

	prompt, err := p.kvstore.GetConnectionPrompt(teamID, connName)
	if err != nil {
		p.API.LogError("Failed to get connection prompt",
			"error_code", errcode.PromptAcceptGetPromptFailed,
			"team_id", teamID, "conn", connName, "error", err.Error())
		writePostActionResponse(w, "Failed to check prompt status.")
		return
	}
	if prompt == nil || prompt.State != store.PromptStatePending {
		writePostActionResponse(w, "This prompt is no longer active.")
		return
	}

	user, appErr := p.getUser(req.UserId)
	if appErr != nil {
		writePostActionResponse(w, "Failed to look up user.")
		return
	}

	inboundConn := store.TeamConnection{Direction: "inbound", Connection: connName}
	if _, _, svcErr := p.initTeamForCrossGuard(user, teamID, inboundConn); svcErr != nil {
		writePostActionResponse(w, fmt.Sprintf("Failed to link connection: %s", svcErr.Message))
		return
	}

	if err := p.kvstore.DeleteConnectionPrompt(teamID, connName); err != nil {
		p.API.LogError("Failed to delete connection prompt",
			"error_code", errcode.PromptDeletePromptFailed,
			"team_id", teamID, "conn", connName, "error", err.Error())
	}

	newMessage := fmt.Sprintf(
		"Inbound Cross Guard connection `%s` was **accepted** by @%s. The connection is now linked to this team. Channels can now be linked using the channel header menu or `/%s init-channel %s`.",
		connName, user.Username, commandTrigger, connKey(inboundConn),
	)
	updatePromptPost(p, prompt.PostID, newMessage)

	writePostActionResponse(w, "")
}

func (p *Plugin) handlePromptBlock(w http.ResponseWriter, r *http.Request) {
	var req model.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePostActionResponse(w, "Invalid request.")
		return
	}

	teamID, _ := req.Context["team_id"].(string)
	connName, _ := req.Context["conn_name"].(string)
	if teamID == "" || connName == "" {
		writePostActionResponse(w, "Missing context.")
		return
	}

	if !p.isTeamAdminOrSystemAdmin(req.UserId, teamID) {
		writePostActionResponse(w, "You must be a team admin or system admin to block connections.")
		return
	}

	prompt, err := p.kvstore.GetConnectionPrompt(teamID, connName)
	if err != nil {
		p.API.LogError("Failed to get connection prompt",
			"error_code", errcode.PromptBlockGetPromptFailed,
			"team_id", teamID, "conn", connName, "error", err.Error())
		writePostActionResponse(w, "Failed to check prompt status.")
		return
	}
	if prompt == nil || prompt.State != store.PromptStatePending {
		writePostActionResponse(w, "This prompt is no longer active.")
		return
	}

	user, appErr := p.getUser(req.UserId)
	if appErr != nil {
		writePostActionResponse(w, "Failed to look up user.")
		return
	}

	if err := p.kvstore.SetConnectionPrompt(teamID, connName, &store.ConnectionPrompt{
		State:  store.PromptStateBlocked,
		PostID: prompt.PostID,
	}); err != nil {
		p.API.LogError("Failed to update connection prompt to blocked",
			"error_code", errcode.PromptSetBlockedFailed,
			"team_id", teamID, "conn", connName, "error", err.Error())
		writePostActionResponse(w, "Failed to block connection.")
		return
	}

	newMessage := fmt.Sprintf(
		"Inbound Cross Guard connection `%s` was **blocked** by @%s. To unblock, run `/%s reset-prompt %s` or use the channel header menu.",
		connName, user.Username, commandTrigger, connName,
	)
	updatePromptPost(p, prompt.PostID, newMessage)

	writePostActionResponse(w, "")
}

// handleUnlinkedInboundChannel posts a one-time interactive prompt to a channel
// asking a team/system admin to accept or block the inbound connection for that channel.
// The team must already be linked; this handles the channel-level acceptance.
func (p *Plugin) handleUnlinkedInboundChannel(team *model.Team, channel *model.Channel, connName string) {
	prompt, err := p.kvstore.GetChannelConnectionPrompt(channel.Id, connName)
	if err != nil {
		p.API.LogError("Failed to get channel connection prompt",
			"error_code", errcode.PromptGetChanPromptFailed,
			"channel_id", channel.Id, "conn", connName, "error", err.Error())
		return
	}
	if prompt != nil {
		return
	}

	message := fmt.Sprintf(
		"An inbound Cross Guard connection `%s` is trying to relay messages to this channel (**%s**) in team **%s**. The team connection is linked, but this channel has not accepted it yet. A team admin or system admin must accept or block this connection for this channel.",
		connName, channel.DisplayName, team.DisplayName,
	)

	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channel.Id,
		Message:   message,
	}
	model.ParseMessageAttachment(post, []*model.MessageAttachment{
		{
			Actions: []*model.PostAction{
				{
					Id:    "accept",
					Name:  "Accept",
					Style: "good",
					Type:  model.PostActionTypeButton,
					Integration: &model.PostActionIntegration{
						URL: fmt.Sprintf("/plugins/%s/api/v1/prompt/channel/accept", manifest.Id),
						Context: map[string]any{
							"channel_id": channel.Id,
							"conn_name":  connName,
						},
					},
				},
				{
					Id:    "block",
					Name:  "Block",
					Style: "danger",
					Type:  model.PostActionTypeButton,
					Integration: &model.PostActionIntegration{
						URL: fmt.Sprintf("/plugins/%s/api/v1/prompt/channel/block", manifest.Id),
						Context: map[string]any{
							"channel_id": channel.Id,
							"conn_name":  connName,
						},
					},
				},
			},
		},
	})

	created, appErr := p.API.CreatePost(post)
	if appErr != nil {
		p.API.LogError("Failed to create channel connection prompt post",
			"error_code", errcode.PromptCreateChanPostFailed,
			"channel_id", channel.Id, "conn", connName, "error", appErr.Error())
		return
	}

	saved, err := p.kvstore.CreateChannelConnectionPrompt(channel.Id, connName, &store.ConnectionPrompt{
		State:  store.PromptStatePending,
		PostID: created.Id,
	})
	if err != nil {
		p.API.LogError("Failed to save channel connection prompt",
			"error_code", errcode.PromptSaveChanPromptFailed,
			"channel_id", channel.Id, "conn", connName, "error", err.Error())
		_ = p.API.DeletePost(created.Id)
		return
	}
	if !saved {
		_ = p.API.DeletePost(created.Id)
	}
}

func (p *Plugin) handleChannelPromptAccept(w http.ResponseWriter, r *http.Request) {
	var req model.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePostActionResponse(w, "Invalid request.")
		return
	}

	channelID, _ := req.Context["channel_id"].(string)
	connName, _ := req.Context["conn_name"].(string)
	if channelID == "" || connName == "" {
		writePostActionResponse(w, "Missing context.")
		return
	}

	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		writePostActionResponse(w, "Channel not found.")
		return
	}

	if !p.isTeamAdminOrSystemAdmin(req.UserId, channel.TeamId) {
		writePostActionResponse(w, "You must be a team admin or system admin to accept connections.")
		return
	}

	prompt, err := p.kvstore.GetChannelConnectionPrompt(channelID, connName)
	if err != nil {
		p.API.LogError("Failed to get channel connection prompt",
			"error_code", errcode.PromptChanAcceptGetFailed,
			"channel_id", channelID, "conn", connName, "error", err.Error())
		writePostActionResponse(w, "Failed to check prompt status.")
		return
	}
	if prompt == nil || prompt.State != store.PromptStatePending {
		writePostActionResponse(w, "This prompt is no longer active.")
		return
	}

	user, appErr := p.getUser(req.UserId)
	if appErr != nil {
		writePostActionResponse(w, "Failed to look up user.")
		return
	}

	inboundConn := store.TeamConnection{Direction: "inbound", Connection: connName}
	if _, _, svcErr := p.initChannelForCrossGuard(user, channelID, inboundConn); svcErr != nil {
		writePostActionResponse(w, fmt.Sprintf("Failed to link connection: %s", svcErr.Message))
		return
	}

	if err := p.kvstore.DeleteChannelConnectionPrompt(channelID, connName); err != nil {
		p.API.LogError("Failed to delete channel connection prompt",
			"error_code", errcode.PromptDeleteChanPromptFailed,
			"channel_id", channelID, "conn", connName, "error", err.Error())
	}

	newMessage := fmt.Sprintf(
		"Inbound Cross Guard connection `%s` was **accepted** for this channel by @%s. Messages will now relay to this channel.",
		connName, user.Username,
	)
	updatePromptPost(p, prompt.PostID, newMessage)

	writePostActionResponse(w, "")
}

func (p *Plugin) handleChannelPromptBlock(w http.ResponseWriter, r *http.Request) {
	var req model.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePostActionResponse(w, "Invalid request.")
		return
	}

	channelID, _ := req.Context["channel_id"].(string)
	connName, _ := req.Context["conn_name"].(string)
	if channelID == "" || connName == "" {
		writePostActionResponse(w, "Missing context.")
		return
	}

	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		writePostActionResponse(w, "Channel not found.")
		return
	}

	if !p.isTeamAdminOrSystemAdmin(req.UserId, channel.TeamId) {
		writePostActionResponse(w, "You must be a team admin or system admin to block connections.")
		return
	}

	prompt, err := p.kvstore.GetChannelConnectionPrompt(channelID, connName)
	if err != nil {
		p.API.LogError("Failed to get channel connection prompt",
			"error_code", errcode.PromptChanBlockGetFailed,
			"channel_id", channelID, "conn", connName, "error", err.Error())
		writePostActionResponse(w, "Failed to check prompt status.")
		return
	}
	if prompt == nil || prompt.State != store.PromptStatePending {
		writePostActionResponse(w, "This prompt is no longer active.")
		return
	}

	user, appErr := p.getUser(req.UserId)
	if appErr != nil {
		writePostActionResponse(w, "Failed to look up user.")
		return
	}

	if err := p.kvstore.SetChannelConnectionPrompt(channelID, connName, &store.ConnectionPrompt{
		State:  store.PromptStateBlocked,
		PostID: prompt.PostID,
	}); err != nil {
		p.API.LogError("Failed to update channel connection prompt to blocked",
			"error_code", errcode.PromptSetChanBlockedFailed,
			"channel_id", channelID, "conn", connName, "error", err.Error())
		writePostActionResponse(w, "Failed to block connection.")
		return
	}

	newMessage := fmt.Sprintf(
		"Inbound Cross Guard connection `%s` was **blocked** for this channel by @%s. To unblock, run `/%s reset-channel-prompt %s`.",
		connName, user.Username, commandTrigger, connName,
	)
	updatePromptPost(p, prompt.PostID, newMessage)

	writePostActionResponse(w, "")
}

func updatePromptPost(p *Plugin, postID, newMessage string) {
	post, appErr := p.API.GetPost(postID)
	if appErr != nil {
		p.API.LogError("Failed to get prompt post for update",
			"error_code", errcode.PromptGetPostForUpdateFailed,
			"post_id", postID, "error", appErr.Error())
		return
	}

	post.Message = newMessage
	post.AddProp("attachments", nil)

	if _, appErr := p.API.UpdatePost(post); appErr != nil {
		p.API.LogError("Failed to update prompt post",
			"error_code", errcode.PromptUpdatePostFailed,
			"post_id", postID, "error", appErr.Error())
	}
}

func writePostActionResponse(w http.ResponseWriter, ephemeralText string) {
	resp := model.PostActionIntegrationResponse{}
	if ephemeralText != "" {
		resp.EphemeralText = ephemeralText
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
