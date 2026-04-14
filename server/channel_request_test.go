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

// channelRequestModeConfig returns a configuration with both team admin request
// mode and channel admin request mode enabled.
func channelRequestModeConfig() *configuration {
	boolTrue := true
	return &configuration{
		RestrictToSystemAdmins:    &boolTrue,
		AllowTeamAdminRequests:    &boolTrue,
		AllowChannelAdminRequests: &boolTrue,
		OutboundConnections:       `[{"name":"my-conn","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.test"}}]`,
	}
}

// ============================================================
// TestIsChannelAdminInRequestMode
// ============================================================

func TestIsChannelAdminInRequestMode(t *testing.T) {
	t.Run("sysadmin returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "admin-id").Return(&mmModel.User{
			Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId,
		}, nil)

		result := p.isChannelAdminInRequestMode("admin-id", "channel-id", "team-id")
		assert.False(t, result)
	})

	t.Run("team admin returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{
			Id: "teamadmin-id", Username: "teamadmin",
		}, nil)
		api.On("GetTeamMember", "team-id", "teamadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "teamadmin-id", SchemeAdmin: true,
		}, nil)

		result := p.isChannelAdminInRequestMode("teamadmin-id", "channel-id", "team-id")
		assert.False(t, result)
	})

	t.Run("channel admin with channel request mode enabled returns true", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "chanadmin-id").Return(&mmModel.User{
			Id: "chanadmin-id", Username: "chanadmin",
		}, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "chanadmin-id", SchemeAdmin: false,
		}, nil)
		api.On("GetChannelMember", "channel-id", "chanadmin-id").Return(&mmModel.ChannelMember{
			ChannelId: "channel-id", UserId: "chanadmin-id", SchemeAdmin: true,
		}, nil)

		result := p.isChannelAdminInRequestMode("chanadmin-id", "channel-id", "team-id")
		assert.True(t, result)
	})

	t.Run("channel request mode disabled returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		boolTrue := true
		boolFalse := false
		p.configuration = &configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowChannelAdminRequests: &boolFalse,
		}

		api.On("GetUser", "chanadmin-id").Return(&mmModel.User{
			Id: "chanadmin-id", Username: "chanadmin",
		}, nil)

		result := p.isChannelAdminInRequestMode("chanadmin-id", "channel-id", "team-id")
		assert.False(t, result)
	})

	t.Run("regular user (not channel admin) returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "regular-id").Return(&mmModel.User{
			Id: "regular-id", Username: "regular",
		}, nil)
		api.On("GetTeamMember", "team-id", "regular-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "regular-id", SchemeAdmin: false,
		}, nil)
		api.On("GetChannelMember", "channel-id", "regular-id").Return(&mmModel.ChannelMember{
			ChannelId: "channel-id", UserId: "regular-id", SchemeAdmin: false,
		}, nil)

		result := p.isChannelAdminInRequestMode("regular-id", "channel-id", "team-id")
		assert.False(t, result)
	})

	t.Run("GetUser failure returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "bad-id").Return(nil, &mmModel.AppError{Message: "user not found"})

		result := p.isChannelAdminInRequestMode("bad-id", "channel-id", "team-id")
		assert.False(t, result)
	})

	t.Run("GetTeamMember failure returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "chanadmin-id").Return(&mmModel.User{
			Id: "chanadmin-id", Username: "chanadmin",
		}, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(nil, &mmModel.AppError{Message: "not found"})

		result := p.isChannelAdminInRequestMode("chanadmin-id", "channel-id", "team-id")
		assert.False(t, result)
	})

	t.Run("GetChannelMember failure returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "chanadmin-id").Return(&mmModel.User{
			Id: "chanadmin-id", Username: "chanadmin",
		}, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "chanadmin-id", SchemeAdmin: false,
		}, nil)
		api.On("GetChannelMember", "channel-id", "chanadmin-id").Return(nil, &mmModel.AppError{Message: "not found"})

		result := p.isChannelAdminInRequestMode("chanadmin-id", "channel-id", "team-id")
		assert.False(t, result)
	})
}

// ============================================================
// TestIsTeamAdminDirect
// ============================================================

