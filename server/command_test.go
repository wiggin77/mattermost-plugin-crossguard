package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

func TestIsTeamAdminOrSystemAdmin(t *testing.T) {
	t.Run("system admin always allowed", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(true)})

		api.On("GetUser", "sysadmin").Return(&mmModel.User{
			Roles: mmModel.SystemAdminRoleId,
		}, nil)

		assert.True(t, p.isTeamAdminOrSystemAdmin("sysadmin", "team-id"))
	})

	t.Run("team admin allowed when unrestricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(false)})

		api.On("GetUser", "teamadmin").Return(&mmModel.User{
			Roles: mmModel.TeamAdminRoleId,
		}, nil)
		api.On("GetTeamMember", "team-id", "teamadmin").Return(&mmModel.TeamMember{
			SchemeAdmin: true,
		}, nil)

		assert.True(t, p.isTeamAdminOrSystemAdmin("teamadmin", "team-id"))
	})

	t.Run("team admin blocked when restricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(true)})

		api.On("GetUser", "teamadmin").Return(&mmModel.User{
			Roles: mmModel.TeamAdminRoleId,
		}, nil)

		assert.False(t, p.isTeamAdminOrSystemAdmin("teamadmin", "team-id"))
	})

	t.Run("regular user blocked when unrestricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{})

		api.On("GetUser", "user").Return(&mmModel.User{
			Roles: mmModel.SystemUserRoleId,
		}, nil)
		api.On("GetTeamMember", "team-id", "user").Return(&mmModel.TeamMember{
			SchemeAdmin: false,
		}, nil)

		assert.False(t, p.isTeamAdminOrSystemAdmin("user", "team-id"))
	})

	t.Run("nil config defaults to unrestricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		api.On("GetUser", "teamadmin").Return(&mmModel.User{
			Roles: mmModel.TeamAdminRoleId,
		}, nil)
		api.On("GetTeamMember", "team-id", "teamadmin").Return(&mmModel.TeamMember{
			SchemeAdmin: true,
		}, nil)

		assert.True(t, p.isTeamAdminOrSystemAdmin("teamadmin", "team-id"))
	})
}

func TestIsChannelAdminOrHigher(t *testing.T) {
	t.Run("channel admin allowed when unrestricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(false)})

		api.On("GetChannelMember", "chan-id", "chanadmin").Return(&mmModel.ChannelMember{
			SchemeAdmin: true,
		}, nil)

		assert.True(t, p.isChannelAdminOrHigher("chanadmin", "chan-id", "team-id"))
	})

	t.Run("channel admin blocked when restricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(true)})

		api.On("GetUser", "chanadmin").Return(&mmModel.User{
			Roles: mmModel.SystemUserRoleId,
		}, nil)

		assert.False(t, p.isChannelAdminOrHigher("chanadmin", "chan-id", "team-id"))
	})

	t.Run("system admin passes when restricted via channel path", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(true)})

		api.On("GetUser", "sysadmin").Return(&mmModel.User{
			Roles: mmModel.SystemAdminRoleId,
		}, nil)

		assert.True(t, p.isChannelAdminOrHigher("sysadmin", "chan-id", "team-id"))
	})

	t.Run("team admin passes channel check when unrestricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(false)})

		api.On("GetChannelMember", "chan-id", "teamadmin").Return(&mmModel.ChannelMember{
			SchemeAdmin: false,
		}, nil)
		api.On("GetUser", "teamadmin").Return(&mmModel.User{
			Roles: mmModel.TeamAdminRoleId,
		}, nil)
		api.On("GetTeamMember", "team-id", "teamadmin").Return(&mmModel.TeamMember{
			SchemeAdmin: true,
		}, nil)

		assert.True(t, p.isChannelAdminOrHigher("teamadmin", "chan-id", "team-id"))
	})

	t.Run("team admin blocked via channel path when restricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(true)})

		api.On("GetUser", "teamadmin").Return(&mmModel.User{
			Roles: mmModel.TeamAdminRoleId,
		}, nil)
		// Should not even reach GetChannelMember when restricted
		api.On("GetChannelMember", mock.Anything, mock.Anything).Unset()

		assert.False(t, p.isChannelAdminOrHigher("teamadmin", "chan-id", "team-id"))
	})
}

// addCmdLogMocks registers permissive log mocks on the given API so that
// LogInfo, LogWarn, LogError, and LogDebug calls never panic.
func addCmdLogMocks(api *plugintest.API) {
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
}

// singleOutboundConfig returns a configuration with one outbound connection named "high".
func singleOutboundConfig() *configuration {
	return &configuration{
		OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
	}
}

// multiConnectionConfig returns a configuration with one outbound and one inbound connection.
func multiConnectionConfig() *configuration {
	return &configuration{
		OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.out"}}]`,
		InboundConnections:  `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.in"}}]`,
	}
}

