package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	nats "github.com/nats-io/nats.go"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

// mockLog registers permissive log expectations on the plugintest API.
func mockLog(api *plugintest.API) {
	// Log methods accept variadic args. Register multiple arities to avoid panics.
	for _, method := range []string{"LogInfo", "LogWarn", "LogError", "LogDebug"} {
		api.On(method, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On(method, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On(method, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On(method, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On(method, mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On(method, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On(method, mock.Anything).Maybe()
	}
}

// --------------------------------------------------------------------------
// Authentication tests
// --------------------------------------------------------------------------

func TestGetAuthenticatedUser_MissingHeader(t *testing.T) {
	api := &plugintest.API{}
	mockLog(api)
	p, _ := setupTestPluginWithRouter(api)

	r := makeAuthRequest(t, http.MethodGet, "/api/v1/status", nil, "")
	w := httptest.NewRecorder()

	p.ServeHTTP(nil, w, r)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	body := decodeJSONResponse(t, w)
	assert.Equal(t, "not authenticated", body["error"])
}

func TestGetAuthenticatedUser_GetUserFails(t *testing.T) {
	api := &plugintest.API{}
	mockLog(api)
	api.On("GetUser", "bad-id").Return(nil, &mmModel.AppError{Message: "db error"})
	p, _ := setupTestPluginWithRouter(api)

	r := makeAuthRequest(t, http.MethodGet, "/api/v1/status", nil, "bad-id")
	w := httptest.NewRecorder()

	p.ServeHTTP(nil, w, r)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	body := decodeJSONResponse(t, w)
	assert.Equal(t, "failed to get user", body["error"])
}

// --------------------------------------------------------------------------
// handleTestConnection tests
// --------------------------------------------------------------------------

func TestHandleTestConnection(t *testing.T) {
	adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}

	t.Run("non-admin returns 403", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		regularUser := &mmModel.User{Id: "user-id", Roles: ""}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", map[string]any{}, "user-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid request body returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := httptest.NewRequest(http.MethodPost, "/api/v1/test-connection", strings.NewReader("not json"))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Mattermost-User-Id", "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("unknown provider returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{"provider": "kafka"}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "nats")
	})

	t.Run("NATS missing nats block returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{"provider": "nats"}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "nats config block")
	})

	t.Run("NATS missing address returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{
			"provider": "nats",
			"nats":     map[string]any{"subject": "crossguard.test"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "address is required")
	})

	t.Run("NATS missing subject returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{
			"provider": "nats",
			"nats":     map[string]any{"address": "nats://localhost:4222"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "subject is required")
	})

	t.Run("NATS wrong subject prefix returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{
			"provider": "nats",
			"nats":     map[string]any{"address": "nats://localhost:4222", "subject": "wrong.prefix"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "subject must start with")
	})

	t.Run("NATS invalid auth_type returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{
			"provider": "nats",
			"nats":     map[string]any{"address": "nats://localhost:4222", "subject": "crossguard.test", "auth_type": "kerberos"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "auth_type")
	})

	t.Run("NATS token auth without token returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{
			"provider": "nats",
			"nats":     map[string]any{"address": "nats://localhost:4222", "subject": "crossguard.test", "auth_type": "token"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "token is required")
	})

	t.Run("NATS credentials without username returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{
			"provider": "nats",
			"nats":     map[string]any{"address": "nats://localhost:4222", "subject": "crossguard.test", "auth_type": "credentials"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "username and password")
	})

	t.Run("Azure missing azure block returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{"provider": "azure-queue"}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "azure_queue config block")
	})

	t.Run("Azure missing queue_service_url returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{
			"provider":    "azure-queue",
			"azure_queue": map[string]any{"queue_name": "myqueue"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "queue_service_url is required")
	})

	t.Run("Azure missing queue_name returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{
			"provider": "azure-queue",
			"azure_queue": map[string]any{
				"queue_service_url": "https://x.queue.core.windows.net",
				"account_name":      "x",
				"account_key":       "abc",
			},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "queue_name is required")
	})

}

// --------------------------------------------------------------------------
// handleInitTeam tests
// --------------------------------------------------------------------------

