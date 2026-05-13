package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

// defaultLogMocks registers Maybe() log mocks on the API so that tests do not
// fail when the production code logs at any level. Because testify mocks match
// by exact positional arity, we register each Log method at a range of argument
// counts so calls with any number of key/value pairs are accepted.
func defaultLogMocks(api *plugintest.API) {
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
}

func registerLogMocks(api *plugintest.API, methods ...string) {
	for _, m := range methods {
		for n := 1; n <= 32; n++ {
			args := make([]any, n)
			for i := range args {
				args[i] = mock.Anything
			}
			api.On(m, args...).Maybe()
		}
	}
}

// postActionRequest builds a PostActionIntegrationRequest body as a JSON reader.
func postActionRequest(t *testing.T, userID string, ctx map[string]any) *http.Request {
	t.Helper()
	req := mmModel.PostActionIntegrationRequest{
		UserId:  userID,
		Context: ctx,
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/prompt/accept", strings.NewReader(string(data)))
	r.Header.Set("Content-Type", "application/json")
	if userID != "" {
		r.Header.Set("Mattermost-User-Id", userID)
	}
	return r
}

// ============================================================
// TestHandleUnlinkedInbound
// ============================================================

func TestHandleUnlinkedInbound(t *testing.T) {
	t.Run("prompt already exists is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "existing-post"}, nil
		}

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		p.handleUnlinkedInbound(team, "high")

		// CreatePost should never be called when prompt already exists.
		api.AssertNotCalled(t, "CreatePost", mock.Anything)
	})

	t.Run("KV error on get prompt logs and returns", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, errors.New("kv failure")
		}

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		p.handleUnlinkedInbound(team, "high")

		api.AssertCalled(t, "LogError", "Failed to get connection prompt", "error_code", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		api.AssertNotCalled(t, "CreatePost", mock.Anything)
	})

	t.Run("town-square not found logs and returns", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		api.On("GetChannelByName", "team-id", mmModel.DefaultChannelName, false).Return(nil, &mmModel.AppError{Message: "not found"})

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		p.handleUnlinkedInbound(team, "high")

		api.AssertCalled(t, "LogError", "Failed to get town-square for prompt", "error_code", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		api.AssertNotCalled(t, "CreatePost", mock.Anything)
	})

	t.Run("happy path creates post and saves prompt", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		var capturedPrompt *store.ConnectionPrompt
		kvs.createConnectionPromptFn = func(teamID, connName string, prompt *store.ConnectionPrompt) (bool, error) {
			capturedPrompt = prompt
			return true, nil
		}

		tsChannel := &mmModel.Channel{Id: "ts-channel-id"}
		api.On("GetChannelByName", "team-id", mmModel.DefaultChannelName, false).Return(tsChannel, nil)

		var capturedPost *mmModel.Post
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			capturedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{Id: "new-post-id"}, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		p.handleUnlinkedInbound(team, "high")

		require.NotNil(t, capturedPost)
		assert.Equal(t, "ts-channel-id", capturedPost.ChannelId)
		assert.Equal(t, "bot-user-id", capturedPost.UserId)
		assert.Contains(t, capturedPost.Message, "high")
		assert.Contains(t, capturedPost.Message, "Test A")

		require.NotNil(t, capturedPrompt)
		assert.Equal(t, store.PromptStatePending, capturedPrompt.State)
		assert.Equal(t, "new-post-id", capturedPrompt.PostID)
	})

	t.Run("CreatePost failure logs and returns", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		tsChannel := &mmModel.Channel{Id: "ts-channel-id"}
		api.On("GetChannelByName", "team-id", mmModel.DefaultChannelName, false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(nil, &mmModel.AppError{Message: "create failed"})

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		p.handleUnlinkedInbound(team, "high")

		api.AssertCalled(t, "LogError", "Failed to create connection prompt post", "error_code", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("race condition deletes duplicate post", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}
		// CreateConnectionPrompt returns false (another goroutine won the race).
		kvs.createConnectionPromptFn = func(teamID, connName string, prompt *store.ConnectionPrompt) (bool, error) {
			return false, nil
		}

		tsChannel := &mmModel.Channel{Id: "ts-channel-id"}
		api.On("GetChannelByName", "team-id", mmModel.DefaultChannelName, false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "dup-post-id"}, nil)
		api.On("DeletePost", "dup-post-id").Return(nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		p.handleUnlinkedInbound(team, "high")

		api.AssertCalled(t, "DeletePost", "dup-post-id")
	})

	t.Run("CreateConnectionPrompt error deletes post and logs", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}
		kvs.createConnectionPromptFn = func(teamID, connName string, prompt *store.ConnectionPrompt) (bool, error) {
			return false, errors.New("kv write error")
		}

		tsChannel := &mmModel.Channel{Id: "ts-channel-id"}
		api.On("GetChannelByName", "team-id", mmModel.DefaultChannelName, false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "cleanup-post-id"}, nil)
		api.On("DeletePost", "cleanup-post-id").Return(nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		p.handleUnlinkedInbound(team, "high")

		api.AssertCalled(t, "LogError", "Failed to save connection prompt", "error_code", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		api.AssertCalled(t, "DeletePost", "cleanup-post-id")
	})
}