func TestExecuteCommand(t *testing.T) {
	t.Run("no subcommand returns help hint", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		args := &mmModel.CommandArgs{Command: "/crossguard"}
		resp, appErr := p.ExecuteCommand(nil, args)
		assert.Nil(t, appErr)
		assert.Contains(t, resp.Text, "help")
	})

	t.Run("unknown subcommand returns error", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		args := &mmModel.CommandArgs{Command: "/crossguard foobar"}
		resp, appErr := p.ExecuteCommand(nil, args)
		assert.Nil(t, appErr)
		assert.Contains(t, resp.Text, "Unknown subcommand")
		assert.Contains(t, resp.Text, "foobar")
	})

	t.Run("dispatches help", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		args := &mmModel.CommandArgs{Command: "/crossguard help"}
		resp, appErr := p.ExecuteCommand(nil, args)
		assert.Nil(t, appErr)
		assert.Contains(t, resp.Text, "Cross Guard Help")
	})

	t.Run("dispatches init-team", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp, appErr := p.ExecuteCommand(nil, args)
		assert.Nil(t, appErr)
		assert.Contains(t, resp.Text, "permissions")
	})
}

func TestExecuteInitTeam(t *testing.T) {
	t.Run("permission denied for non-admin", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Contains(t, resp.Text, "don't have permissions")
		assert.Equal(t, mmModel.CommandResponseTypeEphemeral, resp.ResponseType)
	})

	t.Run("no connections configured", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Contains(t, resp.Text, "No connections configured")
	})

	t.Run("single connection auto-selects happy path", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		api.On("GetTeam", "team-id").Return(team, nil)

		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		// Start with no team connections so the init succeeds as new
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}
		kvs.addTeamConnectionFn = func(teamID string, conn store.TeamConnection) error {
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Empty(t, resp.Text)
	})

	t.Run("explicit connection name happy path", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		api.On("GetTeam", "team-id").Return(team, nil)

		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}
		kvs.addTeamConnectionFn = func(teamID string, conn store.TeamConnection) error {
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team outbound:high",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Empty(t, resp.Text)
	})

	t.Run("multiple connections without name opens dialog", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("OpenInteractiveDialog", mock.Anything).Return(nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-team",
			UserId:    "admin-id",
			TeamId:    "team-id",
			TriggerId: "trigger-id",
		}
		resp := p.executeInitTeam(args)
		assert.Empty(t, resp.Text)
		api.AssertCalled(t, "OpenInteractiveDialog", mock.Anything)
	})

	t.Run("invalid connection name", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team outbound:nonexistent",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Contains(t, resp.Text, "connection not found")
		assert.Contains(t, resp.Text, "Available connections")
	})

	t.Run("already linked returns message", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		api.On("GetTeam", "team-id").Return(team, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team outbound:high",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Contains(t, resp.Text, "already linked")
	})
}

func TestExecuteInitChannel(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetChannelMember", "chan-id", "user-id").Return(&mmModel.ChannelMember{SchemeAdmin: false}, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel",
			UserId:    "user-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "channel admin")
	})

	t.Run("team not initialized", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		// Override to return empty team connections (team not initialized)
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel outbound:high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "must be initialized first")
	})

	t.Run("connection not linked to team", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel outbound:high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "not linked to this team")
	})

	t.Run("happy path", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}
		kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		channel := &mmModel.Channel{Id: "chan-id", Name: "test-channel", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("UpdateChannel", mock.Anything).Return(&mmModel.Channel{}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel outbound:high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Empty(t, resp.Text)
	})

	t.Run("already linked", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}
		kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		channel := &mmModel.Channel{Id: "chan-id", Name: "test-channel", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel outbound:high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "already linked")
	})

	t.Run("no connections configured", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "No connections configured")
	})
}

