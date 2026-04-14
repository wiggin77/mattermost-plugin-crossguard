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

// requestModeConfig returns a configuration with request mode enabled
// (RestrictToSystemAdmins=true and AllowTeamAdminRequests=true).
func requestModeConfig() *configuration {
	boolTrue := true
	return &configuration{
		RestrictToSystemAdmins: &boolTrue,
		AllowTeamAdminRequests: &boolTrue,
		OutboundConnections:    `[{"name":"my-conn","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"test"}}]`,
	}
}

// ============================================================
// TestIsTeamAdminInRequestMode
// ============================================================

func TestIsTeamAdminInRequestMode(t *testing.T) {
	t.Run("sysadmin returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		api.On("GetUser", "admin-id").Return(&mmModel.User{
			Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId,
		}, nil)

		result := p.isTeamAdminInRequestMode("admin-id", "team-id")
		assert.False(t, result)
	})

	t.Run("team admin with request mode enabled returns true", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{
			Id: "teamadmin-id", Username: "teamadmin",
		}, nil)
		api.On("GetTeamMember", "team-id", "teamadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "teamadmin-id", SchemeAdmin: true,
		}, nil)

		result := p.isTeamAdminInRequestMode("teamadmin-id", "team-id")
		assert.True(t, result)
	})

	t.Run("team admin with request mode disabled returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		boolTrue := true
		boolFalse := false
		p.configuration = &configuration{
			RestrictToSystemAdmins: &boolTrue,
			AllowTeamAdminRequests: &boolFalse,
		}

		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{
			Id: "teamadmin-id", Username: "teamadmin",
		}, nil)

		result := p.isTeamAdminInRequestMode("teamadmin-id", "team-id")
		assert.False(t, result)
	})

	t.Run("regular user (not team admin) returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		api.On("GetUser", "regular-id").Return(&mmModel.User{
			Id: "regular-id", Username: "regular",
		}, nil)
		api.On("GetTeamMember", "team-id", "regular-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "regular-id", SchemeAdmin: false,
		}, nil)

		result := p.isTeamAdminInRequestMode("regular-id", "team-id")
		assert.False(t, result)
	})

	t.Run("GetUser failure returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		api.On("GetUser", "bad-id").Return(nil, &mmModel.AppError{Message: "user not found"})

		result := p.isTeamAdminInRequestMode("bad-id", "team-id")
		assert.False(t, result)
	})

	t.Run("GetTeamMember failure returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{
			Id: "teamadmin-id", Username: "teamadmin",
		}, nil)
		api.On("GetTeamMember", "team-id", "teamadmin-id").Return(nil, &mmModel.AppError{Message: "not found"})

		result := p.isTeamAdminInRequestMode("teamadmin-id", "team-id")
		assert.False(t, result)
	})
}

// ============================================================
// TestCreateConnectionRequest
// ============================================================

