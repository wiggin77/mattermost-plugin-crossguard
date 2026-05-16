package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

// isChannelAdminInRequestMode returns true if the user is a channel admin (not
// a sysadmin or team admin) and the plugin is configured for channel request
// mode. System admins and team admins always have higher authority.
func (p *Plugin) isChannelAdminInRequestMode(userID, channelID, teamID string) bool {
	user, appErr := p.getUser(userID)
	if appErr != nil {
		return false
	}

	if user.IsSystemAdmin() {
		return false
	}

	if !p.getConfiguration().isChannelRequestMode() {
		return false
	}

	// Team admins get direct channel permission via the gap fix (Task 7),
	// so they should not enter channel request mode.
	teamMember, appErr := p.API.GetTeamMember(teamID, userID)
	if appErr != nil {
		return false
	}
	if teamMember.SchemeAdmin {
		return false
	}

	member, appErr := p.API.GetChannelMember(channelID, userID)
	if appErr != nil || member == nil {
		return false
	}

	return member.SchemeAdmin
}

// canDirectlyManageChannelConns returns true if the user is a team admin (or
// sysadmin) and channel request mode is enabled. Team admins are the approvers
// for channel connection requests, so they should be able to link/unlink
// channels directly without going through the request workflow.
func (p *Plugin) canDirectlyManageChannelConns(userID, teamID string) bool {
	if !p.getConfiguration().isChannelRequestMode() {
		return false
	}
	return p.isTeamAdminDirect(userID, teamID)
}

// isTeamAdminDirect returns true if the user is a team admin or sysadmin.
// Unlike isTeamAdminOrSystemAdmin, this does NOT check RestrictToSystemAdmins,
// so team admins can approve/deny channel requests even when restricted.
func (p *Plugin) isTeamAdminDirect(userID, teamID string) bool {
	user, appErr := p.getUser(userID)
	if appErr != nil {
		return false
	}

	if user.IsSystemAdmin() {
		return true
	}

	member, appErr := p.API.GetTeamMember(teamID, userID)
	if appErr != nil {
		return false
	}

	return member.SchemeAdmin
}

// getTeamAdmins returns all team admin users for the given team, including
// sysadmins who are team members (even without SchemeAdmin on the membership).
func (p *Plugin) getTeamAdmins(teamID string) ([]*model.User, error) {
	var admins []*model.User
	page := 0
	for {
		members, appErr := p.API.GetTeamMembers(teamID, page, 200)
		if appErr != nil {
			p.API.LogError("Failed to get team members for admin lookup",
				"error_code", errcode.ChanRequestGetTeamAdminsFailed,
				"team_id", teamID, "error", appErr.Error())
			return nil, fmt.Errorf("failed to get team members: %s", appErr.Error())
		}
		if len(members) == 0 {
			break
		}
		for _, m := range members {
			user, uErr := p.getUser(m.UserId)
			if uErr != nil {
				p.API.LogWarn("Failed to get user during team admin lookup",
					"error_code", errcode.ChanRequestGetTeamAdminsFailed,
					"user_id", m.UserId, "error", uErr.Error())
				continue
			}
			if m.SchemeAdmin || user.IsSystemAdmin() {
				admins = append(admins, user)
			}
		}
		page++
	}
	return admins, nil
}