// ============================================================
// TestHandlePromptAccept
// ============================================================

func TestHandlePromptAccept(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		r := httptest.NewRequest(http.MethodPost, "/api/v1/prompt/accept", strings.NewReader("not json"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		p.handlePromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Invalid request.", resp.EphemeralText)
	})

	t.Run("missing context fields", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		r := postActionRequest(t, "user-id", map[string]any{})
		w := httptest.NewRecorder()

		p.handlePromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Missing context.", resp.EphemeralText)
	})

	t.Run("missing team_id only", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		r := postActionRequest(t, "user-id", map[string]any{
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Missing context.", resp.EphemeralText)
	})

	t.Run("non-admin returns permission error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		regularUser := &mmModel.User{Id: "regular-user", Roles: ""}
		api.On("GetUser", "regular-user").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "regular-user").Return(&mmModel.TeamMember{
			SchemeAdmin: false,
		}, nil)

		r := postActionRequest(t, "regular-user", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "You must be a team admin or system admin to accept connections.", resp.EphemeralText)
	})

	t.Run("GetConnectionPrompt error returns failure message", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, errors.New("kv read error")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Failed to check prompt status.", resp.EphemeralText)
	})

	t.Run("prompt not found returns no longer active", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "This prompt is no longer active.", resp.EphemeralText)
	})

	t.Run("prompt already blocked returns no longer active", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStateBlocked, PostID: "old-post"}, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "This prompt is no longer active.", resp.EphemeralText)
	})

	t.Run("GetUser failure returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}

		// First GetUser call (from isTeamAdminOrSystemAdmin) succeeds,
		// second GetUser call (from the handler body) fails.
		api.On("GetUser", "admin-id").Return(adminUser, nil).Once()
		api.On("GetUser", "admin-id").Return(nil, &mmModel.AppError{Message: "db error"}).Once()

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "prompt-post-id"}, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Failed to look up user.", resp.EphemeralText)
	})

	t.Run("happy path accepts and updates post", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "prompt-post-id"}, nil
		}

		// initTeamForCrossGuard needs: GetTeam, GetTeamConnections, AddTeamConnection,
		// re-read GetTeamConnections, AddInitializedTeamID, GetChannelByName, CreatePost
		team := &mmModel.Team{Id: "team-id", Name: "test-a"}
		api.On("GetTeam", "team-id").Return(team, nil)

		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", mmModel.DefaultChannelName, false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		var deletedPromptTeamID, deletedPromptConn string
		kvs.deleteConnectionPromptFn = func(teamID, connName string) error {
			deletedPromptTeamID = teamID
			deletedPromptConn = connName
			return nil
		}

		promptPost := &mmModel.Post{Id: "prompt-post-id", Message: "old message"}
		api.On("GetPost", "prompt-post-id").Return(promptPost, nil)

		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)

		assert.Equal(t, "team-id", deletedPromptTeamID)
		assert.Equal(t, "high", deletedPromptConn)

		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "accepted")
		assert.Contains(t, updatedPost.Message, "@admin")
		assert.Contains(t, updatedPost.Message, "high")
	})

	t.Run("initTeamForCrossGuard failure returns link error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "prompt-post-id"}, nil
		}

		// GetTeam fails, causing initTeamForCrossGuard to return an error.
		api.On("GetTeam", "team-id").Return(nil, &mmModel.AppError{Message: "team not found"})

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to link connection")
	})
}