func TestCreateConnectionRequest(t *testing.T) {
	t.Run("successful request creation DMs all sysadmins", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		requester := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin", FirstName: "Team", LastName: "Admin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		admin1 := &mmModel.User{Id: "admin1-id", Username: "admin1", Roles: mmModel.SystemAdminRoleId}
		admin2 := &mmModel.User{Id: "admin2-id", Username: "admin2", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 0, PerPage: 100,
		}).Return([]*mmModel.User{admin1, admin2}, nil)
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 1, PerPage: 100,
		}).Return([]*mmModel.User{}, nil)

		api.On("GetDirectChannel", "bot-user-id", "admin1-id").Return(&mmModel.Channel{Id: "dm1-id"}, nil)
		api.On("GetDirectChannel", "bot-user-id", "admin2-id").Return(&mmModel.Channel{Id: "dm2-id"}, nil)
		api.On("GetDirectChannel", "bot-user-id", "teamadmin-id").Return(&mmModel.Channel{Id: "requester-dm-id"}, nil)

		var createdPosts []*mmModel.Post
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			createdPosts = append(createdPosts, args.Get(0).(*mmModel.Post))
		}).Return(&mmModel.Post{Id: "post-id"}, nil)

		var savedReq *store.ConnectionRequest
		kvs.createConnectionRequestFn = func(teamID, connKey string, req *store.ConnectionRequest) (bool, error) {
			savedReq = req
			return true, nil
		}

		msg, err := p.createConnectionRequest(requester, "team-id", conn)
		require.NoError(t, err)
		assert.Contains(t, msg, "outbound:my-conn")
		assert.Contains(t, msg, "Test Team")

		// 2 admin DMs + 1 requester confirmation DM
		require.Len(t, createdPosts, 3)
		assert.Equal(t, "dm1-id", createdPosts[0].ChannelId)
		assert.Equal(t, "dm2-id", createdPosts[1].ChannelId)
		assert.Contains(t, createdPosts[0].Message, "#### :link: Connection Link Request")
		assert.Contains(t, createdPosts[0].Message, "| **Requested by** | @teamadmin (Team Admin) |")
		assert.Contains(t, createdPosts[0].Message, "| **Team** | [**Test Team**](/test/channels/town-square) |")
		assert.Contains(t, createdPosts[0].Message, "| **Provider** | nats |")
		assert.Contains(t, createdPosts[0].Message, "| **Direction** | outbound |")

		// Requester confirmation DM
		confirmPost := createdPosts[2]
		assert.Equal(t, "requester-dm-id", confirmPost.ChannelId)
		assert.Contains(t, confirmPost.Message, "#### :link: Connection Link Request Submitted")
		assert.Contains(t, confirmPost.Message, "| **Team** | [**Test Team**](/test/channels/town-square) |")
		assert.NotContains(t, confirmPost.Message, "Pending approval")

		require.NotNil(t, savedReq)
		assert.Equal(t, "teamadmin-id", savedReq.RequesterID)
		assert.Equal(t, "team-id", savedReq.TeamID)
		assert.Equal(t, "outbound:my-conn", savedReq.ConnKey)
		assert.Len(t, savedReq.PostIDs, 2)
	})

	t.Run("empty display name omits parenthetical", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		requester := &mmModel.User{Id: "user-id", Username: "usera"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		admin := &mmModel.User{Id: "admin1-id", Username: "admin1", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 0, PerPage: 100,
		}).Return([]*mmModel.User{admin}, nil)
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 1, PerPage: 100,
		}).Return([]*mmModel.User{}, nil)

		api.On("GetDirectChannel", "bot-user-id", "admin1-id").Return(&mmModel.Channel{Id: "dm1-id"}, nil)
		api.On("GetDirectChannel", "bot-user-id", "user-id").Return(&mmModel.Channel{Id: "requester-dm-id"}, nil)

		var createdPosts []*mmModel.Post
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			createdPosts = append(createdPosts, args.Get(0).(*mmModel.Post))
		}).Return(&mmModel.Post{Id: "post-id"}, nil)

		kvs.createConnectionRequestFn = func(teamID, connKey string, req *store.ConnectionRequest) (bool, error) {
			return true, nil
		}

		msg, err := p.createConnectionRequest(requester, "team-id", conn)
		require.NoError(t, err)
		assert.Contains(t, msg, "outbound:my-conn")

		// 1 admin DM + 1 requester confirmation DM
		require.Len(t, createdPosts, 2)
		assert.Contains(t, createdPosts[0].Message, "| **Requested by** | @usera |")
		assert.NotContains(t, createdPosts[0].Message, "( )")
		assert.NotContains(t, createdPosts[0].Message, "()")
	})

	t.Run("duplicate request rejected when already pending", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{RequesterID: "other-user"}, nil
		}

		requester := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createConnectionRequest(requester, "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already pending")
	})

	t.Run("no system admins returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 0, PerPage: 100,
		}).Return([]*mmModel.User{}, nil)

		requester := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createConnectionRequest(requester, "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no system admins")
	})

	t.Run("KV GetConnectionRequest error returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, errors.New("kv failure")
		}

		requester := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createConnectionRequest(requester, "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to check existing requests")
	})

	t.Run("GetTeam error returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		api.On("GetTeam", "team-id").Return(nil, &mmModel.AppError{Message: "team not found"})

		requester := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createConnectionRequest(requester, "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "team not found")
	})

	t.Run("GetUsers API error returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)
		api.On("GetUsers", mock.Anything).Return(nil, &mmModel.AppError{Message: "api error"})

		requester := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createConnectionRequest(requester, "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to look up system admins")
	})

	t.Run("GetDirectChannel error skips admin but continues", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		admin1 := &mmModel.User{Id: "admin1-id", Username: "admin1", Roles: mmModel.SystemAdminRoleId}
		admin2 := &mmModel.User{Id: "admin2-id", Username: "admin2", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 0, PerPage: 100,
		}).Return([]*mmModel.User{admin1, admin2}, nil)
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 1, PerPage: 100,
		}).Return([]*mmModel.User{}, nil)

		api.On("GetDirectChannel", "bot-user-id", "admin1-id").Return(nil, &mmModel.AppError{Message: "dm error"})
		api.On("GetDirectChannel", "bot-user-id", "admin2-id").Return(&mmModel.Channel{Id: "dm2-id"}, nil)
		api.On("GetDirectChannel", "bot-user-id", "teamadmin-id").Return(&mmModel.Channel{Id: "requester-dm-id"}, nil)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{Id: "post-id"}, nil)

		kvs.createConnectionRequestFn = func(teamID, connKey string, req *store.ConnectionRequest) (bool, error) {
			return true, nil
		}

		requester := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		msg, err := p.createConnectionRequest(requester, "team-id", conn)
		require.NoError(t, err)
		assert.Contains(t, msg, "submitted")
	})

	t.Run("all admin DM posts fail returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		admin := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 0, PerPage: 100,
		}).Return([]*mmModel.User{admin}, nil)
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 1, PerPage: 100,
		}).Return([]*mmModel.User{}, nil)

		api.On("GetDirectChannel", "bot-user-id", "admin-id").Return(&mmModel.Channel{Id: "dm-id"}, nil)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(nil, &mmModel.AppError{Message: "post error"})

		requester := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createConnectionRequest(requester, "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to notify any system admin")
	})

	t.Run("KV save error cleans up posts", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		admin := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 0, PerPage: 100,
		}).Return([]*mmModel.User{admin}, nil)
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 1, PerPage: 100,
		}).Return([]*mmModel.User{}, nil)

		api.On("GetDirectChannel", "bot-user-id", "admin-id").Return(&mmModel.Channel{Id: "dm-id"}, nil)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{Id: "post-id"}, nil)

		kvs.createConnectionRequestFn = func(teamID, connKey string, req *store.ConnectionRequest) (bool, error) {
			return false, errors.New("kv save error")
		}

		var deletedPostIDs []string
		api.On("DeletePost", mock.AnythingOfType("string")).Run(func(args mock.Arguments) {
			deletedPostIDs = append(deletedPostIDs, args.Get(0).(string))
		}).Return(nil)

		requester := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createConnectionRequest(requester, "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to save connection request")
		assert.Equal(t, []string{"post-id"}, deletedPostIDs)
	})

	t.Run("CAS race cleans up posts", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		admin := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 0, PerPage: 100,
		}).Return([]*mmModel.User{admin}, nil)
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 1, PerPage: 100,
		}).Return([]*mmModel.User{}, nil)

		api.On("GetDirectChannel", "bot-user-id", "admin-id").Return(&mmModel.Channel{Id: "dm-id"}, nil)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{Id: "post-id"}, nil)

		kvs.createConnectionRequestFn = func(teamID, connKey string, req *store.ConnectionRequest) (bool, error) {
			return false, nil
		}

		var deletedPostIDs []string
		api.On("DeletePost", mock.AnythingOfType("string")).Run(func(args mock.Arguments) {
			deletedPostIDs = append(deletedPostIDs, args.Get(0).(string))
		}).Return(nil)

		requester := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createConnectionRequest(requester, "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already pending")
		assert.Equal(t, []string{"post-id"}, deletedPostIDs)
	})
}