func TestIsTeamAdminDirect(t *testing.T) {
	t.Run("sysadmin returns true", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetUser", "admin-id").Return(&mmModel.User{
			Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId,
		}, nil)

		assert.True(t, p.isTeamAdminDirect("admin-id", "team-id"))
	})

	t.Run("team admin returns true regardless of restrict setting", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		// RestrictToSystemAdmins is on, but isTeamAdminDirect bypasses it.
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{
			Id: "teamadmin-id", Username: "teamadmin",
		}, nil)
		api.On("GetTeamMember", "team-id", "teamadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "teamadmin-id", SchemeAdmin: true,
		}, nil)

		assert.True(t, p.isTeamAdminDirect("teamadmin-id", "team-id"))
	})

	t.Run("regular user returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetUser", "regular-id").Return(&mmModel.User{
			Id: "regular-id", Username: "regular",
		}, nil)
		api.On("GetTeamMember", "team-id", "regular-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "regular-id", SchemeAdmin: false,
		}, nil)

		assert.False(t, p.isTeamAdminDirect("regular-id", "team-id"))
	})

	t.Run("GetUser failure returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetUser", "bad-id").Return(nil, &mmModel.AppError{Message: "user not found"})

		assert.False(t, p.isTeamAdminDirect("bad-id", "team-id"))
	})

	t.Run("GetTeamMember failure returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetUser", "user-id").Return(&mmModel.User{
			Id: "user-id", Username: "user",
		}, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(nil, &mmModel.AppError{Message: "not found"})

		assert.False(t, p.isTeamAdminDirect("user-id", "team-id"))
	})
}

// ============================================================
// TestCanDirectlyManageChannelConns
// ============================================================

func TestCanDirectlyManageChannelConns(t *testing.T) {
	t.Run("team admin with channel request mode enabled returns true", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{Id: "teamadmin-id", Roles: "system_user"}, nil)
		api.On("GetTeamMember", "team-id", "teamadmin-id").Return(&mmModel.TeamMember{SchemeAdmin: true}, nil)

		assert.True(t, p.canDirectlyManageChannelConns("teamadmin-id", "team-id"))
	})

	t.Run("sysadmin with channel request mode enabled returns true", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "sysadmin-id").Return(&mmModel.User{Id: "sysadmin-id", Roles: "system_admin system_user"}, nil)

		assert.True(t, p.canDirectlyManageChannelConns("sysadmin-id", "team-id"))
	})

	t.Run("channel request mode disabled returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		boolFalse := false
		p.configuration = &configuration{AllowChannelAdminRequests: &boolFalse}

		assert.False(t, p.canDirectlyManageChannelConns("teamadmin-id", "team-id"))
	})

	t.Run("regular user returns false", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "regular-id").Return(&mmModel.User{Id: "regular-id", Roles: "system_user"}, nil)
		api.On("GetTeamMember", "team-id", "regular-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		assert.False(t, p.canDirectlyManageChannelConns("regular-id", "team-id"))
	})
}

// ============================================================
// TestGetTeamAdmins
// ============================================================

