package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/nats-io/nats.go"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	cgModel "github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

const maxRequestBodySize = 5 << 20 // 5 MB

// Test seams so handler-level tests can exercise every branch without hitting Azure.
var (
	testAzureQueueConnectionFn = testAzureQueueConnection
	testAzureBlobConnectionFn  = testAzureBlobConnection
)

func (p *Plugin) initAPI() {
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/test-connection", p.handleTestConnection).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/teams/{team_id}/init", p.handleInitTeam).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/teams/{team_id}/teardown", p.handleTeardownTeam).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/channels/{channel_id}/init", p.handleInitChannel).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/channels/{channel_id}/teardown", p.handleTeardownChannel).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/dialog/select-connection", p.handleDialogSelectConnection).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/prompt/accept", p.handlePromptAccept).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/prompt/block", p.handlePromptBlock).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/prompt/channel/accept", p.handleChannelPromptAccept).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/prompt/channel/block", p.handleChannelPromptBlock).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/request/approve", p.handleRequestApprove).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/request/deny", p.handleRequestDeny).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/request/deny-submit", p.handleRequestDenySubmit).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/channel-request/approve", p.handleChannelRequestApprove).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/channel-request/deny", p.handleChannelRequestDeny).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/channel-request/deny-submit", p.handleChannelRequestDenySubmit).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/status", p.handleGlobalStatus).Methods(http.MethodGet)
	router.HandleFunc("/api/v1/teams/{team_id}/status", p.handleTeamStatus).Methods(http.MethodGet)
	router.HandleFunc("/api/v1/channels/{channel_id}/status", p.handleChannelStatus).Methods(http.MethodGet)
	router.HandleFunc("/api/v1/channels/connections", p.handleBulkChannelConnections).Methods(http.MethodGet)
	router.HandleFunc("/api/v1/teams/{team_id}/rewrite", p.handleSetTeamRewrite).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/teams/{team_id}/rewrite", p.handleDeleteTeamRewrite).Methods(http.MethodDelete)
	p.router = router
}

func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

func (p *Plugin) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	if !user.IsSystemAdmin() {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var conn ConnectionConfig
	if err := json.NewDecoder(r.Body).Decode(&conn); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	direction := r.URL.Query().Get("direction")

	switch conn.Provider {
	case ProviderNATS, "":
		p.handleTestNATSConnection(w, conn, direction)
	case ProviderAzureQueue:
		p.handleTestAzureQueueConnection(w, conn, direction)
	case ProviderAzureBlob:
		p.handleTestAzureBlobConnection(w, conn, direction)
	default:
		writeJSONError(w, "provider must be \"nats\", \"azure-queue\", or \"azure-blob\"", http.StatusBadRequest)
	}
}