// ============================================================
// TestHandleRequestApprove
// ============================================================

func TestHandleRequestApprove(t *testing.T) {
	t.Run("sysadmin approves request", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "teamadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		var deletedRequest bool
		kvs.deleteConnectionRequestFn = func(teamID, connKey string) error {
			deletedRequest = true
			return nil
		}

		// initTeamForCrossGuard mocks
		team := &mmModel.Team{Id: "team-id", Name: "test"}
		api.On("GetTeam", "team-id").Return(team, nil)
		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", mmModel.DefaultChannelName, false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "announce-post"}, nil)

		// updateRequestPosts mocks
		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		// notifyRequester mocks
		api.On("GetDirectChannel", "bot-user-id", "teamadmin-id").Return(&mmModel.Channel{Id: "requester-dm"}, nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)

		assert.True(t, deletedRequest)
		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "| **Status** | :white_check_mark: Approved |")
		assert.Contains(t, updatedPost.Message, "| **Approved by** | @admin |")
	})

	t.Run("non-sysadmin cannot approve", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{
			Id: "teamadmin-id", Username: "teamadmin",
		}, nil)

		r := postActionRequest(t, "teamadmin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Only system admins")
	})

	t.Run("request no longer active returns message", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "no longer active")
	})

	t.Run("connection removed from config cancels request", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		// Config with no connections so the conn lookup fails.
		boolTrue := true
		p.configuration = &configuration{
			RestrictToSystemAdmins: &boolTrue,
			AllowTeamAdminRequests: &boolTrue,
			OutboundConnections:    `[]`,
		}

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "teamadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		var deletedRequest bool
		kvs.deleteConnectionRequestFn = func(teamID, connKey string) error {
			deletedRequest = true
			return nil
		}

		// updateRequestPosts mocks
		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		// notifyRequester mocks
		api.On("GetDirectChannel", "bot-user-id", "teamadmin-id").Return(&mmModel.Channel{Id: "req-dm"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "notify-post"}, nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)

		assert.True(t, deletedRequest)
		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "| **Status** | :warning: Cancelled (connection removed from configuration) |")
	})

	t.Run("invalid JSON body returns Invalid request", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		r := httptest.NewRequest(http.MethodPost, "/api/v1/request/approve", strings.NewReader("{bad"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Invalid request")
	})

	t.Run("missing context returns Missing context", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		r := postActionRequest(t, "admin-id", map[string]any{})
		r.URL.Path = "/api/v1/request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Missing context")
	})

	t.Run("GetUser error returns failure message", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetUser", "bad-id").Return(nil, &mmModel.AppError{Message: "user not found"})

		r := postActionRequest(t, "bad-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to look up user")
	})

	t.Run("KV GetConnectionRequest error returns failure", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, errors.New("kv failure")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to check request status")
	})

	t.Run("initTeamForCrossGuard failure returns error message", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "teamadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		// initTeamForCrossGuard needs GetTeam, GetChannelByName
		api.On("GetTeam", "team-id").Return(nil, &mmModel.AppError{Message: "team lookup failed"})

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to link connection")
	})

	t.Run("DeleteConnectionRequest error after approval still notifies", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "teamadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		kvs.deleteConnectionRequestFn = func(teamID, connKey string) error {
			return errors.New("delete failed")
		}

		// initTeamForCrossGuard mocks
		team := &mmModel.Team{Id: "team-id", Name: "test"}
		api.On("GetTeam", "team-id").Return(team, nil)
		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", mmModel.DefaultChannelName, false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "announce-post"}, nil)

		// updateRequestPosts + notifyRequester mocks
		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{}, nil)
		api.On("GetDirectChannel", "bot-user-id", "teamadmin-id").Return(&mmModel.Channel{Id: "req-dm"}, nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)
		api.AssertCalled(t, "UpdatePost", mock.Anything)
	})
}