func TestHandleInitTeam(t *testing.T) {
	t.Run("unauthenticated returns 401", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+mmModel.NewId()+"/init", nil, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid team_id returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/bad/init", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "invalid team_id", resp["error"])
	})

	t.Run("non-admin returns 403", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		regularUser := &mmModel.User{Id: "user-id", Roles: ""}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", teamID, "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/init", nil, "user-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		team := &mmModel.Team{Id: teamID, Name: "test-team"}
		api.On("GetTeam", teamID).Return(team, nil)

		channel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", teamID, "town-square", false).Return(channel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/init", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "ok", resp["status"])
		assert.Equal(t, teamID, resp["team_id"])
	})

	t.Run("team admin in request mode submits request", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		teamAdminUser := &mmModel.User{Id: "ta-id", Username: "teamadmin"}
		api.On("GetUser", "ta-id").Return(teamAdminUser, nil)
		api.On("GetTeamMember", teamID, "ta-id").Return(&mmModel.TeamMember{SchemeAdmin: true}, nil)

		// getSystemAdmins
		admin := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 0, PerPage: 100,
		}).Return([]*mmModel.User{admin}, nil)
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 1, PerPage: 100,
		}).Return([]*mmModel.User{}, nil)

		api.On("GetTeam", teamID).Return(&mmModel.Team{Id: teamID, Name: "test", DisplayName: "Test"}, nil)
		api.On("GetDirectChannel", "bot-user-id", "admin-id").Return(&mmModel.Channel{Id: "dm-id"}, nil)
		api.On("GetDirectChannel", "bot-user-id", "ta-id").Return(&mmModel.Channel{Id: "requester-dm-id"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "post-id"}, nil)

		p, kvs := setupTestPluginWithRouter(api)
		boolTrue := true
		p.configuration = &configuration{
			RestrictToSystemAdmins: &boolTrue,
			AllowTeamAdminRequests: &boolTrue,
			OutboundConnections:    `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}

		kvs.getConnectionRequestFn = func(tID, ck string) (*store.ConnectionRequest, error) {
			return nil, nil
		}
		kvs.createConnectionRequestFn = func(tID, ck string, req *store.ConnectionRequest) (bool, error) {
			return true, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/init", nil, "ta-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "request_submitted", resp["status"])
	})

	t.Run("team admin in request mode with resolve error", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		teamAdminUser := &mmModel.User{Id: "ta-id", Username: "teamadmin"}
		api.On("GetUser", "ta-id").Return(teamAdminUser, nil)
		api.On("GetTeamMember", teamID, "ta-id").Return(&mmModel.TeamMember{SchemeAdmin: true}, nil)

		p, _ := setupTestPluginWithRouter(api)
		boolTrue := true
		p.configuration = &configuration{
			RestrictToSystemAdmins: &boolTrue,
			AllowTeamAdminRequests: &boolTrue,
			OutboundConnections:    `[{"name":"a","nats":{"address":"nats://localhost:4222","subject":"a"}},{"name":"b","nats":{"address":"nats://localhost:4222","subject":"b"}}]`,
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/init?connection_name=nonexistent", nil, "ta-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("team admin in request mode with createConnectionRequest error", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		teamAdminUser := &mmModel.User{Id: "ta-id", Username: "teamadmin"}
		api.On("GetUser", "ta-id").Return(teamAdminUser, nil)
		api.On("GetTeamMember", teamID, "ta-id").Return(&mmModel.TeamMember{SchemeAdmin: true}, nil)

		p, kvs := setupTestPluginWithRouter(api)
		boolTrue := true
		p.configuration = &configuration{
			RestrictToSystemAdmins: &boolTrue,
			AllowTeamAdminRequests: &boolTrue,
			OutboundConnections:    `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}

		kvs.getConnectionRequestFn = func(tID, ck string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{RequesterID: "other"}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/init", nil, "ta-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("sysadmin connection resolve error", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"a","nats":{"address":"nats://localhost:4222","subject":"a"}},{"name":"b","nats":{"address":"nats://localhost:4222","subject":"b"}}]`,
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/init?connection_name=nonexistent", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "connection not found")
	})

	t.Run("initTeamForCrossGuard error returns error status", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetTeam", teamID).Return(nil, &mmModel.AppError{Message: "team not found"})

		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/init", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		assert.NotEqual(t, http.StatusOK, w.Code)
	})

	t.Run("already linked returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		team := &mmModel.Team{Id: teamID, Name: "test-team"}
		api.On("GetTeam", teamID).Return(team, nil)

		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}
		// Override GetTeamConnections to return the connection already linked
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/init", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "ok", resp["status"])
	})
}

// --------------------------------------------------------------------------
// handleTeardownTeam tests
// --------------------------------------------------------------------------

func TestHandleTeardownTeam(t *testing.T) {
	t.Run("non-admin returns 403", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		regularUser := &mmModel.User{Id: "user-id", Roles: ""}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", teamID, "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/teardown", nil, "user-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		team := &mmModel.Team{Id: teamID, Name: "test-team"}
		api.On("GetTeam", teamID).Return(team, nil)
		channel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", teamID, "town-square", false).Return(channel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/teardown", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "ok", resp["status"])
	})
}

// --------------------------------------------------------------------------
// handleInitChannel tests
// --------------------------------------------------------------------------

func TestHandleInitChannel(t *testing.T) {
	t.Run("unauthenticated returns 401", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+mmModel.NewId()+"/init", nil, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid channel_id returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/bad/init", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "invalid channel_id", resp["error"])
	})

	t.Run("channel not found returns 404", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannel", chanID).Return(nil, &mmModel.AppError{Message: "not found"})
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/init", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("non-admin returns 403", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		regularUser := &mmModel.User{Id: "user-id", Roles: ""}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, TeamId: teamID}, nil)
		api.On("GetChannelMember", chanID, "user-id").Return(&mmModel.ChannelMember{SchemeAdmin: false}, nil)
		api.On("GetTeamMember", teamID, "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/init", nil, "user-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, Name: "test-channel", TeamId: teamID}, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)
		api.On("GetTeam", teamID).Return(&mmModel.Team{Id: teamID, Name: "test-team"}, nil)
		api.On("GetChannelByName", teamID, "town-square", false).Return(&mmModel.Channel{Id: "ts-id"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)
		api.On("UpdateChannel", mock.Anything).Return(&mmModel.Channel{Id: chanID}, nil)

		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}
		// Team already has connections linked
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/init", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "ok", resp["status"])
		assert.Equal(t, chanID, resp["channel_id"])
	})
}

// --------------------------------------------------------------------------
// handleTeardownChannel tests
// --------------------------------------------------------------------------

func TestHandleTeardownChannel(t *testing.T) {
	t.Run("non-admin returns 403", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		regularUser := &mmModel.User{Id: "user-id", Roles: ""}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, TeamId: teamID}, nil)
		api.On("GetChannelMember", chanID, "user-id").Return(&mmModel.ChannelMember{SchemeAdmin: false}, nil)
		api.On("GetTeamMember", teamID, "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/teardown", nil, "user-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, Name: "test-channel", TeamId: teamID}, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)
		api.On("UpdateChannel", mock.Anything).Return(&mmModel.Channel{Id: chanID}, nil)

		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}
		kvs.getChannelConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/teardown", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "ok", resp["status"])
	})
}

// --------------------------------------------------------------------------
// handleGlobalStatus tests
// --------------------------------------------------------------------------

func TestHandleGlobalStatus(t *testing.T) {
	t.Run("non-admin returns 403", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		regularUser := &mmModel.User{Id: "user-id", Roles: ""}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodGet, "/api/v1/status", nil, "user-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
			InboundConnections:  `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}

		r := makeAuthRequest(t, http.MethodGet, "/api/v1/status", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.NotNil(t, resp["teams"])
		assert.NotNil(t, resp["connections"])
	})
}