func TestExecuteInitChannelRequest(t *testing.T) {
	t.Run("channel admin in request mode submits request", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		boolTrue := true
		p.setConfiguration(&configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowTeamAdminRequests:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
			OutboundConnections:       `[{"name":"high","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		})

		// Channel admin user (not team admin, not sysadmin).
		chanadmin := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		api.On("GetUser", "chanadmin-id").Return(chanadmin, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "chanadmin-id", SchemeAdmin: false,
		}, nil)
		api.On("GetChannelMember", "chan-id", "chanadmin-id").Return(&mmModel.ChannelMember{
			ChannelId: "chan-id", UserId: "chanadmin-id", SchemeAdmin: true,
		}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}
		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return nil, nil
		}
		kvs.createChannelConnectionRequestFn = func(channelID, connKey string, req *store.ConnectionRequest) (bool, error) {
			return true, nil
		}

		api.On("GetChannel", "chan-id").Return(&mmModel.Channel{
			Id: "chan-id", Name: "test-channel", DisplayName: "Test Channel", TeamId: "team-id",
		}, nil)
		api.On("GetTeam", "team-id").Return(&mmModel.Team{
			Id: "team-id", Name: "test", DisplayName: "Test Team",
		}, nil)

		// getTeamAdmins mocks
		api.On("GetTeamMembers", "team-id", 0, 200).Return([]*mmModel.TeamMember{
			{TeamId: "team-id", UserId: "teamadmin-id", SchemeAdmin: true},
		}, nil)
		api.On("GetTeamMembers", "team-id", 1, 200).Return([]*mmModel.TeamMember{}, nil)
		api.On("GetUser", "teamadmin-id").Return(&mmModel.User{Id: "teamadmin-id", Username: "teamadmin"}, nil)

		api.On("GetDirectChannel", "bot-user-id", mock.Anything).Return(&mmModel.Channel{Id: "dm-id"}, nil)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{Id: "post-id"}, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel outbound:high",
			UserId:    "chanadmin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "submitted for team admin approval")
	})

	t.Run("team not initialized returns error", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		boolTrue := true
		p.setConfiguration(&configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowTeamAdminRequests:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
			OutboundConnections:       `[{"name":"high","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		})

		chanadmin := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		api.On("GetUser", "chanadmin-id").Return(chanadmin, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "chanadmin-id", SchemeAdmin: false,
		}, nil)
		api.On("GetChannelMember", "chan-id", "chanadmin-id").Return(&mmModel.ChannelMember{
			ChannelId: "chan-id", UserId: "chanadmin-id", SchemeAdmin: true,
		}, nil)

		// No team connections (team not initialized).
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel",
			UserId:    "chanadmin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "must be initialized first")
	})

	t.Run("multiple connections opens dialog", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		boolTrue := true
		p.setConfiguration(&configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowTeamAdminRequests:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
			OutboundConnections:       `[{"name":"high","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.out"}}]`,
			InboundConnections:        `[{"name":"high","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.in"}}]`,
		})

		chanadmin := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		api.On("GetUser", "chanadmin-id").Return(chanadmin, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{
			TeamId: "team-id", UserId: "chanadmin-id", SchemeAdmin: false,
		}, nil)
		api.On("GetChannelMember", "chan-id", "chanadmin-id").Return(&mmModel.ChannelMember{
			ChannelId: "chan-id", UserId: "chanadmin-id", SchemeAdmin: true,
		}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "inbound", Connection: "high"},
			}, nil
		}

		api.On("OpenInteractiveDialog", mock.Anything).Return(nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel",
			UserId:    "chanadmin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
			TriggerId: "trigger-id",
		}
		resp := p.executeInitChannel(args)
		assert.Empty(t, resp.Text)
		api.AssertCalled(t, "OpenInteractiveDialog", mock.Anything)
	})

	t.Run("GetUser error returns failure", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		boolTrue := true
		p.setConfiguration(&configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowTeamAdminRequests:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
			OutboundConnections:       `[{"name":"high","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		})

		chanadmin := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		// Permission check calls (isTeamAdminOrSystemAdmin, isTeamAdminInRequestMode,
		// isChannelAdminInRequestMode) each call GetUser. The 4th call is inside
		// executeInitChannelRequest, which we want to fail.
		api.On("GetUser", "chanadmin-id").Return(chanadmin, nil).Times(3)
		api.On("GetUser", "chanadmin-id").Return(nil, &mmModel.AppError{Message: "not found"}).Once()
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		api.On("GetChannelMember", "chan-id", "chanadmin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel",
			UserId:    "chanadmin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "Failed to look up user")
	})

	t.Run("GetTeamConnections error returns failure", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		boolTrue := true
		p.setConfiguration(&configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowTeamAdminRequests:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
			OutboundConnections:       `[{"name":"high","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		})

		chanadmin := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		api.On("GetUser", "chanadmin-id").Return(chanadmin, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		api.On("GetChannelMember", "chan-id", "chanadmin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, errors.New("store error")
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel",
			UserId:    "chanadmin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "Failed to check team connections")
	})

	t.Run("bad connection name returns available connections", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		boolTrue := true
		p.setConfiguration(&configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowTeamAdminRequests:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
			OutboundConnections:       `[{"name":"high","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		})

		chanadmin := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		api.On("GetUser", "chanadmin-id").Return(chanadmin, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		api.On("GetChannelMember", "chan-id", "chanadmin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel outbound:nonexistent",
			UserId:    "chanadmin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "Available connections")
	})

	t.Run("create request error returns error message", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		boolTrue := true
		p.setConfiguration(&configuration{
			RestrictToSystemAdmins:    &boolTrue,
			AllowTeamAdminRequests:    &boolTrue,
			AllowChannelAdminRequests: &boolTrue,
			OutboundConnections:       `[{"name":"high","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		})

		chanadmin := &mmModel.User{Id: "chanadmin-id", Username: "chanadmin"}
		api.On("GetUser", "chanadmin-id").Return(chanadmin, nil)
		api.On("GetTeamMember", "team-id", "chanadmin-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
		api.On("GetChannelMember", "chan-id", "chanadmin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}
		// Already pending request
		kvs.getChannelConnectionRequestFn = func(channelID, connKey string) (*store.ConnectionRequest, error) {
			return &store.ConnectionRequest{RequesterID: "other-user"}, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel outbound:high",
			UserId:    "chanadmin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "already pending")
	})
}

func TestExecuteTeardownTeam(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard teardown-team",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeTeardownTeam(args)
		assert.Contains(t, resp.Text, "don't have permissions")
	})

	t.Run("no connections linked", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard teardown-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeTeardownTeam(args)
		assert.Contains(t, resp.Text, "No connections are linked")
	})

	t.Run("happy path single connection", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		api.On("GetTeam", "team-id").Return(team, nil)

		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		conn := store.TeamConnection{Direction: "outbound", Connection: "high"}
		removed := false
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			if removed {
				return nil, nil
			}
			return []store.TeamConnection{conn}, nil
		}
		kvs.removeTeamConnectionFn = func(teamID string, c store.TeamConnection) error {
			removed = true
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard teardown-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeTeardownTeam(args)
		assert.Empty(t, resp.Text)
		assert.True(t, removed)
	})

	t.Run("multiple connections without name opens dialog", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("OpenInteractiveDialog", mock.Anything).Return(nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "inbound", Connection: "high"},
			}, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard teardown-team",
			UserId:    "admin-id",
			TeamId:    "team-id",
			TriggerId: "trigger-id",
		}
		resp := p.executeTeardownTeam(args)
		assert.Empty(t, resp.Text)
		api.AssertCalled(t, "OpenInteractiveDialog", mock.Anything)
	})
}

func TestExecuteTeardownChannel(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetChannelMember", "chan-id", "user-id").Return(&mmModel.ChannelMember{SchemeAdmin: false}, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard teardown-channel",
			UserId:    "user-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeTeardownChannel(args)
		assert.Contains(t, resp.Text, "channel admin")
	})

	t.Run("no connections linked", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard teardown-channel",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeTeardownChannel(args)
		assert.Contains(t, resp.Text, "No connections are linked")
	})

	t.Run("happy path single connection", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		channel := &mmModel.Channel{Id: "chan-id", Name: "test-channel", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("UpdateChannel", mock.Anything).Return(&mmModel.Channel{}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		conn := store.TeamConnection{Direction: "outbound", Connection: "high"}
		removed := false
		kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
			if removed {
				return nil, nil
			}
			return []store.TeamConnection{conn}, nil
		}
		kvs.removeChannelConnectionFn = func(channelID string, c store.TeamConnection) error {
			removed = true
			return nil
		}
		kvs.deleteChannelConnectionsFn = func(channelID string) error {
			return nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard teardown-channel",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeTeardownChannel(args)
		assert.Empty(t, resp.Text)
		assert.True(t, removed)
	})
}