// ============================================================
// TestHandleRequestDeny
// ============================================================

func TestHandleRequestDeny(t *testing.T) {
	t.Run("sysadmin clicks deny opens dialog", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "teamadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		var dialogOpened bool
		api.On("OpenInteractiveDialog", mock.Anything).Run(func(args mock.Arguments) {
			dialogOpened = true
			dialogReq := args.Get(0).(mmModel.OpenDialogRequest)
			assert.Contains(t, dialogReq.Dialog.State, "team-id|outbound:my-conn")
		}).Return(nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)
		assert.True(t, dialogOpened)
	})

	t.Run("invalid JSON body returns Invalid request", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		r := httptest.NewRequest(http.MethodPost, "/api/v1/request/deny", strings.NewReader("{bad"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Invalid request")
	})

	t.Run("missing context returns Missing context", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		r := postActionRequest(t, "admin-id", map[string]any{})
		r.URL.Path = "/api/v1/request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Missing context")
	})

	t.Run("GetUser error returns failure message", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetUser", "bad-id").Return(nil, &mmModel.AppError{Message: "user not found"})

		r := postActionRequest(t, "bad-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to look up user")
	})

	t.Run("request no longer active returns message", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "no longer active")
	})

	t.Run("KV error returns failure message", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, errors.New("kv failure")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to check request status")
	})

	t.Run("OpenInteractiveDialog error returns failure", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "teamadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		api.On("OpenInteractiveDialog", mock.Anything).Return(&mmModel.AppError{Message: "dialog error"})

		r := postActionRequest(t, "admin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to open dialog")
	})

	t.Run("non-sysadmin cannot deny", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{
			Id: "teamadmin-id", Username: "teamadmin",
		}, nil)

		r := postActionRequest(t, "teamadmin-id", map[string]any{
			"team_id":  "team-id",
			"conn_key": "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Only system admins")
	})
}