func TestGetTeamAdmins(t *testing.T) {
	t.Run("returns team admins from paginated results", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetTeamMembers", "team-id", 0, 200).Return([]*mmModel.TeamMember{
			{TeamId: "team-id", UserId: "admin1-id", SchemeAdmin: true},
			{TeamId: "team-id", UserId: "regular-id", SchemeAdmin: false},
			{TeamId: "team-id", UserId: "admin2-id", SchemeAdmin: true},
		}, nil)
		api.On("GetTeamMembers", "team-id", 1, 200).Return([]*mmModel.TeamMember{}, nil)

		admin1 := &mmModel.User{Id: "admin1-id", Username: "admin1"}
		regular := &mmModel.User{Id: "regular-id", Username: "regular", Roles: "system_user"}
		admin2 := &mmModel.User{Id: "admin2-id", Username: "admin2"}
		api.On("GetUser", "admin1-id").Return(admin1, nil)
		api.On("GetUser", "regular-id").Return(regular, nil)
		api.On("GetUser", "admin2-id").Return(admin2, nil)

		admins, err := p.getTeamAdmins("team-id")
		require.NoError(t, err)
		require.Len(t, admins, 2)
		assert.Equal(t, "admin1-id", admins[0].Id)
		assert.Equal(t, "admin2-id", admins[1].Id)
	})

	t.Run("includes sysadmins without SchemeAdmin on team membership", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetTeamMembers", "team-id", 0, 200).Return([]*mmModel.TeamMember{
			{TeamId: "team-id", UserId: "sysadmin-id", SchemeAdmin: false},
			{TeamId: "team-id", UserId: "regular-id", SchemeAdmin: false},
		}, nil)
		api.On("GetTeamMembers", "team-id", 1, 200).Return([]*mmModel.TeamMember{}, nil)

		sysadmin := &mmModel.User{Id: "sysadmin-id", Username: "sysadmin", Roles: "system_admin system_user"}
		regular := &mmModel.User{Id: "regular-id", Username: "regular", Roles: "system_user"}
		api.On("GetUser", "sysadmin-id").Return(sysadmin, nil)
		api.On("GetUser", "regular-id").Return(regular, nil)

		admins, err := p.getTeamAdmins("team-id")
		require.NoError(t, err)
		require.Len(t, admins, 1)
		assert.Equal(t, "sysadmin-id", admins[0].Id)
	})

	t.Run("GetTeamMembers error returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetTeamMembers", "team-id", 0, 200).Return(nil, &mmModel.AppError{Message: "api error"})

		_, err := p.getTeamAdmins("team-id")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get team members")
	})

	t.Run("GetUser failure for admin skips that admin", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetTeamMembers", "team-id", 0, 200).Return([]*mmModel.TeamMember{
			{TeamId: "team-id", UserId: "admin1-id", SchemeAdmin: true},
			{TeamId: "team-id", UserId: "admin2-id", SchemeAdmin: true},
		}, nil)
		api.On("GetTeamMembers", "team-id", 1, 200).Return([]*mmModel.TeamMember{}, nil)

		api.On("GetUser", "admin1-id").Return(nil, &mmModel.AppError{Message: "user not found"})
		admin2 := &mmModel.User{Id: "admin2-id", Username: "admin2"}
		api.On("GetUser", "admin2-id").Return(admin2, nil)

		admins, err := p.getTeamAdmins("team-id")
		require.NoError(t, err)
		require.Len(t, admins, 1)
		assert.Equal(t, "admin2-id", admins[0].Id)
	})

	t.Run("empty team returns empty list", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetTeamMembers", "team-id", 0, 200).Return([]*mmModel.TeamMember{}, nil)

		admins, err := p.getTeamAdmins("team-id")
		require.NoError(t, err)
		assert.Empty(t, admins)
	})
}

// ============================================================
// TestCreateChannelConnectionRequest
// ============================================================