func TestExecuteResetPrompt(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard reset-prompt high",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeResetPrompt(args)
		assert.Contains(t, resp.Text, "team admin or system admin")
	})

	t.Run("missing connection name", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard reset-prompt",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeResetPrompt(args)
		assert.Contains(t, resp.Text, "Usage")
	})

	t.Run("no prompt found", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard reset-prompt high",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeResetPrompt(args)
		assert.Contains(t, resp.Text, "No prompt found")
	})

	t.Run("happy path clears prompt", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: "pending", PostID: "post-1"}, nil
		}
		deleted := false
		kvs.deleteConnectionPromptFn = func(teamID, connName string) error {
			deleted = true
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard reset-prompt high",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeResetPrompt(args)
		assert.Contains(t, resp.Text, "cleared")
		assert.True(t, deleted)
	})
}

func TestExecuteResetChannelPrompt(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard reset-channel-prompt high",
			UserId:    "user-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeResetChannelPrompt(args)
		assert.Contains(t, resp.Text, "team admin or system admin")
	})

	t.Run("missing connection name", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard reset-channel-prompt",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeResetChannelPrompt(args)
		assert.Contains(t, resp.Text, "Usage")
	})

	t.Run("no prompt found", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard reset-channel-prompt high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeResetChannelPrompt(args)
		assert.Contains(t, resp.Text, "No channel prompt found")
	})

	t.Run("happy path clears prompt", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: "blocked", PostID: "post-2"}, nil
		}
		deleted := false
		kvs.deleteChannelConnectionPromptFn = func(channelID, connName string) error {
			deleted = true
			return nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard reset-channel-prompt high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeResetChannelPrompt(args)
		assert.Contains(t, resp.Text, "cleared")
		assert.True(t, deleted)
	})
}

func TestExecuteRewriteTeam(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team high remote-team",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "team admin or system admin")
	})

	t.Run("no inbound connections", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "No inbound connections")
	})

	t.Run("single inbound auto-selects and sets rewrite", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "high"}}, nil
		}
		rewriteSet := false
		kvs.setTeamRewriteIndexFn = func(connName, remoteTeamName, localTeamID string) error {
			rewriteSet = true
			assert.Equal(t, "high", connName)
			assert.Equal(t, "remote-team", remoteTeamName)
			assert.Equal(t, "team-id", localTeamID)
			return nil
		}
		kvs.setTeamConnectionsFn = func(teamID string, conns []store.TeamConnection) error {
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team high remote-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "Rewrite set")
		assert.Contains(t, resp.Text, "remote-team")
		assert.True(t, rewriteSet)
	})

	t.Run("clear rewrite happy path", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "high", RemoteTeamName: "old-remote"}}, nil
		}
		kvs.setTeamConnectionsFn = func(teamID string, conns []store.TeamConnection) error {
			return nil
		}
		indexDeleted := false
		kvs.deleteTeamRewriteIndexFn = func(connName, remoteTeamName string) error {
			indexDeleted = true
			assert.Equal(t, "old-remote", remoteTeamName)
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team high",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "Rewrite cleared")
		assert.True(t, indexDeleted)
	})

	t.Run("multiple inbound connections require name", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "inbound", Connection: "alpha"},
				{Direction: "inbound", Connection: "beta"},
			}, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "Multiple inbound connections")
	})

	t.Run("inbound connection name not found", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team nonexistent remote-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "not linked to this team")
	})

	t.Run("set rewrite index error", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "high"}}, nil
		}
		kvs.setTeamRewriteIndexFn = func(connName, remoteTeamName, localTeamID string) error {
			return fmt.Errorf("conflict: rewrite already exists")
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team high remote-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "conflict")
	})
}