// ============================================================
// TestHandlePromptBlock
// ============================================================

func TestHandlePromptBlock(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		r := httptest.NewRequest(http.MethodPost, "/api/v1/prompt/block", strings.NewReader("{bad"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		p.handlePromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Invalid request.", resp.EphemeralText)
	})

	t.Run("missing context fields", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		r := postActionRequest(t, "user-id", map[string]any{})
		w := httptest.NewRecorder()

		p.handlePromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Missing context.", resp.EphemeralText)
	})

	t.Run("non-admin returns permission error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		regularUser := &mmModel.User{Id: "regular-user", Roles: ""}
		api.On("GetUser", "regular-user").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "regular-user").Return(&mmModel.TeamMember{
			SchemeAdmin: false,
		}, nil)

		r := postActionRequest(t, "regular-user", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "You must be a team admin or system admin to block connections.", resp.EphemeralText)
	})

	t.Run("GetConnectionPrompt error returns failure message", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, errors.New("kv read error")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Failed to check prompt status.", resp.EphemeralText)
	})

	t.Run("prompt not pending returns no longer active", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStateBlocked, PostID: "old-post"}, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "This prompt is no longer active.", resp.EphemeralText)
	})

	t.Run("prompt nil returns no longer active", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "This prompt is no longer active.", resp.EphemeralText)
	})

	t.Run("SetConnectionPrompt error returns failure message", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "prompt-post-id"}, nil
		}
		kvs.setConnectionPromptFn = func(teamID, connName string, prompt *store.ConnectionPrompt) error {
			return errors.New("kv write error")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Failed to block connection.", resp.EphemeralText)
	})

	t.Run("happy path blocks and updates post", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "prompt-post-id"}, nil
		}

		var savedPrompt *store.ConnectionPrompt
		kvs.setConnectionPromptFn = func(teamID, connName string, prompt *store.ConnectionPrompt) error {
			savedPrompt = prompt
			return nil
		}

		promptPost := &mmModel.Post{Id: "prompt-post-id", Message: "old message"}
		api.On("GetPost", "prompt-post-id").Return(promptPost, nil)

		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":   "team-id",
			"conn_name": "high",
		})
		w := httptest.NewRecorder()

		p.handlePromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)

		require.NotNil(t, savedPrompt)
		assert.Equal(t, store.PromptStateBlocked, savedPrompt.State)
		assert.Equal(t, "prompt-post-id", savedPrompt.PostID)

		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "blocked")
		assert.Contains(t, updatedPost.Message, "@admin")
		assert.Contains(t, updatedPost.Message, "high")
	})
}

// ============================================================
// TestHandleUnlinkedInboundChannel
// ============================================================