func (p *Plugin) handleTestNATSConnection(w http.ResponseWriter, conn ConnectionConfig, direction string) {
	if conn.NATS == nil {
		writeJSONError(w, "nats config block is required", http.StatusBadRequest)
		return
	}

	natsCfg := conn.NATS

	if strings.TrimSpace(natsCfg.Address) == "" {
		writeJSONError(w, "address is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(natsCfg.Subject) == "" {
		writeJSONError(w, "subject is required", http.StatusBadRequest)
		return
	}

	if !strings.HasPrefix(natsCfg.Subject, subjectPrefix) {
		writeJSONError(w, fmt.Sprintf("subject must start with %q", subjectPrefix), http.StatusBadRequest)
		return
	}

	switch natsCfg.AuthType {
	case AuthTypeNone, AuthTypeToken, AuthTypeCredentials, "":
		// valid
	default:
		writeJSONError(w, "auth_type must be \"none\", \"token\", or \"credentials\"", http.StatusBadRequest)
		return
	}

	if natsCfg.AuthType == AuthTypeToken && strings.TrimSpace(natsCfg.Token) == "" {
		writeJSONError(w, "token is required when auth_type is \"token\"", http.StatusBadRequest)
		return
	}

	if natsCfg.AuthType == AuthTypeCredentials {
		if strings.TrimSpace(natsCfg.Username) == "" || strings.TrimSpace(natsCfg.Password) == "" {
			writeJSONError(w, "username and password are required when auth_type is \"credentials\"", http.StatusBadRequest)
			return
		}
	}

	switch conn.MessageFormat {
	case "json", "xml", "":
		// valid
	default:
		writeJSONError(w, `message_format must be "json" or "xml"`, http.StatusBadRequest)
		return
	}

	nc, err := newNATSProviderForTest(*natsCfg)
	if err != nil {
		p.API.LogError("NATS connection test failed",
			"error_code", errcode.APINATSTestConnectFailed,
			"address", natsCfg.Address, "error", err.Error())
		writeJSONError(w, "failed to connect to NATS server", http.StatusBadGateway)
		return
	}
	defer nc.Close()

	switch direction {
	case "inbound":
		p.handleTestNATSInbound(w, nc, conn)
	case "outbound", "":
		p.handleTestNATSOutbound(w, nc, conn)
	default:
		writeJSONError(w, "direction must be \"inbound\" or \"outbound\"", http.StatusBadRequest)
	}
}

func (p *Plugin) handleTestNATSOutbound(w http.ResponseWriter, nc *nats.Conn, conn ConnectionConfig) {
	format := cgModel.Format(conn.MessageFormat)
	if format == "" {
		format = cgModel.FormatJSON
	}
	data, msgID, err := buildTestMessage(format)
	if err != nil {
		p.API.LogError("Failed to build test message",
			"error_code", errcode.APIBuildTestMessageFailed,
			"error", err.Error())
		writeJSONError(w, "failed to build test message", http.StatusInternalServerError)
		return
	}

	if err := nc.Publish(conn.NATS.Subject, data); err != nil {
		p.API.LogError("Failed to publish test message",
			"error_code", errcode.APIPublishTestMessageFailed,
			"subject", conn.NATS.Subject, "error", err.Error())
		writeJSONError(w, "failed to publish test message", http.StatusBadGateway)
		return
	}

	if err := nc.Flush(); err != nil {
		p.API.LogError("Failed to flush test message",
			"error_code", errcode.APIFlushTestMessageFailed,
			"subject", conn.NATS.Subject, "error", err.Error())
		writeJSONError(w, "failed to confirm test message delivery", http.StatusBadGateway)
		return
	}

	p.API.LogInfo("Test message sent",
		"error_code", errcode.APITestMessageSent,
		"subject", conn.NATS.Subject, "msg_id", msgID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Test message sent with ID: " + msgID,
		"id":      msgID,
	})
}

func (p *Plugin) handleTestNATSInbound(w http.ResponseWriter, nc *nats.Conn, conn ConnectionConfig) {
	sub, err := nc.SubscribeSync(conn.NATS.Subject)
	if err != nil {
		p.API.LogError("Failed to subscribe for inbound test",
			"error_code", errcode.APISubscribeInboundTestFailed,
			"subject", conn.NATS.Subject, "error", err.Error())
		writeJSONError(w, "failed to subscribe to subject", http.StatusBadGateway)
		return
	}
	defer func() { _ = sub.Unsubscribe() }()

	if err := nc.Flush(); err != nil {
		p.API.LogError("Failed to flush NATS connection",
			"error_code", errcode.APIFlushNATSConnFailed,
			"error", err.Error())
		writeJSONError(w, "failed to flush NATS connection", http.StatusBadGateway)
		return
	}

	p.API.LogInfo("Inbound test subscription successful",
		"error_code", errcode.APIInboundTestSubscribeOK,
		"subject", conn.NATS.Subject)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Connected and subscribed to subject successfully",
	})
}