// createChannelConnectionRequest creates a pending channel link request and DMs
// all team admins with interactive Approve/Deny buttons. Returns a user-facing message.
func (p *Plugin) createChannelConnectionRequest(user *model.User, channelID, teamID string, conn store.TeamConnection) (string, error) {
	ck := connKey(conn)

	existing, err := p.kvstore.GetChannelConnectionRequest(channelID, ck)
	if err != nil {
		p.API.LogError("Failed to get channel connection request",
			"error_code", errcode.ChanRequestGetFailed,
			"channel_id", channelID, "conn_key", ck, "error", err.Error())
		return "", fmt.Errorf("failed to check existing requests")
	}
	if existing != nil {
		return "", fmt.Errorf("a channel request is already pending for this connection")
	}

	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return "", fmt.Errorf("channel not found")
	}

	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		return "", fmt.Errorf("team not found")
	}

	// Verify team has the connection linked (prerequisite).
	teamConns, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		return "", fmt.Errorf("failed to check team connections")
	}
	teamHasConn := false
	for _, tc := range teamConns {
		if tc.Matches(conn) {
			teamHasConn = true
			break
		}
	}
	if !teamHasConn {
		return "", fmt.Errorf("connection `%s` is not linked to this team; the team must be initialized first", ck)
	}

	admins, err := p.getTeamAdmins(teamID)
	if err != nil {
		return "", fmt.Errorf("failed to look up team admins")
	}
	if len(admins) == 0 {
		p.API.LogWarn("No team admins available to review channel connection request",
			"error_code", errcode.ChanRequestNoTeamAdmins,
			"channel_id", channelID, "team_id", teamID, "conn_key", ck)
		return "", fmt.Errorf("no team admins available to review your request")
	}

	userLabel := "@" + user.Username
	if fullName := strings.TrimSpace(user.FirstName + " " + user.LastName); fullName != "" {
		userLabel += " (" + fullName + ")"
	}

	teamLink := teamTownSquareLink(team)
	chanLink := channelLink(channel, team)

	// Reserve the request slot atomically before sending DMs, so concurrent
	// requests do not race to create duplicate notifications.
	req := &store.ConnectionRequest{
		RequesterID: user.Id,
		TeamID:      teamID,
		ChannelID:   channelID,
		ConnKey:     ck,
		CreatedAt:   time.Now().UnixMilli(),
	}

	saved, err := p.kvstore.CreateChannelConnectionRequest(channelID, ck, req)
	if err != nil {
		p.API.LogError("Failed to save channel connection request",
			"error_code", errcode.ChanRequestSaveFailed,
			"channel_id", channelID, "conn_key", ck, "error", err.Error())
		return "", fmt.Errorf("failed to save channel connection request")
	}
	if !saved {
		return "", fmt.Errorf("a channel request is already pending for this connection")
	}

	// CAS succeeded, now send DM notifications to team admins.
	message := buildRequestDMMessage(userLabel, teamLink, ck, p.getConnectionMap(), chanLink)

	var postIDs []string
	for _, admin := range admins {
		dmChannel, appErr := p.API.GetDirectChannel(p.botUserID, admin.Id)
		if appErr != nil {
			p.API.LogError("Failed to get DM channel for channel connection request",
				"error_code", errcode.ChanRequestGetDMChannelFailed,
				"admin_id", admin.Id, "error", appErr.Error())
			continue
		}

		post := &model.Post{
			UserId:    p.botUserID,
			ChannelId: dmChannel.Id,
			Message:   message,
		}
		model.ParseMessageAttachment(post, []*model.MessageAttachment{
			{
				Actions: []*model.PostAction{
					{
						Id:    "approve",
						Name:  "Approve",
						Style: "good",
						Type:  model.PostActionTypeButton,
						Integration: &model.PostActionIntegration{
							URL: fmt.Sprintf("/plugins/%s/api/v1/channel-request/approve", manifest.Id),
							Context: map[string]any{
								"channel_id": channelID,
								"team_id":    teamID,
								"conn_key":   ck,
							},
						},
					},
					{
						Id:    "deny",
						Name:  "Deny",
						Style: "danger",
						Type:  model.PostActionTypeButton,
						Integration: &model.PostActionIntegration{
							URL: fmt.Sprintf("/plugins/%s/api/v1/channel-request/deny", manifest.Id),
							Context: map[string]any{
								"channel_id": channelID,
								"team_id":    teamID,
								"conn_key":   ck,
							},
						},
					},
				},
			},
		})

		created, appErr := p.API.CreatePost(post)
		if appErr != nil {
			p.API.LogError("Failed to create DM post for channel connection request",
				"error_code", errcode.ChanRequestCreateDMPostFailed,
				"admin_id", admin.Id, "error", appErr.Error())
			continue
		}
		postIDs = append(postIDs, created.Id)
	}

	if len(postIDs) == 0 {
		p.API.LogError("Failed to notify any team admin about channel connection request",
			"error_code", errcode.ChanRequestNoAdminsNotified,
			"channel_id", channelID, "team_id", teamID, "conn_key", ck)
		// Clean up the reserved request since no admins were notified.
		_ = p.kvstore.DeleteChannelConnectionRequest(channelID, ck)
		return "", fmt.Errorf("failed to notify any team admin about your request")
	}

	// Update the stored request with the post IDs for later cleanup.
	req.PostIDs = postIDs
	if updateErr := p.kvstore.UpdateChannelConnectionRequest(channelID, ck, req); updateErr != nil {
		p.API.LogWarn("Failed to update channel connection request with post IDs",
			"error_code", errcode.ChanRequestSaveFailed,
			"channel_id", channelID, "conn_key", ck, "error", updateErr.Error())
	}

	confirmMsg := buildRequestConfirmationMessage(teamLink, ck, p.getConnectionMap(), chanLink, "team admins")
	if dmChannel, appErr := p.API.GetDirectChannel(p.botUserID, user.Id); appErr != nil {
		p.API.LogWarn("Failed to get DM channel for channel requester confirmation",
			"error_code", errcode.ChanRequestConfirmDMFailed,
			"user_id", user.Id, "error", appErr.Error())
	} else if _, appErr := p.API.CreatePost(&model.Post{
		UserId:    p.botUserID,
		ChannelId: dmChannel.Id,
		Message:   confirmMsg,
	}); appErr != nil {
		p.API.LogWarn("Failed to send channel requester confirmation DM",
			"error_code", errcode.ChanRequestConfirmDMFailed,
			"user_id", user.Id, "error", appErr.Error())
	}

	return fmt.Sprintf("Your request to link connection `%s` for channel %s has been submitted for team admin approval.", ck, chanLink), nil
}