func TestCreateChannelConnectionRequest(t *testing.T) {
	t.Run("successful request DMs team admins", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin", FirstName: "Chan", LastName: "Admin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		// Team connection must exist (prerequisite).
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{conn}, nil
		}

		api.On("GetChannel", "channel-id").Return(&mmModel.Channel{
			Id: "channel-id", Name: "test-channel", DisplayName: "Test Channel",
		}, nil)
		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		// getTeamAdmins mocks
		api.On("GetTeamMembers", "team-id", 0, 200).Return([]*mmModel.TeamMember{
			{TeamId: "team-id", UserId: "teamadmin1-id", SchemeAdmin: true},
			{TeamId: "team-id", UserId: "teamadmin2-id", SchemeAdmin: true},
		}, nil)
		api.On("GetTeamMembers", "team-id", 1, 200).Return([]*mmModel.TeamMember{}, nil)
		api.On("GetUser", "teamadmin1-id").Return(&mmModel.User{Id: "teamadmin1-id", Username: "teamadmin1"}, nil)
		api.On("GetUser", "teamadmin2-id").Return(&mmModel.User{Id: "teamadmin2-id", Username: "teamadmin2"}, nil)

		api.On("GetDirectChannel", "bot-user-id", "teamadmin1-id").Return(&mmModel.Channel{Id: "dm1-id"}, nil)
		api.On("GetDirectChannel", "bot-user-id", "teamadmin2-id").Return(&mmModel.Channel{Id: "dm2-id"}, nil)
		api.On("GetDirectChannel", "bot-user-id", "chanadmin-id").Return(&mmModel.Channel{Id: "requester-dm-id"}, nil)

		var createdPosts []*mmModel.Post
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			createdPosts = append(createdPosts, args.Get(0).(*mmModel.Post))
		}).Return(&mmModel.Post{Id: "post-id"}, nil)

		var savedReq *store.ConnectionRequest
		kvs.createChannelConnectionRequestFn = func(channelID, connKey string, req *store.ConnectionRequest) (bool, error) {
			savedReq = req
			return true, nil
		}

		msg, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.NoError(t, err)
		assert.Contains(t, msg, "outbound:my-conn")

		// 2 team admin DMs + 1 requester confirmation DM
		require.Len(t, createdPosts, 3)
		assert.Equal(t, "dm1-id", createdPosts[0].ChannelId)
		assert.Equal(t, "dm2-id", createdPosts[1].ChannelId)
		assert.Contains(t, createdPosts[0].Message, "#### :link: Channel Connection Link Request")
		assert.Contains(t, createdPosts[0].Message, "| **Requested by** | @chanadmin (Chan Admin) |")
		assert.Contains(t, createdPosts[0].Message, "| **Channel** |")

		// Requester confirmation DM
		confirmPost := createdPosts[2]
		assert.Equal(t, "requester-dm-id", confirmPost.ChannelId)
		assert.Contains(t, confirmPost.Message, "#### :link: Connection Link Request Submitted")
		assert.Contains(t, confirmPost.Message, "team admins")

		require.NotNil(t, savedReq)
		assert.Equal(t, "chanadmin-id", savedReq.RequesterID)
		assert.Equal(t, "team-id", savedReq.TeamID)
		assert.Equal(t, "channel-id", savedReq.ChannelID)
		assert.Equal(t, "outbound:my-conn", savedReq.ConnKey)
		assert.Len(t, savedReq.PostIDs, 2)
	})

	t.Run("duplicate request rejected when already pending", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{RequesterID: "other-user"}, nil
		}

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already pending")
	})

	t.Run("team connection not linked returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		api.On("GetChannel", "channel-id").Return(&mmModel.Channel{
			Id: "channel-id", Name: "test-channel", DisplayName: "Test Channel",
		}, nil)
		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		// Empty team connections.
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not linked to this team")
	})

	t.Run("no team admins returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "my-conn"}}, nil
		}

		api.On("GetChannel", "channel-id").Return(&mmModel.Channel{
			Id: "channel-id", Name: "test-channel", DisplayName: "Test Channel",
		}, nil)
		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		api.On("GetTeamMembers", "team-id", 0, 200).Return([]*mmModel.TeamMember{}, nil)

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no team admins")
	})

	t.Run("KV GetChannelConnectionRequest error returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, errors.New("kv failure")
		}

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to check existing requests")
	})

	t.Run("KV save error returns error without creating posts", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "my-conn"}}, nil
		}

		api.On("GetChannel", "channel-id").Return(&mmModel.Channel{
			Id: "channel-id", Name: "test-channel", DisplayName: "Test Channel",
		}, nil)
		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		api.On("GetTeamMembers", "team-id", 0, 200).Return([]*mmModel.TeamMember{
			{TeamId: "team-id", UserId: "teamadmin-id", SchemeAdmin: true},
		}, nil)
		api.On("GetTeamMembers", "team-id", 1, 200).Return([]*mmModel.TeamMember{}, nil)
		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}, nil)

		// CAS fails with error. No posts should be created.
		kvs.createChannelConnectionRequestFn = func(channelID, connKey string, req *store.ConnectionRequest) (bool, error) {
			return false, errors.New("kv save error")
		}

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to save channel connection request")
		// No CreatePost calls should have been made since CAS happens before DMs.
		api.AssertNotCalled(t, "CreatePost", mock.Anything)
	})

	t.Run("CAS race returns already pending without creating posts", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "my-conn"}}, nil
		}

		api.On("GetChannel", "channel-id").Return(&mmModel.Channel{
			Id: "channel-id", Name: "test-channel", DisplayName: "Test Channel",
		}, nil)
		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		api.On("GetTeamMembers", "team-id", 0, 200).Return([]*mmModel.TeamMember{
			{TeamId: "team-id", UserId: "teamadmin-id", SchemeAdmin: true},
		}, nil)
		api.On("GetTeamMembers", "team-id", 1, 200).Return([]*mmModel.TeamMember{}, nil)
		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}, nil)

		// CAS returns false (another request won the race). No posts should be created.
		kvs.createChannelConnectionRequestFn = func(channelID, connKey string, req *store.ConnectionRequest) (bool, error) {
			return false, nil
		}

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already pending")
		api.AssertNotCalled(t, "CreatePost", mock.Anything)
	})

	t.Run("channel not found returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}
		api.On("GetChannel", "channel-id").Return(nil, &mmModel.AppError{Message: "not found"})

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "channel not found")
	})

	t.Run("team not found returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}
		api.On("GetChannel", "channel-id").Return(&mmModel.Channel{
			Id: "channel-id", Name: "test-channel",
		}, nil)
		api.On("GetTeam", "team-id").Return(nil, &mmModel.AppError{Message: "not found"})

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "team not found")
	})

	t.Run("GetTeamConnections error returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, errors.New("store error")
		}
		api.On("GetChannel", "channel-id").Return(&mmModel.Channel{
			Id: "channel-id", Name: "test-channel",
		}, nil)
		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test",
		}, nil)

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		_, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to check team connections")
	})

	t.Run("UpdateChannelConnectionRequest failure logs warning but succeeds", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "my-conn"}}, nil
		}
		kvs.createChannelConnectionRequestFn = func(channelID, connKey string, req *store.ConnectionRequest) (bool, error) {
			return true, nil
		}
		kvs.updateChannelConnectionRequestFn = func(channelID, connKey string, req *store.ConnectionRequest) error {
			return errors.New("update failed")
		}

		api.On("GetChannel", "channel-id").Return(&mmModel.Channel{
			Id: "channel-id", Name: "test-channel", DisplayName: "Test Channel", TeamId: "team-id",
		}, nil)
		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)
		api.On("GetTeamMembers", "team-id", 0, 200).Return([]*mmModel.TeamMember{
			{TeamId: "team-id", UserId: "teamadmin-id", SchemeAdmin: true},
		}, nil)
		api.On("GetTeamMembers", "team-id", 1, 200).Return([]*mmModel.TeamMember{}, nil)
		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}, nil)
		api.On("GetDirectChannel", mock.Anything, mock.Anything).Return(&mmModel.Channel{Id: "dm-id"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "post-id"}, nil)

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		msg, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.NoError(t, err)
		assert.Contains(t, msg, "submitted for team admin approval")
	})

	t.Run("GetDirectChannel failure for admin skips that admin", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "my-conn"}}, nil
		}
		kvs.createChannelConnectionRequestFn = func(channelID, connKey string, req *store.ConnectionRequest) (bool, error) {
			return true, nil
		}

		api.On("GetChannel", "channel-id").Return(&mmModel.Channel{
			Id: "channel-id", Name: "test-channel", DisplayName: "Test Channel", TeamId: "team-id",
		}, nil)
		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		// Two team admins. DM channel fails for the first, succeeds for the second.
		api.On("GetTeamMembers", "team-id", 0, 200).Return([]*mmModel.TeamMember{
			{TeamId: "team-id", UserId: "admin1", SchemeAdmin: true},
			{TeamId: "team-id", UserId: "admin2", SchemeAdmin: true},
		}, nil)
		api.On("GetTeamMembers", "team-id", 1, 200).Return([]*mmModel.TeamMember{}, nil)
		api.On("GetUser", "admin1").Return(&mmModel.User{Id: "admin1", Username: "admin1"}, nil)
		api.On("GetUser", "admin2").Return(&mmModel.User{Id: "admin2", Username: "admin2"}, nil)

		api.On("GetDirectChannel", "bot-user-id", "admin1").Return(nil, &mmModel.AppError{Message: "dm fail"})
		api.On("GetDirectChannel", "bot-user-id", "admin2").Return(&mmModel.Channel{Id: "dm2"}, nil)
		api.On("GetDirectChannel", "bot-user-id", "chanadmin-id").Return(&mmModel.Channel{Id: "dm-req"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "post-id"}, nil)

		requester := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		conn := store.TeamConnection{Direction: "outbound", Connection: "my-conn"}

		msg, err := p.createChannelConnectionRequest(requester, "channel-id", "team-id", conn)
		require.NoError(t, err)
		assert.Contains(t, msg, "submitted for team admin approval")
	})
}