func (p *Plugin) handleTestAzureQueueConnection(w http.ResponseWriter, conn ConnectionConfig, _ string) {
	if conn.AzureQueue == nil {
		writeJSONError(w, "azure_queue config block is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(conn.AzureQueue.QueueServiceURL) == "" {
		writeJSONError(w, "queue_service_url is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(conn.AzureQueue.AccountName) == "" {
		writeJSONError(w, "account_name is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(conn.AzureQueue.AccountKey) == "" {
		writeJSONError(w, "account_key is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(conn.AzureQueue.QueueName) == "" {
		writeJSONError(w, "queue_name is required", http.StatusBadRequest)
		return
	}

	if err := testAzureQueueConnectionFn(*conn.AzureQueue); err != nil {
		p.API.LogError("Azure Queue connection test failed",
			"error_code", errcode.APIAzureQueueTestFailed,
			"error", err.Error())
		writeJSONError(w, "Azure Queue connection test failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Azure Queue Storage connection test successful",
	})
}

func (p *Plugin) handleTestAzureBlobConnection(w http.ResponseWriter, conn ConnectionConfig, _ string) {
	if conn.AzureBlob == nil {
		writeJSONError(w, "azure_blob config block is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(conn.AzureBlob.ServiceURL) == "" {
		writeJSONError(w, "service_url is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(conn.AzureBlob.AccountName) == "" {
		writeJSONError(w, "account_name is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(conn.AzureBlob.AccountKey) == "" {
		writeJSONError(w, "account_key is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(conn.AzureBlob.BlobContainerName) == "" {
		writeJSONError(w, "blob_container_name is required", http.StatusBadRequest)
		return
	}

	// Share the same numeric validation as config persistence so the "test"
	// button catches out-of-range flush_interval_seconds / blob_lock_max_age_seconds
	// before we even try to connect.
	if errs := validateAzureBlobConnection(conn, "connection "+conn.Name); len(errs) > 0 {
		writeJSONError(w, strings.Join(errs, "; "), http.StatusBadRequest)
		return
	}

	if err := testAzureBlobConnectionFn(*conn.AzureBlob); err != nil {
		p.API.LogError("Azure Blob connection test failed",
			"error_code", errcode.APIAzureBlobTestFailed,
			"error", err.Error())
		writeJSONError(w, "Azure Blob connection test failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Azure Blob Storage connection test successful",
	})
}

func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// getAuthenticatedUser extracts the user ID from the request header, looks up
// the user, and returns it. Returns nil and writes an error response if auth fails.
func (p *Plugin) getAuthenticatedUser(w http.ResponseWriter, r *http.Request) *mmModel.User {
	userID := r.Header.Get("Mattermost-User-Id")
	if userID == "" {
		writeJSONError(w, "not authenticated", http.StatusUnauthorized)
		return nil
	}

	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		p.API.LogError("Failed to get user",
			"error_code", errcode.APIGetUserFailed,
			"user_id", userID, "error", appErr.Error())
		writeJSONError(w, "failed to get user", http.StatusInternalServerError)
		return nil
	}

	return user
}

func (p *Plugin) handleInitTeam(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	teamID := mux.Vars(r)["team_id"]
	if !mmModel.IsValidId(teamID) {
		writeJSONError(w, "invalid team_id", http.StatusBadRequest)
		return
	}

	if !p.isTeamAdminOrSystemAdmin(user.Id, teamID) {
		if p.isTeamAdminInRequestMode(user.Id, teamID) {
			conn, allConns, resolveErr := p.resolveConnectionName(r.URL.Query().Get("connection_name"), p.getAllConnectionNames())
			if resolveErr != "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":       resolveErr,
					"connections": connectionDisplayNames(allConns),
				})
				return
			}
			msg, err := p.createConnectionRequest(user, teamID, conn)
			if err != nil {
				writeJSONError(w, err.Error(), http.StatusConflict)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status":          "request_submitted",
				"message":         msg,
				"team_id":         teamID,
				"connection_name": connKey(conn),
			})
			return
		}
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	connName, allConns, resolveErr := p.resolveConnectionName(r.URL.Query().Get("connection_name"), p.getAllConnectionNames())
	if resolveErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":       resolveErr,
			"connections": connectionDisplayNames(allConns),
		})
		return
	}

	team, _, svcErr := p.initTeamForCrossGuard(user, teamID, connName)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":          "ok",
		"team_id":         team.Id,
		"team_name":       team.Name,
		"connection_name": connKey(connName),
	})
}