func (p *Plugin) handleChannelRequestApprove(w http.ResponseWriter, r *http.Request) {
	var req model.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePostActionResponse(w, "Invalid request.")
		return
	}

	channelID, _ := req.Context["channel_id"].(string)
	teamID, _ := req.Context["team_id"].(string)
	ck, _ := req.Context["conn_key"].(string)
	if channelID == "" || teamID == "" || ck == "" {
		writePostActionResponse(w, "Missing context.")
		return
	}

	user, appErr := p.getUser(req.UserId)
	if appErr != nil {
		writePostActionResponse(w, "Failed to look up user.")
		return
	}

	if !p.isTeamAdminDirect(user.Id, teamID) {
		writePostActionResponse(w, "Only team admins and system admins can approve channel connection requests.")
		return
	}

	// Delete-first: remove the request atomically so concurrent approve/deny
	// clicks from other admins see "no longer active" instead of duplicating
	// side effects (DMs, init calls).
	connReq, err := p.kvstore.GetChannelConnectionRequest(channelID, ck)
	if err != nil {
		p.API.LogError("Failed to get channel connection request for approve",
			"error_code", errcode.ChanRequestApproveGetFailed,
			"channel_id", channelID, "conn_key", ck, "error", err.Error())
		writePostActionResponse(w, "Failed to check request status.")
		return
	}
	if connReq == nil {
		writePostActionResponse(w, "This request is no longer active.")
		return
	}

	if err := p.kvstore.DeleteChannelConnectionRequest(channelID, ck); err != nil {
		p.API.LogError("Failed to delete channel connection request during approve",
			"error_code", errcode.ChanRequestApproveDeleteFailed,
			"channel_id", channelID, "conn_key", ck, "error", err.Error())
		writePostActionResponse(w, "Failed to process request. Please try again.")
		return
	}

	// Validate connection still in config.
	allConns := p.getAllConnectionNames()
	connFound := false
	var conn store.TeamConnection
	for _, tc := range allConns {
		if connKey(tc) == ck {
			connFound = true
			conn = tc
			break
		}
	}
	if !connFound {
		p.API.LogWarn("Connection removed from config while channel request was pending",
			"error_code", errcode.ChanRequestConnRemovedFromConfig,
			"channel_id", channelID, "conn_key", ck)
		p.updateRequestPosts(connReq.PostIDs, "| **Status** | :warning: Cancelled (connection removed from configuration) |")
		p.notifyRequester(connReq.RequesterID, fmt.Sprintf("Your request to link connection `%s` to a channel was cancelled because the connection no longer exists in the configuration.", ck))
		writePostActionResponse(w, "")
		return
	}

	// Validate team still has the connection.
	teamConns, teamErr := p.kvstore.GetTeamConnections(teamID)
	if teamErr != nil {
		p.API.LogError("Failed to get team connections during channel request approve",
			"error_code", errcode.ChanRequestApproveGetFailed,
			"team_id", teamID, "conn_key", ck, "error", teamErr.Error())
		writePostActionResponse(w, "Failed to verify team connections. Please try again.")
		return
	}
	teamHasConn := false
	for _, tc := range teamConns {
		if tc.Matches(conn) {
			teamHasConn = true
			break
		}
	}
	if !teamHasConn {
		p.updateRequestPosts(connReq.PostIDs, "| **Status** | :warning: Cancelled (connection unlinked from team) |")
		p.notifyRequester(connReq.RequesterID, fmt.Sprintf("Your request to link connection `%s` to a channel was cancelled because the connection was unlinked from the team.", ck))
		writePostActionResponse(w, "")
		return
	}

	// Verify channel still exists.
	if _, appErr := p.API.GetChannel(channelID); appErr != nil {
		p.updateRequestPosts(connReq.PostIDs, "| **Status** | :warning: Cancelled (channel was deleted) |")
		p.notifyRequester(connReq.RequesterID, fmt.Sprintf("Your request to link connection `%s` to a channel was cancelled because the channel no longer exists.", ck))
		writePostActionResponse(w, "")
		return
	}

	if _, _, svcErr := p.initChannelForCrossGuard(user, channelID, conn); svcErr != nil {
		p.API.LogError("Failed to execute approved channel connection request",
			"error_code", errcode.ChanRequestApproveExecFailed,
			"channel_id", channelID, "conn_key", ck, "error", svcErr.Message)
		writePostActionResponse(w, fmt.Sprintf("Failed to link connection: %s", svcErr.Message))
		return
	}

	p.updateRequestPosts(connReq.PostIDs, fmt.Sprintf(
		"| **Status** | :white_check_mark: Approved |\n"+
			"| **Approved by** | @%s |", user.Username))
	p.notifyRequester(connReq.RequesterID, fmt.Sprintf("Your request to link connection `%s` to a channel has been **approved** by @%s.", ck, user.Username))

	writePostActionResponse(w, "")
}