func TestHandleUnlinkedInboundChannel(t *testing.T) {
	t.Run("prompt already exists is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "existing-post"}, nil
		}

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		channel := &mmModel.Channel{Id: "chan-id", DisplayName: "General", TeamId: "team-id"}
		p.handleUnlinkedInboundChannel(team, channel, "high")

		api.AssertNotCalled(t, "CreatePost", mock.Anything)
	})

	t.Run("KV error on get channel prompt logs and returns", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return nil, errors.New("kv failure")
		}

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		channel := &mmModel.Channel{Id: "chan-id", DisplayName: "General", TeamId: "team-id"}
		p.handleUnlinkedInboundChannel(team, channel, "high")

		api.AssertCalled(t, "LogError", "Failed to get channel connection prompt", "error_code", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		api.AssertNotCalled(t, "CreatePost", mock.Anything)
	})

	t.Run("happy path creates channel prompt post", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		var capturedPrompt *store.ConnectionPrompt
		kvs.createChannelConnectionPromptFn = func(channelID, connName string, prompt *store.ConnectionPrompt) (bool, error) {
			capturedPrompt = prompt
			return true, nil
		}

		var capturedPost *mmModel.Post
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			capturedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{Id: "new-chan-post-id"}, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		channel := &mmModel.Channel{Id: "chan-id", DisplayName: "General", TeamId: "team-id"}
		p.handleUnlinkedInboundChannel(team, channel, "high")

		require.NotNil(t, capturedPost)
		assert.Equal(t, "chan-id", capturedPost.ChannelId)
		assert.Equal(t, "bot-user-id", capturedPost.UserId)
		assert.Contains(t, capturedPost.Message, "high")
		assert.Contains(t, capturedPost.Message, "General")
		assert.Contains(t, capturedPost.Message, "Test A")

		require.NotNil(t, capturedPrompt)
		assert.Equal(t, store.PromptStatePending, capturedPrompt.State)
		assert.Equal(t, "new-chan-post-id", capturedPrompt.PostID)
	})

	t.Run("CreatePost failure logs and returns", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		api.On("CreatePost", mock.Anything).Return(nil, &mmModel.AppError{Message: "create failed"})

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		channel := &mmModel.Channel{Id: "chan-id", DisplayName: "General", TeamId: "team-id"}
		p.handleUnlinkedInboundChannel(team, channel, "high")

		api.AssertCalled(t, "LogError", "Failed to create channel connection prompt post", "error_code", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("race condition deletes duplicate post", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}
		kvs.createChannelConnectionPromptFn = func(channelID, connName string, prompt *store.ConnectionPrompt) (bool, error) {
			return false, nil
		}

		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "dup-chan-post-id"}, nil)
		api.On("DeletePost", "dup-chan-post-id").Return(nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		channel := &mmModel.Channel{Id: "chan-id", DisplayName: "General", TeamId: "team-id"}
		p.handleUnlinkedInboundChannel(team, channel, "high")

		api.AssertCalled(t, "DeletePost", "dup-chan-post-id")
	})

	t.Run("CreateChannelConnectionPrompt error deletes post and logs", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}
		kvs.createChannelConnectionPromptFn = func(channelID, connName string, prompt *store.ConnectionPrompt) (bool, error) {
			return false, errors.New("kv write error")
		}

		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "cleanup-chan-post-id"}, nil)
		api.On("DeletePost", "cleanup-chan-post-id").Return(nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		channel := &mmModel.Channel{Id: "chan-id", DisplayName: "General", TeamId: "team-id"}
		p.handleUnlinkedInboundChannel(team, channel, "high")

		api.AssertCalled(t, "LogError", "Failed to save channel connection prompt", "error_code", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		api.AssertCalled(t, "DeletePost", "cleanup-chan-post-id")
	})
}

// ============================================================
// TestHandleChannelPromptAccept
// ============================================================