// ============================================================
// TestHandleChannelRequestApprove
// ============================================================

func TestHandleChannelRequestApprove(t *testing.T) {
	t.Run("team admin approves channel request", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		teamAdmin := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		api.On("GetUser", "teamadmin-id").Return(teamAdmin, nil)
		api.On("GetTeamMember", "team-id", "teamadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "teamadmin-id", SchemeAdmin: true,
		}, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "chanadmin-id",
				TeamID:      "team-id",
				ChannelID:   "channel-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		// Team must still have the connection linked.
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "my-conn"}}, nil
		}

		var deletedRequest bool
		kvs.deleteChannelConnectionRequestFn = func(channelID, connKey string) error {
			deletedRequest = true
			return nil
		}

		// initChannelForCrossGuard mocks
		channel := &mmModel.Channel{
			Id: "channel-id", Name: "test-channel", DisplayName: "Test Channel", TeamId: "team-id",
		}
		api.On("GetChannel", "channel-id").Return(channel, nil)
		api.On("UpdateChannel", mock.AnythingOfType("*model.Channel")).Return(channel, nil)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "announce-post"}, nil)

		// updateRequestPosts mocks
		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		// notifyRequester mocks
		api.On("GetDirectChannel", "bot-user-id", "chanadmin-id").Return(&mmModel.Channel{Id: "requester-dm"}, nil)

		r := postActionRequest(t, "teamadmin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)

		assert.True(t, deletedRequest)
		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "| **Status** | :white_check_mark: Approved |")
		assert.Contains(t, updatedPost.Message, "| **Approved by** | @teamadmin |")
	})

	t.Run("sysadmin can also approve channel request", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "chanadmin-id",
				TeamID:      "team-id",
				ChannelID:   "channel-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		kvs.deleteChannelConnectionRequestFn = func(channelID, connKey string) error {
			return nil
		}

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "my-conn"}}, nil
		}

		channel := &mmModel.Channel{
			Id: "channel-id", Name: "test-channel", DisplayName: "Test Channel", TeamId: "team-id",
		}
		api.On("GetChannel", "channel-id").Return(channel, nil)
		api.On("UpdateChannel", mock.AnythingOfType("*model.Channel")).Return(channel, nil)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "announce-post"}, nil)
		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{}, nil)
		api.On("GetDirectChannel", "bot-user-id", "chanadmin-id").Return(&mmModel.Channel{Id: "requester-dm"}, nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)
	})

	t.Run("channel admin cannot approve", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "chanadmin-id").Return(&mmModel.User{
			Id: "chanadmin-id", Username: "chanadmin",
		}, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "chanadmin-id", SchemeAdmin: false,
		}, nil)

		r := postActionRequest(t, "chanadmin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Only team admins")
	})

	t.Run("request no longer active returns message", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
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

		// Config with no connections.
		boolTrue := true
		p.configuration = &configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowTeamAdminRequests:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
			OutboundConnections:       `[]`,
		}

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "chanadmin-id",
				TeamID:      "team-id",
				ChannelID:   "channel-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		var deletedRequest bool
		kvs.deleteChannelConnectionRequestFn = func(channelID, connKey string) error {
			deletedRequest = true
			return nil
		}

		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		api.On("GetDirectChannel", "bot-user-id", "chanadmin-id").Return(&mmModel.Channel{Id: "req-dm"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "notify-post"}, nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)

		assert.True(t, deletedRequest)
		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "Cancelled (connection removed from configuration)")
	})

	t.Run("team connection unlinked cancels request", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "chanadmin-id",
				TeamID:      "team-id",
				ChannelID:   "channel-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		// Team has no connections (unlinked).
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		var deletedRequest bool
		kvs.deleteChannelConnectionRequestFn = func(channelID, connKey string) error {
			deletedRequest = true
			return nil
		}

		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		api.On("GetDirectChannel", "bot-user-id", "chanadmin-id").Return(&mmModel.Channel{Id: "req-dm"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "notify-post"}, nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)

		assert.True(t, deletedRequest)
		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "Cancelled (connection unlinked from team)")
	})

	t.Run("invalid JSON body returns Invalid request", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		r := httptest.NewRequest(http.MethodPost, "/api/v1/channel-request/approve", strings.NewReader("{bad"))
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
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Missing context")
	})

	t.Run("GetUser failure returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetUser", "bad-user").Return(nil, &mmModel.AppError{Message: "not found"})

		r := postActionRequest(t, "bad-user", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to look up user")
	})

	t.Run("GetChannelConnectionRequest error returns failure", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, errors.New("kv store error")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to check request status")
	})

	t.Run("DeleteChannelConnectionRequest error returns failure", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{RequesterID: "u1", ConnKey: "outbound:my-conn"}, nil
		}
		kvs.deleteChannelConnectionRequestFn = func(channelID, connKey string) error {
			return errors.New("delete failed")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to process request")
	})

	t.Run("GetTeamConnections error during approve", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{RequesterID: "u1", ConnKey: "outbound:my-conn"}, nil
		}
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, errors.New("store error")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to verify team connections")
	})

	t.Run("channel deleted during approval", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "chanadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "my-conn"}}, nil
		}
		api.On("GetChannel", "channel-id").Return(nil, &mmModel.AppError{Message: "not found"})
		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{}, nil)
		api.On("GetDirectChannel", "bot-user-id", "chanadmin-id").Return(&mmModel.Channel{Id: "dm"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)
	})

	t.Run("initChannelForCrossGuard failure", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "chanadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
			}, nil
		}
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "my-conn"}}, nil
		}
		// initChannelForCrossGuard calls GetChannel; return valid channel.
		// Then it calls GetChannelConnections. Make that fail via the store.
		api.On("GetChannel", "channel-id").Return(&mmModel.Channel{Id: "channel-id", TeamId: "team-id"}, nil)
		kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
			return nil, errors.New("store error")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/approve"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to link connection")
	})
}