// ============================================================
// TestHandleRequestDenySubmit
// ============================================================

func TestHandleRequestDenySubmit(t *testing.T) {
	t.Run("sysadmin submits deny with reason", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "teamadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		var deletedRequest bool
		kvs.deleteConnectionRequestFn = func(teamID, connKey string) error {
			deletedRequest = true
			return nil
		}

		// updateRequestPosts mocks
		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		// notifyRequester mocks
		api.On("GetDirectChannel", "bot-user-id", "teamadmin-id").Return(&mmModel.Channel{Id: "req-dm"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "notify-post"}, nil)

		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "team-id|outbound:my-conn",
			Submission: map[string]any{"reason": "Not allowed at this time"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/request/deny-submit", submitReq, "admin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, deletedRequest)
		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "| **Status** | :no_entry_sign: Denied |")
		assert.Contains(t, updatedPost.Message, "| **Denied by** | @admin |")
		assert.Contains(t, updatedPost.Message, "| **Reason** | Not allowed at this time |")
	})

	t.Run("cancelled dialog does nothing", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		submitReq := mmModel.SubmitDialogRequest{
			UserId:    "admin-id",
			State:     "team-id|outbound:my-conn",
			Cancelled: true,
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/request/deny-submit", submitReq, "admin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		// No KV operations should be called.
		api.AssertNotCalled(t, "GetPost", mock.Anything)
		api.AssertNotCalled(t, "UpdatePost", mock.Anything)
	})

	t.Run("non-sysadmin submit is rejected with 403", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{
			Id: "teamadmin-id", Username: "teamadmin",
		}, nil)

		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "teamadmin-id",
			State:      "team-id|outbound:my-conn",
			Submission: map[string]any{"reason": "some reason"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/request/deny-submit", submitReq, "teamadmin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		r := httptest.NewRequest(http.MethodPost, "/api/v1/request/deny-submit", strings.NewReader("{bad"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("request already deleted is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "team-id|outbound:my-conn",
			Submission: map[string]any{"reason": "too late"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/request/deny-submit", submitReq, "admin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		api.AssertNotCalled(t, "GetPost", mock.Anything)
		api.AssertNotCalled(t, "UpdatePost", mock.Anything)
	})

	t.Run("KV get error returns OK without side effects", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, errors.New("kv failure")
		}

		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "team-id|outbound:my-conn",
			Submission: map[string]any{"reason": "reason"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/request/deny-submit", submitReq, "admin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		api.AssertNotCalled(t, "UpdatePost", mock.Anything)
	})

	t.Run("malformed state returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "no-pipe-delimiter",
			Submission: map[string]any{"reason": "reason"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/request/deny-submit", submitReq, "admin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("deny without reason omits reason row", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "teamadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		kvs.deleteConnectionRequestFn = func(teamID, connKey string) error {
			return nil
		}

		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		api.On("GetDirectChannel", "bot-user-id", "teamadmin-id").Return(&mmModel.Channel{Id: "req-dm"}, nil)
		var notifyPost *mmModel.Post
		api.On("CreatePost", mock.Anything).Run(func(args mock.Arguments) {
			notifyPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{Id: "notify-post"}, nil)

		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "team-id|outbound:my-conn",
			Submission: map[string]any{"reason": ""},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/request/deny-submit", submitReq, "admin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "| **Status** | :no_entry_sign: Denied |")
		assert.NotContains(t, updatedPost.Message, "**Reason**")
		require.NotNil(t, notifyPost)
		assert.NotContains(t, notifyPost.Message, "Reason")
	})
}

// ============================================================
// TestCancelPendingConnectionRequest
// ============================================================

func TestCancelPendingConnectionRequest(t *testing.T) {
	t.Run("pending request is cancelled", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "teamadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1", "dm-post-2"},
			}, nil
		}

		var deletedRequest bool
		kvs.deleteConnectionRequestFn = func(teamID, connKey string) error {
			deletedRequest = true
			return nil
		}

		// updateRequestPosts mocks
		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		api.On("GetPost", "dm-post-2").Return(&mmModel.Post{Id: "dm-post-2", Message: "old"}, nil)
		var updatedPosts []*mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPosts = append(updatedPosts, args.Get(0).(*mmModel.Post))
		}).Return(&mmModel.Post{}, nil)

		// notifyRequester mocks
		api.On("GetDirectChannel", "bot-user-id", "teamadmin-id").Return(&mmModel.Channel{Id: "req-dm"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "notify-post"}, nil)

		p.cancelPendingConnectionRequest("team-id", "outbound:my-conn")

		assert.True(t, deletedRequest)
		assert.Len(t, updatedPosts, 2)
		for _, post := range updatedPosts {
			assert.Contains(t, post.Message, "| **Status** | :warning: Cancelled (connection was unlinked) |")
		}
	})

	t.Run("no pending request is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		p.cancelPendingConnectionRequest("team-id", "outbound:my-conn")

		api.AssertNotCalled(t, "GetPost", mock.Anything)
		api.AssertNotCalled(t, "UpdatePost", mock.Anything)
		api.AssertNotCalled(t, "CreatePost", mock.Anything)
	})

	t.Run("KV get error logs and returns early", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return nil, errors.New("kv failure")
		}

		p.cancelPendingConnectionRequest("team-id", "outbound:my-conn")

		api.AssertNotCalled(t, "GetPost", mock.Anything)
		api.AssertNotCalled(t, "UpdatePost", mock.Anything)
		api.AssertNotCalled(t, "CreatePost", mock.Anything)
	})

	t.Run("KV delete error still updates posts and notifies", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = requestModeConfig()

		kvs.getConnectionRequestFn = func(teamID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "teamadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		kvs.deleteConnectionRequestFn = func(teamID, connKey string) error {
			return errors.New("delete failed")
		}

		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{}, nil)
		api.On("GetDirectChannel", "bot-user-id", "teamadmin-id").Return(&mmModel.Channel{Id: "req-dm"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "notify-post"}, nil)

		p.cancelPendingConnectionRequest("team-id", "outbound:my-conn")

		api.AssertCalled(t, "UpdatePost", mock.Anything)
		api.AssertCalled(t, "CreatePost", mock.Anything)
	})
}