func TestHandleChannelPromptAccept(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		r := httptest.NewRequest(http.MethodPost, "/api/v1/prompt/channel/accept", strings.NewReader("not json"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		p.handleChannelPromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Invalid request.", resp.EphemeralText)
	})

	t.Run("missing context fields", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		r := postActionRequest(t, "user-id", map[string]any{})
		w := httptest.NewRecorder()

		p.handleChannelPromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Missing context.", resp.EphemeralText)
	})

	t.Run("channel not found", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		api.On("GetChannel", "missing-chan").Return(nil, &mmModel.AppError{Message: "not found"})

		r := postActionRequest(t, "user-id", map[string]any{
			"channel_id": "missing-chan",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Channel not found.", resp.EphemeralText)
	})

	t.Run("non-admin returns permission error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		channel := &mmModel.Channel{Id: "chan-id", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		regularUser := &mmModel.User{Id: "regular-user", Roles: ""}
		api.On("GetUser", "regular-user").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "regular-user").Return(&mmModel.TeamMember{
			SchemeAdmin: false,
		}, nil)

		r := postActionRequest(t, "regular-user", map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "You must be a team admin or system admin to accept connections.", resp.EphemeralText)
	})

	t.Run("GetChannelConnectionPrompt error returns failure", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		channel := &mmModel.Channel{Id: "chan-id", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return nil, errors.New("kv read error")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Failed to check prompt status.", resp.EphemeralText)
	})

	t.Run("prompt not pending returns no longer active", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		channel := &mmModel.Channel{Id: "chan-id", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStateBlocked, PostID: "old-post"}, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "This prompt is no longer active.", resp.EphemeralText)
	})

	t.Run("prompt nil returns no longer active", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		channel := &mmModel.Channel{Id: "chan-id", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "This prompt is no longer active.", resp.EphemeralText)
	})

	t.Run("happy path accepts channel connection", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		channel := &mmModel.Channel{Id: "chan-id", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "chan-prompt-post-id"}, nil
		}

		// initChannelForCrossGuard needs: GetChannel (already mocked above for the handler),
		// GetTeamConnections, GetChannelConnections, AddChannelConnection, re-read GetChannelConnections,
		// GetChannelByName, CreatePost
		// Also may call initTeamForCrossGuard if team not linked.
		// The testKVStore defaults return inbound/outbound "high" connections, so the team check passes.

		// For the announcement post inside initChannelForCrossGuard
		api.On("GetChannelByName", "team-id", mmModel.DefaultChannelName, false).Return(&mmModel.Channel{Id: "ts-id"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		var deletedChanID, deletedConn string
		kvs.deleteChannelConnectionPromptFn = func(channelID, connName string) error {
			deletedChanID = channelID
			deletedConn = connName
			return nil
		}

		promptPost := &mmModel.Post{Id: "chan-prompt-post-id", Message: "old message"}
		api.On("GetPost", "chan-prompt-post-id").Return(promptPost, nil)

		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)

		assert.Equal(t, "chan-id", deletedChanID)
		assert.Equal(t, "high", deletedConn)

		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "accepted")
		assert.Contains(t, updatedPost.Message, "@admin")
		assert.Contains(t, updatedPost.Message, "high")
	})

	t.Run("initChannelForCrossGuard failure returns link error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		channel := &mmModel.Channel{Id: "chan-id", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "prompt-post-id"}, nil
		}

		// Make GetTeamConnections fail to cause initChannelForCrossGuard to error.
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, errors.New("kv failure")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptAccept(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to link connection")
	})
}

// ============================================================
// TestHandleChannelPromptBlock
// ============================================================

func TestHandleChannelPromptBlock(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		r := httptest.NewRequest(http.MethodPost, "/api/v1/prompt/channel/block", strings.NewReader("bad json"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		p.handleChannelPromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Invalid request.", resp.EphemeralText)
	})

	t.Run("missing context fields", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		r := postActionRequest(t, "user-id", map[string]any{})
		w := httptest.NewRecorder()

		p.handleChannelPromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Missing context.", resp.EphemeralText)
	})

	t.Run("channel not found", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		api.On("GetChannel", "missing-chan").Return(nil, &mmModel.AppError{Message: "not found"})

		r := postActionRequest(t, "user-id", map[string]any{
			"channel_id": "missing-chan",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Channel not found.", resp.EphemeralText)
	})

	t.Run("non-admin returns permission error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		channel := &mmModel.Channel{Id: "chan-id", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		regularUser := &mmModel.User{Id: "regular-user", Roles: ""}
		api.On("GetUser", "regular-user").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "regular-user").Return(&mmModel.TeamMember{
			SchemeAdmin: false,
		}, nil)

		r := postActionRequest(t, "regular-user", map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "You must be a team admin or system admin to block connections.", resp.EphemeralText)
	})

	t.Run("GetChannelConnectionPrompt error returns failure", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		channel := &mmModel.Channel{Id: "chan-id", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return nil, errors.New("kv read error")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Failed to check prompt status.", resp.EphemeralText)
	})

	t.Run("prompt not pending returns no longer active", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		channel := &mmModel.Channel{Id: "chan-id", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStateBlocked, PostID: "old-post"}, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "This prompt is no longer active.", resp.EphemeralText)
	})

	t.Run("SetChannelConnectionPrompt error returns failure", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		channel := &mmModel.Channel{Id: "chan-id", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "prompt-post-id"}, nil
		}
		kvs.setChannelConnectionPromptFn = func(channelID, connName string, prompt *store.ConnectionPrompt) error {
			return errors.New("kv write error")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Failed to block connection.", resp.EphemeralText)
	})

	t.Run("happy path blocks channel connection", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		channel := &mmModel.Channel{Id: "chan-id", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "chan-prompt-post-id"}, nil
		}

		var savedPrompt *store.ConnectionPrompt
		kvs.setChannelConnectionPromptFn = func(channelID, connName string, prompt *store.ConnectionPrompt) error {
			savedPrompt = prompt
			return nil
		}

		promptPost := &mmModel.Post{Id: "chan-prompt-post-id", Message: "old message"}
		api.On("GetPost", "chan-prompt-post-id").Return(promptPost, nil)

		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		})
		w := httptest.NewRecorder()

		p.handleChannelPromptBlock(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)

		require.NotNil(t, savedPrompt)
		assert.Equal(t, store.PromptStateBlocked, savedPrompt.State)
		assert.Equal(t, "chan-prompt-post-id", savedPrompt.PostID)

		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "blocked")
		assert.Contains(t, updatedPost.Message, "@admin")
		assert.Contains(t, updatedPost.Message, "high")
	})
}