func TestExecuteStatus(t *testing.T) {
	t.Run("system admin gets global status", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getInitializedTeamIDsFn = func() ([]string, error) {
			return []string{"team-id"}, nil
		}
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		team := &mmModel.Team{Id: "team-id", Name: "test-team", DisplayName: "Test Team"}
		api.On("GetTeam", "team-id").Return(team, nil)
		api.On("GetChannel", "chan-id").Return(&mmModel.Channel{Id: "chan-id", Name: "test-chan", DisplayName: "Test Chan"}, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard status",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeStatus(args)
		assert.Contains(t, resp.Text, "Cross Guard Status")
		assert.Contains(t, resp.Text, "Initialized Teams")
	})

	t.Run("regular user gets team status", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-team", DisplayName: "Test Team"}
		api.On("GetTeam", "team-id").Return(team, nil)
		api.On("GetChannel", "chan-id").Return(&mmModel.Channel{Id: "chan-id", Name: "test-chan", DisplayName: "Test Chan"}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard status",
			UserId:    "user-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeStatus(args)
		assert.Contains(t, resp.Text, "Cross Guard Status")
		assert.NotContains(t, resp.Text, "Initialized Teams")
	})

	t.Run("GetUser failure", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		api.On("GetUser", "bad-id").Return(nil, &mmModel.AppError{Message: "not found"})

		args := &mmModel.CommandArgs{
			Command:   "/crossguard status",
			UserId:    "bad-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeStatus(args)
		assert.Contains(t, resp.Text, "Failed to look up user")
	})
}

func TestExecuteHelp(t *testing.T) {
	t.Run("returns non-empty help text", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPluginWithRouter(api)
		resp := p.executeHelp()
		assert.Contains(t, resp.Text, "Cross Guard Help")
		assert.Equal(t, mmModel.CommandResponseTypeEphemeral, resp.ResponseType)
	})

	t.Run("help contains all commands", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPluginWithRouter(api)
		resp := p.executeHelp()
		assert.Contains(t, resp.Text, "init-team")
		assert.Contains(t, resp.Text, "init-channel")
		assert.Contains(t, resp.Text, "teardown-team")
		assert.Contains(t, resp.Text, "teardown-channel")
		assert.Contains(t, resp.Text, "reset-prompt")
		assert.Contains(t, resp.Text, "reset-channel-prompt")
		assert.Contains(t, resp.Text, "rewrite-team")
		assert.Contains(t, resp.Text, "status")
	})
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestFileTransferLabel(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		assert.Equal(t, "files off", fileTransferLabel(false, "", ""))
	})
	t.Run("enabled no filter", func(t *testing.T) {
		assert.Equal(t, "files on", fileTransferLabel(true, "", ""))
	})
	t.Run("allow mode", func(t *testing.T) {
		assert.Equal(t, "files on, allow: .pdf,.docx", fileTransferLabel(true, "allow", ".pdf,.docx"))
	})
	t.Run("deny mode", func(t *testing.T) {
		assert.Equal(t, "files on, deny: .exe", fileTransferLabel(true, "deny", ".exe"))
	})
}

func TestConnectionLabel(t *testing.T) {
	t.Run("json format", func(t *testing.T) {
		label := connectionLabel("json", true, "", "")
		assert.Equal(t, "files on", label)
	})
	t.Run("xml format", func(t *testing.T) {
		label := connectionLabel("xml", false, "", "")
		assert.Equal(t, "xml, files off", label)
	})
	t.Run("empty format defaults to no prefix", func(t *testing.T) {
		label := connectionLabel("", true, "allow", ".pdf")
		assert.Equal(t, "files on, allow: .pdf", label)
	})
}

func TestFileTransferLabelEmoji(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		assert.Equal(t, ":x: Off", fileTransferLabelEmoji(false, "", ""))
	})
	t.Run("enabled no filter", func(t *testing.T) {
		assert.Equal(t, ":white_check_mark: On", fileTransferLabelEmoji(true, "", ""))
	})
	t.Run("allow mode", func(t *testing.T) {
		assert.Equal(t, ":white_check_mark: On (allow: .pdf)", fileTransferLabelEmoji(true, "allow", ".pdf"))
	})
	t.Run("deny mode", func(t *testing.T) {
		assert.Equal(t, ":white_check_mark: On (deny: .exe)", fileTransferLabelEmoji(true, "deny", ".exe"))
	})
}

func TestGetAutocompleteData_Structure(t *testing.T) {
	data := getAutocompleteData()
	assert.Equal(t, commandTrigger, data.Trigger)
	subcommands := make(map[string]bool)
	for _, sub := range data.SubCommands {
		subcommands[sub.Trigger] = true
	}
	assert.True(t, subcommands["init-team"])
	assert.True(t, subcommands["init-channel"])
	assert.True(t, subcommands["teardown-team"])
	assert.True(t, subcommands["teardown-channel"])
	assert.True(t, subcommands["status"])
	assert.True(t, subcommands["help"])
	assert.True(t, subcommands["reset-prompt"])
	assert.True(t, subcommands["reset-channel-prompt"])
	assert.True(t, subcommands["rewrite-team"])
}

// ---------------------------------------------------------------------------
// Additional command edge-case tests (new)
// ---------------------------------------------------------------------------

