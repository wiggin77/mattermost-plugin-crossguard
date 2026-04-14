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

// isTeamAdminInRequestMode returns true if the user is a team admin (not a
// sysadmin) and the plugin is configured for request mode. System admins
// always have direct permission so this returns false for them.
func (p *Plugin) isTeamAdminInRequestMode(userID, teamID string) bool {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return false
	}

	if user.IsSystemAdmin() {
		return false
	}

	if !p.getConfiguration().isRequestMode() {
		return false
	}

	member, appErr := p.API.GetTeamMember(teamID, userID)
	if appErr != nil {
		return false
	}

	return member.SchemeAdmin
}

// createConnectionRequest creates a pending link request and DMs all system
// admins with interactive Approve/Deny buttons. Returns a user-facing message.
func (p *Plugin) createConnectionRequest(user *model.User, teamID string, conn store.TeamConnection) (string, error) {
	ck := connKey(conn)

	existing, err := p.kvstore.GetConnectionRequest(teamID, ck)
	if err != nil {
		p.API.LogError("Failed to get connection request",
			"error_code", errcode.RequestGetFailed,
			"team_id", teamID, "conn_key", ck, "error", err.Error())
		return "", fmt.Errorf("failed to check existing requests")
	}
	if existing != nil {
		return "", fmt.Errorf("a request is already pending for this connection")
	}

	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		return "", fmt.Errorf("team not found")
	}

	admins, err := p.getSystemAdmins()
	if err != nil {
		return "", fmt.Errorf("failed to look up system admins")
	}
	if len(admins) == 0 {
		p.API.LogWarn("No system admins available to review connection request",
			"error_code", errcode.RequestNoSystemAdmins,
			"team_id", teamID, "conn_key", ck)
		return "", fmt.Errorf("no system admins available to review your request")
	}

	userLabel := "@" + user.Username
	if fullName := strings.TrimSpace(user.FirstName + " " + user.LastName); fullName != "" {
		userLabel += " (" + fullName + ")"
	}

	teamLink := teamTownSquareLink(team)
	message := buildRequestDMMessage(userLabel, teamLink, ck, p.getConnectionMap(), "")

	var postIDs []string
	for _, admin := range admins {
		dmChannel, appErr := p.API.GetDirectChannel(p.botUserID, admin.Id)
		if appErr != nil {
			p.API.LogError("Failed to get DM channel for connection request",
				"error_code", errcode.RequestGetDMChannelFailed,
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
							URL: fmt.Sprintf("/plugins/%s/api/v1/request/approve", manifest.Id),
							Context: map[string]any{
								"team_id":  teamID,
								"conn_key": ck,
							},
						},
					},
					{
						Id:    "deny",
						Name:  "Deny",
						Style: "danger",
						Type:  model.PostActionTypeButton,
						Integration: &model.PostActionIntegration{
							URL: fmt.Sprintf("/plugins/%s/api/v1/request/deny", manifest.Id),
							Context: map[string]any{
								"team_id":  teamID,
								"conn_key": ck,
							},
						},
					},
				},
			},
		})

		created, appErr := p.API.CreatePost(post)
		if appErr != nil {
			p.API.LogError("Failed to create DM post for connection request",
				"error_code", errcode.RequestCreateDMPostFailed,
				"admin_id", admin.Id, "error", appErr.Error())
			continue
		}
		postIDs = append(postIDs, created.Id)
	}

	if len(postIDs) == 0 {
		p.API.LogError("Failed to notify any system admin about connection request",
			"error_code", errcode.RequestNoAdminsNotified,
			"team_id", teamID, "conn_key", ck)
		return "", fmt.Errorf("failed to notify any system admin about your request")
	}

	req := &store.ConnectionRequest{
		RequesterID: user.Id,
		TeamID:      teamID,
		ConnKey:     ck,
		PostIDs:     postIDs,
		CreatedAt:   time.Now().UnixMilli(),
	}

	saved, err := p.kvstore.CreateConnectionRequest(teamID, ck, req)
	if err != nil {
		p.API.LogError("Failed to save connection request",
			"error_code", errcode.RequestSaveFailed,
			"team_id", teamID, "conn_key", ck, "error", err.Error())
		for _, pid := range postIDs {
			_ = p.API.DeletePost(pid)
		}
		return "", fmt.Errorf("failed to save connection request")
	}
	if !saved {
		for _, pid := range postIDs {
			_ = p.API.DeletePost(pid)
		}
		return "", fmt.Errorf("a request is already pending for this connection")
	}

	// Send an informational DM to the requester with the full request details.
	confirmMsg := buildRequestConfirmationMessage(teamLink, ck, p.getConnectionMap(), "", "system admins")
	if dmChannel, appErr := p.API.GetDirectChannel(p.botUserID, user.Id); appErr != nil {
		p.API.LogWarn("Failed to get DM channel for requester confirmation",
			"error_code", errcode.RequestConfirmDMFailed,
			"user_id", user.Id, "error", appErr.Error())
	} else if _, appErr := p.API.CreatePost(&model.Post{
		UserId:    p.botUserID,
		ChannelId: dmChannel.Id,
		Message:   confirmMsg,
	}); appErr != nil {
		p.API.LogWarn("Failed to send requester confirmation DM",
			"error_code", errcode.RequestConfirmDMFailed,
			"user_id", user.Id, "error", appErr.Error())
	}

	return fmt.Sprintf("Your request to link connection `%s` for team %s has been submitted for system admin approval.", ck, teamLink), nil
}