// ============================================================
// TestUpdatePromptPost
// ============================================================

func TestUpdatePromptPost(t *testing.T) {
	t.Run("post not found logs error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		api.On("GetPost", "missing-post-id").Return(nil, &mmModel.AppError{Message: "not found"})

		updatePromptPost(p, "missing-post-id", "new message")

		api.AssertCalled(t, "LogError", "Failed to get prompt post for update", "error_code", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		api.AssertNotCalled(t, "UpdatePost", mock.Anything)
	})

	t.Run("happy path updates message and removes attachments", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		existingPost := &mmModel.Post{Id: "post-id", Message: "old message"}
		api.On("GetPost", "post-id").Return(existingPost, nil)

		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		updatePromptPost(p, "post-id", "updated message text")

		require.NotNil(t, updatedPost)
		assert.Equal(t, "updated message text", updatedPost.Message)
		// Attachments should be cleared via AddProp("attachments", nil).
		attachments := updatedPost.GetProp("attachments")
		assert.Nil(t, attachments)
	})

	t.Run("UpdatePost failure logs error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		existingPost := &mmModel.Post{Id: "post-id", Message: "old message"}
		api.On("GetPost", "post-id").Return(existingPost, nil)
		api.On("UpdatePost", mock.Anything).Return(nil, &mmModel.AppError{Message: "update failed"})

		updatePromptPost(p, "post-id", "new message")

		api.AssertCalled(t, "LogError", "Failed to update prompt post", "error_code", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})
}

// ============================================================
// TestWritePostActionResponse
// ============================================================

func TestWritePostActionResponse(t *testing.T) {
	t.Run("empty text produces response without ephemeral", func(t *testing.T) {
		w := httptest.NewRecorder()
		writePostActionResponse(w, "")

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)
		assert.Nil(t, resp.Update)
	})

	t.Run("non-empty text includes ephemeral text", func(t *testing.T) {
		w := httptest.NewRecorder()
		writePostActionResponse(w, "Something went wrong.")

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Something went wrong.", resp.EphemeralText)
		assert.Nil(t, resp.Update)
	})
}

// ---------------------------------------------------------------------------
// Additional prompt edge-case tests (new)
// ---------------------------------------------------------------------------

func TestHandlePromptAccept_AlreadyResolved(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.configuration = &configuration{}

	adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
	api.On("GetUser", "admin-id").Return(adminUser, nil)

	// Prompt is in "blocked" state, not "pending".
	kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
		return &store.ConnectionPrompt{State: store.PromptStateBlocked, PostID: "old-post"}, nil
	}

	r := postActionRequest(t, "admin-id", map[string]any{
		"team_id":   "team-id",
		"conn_name": "high",
	})
	w := httptest.NewRecorder()

	p.handlePromptAccept(w, r)

	var resp mmModel.PostActionIntegrationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "This prompt is no longer active.", resp.EphemeralText)
}