func (p *Plugin) handleInitChannel(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	channelID := mux.Vars(r)["channel_id"]
	if !mmModel.IsValidId(channelID) {
		writeJSONError(w, "invalid channel_id", http.StatusBadRequest)
		return
	}

	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		writeJSONError(w, "channel not found", http.StatusNotFound)
		return
	}

	if !p.isChannelAdminOrHigher(user.Id, channelID, channel.TeamId) {
		switch {
		case p.canDirectlyManageChannelConns(user.Id, channel.TeamId):
			// Team admins get direct channel access when channel request mode
			// is on (they are the approvers). Fall through to normal flow.
		case p.isChannelAdminInRequestMode(user.Id, channelID, channel.TeamId):
			teamConns, err := p.kvstore.GetTeamConnections(channel.TeamId)
			if err != nil {
				p.API.LogError("Failed to get team connections",
					"error_code", errcode.APIInitChannelGetTeamConns,
					"team_id", channel.TeamId, "error", err.Error())
				writeJSONError(w, "failed to check team connections", http.StatusInternalServerError)
				return
			}
			conn, _, resolveErr := p.resolveConnectionName(r.URL.Query().Get("connection_name"), teamConns)
			if resolveErr != "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":       resolveErr,
					"connections": connectionDisplayNames(teamConns),
				})
				return
			}
			msg, crErr := p.createChannelConnectionRequest(user, channelID, channel.TeamId, conn)
			if crErr != nil {
				writeJSONError(w, crErr.Error(), http.StatusConflict)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status":          "request_submitted",
				"message":         msg,
				"channel_id":      channelID,
				"connection_name": connKey(conn),
			})
			return
		default:
			writeJSONError(w, "insufficient permissions", http.StatusForbidden)
			return
		}
	}

	teamConns, err := p.kvstore.GetTeamConnections(channel.TeamId)
	if err != nil {
		p.API.LogError("Failed to get team connections",
			"error_code", errcode.APIInitChannelGetTeamConns,
			"team_id", channel.TeamId, "error", err.Error())
		writeJSONError(w, "failed to check team connections", http.StatusInternalServerError)
		return
	}

	connName, _, resolveErr := p.resolveConnectionName(r.URL.Query().Get("connection_name"), teamConns)
	if resolveErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":       resolveErr,
			"connections": connectionDisplayNames(teamConns),
		})
		return
	}

	ch, _, svcErr := p.initChannelForCrossGuard(user, channelID, connName)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":          "ok",
		"channel_id":      ch.Id,
		"channel_name":    ch.Name,
		"connection_name": connKey(connName),
	})
}

func (p *Plugin) handleTeardownChannel(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	channelID := mux.Vars(r)["channel_id"]
	if !mmModel.IsValidId(channelID) {
		writeJSONError(w, "invalid channel_id", http.StatusBadRequest)
		return
	}

	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		writeJSONError(w, "channel not found", http.StatusNotFound)
		return
	}

	if !p.isChannelAdminOrHigher(user.Id, channelID, channel.TeamId) {
		if !p.canDirectlyManageChannelConns(user.Id, channel.TeamId) &&
			!p.isChannelAdminInRequestMode(user.Id, channelID, channel.TeamId) {
			writeJSONError(w, "insufficient permissions", http.StatusForbidden)
			return
		}
	}

	chanConns, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to get channel connections",
			"error_code", errcode.APITeardownChannelGetChanConns,
			"channel_id", channelID, "error", err.Error())
		writeJSONError(w, "failed to check channel connections", http.StatusInternalServerError)
		return
	}

	connName, _, resolveErr := p.resolveConnectionName(r.URL.Query().Get("connection_name"), chanConns)
	if resolveErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":       resolveErr,
			"connections": connectionDisplayNames(chanConns),
		})
		return
	}

	ch, svcErr := p.teardownChannelForCrossGuard(user, channelID, connName)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	p.cancelPendingChannelConnectionRequest(channelID, connKey(connName))

	writeJSON(w, http.StatusOK, map[string]string{
		"status":          "ok",
		"channel_id":      ch.Id,
		"channel_name":    ch.Name,
		"connection_name": connKey(connName),
	})
}