// ============================================================
// TestUpdateRequestPosts
// ============================================================

func TestUpdateRequestPosts(t *testing.T) {
	t.Run("updates multiple posts successfully", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetPost", "post-1").Return(&mmModel.Post{Id: "post-1", Message: "old"}, nil)
		api.On("GetPost", "post-2").Return(&mmModel.Post{Id: "post-2", Message: "old"}, nil)
		var updated []*mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updated = append(updated, args.Get(0).(*mmModel.Post))
		}).Return(&mmModel.Post{}, nil)

		p.updateRequestPosts([]string{"post-1", "post-2"}, "new message")

		require.Len(t, updated, 2)
		for _, post := range updated {
			assert.Equal(t, "old\nnew message", post.Message)
		}
	})

	t.Run("GetPost error continues to next post", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetPost", "bad-post").Return(nil, &mmModel.AppError{Message: "not found"})
		api.On("GetPost", "good-post").Return(&mmModel.Post{Id: "good-post", Message: "old"}, nil)
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{}, nil)

		p.updateRequestPosts([]string{"bad-post", "good-post"}, "updated")

		api.AssertNumberOfCalls(t, "UpdatePost", 1)
	})

	t.Run("UpdatePost error continues to next post", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetPost", "post-1").Return(&mmModel.Post{Id: "post-1", Message: "old"}, nil)
		api.On("GetPost", "post-2").Return(&mmModel.Post{Id: "post-2", Message: "old"}, nil)
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(nil, &mmModel.AppError{Message: "update failed"}).Once()
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{}, nil).Once()

		p.updateRequestPosts([]string{"post-1", "post-2"}, "updated")

		api.AssertNumberOfCalls(t, "UpdatePost", 2)
	})

	t.Run("empty post IDs does nothing", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		p.updateRequestPosts([]string{}, "message")

		api.AssertNotCalled(t, "GetPost", mock.Anything)
	})
}