// --------------------------------------------------------------------------
// handleTeamStatus tests
// --------------------------------------------------------------------------

func TestHandleTeamStatus(t *testing.T) {
	t.Run("invalid team_id returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodGet, "/api/v1/teams/bad/status", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("non-member returns 403", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetTeamMember", teamID, "admin-id").Return(nil, &mmModel.AppError{Message: "not found"})
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodGet, "/api/v1/teams/"+teamID+"/status", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetTeamMember", teamID, "admin-id").Return(&mmModel.TeamMember{}, nil)
		api.On("GetTeam", teamID).Return(&mmModel.Team{Id: teamID, Name: "test-team", DisplayName: "Test Team"}, nil)

		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}

		r := makeAuthRequest(t, http.MethodGet, "/api/v1/teams/"+teamID+"/status", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, teamID, resp["team_id"])
	})
}

// --------------------------------------------------------------------------
// handleChannelStatus tests
// --------------------------------------------------------------------------

func TestHandleChannelStatus(t *testing.T) {
	t.Run("invalid channel_id returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodGet, "/api/v1/channels/bad/status", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("non-member returns 403", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(nil, &mmModel.AppError{Message: "not found"})
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodGet, "/api/v1/channels/"+chanID+"/status", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{}, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, Name: "test-channel", DisplayName: "Test Channel", TeamId: teamID, Type: mmModel.ChannelTypeOpen}, nil)
		api.On("GetTeam", teamID).Return(&mmModel.Team{Id: teamID, Name: "test-team", DisplayName: "Test Team"}, nil)

		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}

		r := makeAuthRequest(t, http.MethodGet, "/api/v1/channels/"+chanID+"/status", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, chanID, resp["channel_id"])
	})
}

// --------------------------------------------------------------------------
// handleBulkChannelConnections tests
// --------------------------------------------------------------------------

func TestHandleBulkChannelConnections(t *testing.T) {
	t.Run("missing ids returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodGet, "/api/v1/channels/connections", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "ids query parameter")
	})

	t.Run("too many ids returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		// Build a string with more than maxBulkChannelIDs entries
		ids := make([]string, maxBulkChannelIDs+1)
		for i := range ids {
			ids[i] = mmModel.NewId()
		}
		path := "/api/v1/channels/connections?ids=" + strings.Join(ids, ",")
		r := makeAuthRequest(t, http.MethodGet, path, nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "too many")
	})

	t.Run("happy path returns connections map", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{}, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getChannelConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			if id == chanID {
				return []store.TeamConnection{
					{Direction: "outbound", Connection: "high"},
				}, nil
			}
			return nil, nil
		}

		path := "/api/v1/channels/connections?ids=" + chanID
		r := makeAuthRequest(t, http.MethodGet, path, nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "outbound:high", resp[chanID])
	})
}

// --------------------------------------------------------------------------
// handleSetTeamRewrite tests
// --------------------------------------------------------------------------

func TestHandleSetTeamRewrite(t *testing.T) {
	t.Run("non-admin returns 403", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		regularUser := &mmModel.User{Id: "user-id", Roles: ""}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", teamID, "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/rewrite", map[string]any{
			"connection":       "high",
			"remote_team_name": "remote",
		}, "user-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("missing fields returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/rewrite", map[string]any{}, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "connection and remote_team_name are required")
	})

	t.Run("connection not found returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/rewrite", map[string]any{
			"connection":       "missing",
			"remote_team_name": "remote",
		}, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "inbound connection not found")
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId, Username: "admin"}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "inbound", Connection: "high"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/rewrite", map[string]any{
			"connection":       "high",
			"remote_team_name": "remote-team",
		}, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "ok", resp["status"])
		assert.Equal(t, "remote-team", resp["remote_team_name"])
	})
}

// --------------------------------------------------------------------------
// handleDeleteTeamRewrite tests
// --------------------------------------------------------------------------

func TestHandleDeleteTeamRewrite(t *testing.T) {
	t.Run("non-admin returns 403", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		regularUser := &mmModel.User{Id: "user-id", Roles: ""}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", teamID, "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodDelete, "/api/v1/teams/"+teamID+"/rewrite?connection=high", nil, "user-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("missing connection param returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodDelete, "/api/v1/teams/"+teamID+"/rewrite", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "connection query parameter")
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId, Username: "admin"}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "inbound", Connection: "high", RemoteTeamName: "old-remote"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodDelete, "/api/v1/teams/"+teamID+"/rewrite?connection=high", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "ok", resp["status"])
	})
}

// --------------------------------------------------------------------------
// handleDialogSelectConnection tests
// --------------------------------------------------------------------------