func (p *Plugin) handleChannelRequestDeny(w http.ResponseWriter, r *http.Request) {
	var req model.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePostActionResponse(w, "Invalid request.")
		return
	}

	channelID, _ := req.Context["channel_id"].(string)
	teamID, _ := req.Context["team_id"].(string)
	ck, _ := req.Context["conn_key"].(string)
	if channelID == "" || teamID == "" || ck == "" {
		writePostActionResponse(w, "Missing context.")
		return
	}

	user, appErr := p.getUser(req.UserId)
	if appErr != nil {
		writePostActionResponse(w, "Failed to look up user.")
		return
	}

	if !p.isTeamAdminDirect(user.Id, teamID) {
		writePostActionResponse(w, "Only team admins and system admins can deny channel connection requests.")
		return
	}

	connReq, err := p.kvstore.GetChannelConnectionRequest(channelID, ck)
	if err != nil {
		p.API.LogError("Failed to get channel connection request for deny",
			"error_code", errcode.ChanRequestDenyGetFailed,
			"channel_id", channelID, "conn_key", ck, "error", err.Error())
		writePostActionResponse(w, "Failed to check request status.")
		return
	}
	if connReq == nil {
		writePostActionResponse(w, "This request is no longer active.")
		return
	}

	appErr = p.API.OpenInteractiveDialog(model.OpenDialogRequest{
		TriggerId: req.TriggerId,
		URL:       fmt.Sprintf("/plugins/%s/api/v1/channel-request/deny-submit", manifest.Id),
		Dialog: model.Dialog{
			Title: "Deny Channel Connection Request",
			Elements: []model.DialogElement{{
				DisplayName: "Reason for denial",
				Name:        "reason",
				Type:        "textarea",
				Optional:    true,
				Placeholder: "Optional reason",
			}},
			SubmitLabel: "Deny",
			State:       channelID + "|" + teamID + "|" + ck,
		},
	})
	if appErr != nil {
		p.API.LogError("Failed to open channel deny dialog",
			"error_code", errcode.ChanRequestDenyDialogFailed,
			"channel_id", channelID, "conn_key", ck, "error", appErr.Error())
		writePostActionResponse(w, "Failed to open dialog.")
		return
	}

	writePostActionResponse(w, "")
}