func (p *Plugin) handleRequestApprove(w http.ResponseWriter, r *http.Request) {
	var req model.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePostActionResponse(w, "Invalid request.")
		return
	}

	teamID, _ := req.Context["team_id"].(string)
	ck, _ := req.Context["conn_key"].(string)
	if teamID == "" || ck == "" {
		writePostActionResponse(w, "Missing context.")
		return
	}

	user, appErr := p.API.GetUser(req.UserId)
	if appErr != nil {
		writePostActionResponse(w, "Failed to look up user.")
		return
	}

	if !user.IsSystemAdmin() {
		writePostActionResponse(w, "Only system admins can approve connection requests.")
		return
	}

	connReq, err := p.kvstore.GetConnectionRequest(teamID, ck)
	if err != nil {
		p.API.LogError("Failed to get connection request for approve",
			"error_code", errcode.RequestApproveGetFailed,
			"team_id", teamID, "conn_key", ck, "error", err.Error())
		writePostActionResponse(w, "Failed to check request status.")
		return
	}
	if connReq == nil {
		writePostActionResponse(w, "This request is no longer active.")
		return
	}

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
		p.API.LogWarn("Connection removed from config while request was pending",
			"error_code", errcode.RequestConnRemovedFromConfig,
			"team_id", teamID, "conn_key", ck)
		if delErr := p.kvstore.DeleteConnectionRequest(teamID, ck); delErr != nil {
			p.API.LogWarn("Failed to delete connection request for removed connection",
				"error_code", errcode.RequestApproveDeleteFailed,
				"team_id", teamID, "conn_key", ck, "error", delErr.Error())
		}
		p.updateRequestPosts(connReq.PostIDs, "| **Status** | :warning: Cancelled (connection removed from configuration) |")
		p.notifyRequester(connReq.RequesterID, fmt.Sprintf("Your request to link connection `%s` was cancelled because the connection no longer exists in the configuration.", ck))
		writePostActionResponse(w, "")
		return
	}

	if _, _, svcErr := p.initTeamForCrossGuard(user, teamID, conn); svcErr != nil {
		p.API.LogError("Failed to execute approved connection request",
			"error_code", errcode.RequestApproveExecFailed,
			"team_id", teamID, "conn_key", ck, "error", svcErr.Message)
		writePostActionResponse(w, fmt.Sprintf("Failed to link connection: %s", svcErr.Message))
		return
	}

	if err := p.kvstore.DeleteConnectionRequest(teamID, ck); err != nil {
		p.API.LogError("Failed to delete approved connection request",
			"error_code", errcode.RequestApproveDeleteFailed,
			"team_id", teamID, "conn_key", ck, "error", err.Error())
	}

	p.updateRequestPosts(connReq.PostIDs, fmt.Sprintf(
		"| **Status** | :white_check_mark: Approved |\n"+
			"| **Approved by** | @%s |", user.Username))
	p.notifyRequester(connReq.RequesterID, fmt.Sprintf("Your request to link connection `%s` has been **approved** by @%s.", ck, user.Username))

	writePostActionResponse(w, "")
}