// ============================================================
// TestNotifyRequester
// ============================================================

func TestNotifyRequester(t *testing.T) {
	t.Run("sends DM to requester", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetDirectChannel", "bot-user-id", "requester-id").Return(&mmModel.Channel{Id: "dm-chan"}, nil)
		var createdPost *mmModel.Post
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			createdPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{Id: "post-id"}, nil)

		p.notifyRequester("requester-id", "Your request was approved.")

		require.NotNil(t, createdPost)
		assert.Equal(t, "dm-chan", createdPost.ChannelId)
		assert.Equal(t, "bot-user-id", createdPost.UserId)
		assert.Equal(t, "Your request was approved.", createdPost.Message)
	})

	t.Run("GetDirectChannel error logs and returns", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetDirectChannel", "bot-user-id", "requester-id").
			Return(nil, &mmModel.AppError{Message: "dm error"})

		p.notifyRequester("requester-id", "Your request was approved.")

		api.AssertNotCalled(t, "CreatePost", mock.Anything)
	})

	t.Run("CreatePost error logs but does not panic", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetDirectChannel", "bot-user-id", "requester-id").Return(&mmModel.Channel{Id: "dm-chan"}, nil)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).
			Return(nil, &mmModel.AppError{Message: "create error"})

		p.notifyRequester("requester-id", "Your request was approved.")

		api.AssertCalled(t, "CreatePost", mock.Anything)
	})
}