func TestExecuteRewriteTeam_DeleteFlow(t *testing.T) {
	api := &plugintest.API{}
	addCmdLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.setConfiguration(defaultTestConfig())

	adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
	api.On("GetUser", "admin-id").Return(adminUser, nil)

	// Team has inbound connection with an existing remote team name.
	kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{
			{Direction: "inbound", Connection: "high", RemoteTeamName: "old-remote"},
		}, nil
	}

	var deletedConn, deletedRemote string
	kvs.deleteTeamRewriteIndexFn = func(connName, remoteTeamName string) error {
		deletedConn = connName
		deletedRemote = remoteTeamName
		return nil
	}
	kvs.setTeamConnectionsFn = func(teamID string, conns []store.TeamConnection) error {
		return nil
	}

	tsChannel := &mmModel.Channel{Id: "ts-id"}
	api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
	api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

	// Command with no remote team name (clear rewrite).
	args := &mmModel.CommandArgs{
		Command: "/crossguard rewrite-team high",
		UserId:  "admin-id",
		TeamId:  "team-id",
	}
	resp := p.executeRewriteTeam(args)
	assert.Contains(t, resp.Text, "Rewrite cleared")
	assert.Equal(t, "high", deletedConn)
	assert.Equal(t, "old-remote", deletedRemote)
}

func TestExecuteInitTeam_SingleAutoSelect(t *testing.T) {
	api := &plugintest.API{}
	addCmdLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.setConfiguration(singleOutboundConfig())

	adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
	api.On("GetUser", "admin-id").Return(adminUser, nil)

	team := &mmModel.Team{Id: "team-id", Name: "test-team"}
	api.On("GetTeam", "team-id").Return(team, nil)

	tsChannel := &mmModel.Channel{Id: "ts-id"}
	api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
	api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

	kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
		return nil, nil
	}
	kvs.addTeamConnectionFn = func(teamID string, conn store.TeamConnection) error {
		return nil
	}

	// No connection name specified, single connection should auto-select.
	args := &mmModel.CommandArgs{
		Command: "/crossguard init-team",
		UserId:  "admin-id",
		TeamId:  "team-id",
	}
	resp := p.executeInitTeam(args)
	// Empty text means success (announcement posted via CreatePost).
	assert.Empty(t, resp.Text)
	// OpenInteractiveDialog should NOT be called for single connection.
	api.AssertNotCalled(t, "OpenInteractiveDialog", mock.Anything)
}

func TestDialogSubmission_InvalidPayload(t *testing.T) {
	api := &plugintest.API{}
	addCmdLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)
	p.setConfiguration(defaultTestConfig())

	// POST malformed JSON to the dialog endpoint.
	r := makeAuthRequest(t, http.MethodPost, "/api/v1/dialog/select-connection", nil, "admin-id")
	r.Body = http.NoBody
	w := httptest.NewRecorder()

	p.router.ServeHTTP(w, r)

	// Should return 200 with an error in the response body (Mattermost dialog pattern),
	// or a non-panic HTTP response.
	assert.NotEqual(t, http.StatusInternalServerError, w.Code)
}

func TestExecuteStatus_RegularUser(t *testing.T) {
	api := &plugintest.API{}
	addCmdLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.setConfiguration(defaultTestConfig())

	regularUser := &mmModel.User{Id: "user-id", Username: "alice", Roles: mmModel.SystemUserRoleId}
	api.On("GetUser", "user-id").Return(regularUser, nil)
	api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

	team := &mmModel.Team{Id: "team-id", Name: "test-team", DisplayName: "Test Team"}
	api.On("GetTeam", "team-id").Return(team, nil)

	channel := &mmModel.Channel{Id: "chan-id", Name: "test-channel", DisplayName: "Test Channel"}
	api.On("GetChannel", "chan-id").Return(channel, nil)

	kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
	}
	kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
		return nil, nil
	}

	args := &mmModel.CommandArgs{
		Command:   "/crossguard status",
		UserId:    "user-id",
		TeamId:    "team-id",
		ChannelId: "chan-id",
	}
	resp, appErr := p.ExecuteCommand(nil, args)
	assert.Nil(t, appErr)
	// Regular user should see team-scoped status, not system admin global view.
	assert.Contains(t, resp.Text, "Cross Guard Status")
	assert.Contains(t, resp.Text, "Test Team")
	// Should NOT contain "Initialized Teams" (that is system admin only).
	assert.NotContains(t, resp.Text, "Initialized Teams")
}

func TestRegisterCommand(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		api.On("RegisterCommand", mock.AnythingOfType("*model.Command")).Return(nil)

		err := p.registerCommand()
		assert.Nil(t, err)
		api.AssertCalled(t, "RegisterCommand", mock.AnythingOfType("*model.Command"))
	})

	t.Run("error", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		api.On("RegisterCommand", mock.AnythingOfType("*model.Command")).Return(fmt.Errorf("registration failed"))

		err := p.registerCommand()
		assert.EqualError(t, err, "registration failed")
	})
}