func (p *Plugin) handleRequestDeny(w http.ResponseWriter, r *http.Request) {
	var req model.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePostActionResponse(w, "Invalid request.")
		return
	}

	teamID, _ := req.Context["team_id"].(string)
	ck, _ := req.Context["conn_key"].(string)
	if teamID == "" || ck == "" {
		writePostActionResponse(w, "Missing context.")
		return
	}

	user, appErr := p.API.GetUser(req.UserId)
	if appErr != nil {
		writePostActionResponse(w, "Failed to look up user.")
		return
	}

	if !user.IsSystemAdmin() {
		writePostActionResponse(w, "Only system admins can deny connection requests.")
		return
	}

	connReq, err := p.kvstore.GetConnectionRequest(teamID, ck)
	if err != nil {
		p.API.LogError("Failed to get connection request for deny",
			"error_code", errcode.RequestDenyGetFailed,
			"team_id", teamID, "conn_key", ck, "error", err.Error())
		writePostActionResponse(w, "Failed to check request status.")
		return
	}
	if connReq == nil {
		writePostActionResponse(w, "This request is no longer active.")
		return
	}

	appErr = p.API.OpenInteractiveDialog(model.OpenDialogRequest{
		TriggerId: req.TriggerId,
		URL:       fmt.Sprintf("/plugins/%s/api/v1/request/deny-submit", manifest.Id),
		Dialog: model.Dialog{
			Title: "Deny Connection Request",
			Elements: []model.DialogElement{{
				DisplayName: "Reason for denial",
				Name:        "reason",
				Type:        "textarea",
				Optional:    true,
				Placeholder: "Optional reason",
			}},
			SubmitLabel: "Deny",
			State:       teamID + "|" + ck,
		},
	})
	if appErr != nil {
		p.API.LogError("Failed to open deny dialog",
			"error_code", errcode.RequestDenyDialogFailed,
			"team_id", teamID, "conn_key", ck, "error", appErr.Error())
		writePostActionResponse(w, "Failed to open dialog.")
		return
	}

	writePostActionResponse(w, "")
}