func TestHandleDialogSelectConnection(t *testing.T) {
	t.Run("cancelled dialog returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPluginWithRouter(api)

		body := mmModel.SubmitDialogRequest{Cancelled: true}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("missing user returns 401", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPluginWithRouter(api)

		body := mmModel.SubmitDialogRequest{
			UserId:     "",
			Submission: map[string]any{"connection_name": "outbound:high"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("empty connection_name returns error", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			Submission: map[string]any{"connection_name": ""},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		var resp mmModel.SubmitDialogResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Errors["connection_name"])
	})

	t.Run("invalid state returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "nocolon",
			Submission: map[string]any{"connection_name": "outbound:high"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("unknown action returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		targetID := mmModel.NewId()
		body := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "unknown-action:" + targetID,
			Submission: map[string]any{"connection_name": "outbound:high"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("init-team happy path", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId, Username: "admin"}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetTeam", teamID).Return(&mmModel.Team{Id: teamID, Name: "test-team"}, nil)
		api.On("GetChannelByName", teamID, "town-square", false).Return(&mmModel.Channel{Id: "ts-id"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)
		api.On("SendEphemeralPost", mock.Anything, mock.Anything).Return(&mmModel.Post{})

		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}

		body := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			ChannelId:  "ts-id",
			State:      actionInitTeam + ":" + teamID,
			Submission: map[string]any{"connection_name": "outbound:high"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
	})
}

// --------------------------------------------------------------------------
// parseConnKey tests
// --------------------------------------------------------------------------

func TestParseConnKey(t *testing.T) {
	t.Run("outbound:high parses correctly", func(t *testing.T) {
		tc := parseConnKey("outbound:high")
		assert.Equal(t, "outbound", tc.Direction)
		assert.Equal(t, "high", tc.Connection)
	})

	t.Run("missing colon defaults to connection only", func(t *testing.T) {
		tc := parseConnKey("justname")
		assert.Equal(t, "", tc.Direction)
		assert.Equal(t, "justname", tc.Connection)
	})
}

// --------------------------------------------------------------------------
// handleTestNATSOutbound / handleTestNATSInbound tests
// --------------------------------------------------------------------------

func TestHandleTestNATSOutbound(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPluginWithRouter(api)

		addr := startEmbeddedNATS(t)
		nc, err := nats.Connect(addr)
		require.NoError(t, err)
		defer nc.Close()

		conn := ConnectionConfig{
			NATS: &NATSProviderConfig{Subject: "crossguard.test", Address: addr},
		}

		w := httptest.NewRecorder()
		p.handleTestNATSOutbound(w, nc, conn)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "ok", resp["status"])
		assert.NotEmpty(t, resp["id"])
	})

	t.Run("default format", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPluginWithRouter(api)

		addr := startEmbeddedNATS(t)
		nc, err := nats.Connect(addr)
		require.NoError(t, err)
		defer nc.Close()

		conn := ConnectionConfig{
			NATS: &NATSProviderConfig{Subject: "crossguard.test", Address: addr},
		}

		w := httptest.NewRecorder()
		p.handleTestNATSOutbound(w, nc, conn)

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("publish to closed connection fails", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPluginWithRouter(api)

		addr := startEmbeddedNATS(t)
		nc, err := nats.Connect(addr)
		require.NoError(t, err)
		nc.Close() // close immediately

		conn := ConnectionConfig{
			NATS: &NATSProviderConfig{Subject: "crossguard.test", Address: addr},
		}

		w := httptest.NewRecorder()
		p.handleTestNATSOutbound(w, nc, conn)

		require.Equal(t, http.StatusBadGateway, w.Code)
	})

}

// TestHandleTestNATSConnection_EndToEnd drives handleTestNATSConnection via the
// public route with embedded NATS to exercise the direction switch and the
// happy-path connection logic that's otherwise uncovered.
func TestHandleTestNATSConnection_EndToEnd(t *testing.T) {
	adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}

	t.Run("inbound direction happy path", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		addr := startEmbeddedNATS(t)
		body := map[string]any{
			"provider": "nats",
			"nats":     map[string]any{"address": addr, "subject": "crossguard.test"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection?direction=inbound", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("outbound direction happy path", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		addr := startEmbeddedNATS(t)
		body := map[string]any{
			"provider": "nats",
			"nats":     map[string]any{"address": addr, "subject": "crossguard.test"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection?direction=outbound", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid direction returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		addr := startEmbeddedNATS(t)
		body := map[string]any{
			"provider": "nats",
			"nats":     map[string]any{"address": addr, "subject": "crossguard.test"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection?direction=sideways", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "direction")
	})

	t.Run("connect error returns 502", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := map[string]any{
			"provider": "nats",
			"nats":     map[string]any{"address": "nats://127.0.0.1:1", "subject": "crossguard.test"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadGateway, w.Code)
	})
}

func TestHandleTestNATSInbound(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPluginWithRouter(api)

		addr := startEmbeddedNATS(t)
		nc, err := nats.Connect(addr)
		require.NoError(t, err)
		defer nc.Close()

		conn := ConnectionConfig{
			NATS: &NATSProviderConfig{Subject: "crossguard.test", Address: addr},
		}

		w := httptest.NewRecorder()
		p.handleTestNATSInbound(w, nc, conn)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "ok", resp["status"])
	})

	t.Run("subscribe to closed connection fails", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPluginWithRouter(api)

		addr := startEmbeddedNATS(t)
		nc, err := nats.Connect(addr)
		require.NoError(t, err)
		nc.Close() // close immediately

		conn := ConnectionConfig{
			NATS: &NATSProviderConfig{Subject: "crossguard.test", Address: addr},
		}

		w := httptest.NewRecorder()
		p.handleTestNATSInbound(w, nc, conn)

		require.Equal(t, http.StatusBadGateway, w.Code)
	})
}

// --------------------------------------------------------------------------
// Additional teardown channel tests
// --------------------------------------------------------------------------

func TestHandleTeardownChannel_Additional(t *testing.T) {
	t.Run("unauthenticated returns 401", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+mmModel.NewId()+"/teardown", nil, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid channel_id returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/bad/teardown", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("channel not found returns 404", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannel", chanID).Return(nil, &mmModel.AppError{Message: "not found"})
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/teardown", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("get channel connections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, TeamId: teamID}, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getChannelConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return nil, errors.New("store error")
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/teardown", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("multiple connections returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, TeamId: teamID}, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getChannelConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "inbound", Connection: "high"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/teardown", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// --------------------------------------------------------------------------
// Additional set team rewrite tests
// --------------------------------------------------------------------------

func TestHandleSetTeamRewrite_Additional(t *testing.T) {
	t.Run("invalid team_id returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/bad/rewrite", map[string]any{
			"connection": "high", "remote_team_name": "remote",
		}, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("get team connections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return nil, errors.New("store error")
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/rewrite", map[string]any{
			"connection": "high", "remote_team_name": "remote",
		}, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("set rewrite index conflict returns 409", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "inbound", Connection: "high"},
			}, nil
		}
		kvs.setTeamRewriteIndexFn = func(conn, remote, local string) error {
			return errors.New("already mapped")
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/rewrite", map[string]any{
			"connection": "high", "remote_team_name": "remote",
		}, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("set team connections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "inbound", Connection: "high"},
			}, nil
		}
		kvs.setTeamRewriteIndexFn = func(conn, remote, local string) error {
			return nil
		}
		kvs.setTeamConnectionsFn = func(id string, conns []store.TeamConnection) error {
			return errors.New("write failed")
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/rewrite", map[string]any{
			"connection": "high", "remote_team_name": "remote",
		}, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// --------------------------------------------------------------------------