func TestExecuteTeardownChannel_GetUserError(t *testing.T) {
	api := &plugintest.API{}
	addCmdLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)
	p.setConfiguration(&configuration{})

	// isChannelAdminOrHigher: config not restricted, channel member SchemeAdmin true
	// => returns true without calling GetUser.
	api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)
	// Line 522: GetUser returns error.
	api.On("GetUser", "admin-id").Return(nil, &mmModel.AppError{Message: "user lookup failed"})

	args := &mmModel.CommandArgs{
		Command:   "/crossguard teardown-channel",
		UserId:    "admin-id",
		TeamId:    "team-id",
		ChannelId: "chan-id",
	}
	resp := p.executeTeardownChannel(args)
	assert.Contains(t, resp.Text, "Failed to look up user")
}

func TestExecuteTeardownChannel_GetChannelConnectionsError(t *testing.T) {
	api := &plugintest.API{}
	addCmdLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.setConfiguration(&configuration{})

	adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
	api.On("GetUser", "admin-id").Return(adminUser, nil)
	api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

	kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
		return nil, fmt.Errorf("kv store error")
	}

	args := &mmModel.CommandArgs{
		Command:   "/crossguard teardown-channel",
		UserId:    "admin-id",
		TeamId:    "team-id",
		ChannelId: "chan-id",
	}
	resp := p.executeTeardownChannel(args)
	assert.Contains(t, resp.Text, "Failed to check channel connections")
}

func TestExecuteTeardownChannel_ExplicitNameNotFound(t *testing.T) {
	api := &plugintest.API{}
	addCmdLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.setConfiguration(singleOutboundConfig())

	adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
	api.On("GetUser", "admin-id").Return(adminUser, nil)
	api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

	kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
	}

	args := &mmModel.CommandArgs{
		Command:   "/crossguard teardown-channel nonexistent:conn",
		UserId:    "admin-id",
		TeamId:    "team-id",
		ChannelId: "chan-id",
	}
	resp := p.executeTeardownChannel(args)
	assert.Contains(t, resp.Text, "connection not found")
	assert.Contains(t, resp.Text, "Linked connections")
}

func TestExecuteTeardownChannel_MultipleConnectionsOpensDialog(t *testing.T) {
	api := &plugintest.API{}
	addCmdLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.setConfiguration(multiConnectionConfig())

	adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
	api.On("GetUser", "admin-id").Return(adminUser, nil)
	api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)
	api.On("OpenInteractiveDialog", mock.Anything).Return(nil)

	kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
			{Direction: "inbound", Connection: "high"},
		}, nil
	}

	args := &mmModel.CommandArgs{
		Command:   "/crossguard teardown-channel",
		UserId:    "admin-id",
		TeamId:    "team-id",
		ChannelId: "chan-id",
		TriggerId: "trigger-id",
	}
	resp := p.executeTeardownChannel(args)
	assert.Empty(t, resp.Text)
	api.AssertCalled(t, "OpenInteractiveDialog", mock.Anything)
}

func TestExecuteTeardownChannel_ServiceError(t *testing.T) {
	api := &plugintest.API{}
	addCmdLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.setConfiguration(singleOutboundConfig())

	adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
	api.On("GetUser", "admin-id").Return(adminUser, nil)
	api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

	// GetChannel fails inside teardownChannelForCrossGuard
	api.On("GetChannel", "chan-id").Return(nil, &mmModel.AppError{Message: "channel not found"})

	kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
	}

	args := &mmModel.CommandArgs{
		Command:   "/crossguard teardown-channel",
		UserId:    "admin-id",
		TeamId:    "team-id",
		ChannelId: "chan-id",
	}
	resp := p.executeTeardownChannel(args)
	assert.Contains(t, resp.Text, "channel not found")
}

func TestExecuteCommand_DispatchAllSubcommands(t *testing.T) {
	// Each subcommand is dispatched via ExecuteCommand. We use a regular user
	// so the permission-denied response proves the routing reached the handler.

	// Subcommands that use a simple permission gate (regular user gets denied).
	permGated := []struct {
		name    string
		command string
		expect  string
	}{
		{"init-channel", "/crossguard init-channel", "channel admin"},
		{"teardown-team", "/crossguard teardown-team", "permissions"},
		{"teardown-channel", "/crossguard teardown-channel", "channel admin"},
		{"reset-prompt", "/crossguard reset-prompt", "team admin or system admin"},
		{"reset-channel-prompt", "/crossguard reset-channel-prompt", "team admin or system admin"},
		{"rewrite-team", "/crossguard rewrite-team", "team admin or system admin"},
	}

	for _, tc := range permGated {
		t.Run("dispatches "+tc.name, func(t *testing.T) {
			api := &plugintest.API{}
			addCmdLogMocks(api)
			p, _ := setupTestPluginWithRouter(api)
			p.setConfiguration(&configuration{})

			regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
			api.On("GetUser", "user-id").Return(regularUser, nil)
			api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)
			api.On("GetChannelMember", "chan-id", "user-id").Return(&mmModel.ChannelMember{SchemeAdmin: false}, nil)

			args := &mmModel.CommandArgs{
				Command:   tc.command,
				UserId:    "user-id",
				TeamId:    "team-id",
				ChannelId: "chan-id",
			}
			resp, appErr := p.ExecuteCommand(nil, args)
			assert.Nil(t, appErr)
			assert.NotNil(t, resp)
			assert.Contains(t, resp.Text, tc.expect)
		})
	}

	t.Run("dispatches status", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		// status calls GetUser, then (for non-admin) executeStatusTeam which needs GetTeam.
		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeam", "team-id").Return(&mmModel.Team{Id: "team-id", Name: "test", DisplayName: "Test"}, nil)
		api.On("GetChannel", "chan-id").Return(&mmModel.Channel{Id: "chan-id", Name: "test-chan", DisplayName: "Test Chan"}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}
		kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard status",
			UserId:    "user-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp, appErr := p.ExecuteCommand(nil, args)
		assert.Nil(t, appErr)
		assert.NotNil(t, resp)
		assert.Contains(t, resp.Text, "Cross Guard Status")
	})
}