// ============================================================
// TestHandleChannelRequestDeny
// ============================================================

func TestHandleChannelRequestDeny(t *testing.T) {
	t.Run("team admin clicks deny opens dialog", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		teamAdmin := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		api.On("GetUser", "teamadmin-id").Return(teamAdmin, nil)
		api.On("GetTeamMember", "team-id", "teamadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "teamadmin-id", SchemeAdmin: true,
		}, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "chanadmin-id",
				TeamID:      "team-id",
				ChannelID:   "channel-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		var dialogOpened bool
		api.On("OpenInteractiveDialog", mock.Anything).Run(func(args mock.Arguments) {
			dialogOpened = true
			dialogReq := args.Get(0).(mmModel.OpenDialogRequest)
			// State should be channelID|teamID|connKey (3-part).
			assert.Contains(t, dialogReq.Dialog.State, "channel-id|team-id|outbound:my-conn")
		}).Return(nil)

		r := postActionRequest(t, "teamadmin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.EphemeralText)
		assert.True(t, dialogOpened)
	})

	t.Run("channel admin cannot deny", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "chanadmin-id").Return(&mmModel.User{
			Id: "chanadmin-id", Username: "chanadmin",
		}, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "chanadmin-id", SchemeAdmin: false,
		}, nil)

		r := postActionRequest(t, "chanadmin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Only team admins")
	})

	t.Run("request no longer active returns message", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "no longer active")
	})

	t.Run("invalid JSON body returns Invalid request", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		r := httptest.NewRequest(http.MethodPost, "/api/v1/channel-request/deny", strings.NewReader("{bad"))
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
		r.URL.Path = "/api/v1/channel-request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Missing context")
	})

	t.Run("GetUser failure returns error", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetUser", "bad-user").Return(nil, &mmModel.AppError{Message: "not found"})

		r := postActionRequest(t, "bad-user", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to look up user")
	})

	t.Run("GetChannelConnectionRequest error returns failure", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, errors.New("kv store error")
		}

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/deny"
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

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "chanadmin-id",
				TeamID:      "team-id",
				ChannelID:   "channel-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		api.On("OpenInteractiveDialog", mock.Anything).Return(&mmModel.AppError{Message: "dialog error"})

		r := postActionRequest(t, "admin-id", map[string]any{
			"channel_id": "channel-id",
			"team_id":    "team-id",
			"conn_key":   "outbound:my-conn",
		})
		r.URL.Path = "/api/v1/channel-request/deny"
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		var resp mmModel.PostActionIntegrationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.EphemeralText, "Failed to open dialog")
	})
}