func (p *Plugin) handleChannelRequestDenySubmit(w http.ResponseWriter, r *http.Request) {
	var req model.SubmitDialogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	user, appErr := p.getUser(req.UserId)
	if appErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if req.Cancelled {
		w.WriteHeader(http.StatusOK)
		return
	}

	parts := strings.SplitN(req.State, "|", 3)
	if len(parts) != 3 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	channelID, teamID, ck := parts[0], parts[1], parts[2]

	if !p.isTeamAdminDirect(user.Id, teamID) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	connReq, err := p.kvstore.GetChannelConnectionRequest(channelID, ck)
	if err != nil {
		p.API.LogError("Failed to get channel connection request for deny submit",
			"error_code", errcode.ChanRequestDenyGetFailed,
			"channel_id", channelID, "conn_key", ck, "error", err.Error())
		w.WriteHeader(http.StatusOK)
		return
	}
	if connReq == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	reason := parseDenyReason(req.Submission)

	if err := p.kvstore.DeleteChannelConnectionRequest(channelID, ck); err != nil {
		p.API.LogError("Failed to delete denied channel connection request",
			"error_code", errcode.ChanRequestDenyDeleteFailed,
			"channel_id", channelID, "conn_key", ck, "error", err.Error())
	}

	postMsg, notifyMsg := buildDenyMessages(user.Username, ck, reason)

	p.updateRequestPosts(connReq.PostIDs, postMsg)
	p.notifyRequester(connReq.RequesterID, notifyMsg)

	w.WriteHeader(http.StatusOK)
}

// cancelPendingChannelConnectionRequest cancels any pending channel link
// request for the given channel+connection. Called after a successful teardown
// or direct link.
func (p *Plugin) cancelPendingChannelConnectionRequest(channelID, ck string) {
	connReq, err := p.kvstore.GetChannelConnectionRequest(channelID, ck)
	if err != nil {
		p.API.LogWarn("Failed to get channel connection request for cancellation",
			"error_code", errcode.ChanRequestGetFailed,
			"channel_id", channelID, "conn_key", ck, "error", err.Error())
		return
	}
	if connReq == nil {
		return
	}
	if err := p.kvstore.DeleteChannelConnectionRequest(channelID, ck); err != nil {
		p.API.LogWarn("Failed to delete channel connection request during cancellation",
			"error_code", errcode.ChanRequestDenyDeleteFailed,
			"channel_id", channelID, "conn_key", ck, "error", err.Error())
	}
	p.updateRequestPosts(connReq.PostIDs, "| **Status** | :warning: Cancelled (connection was unlinked) |")
	p.notifyRequester(connReq.RequesterID, fmt.Sprintf("Your pending request to link connection `%s` to a channel was cancelled because the connection was unlinked.", ck))
}