func (p *Plugin) handleTeardownTeam(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	teamID := mux.Vars(r)["team_id"]
	if !mmModel.IsValidId(teamID) {
		writeJSONError(w, "invalid team_id", http.StatusBadRequest)
		return
	}

	if !p.isTeamAdminOrSystemAdmin(user.Id, teamID) {
		if !p.isTeamAdminInRequestMode(user.Id, teamID) {
			writeJSONError(w, "insufficient permissions", http.StatusForbidden)
			return
		}
	}

	teamConns, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to get team connections",
			"error_code", errcode.APITeardownTeamGetTeamConns,
			"team_id", teamID, "error", err.Error())
		writeJSONError(w, "failed to check team connections", http.StatusInternalServerError)
		return
	}

	connName, _, resolveErr := p.resolveConnectionName(r.URL.Query().Get("connection_name"), teamConns)
	if resolveErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":       resolveErr,
			"connections": connectionDisplayNames(teamConns),
		})
		return
	}

	team, svcErr := p.teardownTeamForCrossGuard(user, teamID, connName)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	p.cancelPendingConnectionRequest(teamID, connKey(connName))

	writeJSON(w, http.StatusOK, map[string]string{
		"status":          "ok",
		"team_id":         team.Id,
		"team_name":       team.Name,
		"connection_name": connKey(connName),
	})
}