// ============================================================
// TestHandleChannelRequestDenySubmit
// ============================================================

func TestHandleChannelRequestDenySubmit(t *testing.T) {
	t.Run("team admin submits deny with reason", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		teamAdmin := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		api.On("GetUser", "teamadmin-id").Return(teamAdmin, nil)
		api.On("GetTeamMember", "team-id", "teamadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "teamadmin-id", SchemeAdmin: true,
		}, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "chanadmin-id",
				TeamID:      "team-id",
				ChannelID:   "channel-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}

		var deletedRequest bool
		kvs.deleteChannelConnectionRequestFn = func(channelID, connKey string) error {
			deletedRequest = true
			return nil
		}

		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		var updatedPost *mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPost = args.Get(0).(*mmModel.Post)
		}).Return(&mmModel.Post{}, nil)

		api.On("GetDirectChannel", "bot-user-id", "chanadmin-id").Return(&mmModel.Channel{Id: "req-dm"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "notify-post"}, nil)

		// State is channelID|teamID|connKey (3-part).
		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "teamadmin-id",
			State:      "channel-id|team-id|outbound:my-conn",
			Submission: map[string]any{"reason": "Not appropriate for this channel"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channel-request/deny-submit", submitReq, "teamadmin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, deletedRequest)
		require.NotNil(t, updatedPost)
		assert.Contains(t, updatedPost.Message, "| **Status** | :no_entry_sign: Denied |")
		assert.Contains(t, updatedPost.Message, "| **Denied by** | @teamadmin |")
		assert.Contains(t, updatedPost.Message, "| **Reason** | Not appropriate for this channel |")
	})

	t.Run("cancelled dialog does nothing", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		teamAdmin := &mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}
		api.On("GetUser", "teamadmin-id").Return(teamAdmin, nil)

		submitReq := mmModel.SubmitDialogRequest{
			UserId:    "teamadmin-id",
			State:     "channel-id|team-id|outbound:my-conn",
			Cancelled: true,
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channel-request/deny-submit", submitReq, "teamadmin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("non-team-admin cannot submit deny", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		api.On("GetUser", "chanadmin-id").Return(&mmModel.User{
			Id: "chanadmin-id", Username: "chanadmin",
		}, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "chanadmin-id", SchemeAdmin: false,
		}, nil)

		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "chanadmin-id",
			State:      "channel-id|team-id|outbound:my-conn",
			Submission: map[string]any{"reason": "test"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channel-request/deny-submit", submitReq, "chanadmin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid state format returns bad request", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "bad-state-only-one-part",
			Submission: map[string]any{"reason": "test"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channel-request/deny-submit", submitReq, "admin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("request already gone still returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "channel-id|team-id|outbound:my-conn",
			Submission: map[string]any{"reason": "test"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channel-request/deny-submit", submitReq, "admin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		r := httptest.NewRequest(http.MethodPost, "/api/v1/channel-request/deny-submit", strings.NewReader("{bad"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("GetUser failure returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		api.On("GetUser", "bad-user").Return(nil, &mmModel.AppError{Message: "not found"})

		submitReq := mmModel.SubmitDialogRequest{
			UserId: "bad-user",
			State:  "channel-id|team-id|outbound:my-conn",
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channel-request/deny-submit", submitReq, "bad-user")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("GetChannelConnectionRequest error returns 200", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, errors.New("kv store error")
		}

		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "channel-id|team-id|outbound:my-conn",
			Submission: map[string]any{"reason": "test"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channel-request/deny-submit", submitReq, "admin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("DeleteChannelConnectionRequest error still completes", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = channelRequestModeConfig()

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "chanadmin-id",
				TeamID:      "team-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1"},
			}, nil
		}
		kvs.deleteChannelConnectionRequestFn = func(channelID, connKey string) error {
			return errors.New("delete failed")
		}

		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{}, nil)
		api.On("GetDirectChannel", "bot-user-id", "chanadmin-id").Return(&mmModel.Channel{Id: "dm"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		submitReq := mmModel.SubmitDialogRequest{
			UserId:     "admin-id",
			State:      "channel-id|team-id|outbound:my-conn",
			Submission: map[string]any{"reason": "bad request"},
		}
		r := makeAuthRequest(t, http.MethodPost, "/api/v1/channel-request/deny-submit", submitReq, "admin-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// ============================================================
// TestCancelPendingChannelConnectionRequest
// ============================================================

func TestCancelPendingChannelConnectionRequest(t *testing.T) {
	t.Run("cancels existing request and updates posts", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{
				RequesterID: "chanadmin-id",
				TeamID:      "team-id",
				ChannelID:   "channel-id",
				ConnKey:     "outbound:my-conn",
				PostIDs:     []string{"dm-post-1", "dm-post-2"},
			}, nil
		}

		var deleted bool
		kvs.deleteChannelConnectionRequestFn = func(channelID, connKey string) error {
			deleted = true
			return nil
		}

		api.On("GetPost", "dm-post-1").Return(&mmModel.Post{Id: "dm-post-1", Message: "old"}, nil)
		api.On("GetPost", "dm-post-2").Return(&mmModel.Post{Id: "dm-post-2", Message: "old"}, nil)
		var updatedPosts []*mmModel.Post
		api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			updatedPosts = append(updatedPosts, args.Get(0).(*mmModel.Post))
		}).Return(&mmModel.Post{}, nil)

		api.On("GetDirectChannel", "bot-user-id", "chanadmin-id").Return(&mmModel.Channel{Id: "req-dm"}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{Id: "notify-post"}, nil)

		p.cancelPendingChannelConnectionRequest("channel-id", "outbound:my-conn")

		assert.True(t, deleted)
		require.Len(t, updatedPosts, 2)
		assert.Contains(t, updatedPosts[0].Message, "Cancelled (connection was unlinked)")
	})

	t.Run("no pending request is a no-op", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}

		// Should not panic or call any other APIs.
		p.cancelPendingChannelConnectionRequest("channel-id", "outbound:my-conn")
	})

	t.Run("KV get error logs warning and returns", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, errors.New("kv failure")
		}

		// Should not panic.
		p.cancelPendingChannelConnectionRequest("channel-id", "outbound:my-conn")
	})
}