func TestProviderDetails(t *testing.T) {
	tests := []struct {
		name string
		conn RedactedConnection
		want string
	}{
		{
			name: "azure-queue with blob",
			conn: RedactedConnection{Provider: "azure-queue", QueueName: "q1", BlobContainerName: "c1"},
			want: "queue: q1, blob: c1",
		},
		{
			name: "azure-queue without blob",
			conn: RedactedConnection{Provider: "azure-queue", QueueName: "q1"},
			want: "queue: q1",
		},
		{
			name: "azure-blob",
			conn: RedactedConnection{Provider: "azure-blob", BlobContainerName: "c1"},
			want: "blob: c1",
		},
		{
			name: "nats with all fields",
			conn: RedactedConnection{Provider: "nats", Address: "nats://localhost:4222", Subject: "sub", AuthType: "token"},
			want: "nats://localhost:4222, subject: sub, auth: token",
		},
		{
			name: "empty provider treated as nats",
			conn: RedactedConnection{Provider: "", Address: "nats://x", Subject: "s"},
			want: "nats://x, subject: s",
		},
		{
			name: "unknown provider returns raw name",
			conn: RedactedConnection{Provider: "custom-thing"},
			want: "custom-thing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, providerDetails(tt.conn))
		})
	}
}

// ---------------------------------------------------------------------------
// executeInitTeamRequest
// ---------------------------------------------------------------------------

func TestExecuteInitTeamRequest(t *testing.T) {
	t.Run("GetUser error returns ephemeral", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		api.On("GetUser", "user-id").
			Return(nil, &mmModel.AppError{Message: "user not found"})

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-request",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeamRequest(args)
		assert.Contains(t, resp.Text, "Failed to look up user")
	})

	t.Run("no connections configured returns ephemeral", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		api.On("GetUser", "user-id").
			Return(&mmModel.User{Id: "user-id", Username: "tester"}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-request",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeamRequest(args)
		assert.Contains(t, resp.Text, "No connections configured")
	})

	t.Run("multiple connections without input opens dialog", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		api.On("GetUser", "user-id").
			Return(&mmModel.User{Id: "user-id", Username: "tester"}, nil)
		api.On("OpenInteractiveDialog", mock.Anything).Return(nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-request",
			UserId:    "user-id",
			TeamId:    "team-id",
			TriggerId: "trigger-id",
		}
		resp := p.executeInitTeamRequest(args)
		assert.Empty(t, resp.Text)
		api.AssertCalled(t, "OpenInteractiveDialog", mock.Anything)
	})

	t.Run("invalid connection name returns available connections", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		api.On("GetUser", "user-id").
			Return(&mmModel.User{Id: "user-id", Username: "tester"}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-request badname",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeamRequest(args)
		assert.Contains(t, resp.Text, "connection not found")
		assert.Contains(t, resp.Text, "Available connections")
	})

	t.Run("createConnectionRequest error returns ephemeral", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		api.On("GetUser", "user-id").
			Return(&mmModel.User{Id: "user-id", Username: "tester"}, nil)

		kvs.getConnectionRequestFn = func(_, _ string) (*store.ConnectionRequest, error) {
			return nil, fmt.Errorf("kv error")
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-request",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeamRequest(args)
		assert.Contains(t, resp.Text, "failed to check existing requests")
	})

	t.Run("happy path returns success message", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		api.On("GetUser", "user-id").
			Return(&mmModel.User{Id: "user-id", Username: "tester"}, nil)
		api.On("GetTeam", "team-id").
			Return(&mmModel.Team{Id: "team-id", Name: "test-team", DisplayName: "Test Team"}, nil)
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 0, PerPage: 100,
		}).Return([]*mmModel.User{{Id: "admin1", Username: "admin1"}}, nil)
		api.On("GetUsers", &mmModel.UserGetOptions{
			Role: mmModel.SystemAdminRoleId, Page: 1, PerPage: 100,
		}).Return([]*mmModel.User{}, nil)
		api.On("GetDirectChannel", "bot-user-id", "admin1").
			Return(&mmModel.Channel{Id: "dm-chan-1"}, nil)
		api.On("GetDirectChannel", "bot-user-id", "user-id").
			Return(&mmModel.Channel{Id: "requester-dm"}, nil)
		api.On("CreatePost", mock.Anything).
			Return(&mmModel.Post{Id: "post-1"}, nil)

		kvs.getConnectionRequestFn = func(_, _ string) (*store.ConnectionRequest, error) {
			return nil, nil
		}
		kvs.createConnectionRequestFn = func(_, _ string, _ *store.ConnectionRequest) (bool, error) {
			return true, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-request",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeamRequest(args)
		assert.Contains(t, resp.Text, "has been submitted for system admin approval")
	})
}