func (p *Plugin) handleGlobalStatus(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	if !user.IsSystemAdmin() {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	resp, svcErr := p.getGlobalStatus()
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (p *Plugin) handleDialogSelectConnection(w http.ResponseWriter, r *http.Request) {
	var req mmModel.SubmitDialogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Cancelled {
		w.WriteHeader(http.StatusOK)
		return
	}

	userID := req.UserId
	if userID == "" {
		writeJSONError(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		writeJSONError(w, "failed to get user", http.StatusInternalServerError)
		return
	}

	connNameStr, _ := req.Submission["connection_name"].(string)
	if connNameStr == "" {
		writeJSON(w, http.StatusOK, mmModel.SubmitDialogResponse{
			Errors: map[string]string{"connection_name": "Please select a connection."},
		})
		return
	}

	connName := parseConnKey(connNameStr)

	parts := strings.SplitN(req.State, ":", 2)
	if len(parts) != 2 {
		writeJSONError(w, "invalid dialog state", http.StatusBadRequest)
		return
	}
	action, targetID := parts[0], parts[1]

	if !mmModel.IsValidId(targetID) {
		writeJSONError(w, "invalid target ID in dialog state", http.StatusBadRequest)
		return
	}

	var responseText string
	displayName := connKey(connName)

	switch action {
	case actionInitTeam, actionTeardownTeam:
		if !p.isTeamAdminOrSystemAdmin(userID, targetID) {
			if action == actionInitTeam && p.isTeamAdminInRequestMode(userID, targetID) {
				msg, err := p.createConnectionRequest(user, targetID, connName)
				if err != nil {
					responseText = err.Error()
				} else {
					responseText = msg
				}
				break
			}
			if action == actionTeardownTeam && p.isTeamAdminInRequestMode(userID, targetID) {
				// Team admins in request mode can unlink directly; fall through.
			} else {
				writeJSONError(w, "insufficient permissions", http.StatusForbidden)
				return
			}
		}
		if action == actionInitTeam {
			team, alreadyLinked, svcErr := p.initTeamForCrossGuard(user, targetID, connName)
			switch {
			case svcErr != nil:
				responseText = svcErr.Message
			case alreadyLinked:
				responseText = fmt.Sprintf("Connection `%s` is already linked to this team. (team ID: %s, team name: %s)", displayName, team.Id, team.Name)
			default:
				responseText = fmt.Sprintf("Connection `%s` linked to this team successfully.", displayName)
			}
		} else {
			_, svcErr := p.teardownTeamForCrossGuard(user, targetID, connName)
			if svcErr != nil {
				responseText = svcErr.Message
			} else {
				p.cancelPendingConnectionRequest(targetID, connKey(connName))
				responseText = fmt.Sprintf("Connection `%s` unlinked from this team successfully.", displayName)
			}
		}

	case actionInitChannel, actionTeardownChannel:
		channel, chErr := p.API.GetChannel(targetID)
		if chErr != nil {
			writeJSONError(w, "channel not found", http.StatusBadRequest)
			return
		}
		if !p.isChannelAdminOrHigher(userID, targetID, channel.TeamId) && !p.canDirectlyManageChannelConns(userID, channel.TeamId) {
			if action == actionInitChannel && p.isChannelAdminInRequestMode(userID, targetID, channel.TeamId) {
				msg, crErr := p.createChannelConnectionRequest(user, targetID, channel.TeamId, connName)
				if crErr != nil {
					responseText = crErr.Error()
				} else {
					responseText = msg
				}
				break
			}
			if action == actionTeardownChannel && p.isChannelAdminInRequestMode(userID, targetID, channel.TeamId) {
				// Channel admins in request mode can unlink directly; fall through.
			} else {
				writeJSONError(w, "insufficient permissions", http.StatusForbidden)
				return
			}
		}
		if action == actionInitChannel {
			ch, alreadyLinked, svcErr := p.initChannelForCrossGuard(user, targetID, connName)
			switch {
			case svcErr != nil:
				responseText = svcErr.Message
			case alreadyLinked:
				responseText = fmt.Sprintf("Connection `%s` is already linked to this channel. (channel ID: %s, channel name: %s)", displayName, ch.Id, ch.Name)
			default:
				responseText = fmt.Sprintf("Connection `%s` linked to this channel successfully.", displayName)
			}
		} else {
			_, svcErr := p.teardownChannelForCrossGuard(user, targetID, connName)
			if svcErr != nil {
				responseText = svcErr.Message
			} else {
				p.cancelPendingChannelConnectionRequest(targetID, connKey(connName))
				responseText = fmt.Sprintf("Connection `%s` unlinked from this channel successfully.", displayName)
			}
		}

	default:
		writeJSONError(w, "unknown action", http.StatusBadRequest)
		return
	}

	p.API.SendEphemeralPost(userID, &mmModel.Post{
		UserId:    p.botUserID,
		ChannelId: req.ChannelId,
		Message:   responseText,
	})

	w.WriteHeader(http.StatusOK)
}

func (p *Plugin) handleTeamStatus(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	teamID := mux.Vars(r)["team_id"]
	if !mmModel.IsValidId(teamID) {
		writeJSONError(w, "invalid team_id", http.StatusBadRequest)
		return
	}

	member, appErr := p.API.GetTeamMember(teamID, user.Id)
	if appErr != nil || member == nil || member.DeleteAt > 0 {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	resp, svcErr := p.getTeamStatus(teamID, user)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (p *Plugin) handleChannelStatus(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	channelID := mux.Vars(r)["channel_id"]
	if !mmModel.IsValidId(channelID) {
		writeJSONError(w, "invalid channel_id", http.StatusBadRequest)
		return
	}

	member, appErr := p.API.GetChannelMember(channelID, user.Id)
	if appErr != nil || member == nil {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	resp, svcErr := p.getChannelStatus(channelID, user)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

const maxBulkChannelIDs = 2048

func (p *Plugin) handleBulkChannelConnections(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	idsParam := strings.TrimSpace(r.URL.Query().Get("ids"))
	if idsParam == "" {
		writeJSONError(w, "ids query parameter is required", http.StatusBadRequest)
		return
	}

	ids := strings.Split(idsParam, ",")
	if len(ids) > maxBulkChannelIDs {
		writeJSONError(w, fmt.Sprintf("too many channel IDs (max %d)", maxBulkChannelIDs), http.StatusBadRequest)
		return
	}

	result := make(map[string]string)

	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !mmModel.IsValidId(id) {
			continue
		}

		member, appErr := p.API.GetChannelMember(id, user.Id)
		if appErr != nil || member == nil {
			continue
		}

		conns, err := p.kvstore.GetChannelConnections(id)
		if err != nil {
			p.API.LogWarn("Failed to get channel connections for bulk lookup",
				"error_code", errcode.APIBulkChannelConnectionsFailed,
				"channel_id", id, "error", err.Error())
			continue
		}

		if len(conns) > 0 {
			keys := make([]string, len(conns))
			for i, tc := range conns {
				keys[i] = connKey(tc)
			}
			result[id] = strings.Join(keys, ",")
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// parseConnKey parses a display key like "outbound:high" back into a TeamConnection.
func parseConnKey(key string) store.TeamConnection {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return store.TeamConnection{Connection: key}
	}
	return store.TeamConnection{Direction: parts[0], Connection: parts[1]}
}

func (p *Plugin) handleSetTeamRewrite(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}
	teamID := mux.Vars(r)["team_id"]
	if !mmModel.IsValidId(teamID) {
		writeJSONError(w, "invalid team_id", http.StatusBadRequest)
		return
	}
	if !p.isTeamAdminOrSystemAdmin(user.Id, teamID) {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	var req struct {
		Connection     string `json:"connection"`
		RemoteTeamName string `json:"remote_team_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Connection == "" || req.RemoteTeamName == "" {
		writeJSONError(w, "connection and remote_team_name are required", http.StatusBadRequest)
		return
	}
	conns, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		writeJSONError(w, "failed to get team connections", http.StatusInternalServerError)
		return
	}
	var target *store.TeamConnection
	for i, tc := range conns {
		if tc.Direction == "inbound" && tc.Connection == req.Connection {
			target = &conns[i]
			break
		}
	}
	if target == nil {
		writeJSONError(w, "inbound connection not found or not linked to this team", http.StatusBadRequest)
		return
	}
	if err := p.kvstore.SetTeamRewriteIndex(req.Connection, req.RemoteTeamName, teamID); err != nil {
		writeJSONError(w, err.Error(), http.StatusConflict)
		return
	}
	target.RemoteTeamName = req.RemoteTeamName
	if err := p.kvstore.SetTeamConnections(teamID, conns); err != nil {
		writeJSONError(w, "failed to update team connections", http.StatusInternalServerError)
		return
	}
	p.API.LogInfo("Team rewrite set",
		"error_code", errcode.APITeamRewriteSet,
		"team_id", teamID, "connection", req.Connection, "remote_team_name", req.RemoteTeamName, "user", user.Username)
	writeJSON(w, http.StatusOK, map[string]string{
		"status":           "ok",
		"team_id":          teamID,
		"connection":       req.Connection,
		"remote_team_name": req.RemoteTeamName,
	})
}

func (p *Plugin) handleDeleteTeamRewrite(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}
	teamID := mux.Vars(r)["team_id"]
	if !mmModel.IsValidId(teamID) {
		writeJSONError(w, "invalid team_id", http.StatusBadRequest)
		return
	}
	if !p.isTeamAdminOrSystemAdmin(user.Id, teamID) {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}
	connParam := r.URL.Query().Get("connection")
	if connParam == "" {
		writeJSONError(w, "connection query parameter is required", http.StatusBadRequest)
		return
	}
	conns, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		writeJSONError(w, "failed to get team connections", http.StatusInternalServerError)
		return
	}
	var target *store.TeamConnection
	for i, tc := range conns {
		if tc.Direction == "inbound" && tc.Connection == connParam {
			target = &conns[i]
			break
		}
	}
	if target == nil {
		writeJSONError(w, "inbound connection not found or not linked to this team", http.StatusBadRequest)
		return
	}
	oldRemote := target.RemoteTeamName
	target.RemoteTeamName = ""
	if err := p.kvstore.SetTeamConnections(teamID, conns); err != nil {
		writeJSONError(w, "failed to update team connections", http.StatusInternalServerError)
		return
	}
	if oldRemote != "" {
		_ = p.kvstore.DeleteTeamRewriteIndex(connParam, oldRemote)
	}
	p.API.LogInfo("Team rewrite cleared",
		"error_code", errcode.APITeamRewriteCleared,
		"team_id", teamID, "connection", connParam, "user", user.Username)
	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "ok",
		"team_id":    teamID,
		"connection": connParam,
	})
}