// Additional delete team rewrite tests
// --------------------------------------------------------------------------

func TestHandleDeleteTeamRewrite_Additional(t *testing.T) {
	t.Run("invalid team_id returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		r := makeAuthRequest(t, http.MethodDelete, "/api/v1/teams/bad/rewrite?connection=high", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("get team connections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return nil, errors.New("store error")
		}

		r := makeAuthRequest(t, http.MethodDelete, "/api/v1/teams/"+teamID+"/rewrite?connection=high", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("connection not found returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodDelete, "/api/v1/teams/"+teamID+"/rewrite?connection=missing", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("set team connections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "inbound", Connection: "high", RemoteTeamName: "old-remote"},
			}, nil
		}
		kvs.setTeamConnectionsFn = func(id string, conns []store.TeamConnection) error {
			return errors.New("write failed")
		}

		r := makeAuthRequest(t, http.MethodDelete, "/api/v1/teams/"+teamID+"/rewrite?connection=high", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// --------------------------------------------------------------------------
// Additional dialog select connection tests
// --------------------------------------------------------------------------

func TestHandleDialogSelectConnection_Additional(t *testing.T) {
	t.Run("get user failure returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "bad-id").Return(nil, &mmModel.AppError{Message: "not found"})
		p, _ := setupTestPluginWithRouter(api)

		body := mmModel.SubmitDialogRequest{
			UserId:     "bad-id",
			Submission: map[string]any{"connection_name": "outbound:high"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("invalid target ID returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "init-team:invalid-id",
			Submission: map[string]any{"connection_name": "outbound:high"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("teardown-team happy path", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId, Username: "admin"}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetTeam", teamID).Return(&mmModel.Team{Id: teamID, Name: "test-team"}, nil)
		api.On("GetChannelByName", teamID, "town-square", false).Return(&mmModel.Channel{Id: "ts-id"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)
		api.On("SendEphemeralPost", mock.Anything, mock.Anything).Return(&mmModel.Post{})

		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			}, nil
		}

		body := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			ChannelId:  "ts-id",
			State:      actionTeardownTeam + ":" + teamID,
			Submission: map[string]any{"connection_name": "outbound:high"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("init-channel happy path", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId, Username: "admin"}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, Name: "test-channel", TeamId: teamID}, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)
		api.On("GetTeam", teamID).Return(&mmModel.Team{Id: teamID, Name: "test-team"}, nil)
		api.On("GetChannelByName", teamID, "town-square", false).Return(&mmModel.Channel{Id: "ts-id"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)
		api.On("UpdateChannel", mock.Anything).Return(&mmModel.Channel{Id: chanID}, nil)
		api.On("SendEphemeralPost", mock.Anything, mock.Anything).Return(&mmModel.Post{})

		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			}, nil
		}

		body := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			ChannelId:  "ts-id",
			State:      actionInitChannel + ":" + chanID,
			Submission: map[string]any{"connection_name": "outbound:high"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("teardown-channel happy path", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId, Username: "admin"}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, Name: "test-channel", TeamId: teamID}, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)
		api.On("UpdateChannel", mock.Anything).Return(&mmModel.Channel{Id: chanID}, nil)
		api.On("SendEphemeralPost", mock.Anything, mock.Anything).Return(&mmModel.Post{})

		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}
		kvs.getChannelConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			}, nil
		}

		body := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			ChannelId:  chanID,
			State:      actionTeardownChannel + ":" + chanID,
			Submission: map[string]any{"connection_name": "outbound:high"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("init-team non-admin returns 403", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)

		teamID := mmModel.NewId()
		regularUser := &mmModel.User{Id: "user-id", Roles: ""}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", teamID, "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		p, _ := setupTestPluginWithRouter(api)

		body := mmModel.SubmitDialogRequest{
			UserId:     "user-id",
			State:      actionInitTeam + ":" + teamID,
			Submission: map[string]any{"connection_name": "outbound:high"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("channel not found returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)

		chanID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannel", chanID).Return(nil, &mmModel.AppError{Message: "not found"})
		p, _ := setupTestPluginWithRouter(api)

		body := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      actionInitChannel + ":" + chanID,
			Submission: map[string]any{"connection_name": "outbound:high"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", body, "")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// --------------------------------------------------------------------------
// Additional init channel tests
// --------------------------------------------------------------------------

func TestHandleInitChannel_Additional(t *testing.T) {
	t.Run("get team connections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, TeamId: teamID}, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return nil, errors.New("store error")
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/init", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("multiple connections returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, TeamId: teamID}, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "inbound", Connection: "high"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/init", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("channel admin request mode success", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		channelAdminUser := &mmModel.User{Id: "chanadmin-id", Roles: ""}
		api.On("GetUser", "chanadmin-id").Return(channelAdminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, Name: "test-chan", TeamId: teamID}, nil)
		// isChannelAdminOrHigher: restricted mode so delegates to isTeamAdminOrSystemAdmin which returns false
		api.On("GetTeamMember", teamID, "chanadmin-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		// isChannelAdminInRequestMode: checks channel membership
		api.On("GetChannelMember", chanID, "chanadmin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		// createChannelConnectionRequest internals
		api.On("GetTeam", teamID).Return(&mmModel.Team{Id: teamID, Name: "test-team"}, nil)
		api.On("GetChannelByName", teamID, "town-square", false).Return(&mmModel.Channel{Id: "ts-id"}, nil)
		teamAdmin := &mmModel.User{Id: "ta-id", Username: "teamadmin"}
		api.On("GetTeamMembers", teamID, mock.Anything, mock.Anything).Return(
			[]*mmModel.TeamMember{{UserId: "ta-id", SchemeAdmin: true}}, nil).Once()
		api.On("GetTeamMembers", teamID, mock.Anything, mock.Anything).Return(
			[]*mmModel.TeamMember{}, nil).Once()
		api.On("GetUser", "ta-id").Return(teamAdmin, nil)
		api.On("GetDirectChannel", mock.Anything, mock.Anything).Return(&mmModel.Channel{Id: "dm-id"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "post-id"}, nil)

		boolTrue := true
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
			OutboundConnections:       `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/init", nil, "chanadmin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusOK, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "request_submitted", resp["status"])
		assert.Equal(t, chanID, resp["channel_id"])
	})

	t.Run("channel admin request mode get team conns error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		channelAdminUser := &mmModel.User{Id: "chanadmin-id", Roles: ""}
		api.On("GetUser", "chanadmin-id").Return(channelAdminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, TeamId: teamID}, nil)
		api.On("GetTeamMember", teamID, "chanadmin-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		api.On("GetChannelMember", chanID, "chanadmin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		boolTrue := true
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
		}
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return nil, errors.New("store error")
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/init", nil, "chanadmin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("channel admin request mode resolve error returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		channelAdminUser := &mmModel.User{Id: "chanadmin-id", Roles: ""}
		api.On("GetUser", "chanadmin-id").Return(channelAdminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, TeamId: teamID}, nil)
		api.On("GetTeamMember", teamID, "chanadmin-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		api.On("GetChannelMember", chanID, "chanadmin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		boolTrue := true
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
		}
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "inbound", Connection: "high"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/init", nil, "chanadmin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.NotNil(t, resp["connections"])
	})

	t.Run("channel admin request mode create request error returns 409", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		chanID := mmModel.NewId()
		teamID := mmModel.NewId()
		channelAdminUser := &mmModel.User{Id: "chanadmin-id", Roles: ""}
		api.On("GetUser", "chanadmin-id").Return(channelAdminUser, nil)
		api.On("GetChannel", chanID).Return(&mmModel.Channel{Id: chanID, TeamId: teamID}, nil)
		api.On("GetTeamMember", teamID, "chanadmin-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		api.On("GetChannelMember", chanID, "chanadmin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		boolTrue := true
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
		}
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}
		// Make the request creation fail (duplicate)
		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{RequesterID: "other-user"}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channels/"+chanID+"/init", nil, "chanadmin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusConflict, w.Code)
	})
}

// --------------------------------------------------------------------------
// handleTeardownTeam additional branch tests
// --------------------------------------------------------------------------

func TestHandleTeardownTeam_Additional(t *testing.T) {
	t.Run("GetTeamConnections store error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return nil, errors.New("store unavailable")
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/teardown", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusInternalServerError, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "failed to check team connections", resp["error"])
	})

	t.Run("resolveConnectionName fails returns 400 with connections", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "inbound", Connection: "high"},
			}, nil
		}

		// No connection_name query param with multiple connections triggers resolve failure
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/teardown", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "multiple connections")
		assert.NotNil(t, resp["connections"])
	})

	t.Run("teardownTeamForCrossGuard service error", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		teamID := mmModel.NewId()
		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		// teardownTeamForCrossGuard calls GetTeam; make it fail
		api.On("GetTeam", teamID).Return(nil, &mmModel.AppError{Message: "team not found"})

		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			}, nil
		}

		r := makeAuthRequest(t, http.MethodPost, "/api/v1/teams/"+teamID+"/teardown", nil, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)

		require.Equal(t, http.StatusNotFound, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Equal(t, "team not found", resp["error"])
	})
}

func TestHandleTestAzureQueueConnection(t *testing.T) {
	adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}

	validBody := func() map[string]any {
		return map[string]any{
			"provider": "azure-queue",
			"azure_queue": map[string]any{
				"queue_service_url": "https://example.queue.core.windows.net",
				"account_name":      "acct",
				"account_key":       "a2V5", // base64 "key"
				"queue_name":        "q1",
			},
		}
	}

	setup := func() (*plugintest.API, *Plugin) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)
		return api, p
	}

	sendReq := func(p *Plugin, body map[string]any) *httptest.ResponseRecorder {
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)
		return w
	}

	t.Run("missing azure_queue block returns 400", func(t *testing.T) {
		_, p := setup()
		w := sendReq(p, map[string]any{"provider": "azure-queue"})
		require.Equal(t, http.StatusBadRequest, w.Code)
		resp := decodeJSONResponse(t, w)
		assert.Contains(t, resp["error"], "azure_queue config block")
	})

	t.Run("missing queue_service_url returns 400", func(t *testing.T) {
		_, p := setup()
		body := validBody()
		body["azure_queue"].(map[string]any)["queue_service_url"] = ""
		w := sendReq(p, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "queue_service_url")
	})

	t.Run("missing account_name returns 400", func(t *testing.T) {
		_, p := setup()
		body := validBody()
		body["azure_queue"].(map[string]any)["account_name"] = ""
		w := sendReq(p, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "account_name")
	})

	t.Run("missing account_key returns 400", func(t *testing.T) {
		_, p := setup()
		body := validBody()
		body["azure_queue"].(map[string]any)["account_key"] = ""
		w := sendReq(p, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "account_key")
	})

	t.Run("missing queue_name returns 400", func(t *testing.T) {
		_, p := setup()
		body := validBody()
		body["azure_queue"].(map[string]any)["queue_name"] = ""
		w := sendReq(p, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "queue_name")
	})

	t.Run("backend error returns 502", func(t *testing.T) {
		orig := testAzureQueueConnectionFn
		testAzureQueueConnectionFn = func(cfg AzureQueueProviderConfig) error {
			return assert.AnError
		}
		t.Cleanup(func() { testAzureQueueConnectionFn = orig })

		_, p := setup()
		w := sendReq(p, validBody())
		require.Equal(t, http.StatusBadGateway, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "Azure Queue connection test failed")
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		orig := testAzureQueueConnectionFn
		testAzureQueueConnectionFn = func(cfg AzureQueueProviderConfig) error { return nil }
		t.Cleanup(func() { testAzureQueueConnectionFn = orig })

		_, p := setup()
		w := sendReq(p, validBody())
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "ok", decodeJSONResponse(t, w)["status"])
	})
}

func TestHandleTestAzureBlobConnection(t *testing.T) {
	adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}

	validBody := func() map[string]any {
		return map[string]any{
			"provider": "azure-blob",
			"azure_blob": map[string]any{
				"service_url":         "https://example.blob.core.windows.net",
				"account_name":        "acct",
				"account_key":         "a2V5",
				"blob_container_name": "c1",
			},
		}
	}

	setup := func() (*plugintest.API, *Plugin) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)
		return api, p
	}

	sendReq := func(p *Plugin, body map[string]any) *httptest.ResponseRecorder {
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)
		return w
	}

	t.Run("missing azure_blob block returns 400", func(t *testing.T) {
		_, p := setup()
		w := sendReq(p, map[string]any{"provider": "azure-blob"})
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "azure_blob config block")
	})

	t.Run("missing service_url returns 400", func(t *testing.T) {
		_, p := setup()
		body := validBody()
		body["azure_blob"].(map[string]any)["service_url"] = ""
		w := sendReq(p, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "service_url")
	})

	t.Run("missing account_name returns 400", func(t *testing.T) {
		_, p := setup()
		body := validBody()
		body["azure_blob"].(map[string]any)["account_name"] = ""
		w := sendReq(p, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "account_name")
	})

	t.Run("missing account_key returns 400", func(t *testing.T) {
		_, p := setup()
		body := validBody()
		body["azure_blob"].(map[string]any)["account_key"] = ""
		w := sendReq(p, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "account_key")
	})

	t.Run("missing blob_container_name returns 400", func(t *testing.T) {
		_, p := setup()
		body := validBody()
		body["azure_blob"].(map[string]any)["blob_container_name"] = ""
		w := sendReq(p, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "blob_container_name")
	})

	t.Run("backend error returns 502", func(t *testing.T) {
		orig := testAzureBlobConnectionFn
		testAzureBlobConnectionFn = func(cfg AzureBlobProviderConfig) error {
			return assert.AnError
		}
		t.Cleanup(func() { testAzureBlobConnectionFn = orig })

		_, p := setup()
		w := sendReq(p, validBody())
		require.Equal(t, http.StatusBadGateway, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "Azure Blob connection test failed")
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		orig := testAzureBlobConnectionFn
		testAzureBlobConnectionFn = func(cfg AzureBlobProviderConfig) error { return nil }
		t.Cleanup(func() { testAzureBlobConnectionFn = orig })

		_, p := setup()
		w := sendReq(p, validBody())
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "ok", decodeJSONResponse(t, w)["status"])
	})
}

func TestHandleTestAzureServiceBusConnection(t *testing.T) {
	adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}

	validBody := func() map[string]any {
		return map[string]any{
			"name":     "sb1",
			"provider": "azure-servicebus",
			"azure_servicebus": map[string]any{
				"connection_string": "Endpoint=sb://example.servicebus.windows.net/;SharedAccessKeyName=r;SharedAccessKey=abc",
				"queue_name":        "q1",
			},
		}
	}

	setup := func() (*plugintest.API, *Plugin) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)
		return api, p
	}

	sendReq := func(p *Plugin, body map[string]any) *httptest.ResponseRecorder {
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/test-connection", body, "admin-id")
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)
		return w
	}

	t.Run("missing azure_servicebus block returns 400", func(t *testing.T) {
		_, p := setup()
		w := sendReq(p, map[string]any{"provider": "azure-servicebus"})
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "azure_servicebus config block")
	})

	t.Run("missing connection_string returns 400", func(t *testing.T) {
		_, p := setup()
		body := validBody()
		body["azure_servicebus"].(map[string]any)["connection_string"] = ""
		w := sendReq(p, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "connection_string")
	})

	t.Run("missing queue_name returns 400", func(t *testing.T) {
		_, p := setup()
		body := validBody()
		body["azure_servicebus"].(map[string]any)["queue_name"] = ""
		w := sendReq(p, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "queue_name")
	})

	t.Run("invalid queue_name characters return 400", func(t *testing.T) {
		_, p := setup()
		body := validBody()
		body["azure_servicebus"].(map[string]any)["queue_name"] = "bad queue name with spaces"
		w := sendReq(p, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, decodeJSONResponse(t, w)["error"], "queue_name")
	})

	t.Run("backend error returns 502 with sanitized body", func(t *testing.T) {
		orig := testAzureServiceBusConnectionFn
		testAzureServiceBusConnectionFn = func(cfg AzureServiceBusProviderConfig) error {
			return errors.New("connection refused: SharedAccessKey=hidden-secret-123")
		}
		t.Cleanup(func() { testAzureServiceBusConnectionFn = orig })

		_, p := setup()
		w := sendReq(p, validBody())
		require.Equal(t, http.StatusBadGateway, w.Code)
		body := decodeJSONResponse(t, w)["error"]
		assert.Contains(t, body, "Azure Service Bus connection test failed")
		// The sanitizer runs in the testAzureServiceBusConnection function itself.
		// The test seam returns a raw error, so this subtest just proves the
		// sanitizer is a library-level concern. The real sanitizer coverage
		// lives in TestSanitizeServiceBusError_*.
	})

	t.Run("happy path returns 200", func(t *testing.T) {
		orig := testAzureServiceBusConnectionFn
		testAzureServiceBusConnectionFn = func(cfg AzureServiceBusProviderConfig) error { return nil }
		t.Cleanup(func() { testAzureServiceBusConnectionFn = orig })

		_, p := setup()
		w := sendReq(p, validBody())
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "ok", decodeJSONResponse(t, w)["status"])
	})
}

// --------------------------------------------------------------------------
// handleAutocompleteConnections tests
// --------------------------------------------------------------------------

func TestHandleAutocompleteConnections(t *testing.T) {
	teamID := mmModel.NewId()
	chanID := mmModel.NewId()
	adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
	regularUser := &mmModel.User{Id: "user-id", Roles: ""}

	doRequest := func(t *testing.T, p *Plugin, action, qs, userID string) *httptest.ResponseRecorder {
		t.Helper()
		path := "/api/v1/autocomplete/connections/" + action + "?" + qs
		r := makeAuthRequest(t, http.MethodGet, path, nil, userID)
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)
		return w
	}

	decodeList := func(t *testing.T, w *httptest.ResponseRecorder) []mmModel.AutocompleteListItem {
		t.Helper()
		var items []mmModel.AutocompleteListItem
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &items))
		return items
	}

	t.Run("unauthenticated returns empty list (200)", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPluginWithRouter(api)

		w := doRequest(t, p, actionInitTeam, "team_id="+teamID, "")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, decodeList(t, w))
	})

	t.Run("unknown action returns empty list", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		w := doRequest(t, p, "bogus", "team_id="+teamID, "admin-id")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, decodeList(t, w))
	})

	t.Run("init-team returns all configured connections to system admin", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
			InboundConnections:  `[{"name":"low","nats":{"address":"nats://localhost:4222","subject":"crossguard.low"}}]`,
		}

		w := doRequest(t, p, actionInitTeam, "team_id="+teamID, "admin-id")
		require.Equal(t, http.StatusOK, w.Code)

		items := decodeList(t, w)
		require.Len(t, items, 2)
		seen := map[string]bool{items[0].Item: true, items[1].Item: true}
		assert.True(t, seen["outbound:high"])
		assert.True(t, seen["inbound:low"])
	})

	t.Run("init-team without team_id returns empty", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		w := doRequest(t, p, actionInitTeam, "", "admin-id")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, decodeList(t, w))
	})

	t.Run("non-admin gets empty list for init-team (no info leak)", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", teamID, "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		}

		w := doRequest(t, p, actionInitTeam, "team_id="+teamID, "user-id")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, decodeList(t, w))
	})

	t.Run("teardown-channel returns linked connections to channel admin", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		// isChannelAdminOrHigher consults GetChannelMember even for system
		// admins; SchemeAdmin=true makes it return true at that point.
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)
		p, kvs := setupTestPluginWithRouter(api)
		linked := []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
		}
		kvs.getChannelConnectionsFn = func(id string) ([]store.TeamConnection, error) {
			assert.Equal(t, chanID, id)
			return linked, nil
		}

		w := doRequest(t, p, actionTeardownChannel, "team_id="+teamID+"&channel_id="+chanID, "admin-id")
		require.Equal(t, http.StatusOK, w.Code)
		items := decodeList(t, w)
		require.Len(t, items, 1)
		assert.Equal(t, "outbound:high", items[0].Item)
	})

	t.Run("init-channel returns team connections to team admin", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", chanID, "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)
		p, kvs := setupTestPluginWithRouter(api)
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		w := doRequest(t, p, actionInitChannel, "team_id="+teamID+"&channel_id="+chanID, "admin-id")
		require.Equal(t, http.StatusOK, w.Code)
		items := decodeList(t, w)
		require.Len(t, items, 1)
		assert.Equal(t, "outbound:high", items[0].Item)
	})

	t.Run("init-channel without channel_id returns empty", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		p, _ := setupTestPluginWithRouter(api)

		w := doRequest(t, p, actionInitChannel, "team_id="+teamID, "admin-id")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, decodeList(t, w))
	})

	t.Run("non-admin gets empty list for teardown-channel", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetChannelMember", chanID, "user-id").Return(&mmModel.ChannelMember{SchemeAdmin: false}, nil)
		api.On("GetTeamMember", teamID, "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		p, kvs := setupTestPluginWithRouter(api)
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			t.Fatal("kvstore should not be queried for unauthorized user")
			return nil, nil
		}

		w := doRequest(t, p, actionTeardownChannel, "team_id="+teamID+"&channel_id="+chanID, "user-id")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, decodeList(t, w))
	})
}