func TestHandlePromptBlock_Success(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.configuration = &configuration{}

	adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
	api.On("GetUser", "admin-id").Return(adminUser, nil)

	kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
		return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "prompt-post-id"}, nil
	}

	var savedPrompt *store.ConnectionPrompt
	kvs.setConnectionPromptFn = func(teamID, connName string, prompt *store.ConnectionPrompt) error {
		savedPrompt = prompt
		return nil
	}

	promptPost := &mmModel.Post{Id: "prompt-post-id", Message: "old message"}
	api.On("GetPost", "prompt-post-id").Return(promptPost, nil)

	var updatedPost *mmModel.Post
	api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
		updatedPost = args.Get(0).(*mmModel.Post)
	}).Return(&mmModel.Post{}, nil)

	r := postActionRequest(t, "admin-id", map[string]any{
		"team_id":   "team-id",
		"conn_name": "high",
	})
	w := httptest.NewRecorder()

	p.handlePromptBlock(w, r)

	var resp mmModel.PostActionIntegrationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.EphemeralText)

	require.NotNil(t, savedPrompt)
	assert.Equal(t, store.PromptStateBlocked, savedPrompt.State)

	require.NotNil(t, updatedPost)
	assert.Contains(t, updatedPost.Message, "blocked")
	assert.Contains(t, updatedPost.Message, "@admin")
}

func TestHandleChannelPromptAccept_Success(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.configuration = &configuration{}

	adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
	api.On("GetUser", "admin-id").Return(adminUser, nil)

	channel := &mmModel.Channel{Id: "chan-id", Name: "test-channel", TeamId: "team-id", Type: mmModel.ChannelTypeOpen}
	api.On("GetChannel", "chan-id").Return(channel, nil)

	kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
		return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "prompt-post-id"}, nil
	}

	// initChannelForCrossGuard needs team connections to contain the inbound conn.
	kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{{Direction: "inbound", Connection: "high"}}, nil
	}
	kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
		return nil, nil
	}
	kvs.addChannelConnectionFn = func(channelID string, conn store.TeamConnection) error {
		return nil
	}

	api.On("UpdateChannel", mock.Anything).Return(&mmModel.Channel{}, nil)
	api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return()

	var deletedPromptChanID, deletedPromptConn string
	kvs.deleteChannelConnectionPromptFn = func(channelID, connName string) error {
		deletedPromptChanID = channelID
		deletedPromptConn = connName
		return nil
	}

	promptPost := &mmModel.Post{Id: "prompt-post-id", Message: "old message"}
	api.On("GetPost", "prompt-post-id").Return(promptPost, nil)
	api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{}, nil)

	reqBody := mmModel.PostActionIntegrationRequest{
		UserId: "admin-id",
		Context: map[string]any{
			"channel_id": "chan-id",
			"conn_name":  "high",
		},
	}
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/prompt/channel/accept", strings.NewReader(string(data)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Mattermost-User-Id", "admin-id")
	w := httptest.NewRecorder()

	p.handleChannelPromptAccept(w, r)

	var resp mmModel.PostActionIntegrationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.EphemeralText)
	assert.Equal(t, "chan-id", deletedPromptChanID)
	assert.Equal(t, "high", deletedPromptConn)
}

func TestHandleUnlinkedInbound_PromptExists(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.configuration = &configuration{}

	kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
		return &store.ConnectionPrompt{State: store.PromptStatePending, PostID: "existing-post"}, nil
	}

	team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
	p.handleUnlinkedInbound(team, "high")

	api.AssertNotCalled(t, "CreatePost", mock.Anything)
}

func TestHandleUnlinkedInboundChannel_CreateRace(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.configuration = &configuration{}

	kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
		return nil, nil
	}
	// CreateChannelConnectionPrompt returns saved=false (another goroutine won the race).
	kvs.createChannelConnectionPromptFn = func(channelID, connName string, prompt *store.ConnectionPrompt) (bool, error) {
		return false, nil
	}

	api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "dup-post-id"}, nil)
	api.On("DeletePost", "dup-post-id").Return(nil)

	team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "test-channel", DisplayName: "Test Channel", TeamId: "team-id"}
	p.handleUnlinkedInboundChannel(team, channel, "high")

	api.AssertCalled(t, "DeletePost", "dup-post-id")
}