func (p *Plugin) handleRequestDenySubmit(w http.ResponseWriter, r *http.Request) {
	var req model.SubmitDialogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	user, appErr := p.API.GetUser(req.UserId)
	if appErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if !user.IsSystemAdmin() {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	if req.Cancelled {
		w.WriteHeader(http.StatusOK)
		return
	}

	parts := strings.SplitN(req.State, "|", 2)
	if len(parts) != 2 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	teamID, ck := parts[0], parts[1]

	connReq, err := p.kvstore.GetConnectionRequest(teamID, ck)
	if err != nil {
		p.API.LogError("Failed to get connection request for deny submit",
			"error_code", errcode.RequestDenyGetFailed,
			"team_id", teamID, "conn_key", ck, "error", err.Error())
		w.WriteHeader(http.StatusOK)
		return
	}
	if connReq == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	reason := parseDenyReason(req.Submission)

	if err := p.kvstore.DeleteConnectionRequest(teamID, ck); err != nil {
		p.API.LogError("Failed to delete denied connection request",
			"error_code", errcode.RequestDenyDeleteFailed,
			"team_id", teamID, "conn_key", ck, "error", err.Error())
	}

	postMsg, notifyMsg := buildDenyMessages(user.Username, ck, reason)

	p.updateRequestPosts(connReq.PostIDs, postMsg)
	p.notifyRequester(connReq.RequesterID, notifyMsg)

	w.WriteHeader(http.StatusOK)
}

// cancelPendingConnectionRequest cancels any pending link request for the
// given team+connection. Called after a successful teardown.
func (p *Plugin) cancelPendingConnectionRequest(teamID, ck string) {
	connReq, err := p.kvstore.GetConnectionRequest(teamID, ck)
	if err != nil {
		p.API.LogWarn("Failed to get connection request for cancellation",
			"error_code", errcode.RequestGetFailed,
			"team_id", teamID, "conn_key", ck, "error", err.Error())
		return
	}
	if connReq == nil {
		return
	}
	if err := p.kvstore.DeleteConnectionRequest(teamID, ck); err != nil {
		p.API.LogWarn("Failed to delete connection request during cancellation",
			"error_code", errcode.RequestDenyDeleteFailed,
			"team_id", teamID, "conn_key", ck, "error", err.Error())
	}
	p.updateRequestPosts(connReq.PostIDs, "| **Status** | :warning: Cancelled (connection was unlinked) |")
	p.notifyRequester(connReq.RequesterID, fmt.Sprintf("Your pending request to link connection `%s` was cancelled because the connection was unlinked.", ck))
}

// getSystemAdmins returns all system admin users by paginating through the API.
func (p *Plugin) getSystemAdmins() ([]*model.User, error) {
	var admins []*model.User
	page := 0
	for {
		users, appErr := p.API.GetUsers(&model.UserGetOptions{
			Role:    model.SystemAdminRoleId,
			Page:    page,
			PerPage: 100,
		})
		if appErr != nil {
			return nil, fmt.Errorf("failed to get system admins: %s", appErr.Error())
		}
		if len(users) == 0 {
			break
		}
		admins = append(admins, users...)
		page++
	}
	return admins, nil
}

const maxDenyReasonLen = 1000

// parseDenyReason extracts and truncates the deny reason from a dialog submission.
func parseDenyReason(submission map[string]any) string {
	reason := ""
	if v, ok := submission["reason"].(string); ok {
		reason = strings.TrimSpace(v)
		if len(reason) > maxDenyReasonLen {
			reason = reason[:maxDenyReasonLen]
		}
	}
	return reason
}

// buildDenyMessages returns the post update message and requester notification
// for a denied connection request.
func buildDenyMessages(username, ck, reason string) (postMsg, notifyMsg string) {
	postMsg = fmt.Sprintf(
		"| **Status** | :no_entry_sign: Denied |\n"+
			"| **Denied by** | @%s |", username)
	if reason != "" {
		postMsg += fmt.Sprintf("\n| **Reason** | %s |", reason)
	}

	notifyMsg = fmt.Sprintf("Your request to link connection `%s` has been **denied** by @%s.", ck, username)
	if reason != "" {
		notifyMsg += "\n\n>**Reason:** " + reason
	}
	return postMsg, notifyMsg
}

// updateRequestPosts removes buttons from all admin DM posts and appends
// additional table rows to the original message, preserving the request
// details as audit history.
func (p *Plugin) updateRequestPosts(postIDs []string, extraRows string) {
	for _, pid := range postIDs {
		post, appErr := p.API.GetPost(pid)
		if appErr != nil {
			p.API.LogWarn("Failed to get request post for update",
				"error_code", errcode.RequestUpdatePostFailed,
				"post_id", pid, "error", appErr.Error())
			continue
		}
		post.Message = strings.TrimRight(post.Message, "\n") + "\n" + extraRows
		post.AddProp("attachments", nil)
		if _, appErr := p.API.UpdatePost(post); appErr != nil {
			p.API.LogWarn("Failed to update request post",
				"error_code", errcode.RequestUpdatePostFailed,
				"post_id", pid, "error", appErr.Error())
		}
	}
}

// notifyRequester sends a DM from the bot to the requester.
func (p *Plugin) notifyRequester(requesterID, message string) {
	dmChannel, appErr := p.API.GetDirectChannel(p.botUserID, requesterID)
	if appErr != nil {
		p.API.LogError("Failed to get DM channel for requester notification",
			"error_code", errcode.RequestApproveNotifyFailed,
			"requester_id", requesterID, "error", appErr.Error())
		return
	}

	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: dmChannel.Id,
		Message:   message,
	}
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogError("Failed to notify requester",
			"error_code", errcode.RequestApproveNotifyFailed,
			"requester_id", requesterID, "error", appErr.Error())
	}
}
