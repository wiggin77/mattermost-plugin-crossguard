package main

import (
	"errors"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

func TestResolveConnectionName(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	t.Run("auto-select single connection", func(t *testing.T) {
		conn, avail, errMsg := p.resolveConnectionName("", []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
		})
		assert.Equal(t, "outbound:high", connKey(conn))
		assert.Equal(t, []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, avail)
		assert.Empty(t, errMsg)
	})

	t.Run("ambiguous with multiple connections", func(t *testing.T) {
		conn, avail, errMsg := p.resolveConnectionName("", []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
			{Direction: "inbound", Connection: "high"},
		})
		assert.Equal(t, store.TeamConnection{}, conn)
		assert.Len(t, avail, 2)
		assert.Contains(t, errMsg, "multiple")
	})

	t.Run("explicit valid name", func(t *testing.T) {
		conn, _, errMsg := p.resolveConnectionName("outbound:high", []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
			{Direction: "inbound", Connection: "high"},
		})
		assert.Equal(t, "outbound:high", connKey(conn))
		assert.Empty(t, errMsg)
	})

	t.Run("explicit invalid name", func(t *testing.T) {
		conn, _, errMsg := p.resolveConnectionName("unknown", []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
		})
		assert.Equal(t, store.TeamConnection{}, conn)
		assert.Contains(t, errMsg, "not found")
	})

	t.Run("no connections configured", func(t *testing.T) {
		conn, _, errMsg := p.resolveConnectionName("", nil)
		assert.Equal(t, store.TeamConnection{}, conn)
		assert.Contains(t, errMsg, "no connections configured")
	})
}

// mockLogCalls registers permissive log and WebSocket mocks on the API.
func mockLogCalls(api *plugintest.API) {
	mockLogOnly(api)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()
}

// mockLogOnly registers permissive log mocks without WebSocket.
// Use this when you need to verify PublishWebSocketEvent explicitly.
func mockLogOnly(api *plugintest.API) {
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
}

// defaultTestConfig returns a configuration with one outbound/inbound "high" connection.
func defaultTestConfig() *configuration {
	return &configuration{
		OutboundConnections: `[{"name":"high","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
		InboundConnections:  `[{"name":"high","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
	}
}

func testUser() *model.User {
	return &model.User{Id: "user-id", Username: "testuser"}
}

func testTeam() *model.Team {
	return &model.Team{Id: "team-id", Name: "test-team", DisplayName: "Test Team"}
}

func testChannel() *model.Channel {
	return &model.Channel{Id: "chan-id", Name: "test-channel", DisplayName: "Test Channel", TeamId: "team-id", Type: model.ChannelTypeOpen}
}

// -------------------------------------------------------------------
// connKey / connectionDisplayNames
// -------------------------------------------------------------------

func TestConnKey(t *testing.T) {
	t.Run("formats direction and connection", func(t *testing.T) {
		tc := store.TeamConnection{Direction: "outbound", Connection: "high"}
		assert.Equal(t, "outbound:high", connKey(tc))
	})

	t.Run("inbound direction", func(t *testing.T) {
		tc := store.TeamConnection{Direction: "inbound", Connection: "low"}
		assert.Equal(t, "inbound:low", connKey(tc))
	})

	t.Run("empty fields", func(t *testing.T) {
		tc := store.TeamConnection{}
		assert.Equal(t, ":", connKey(tc))
	})
}

func TestConnectionDisplayNames(t *testing.T) {
	t.Run("formats multiple connections", func(t *testing.T) {
		conns := []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
			{Direction: "inbound", Connection: "low"},
		}
		names := connectionDisplayNames(conns)
		assert.Equal(t, []string{"outbound:high", "inbound:low"}, names)
	})

	t.Run("empty slice", func(t *testing.T) {
		names := connectionDisplayNames(nil)
		assert.Empty(t, names)
	})
}

func TestTeamTownSquareLink(t *testing.T) {
	team := &model.Team{Id: "t1", Name: "my-team", DisplayName: "My Team"}
	assert.Equal(t, "[**My Team**](/my-team/channels/town-square)", teamTownSquareLink(team))
}

func TestBuildRequestDMMessage(t *testing.T) {
	t.Run("full details", func(t *testing.T) {
		m := map[string]ConnectionConfig{
			"outbound:high": {
				Name:                "high",
				Provider:            "nats",
				MessageFormat:       "xml",
				FileTransferEnabled: true,
				FileFilterMode:      "allow",
				FileFilterTypes:     ".pdf,.docx",
			},
		}
		out := buildRequestDMMessage("@alice (Alice Smith)", "[**My Team**](/my-team/channels/town-square)", "outbound:high", m)
		assert.Contains(t, out, "#### :link: Connection Link Request")
		assert.Contains(t, out, "| **Requested by** | @alice (Alice Smith) |")
		assert.Contains(t, out, "| **Team** | [**My Team**](/my-team/channels/town-square) |")
		assert.Contains(t, out, "| **Connection** | `outbound:high` |")
		assert.Contains(t, out, "| **Direction** | outbound |")
		assert.Contains(t, out, "| **Provider** | nats |")
		assert.Contains(t, out, "| **Message format** | xml |")
		assert.Contains(t, out, "| **File transfer** | Enabled (allow: .pdf,.docx) |")
	})

	t.Run("defaults for empty provider and message format", func(t *testing.T) {
		m := map[string]ConnectionConfig{
			"inbound:low": {Name: "low"},
		}
		out := buildRequestDMMessage("@bob", "[**T**](/t/channels/town-square)", "inbound:low", m)
		assert.Contains(t, out, "| **Direction** | inbound |")
		assert.Contains(t, out, "| **Provider** | nats |")
		assert.Contains(t, out, "| **Message format** | json |")
		assert.Contains(t, out, "| **File transfer** | Disabled |")
	})

	t.Run("file transfer enabled without filter", func(t *testing.T) {
		m := map[string]ConnectionConfig{
			"outbound:x": {Name: "x", FileTransferEnabled: true},
		}
		out := buildRequestDMMessage("@u", "team", "outbound:x", m)
		assert.Contains(t, out, "| **File transfer** | Enabled |")
	})

	t.Run("connection not in map still renders table rows", func(t *testing.T) {
		out := buildRequestDMMessage("@u", "team", "outbound:missing", nil)
		assert.Contains(t, out, "| **Connection** | `outbound:missing` |")
		assert.Contains(t, out, "| **Direction** | outbound |")
		assert.Contains(t, out, "| **Provider** | nats |")
		assert.NotContains(t, out, "**File transfer**")
	})
}

func TestBuildRequestConfirmationMessage(t *testing.T) {
	t.Run("basic confirmation", func(t *testing.T) {
		m := map[string]ConnectionConfig{
			"outbound:high": {
				Name:     "high",
				Provider: "nats",
			},
		}
		out := buildRequestConfirmationMessage("[**T**](/t/channels/town-square)", "outbound:high", m)
		assert.Contains(t, out, "#### :link: Connection Link Request Submitted")
		assert.Contains(t, out, "Your request has been sent to system admins for approval.")
		assert.Contains(t, out, "| **Team** | [**T**](/t/channels/town-square) |")
		assert.Contains(t, out, "| **Connection** | `outbound:high` |")
		assert.Contains(t, out, "| **Direction** | outbound |")
		assert.Contains(t, out, "| **Provider** | nats |")
		assert.NotContains(t, out, "**Requested by**")
	})

	t.Run("file transfer enabled with filter", func(t *testing.T) {
		m := map[string]ConnectionConfig{
			"outbound:high": {
				Name:                "high",
				Provider:            "nats",
				FileTransferEnabled: true,
				FileFilterMode:      "allow",
				FileFilterTypes:     ".pdf",
			},
		}
		out := buildRequestConfirmationMessage("team", "outbound:high", m)
		assert.Contains(t, out, "| **File transfer** | Enabled (allow: .pdf) |")
	})

	t.Run("file transfer disabled", func(t *testing.T) {
		m := map[string]ConnectionConfig{
			"outbound:low": {Name: "low"},
		}
		out := buildRequestConfirmationMessage("team", "outbound:low", m)
		assert.Contains(t, out, "| **File transfer** | Disabled |")
	})
}

// -------------------------------------------------------------------
// addCrossguardHeaderPrefix / removeCrossguardHeaderPrefix
// -------------------------------------------------------------------

func TestCrossguardHeaderPrefix(t *testing.T) {
	t.Run("adds prefix to plain header", func(t *testing.T) {
		result := addCrossguardHeaderPrefix("my header")
		assert.Equal(t, crossguardHeaderPrefix+"my header", result)
	})

	t.Run("no double prefix", func(t *testing.T) {
		prefixed := addCrossguardHeaderPrefix("my header")
		result := addCrossguardHeaderPrefix(prefixed)
		assert.Equal(t, crossguardHeaderPrefix+"my header", result)
	})

	t.Run("adds prefix to empty header", func(t *testing.T) {
		result := addCrossguardHeaderPrefix("")
		assert.Equal(t, crossguardHeaderPrefix, result)
	})

	t.Run("removes prefix", func(t *testing.T) {
		prefixed := addCrossguardHeaderPrefix("my header")
		result := removeCrossguardHeaderPrefix(prefixed)
		assert.Equal(t, "my header", result)
	})

	t.Run("removes prefix from empty header", func(t *testing.T) {
		result := removeCrossguardHeaderPrefix(crossguardHeaderPrefix)
		assert.Equal(t, "", result)
	})

	t.Run("no-op when prefix not present", func(t *testing.T) {
		result := removeCrossguardHeaderPrefix("plain header")
		assert.Equal(t, "plain header", result)
	})
}

// -------------------------------------------------------------------
// redactConnections / redactConnection
// -------------------------------------------------------------------

func TestRedactConnections(t *testing.T) {
	t.Run("strips sensitive NATS fields", func(t *testing.T) {
		outbound := []ConnectionConfig{
			{
				Name:                "high",
				Provider:            ProviderNATS,
				FileTransferEnabled: true,
				FileFilterMode:      "allow",
				FileFilterTypes:     ".pdf",
				MessageFormat:       "json",
				NATS: &NATSProviderConfig{
					Address:  "nats://localhost:4222",
					Subject:  "crossguard.high",
					AuthType: "token",
					Token:    "secret-token",
					Password: "secret-password",
					Username: "admin",
				},
			},
		}
		inbound := []ConnectionConfig{
			{
				Name:     "low",
				Provider: ProviderNATS,
				NATS: &NATSProviderConfig{
					Address:  "nats://localhost:4222",
					Subject:  "crossguard.low",
					AuthType: "none",
				},
			},
		}

		result := redactConnections(outbound, inbound)
		require.Len(t, result, 2)

		// Outbound
		assert.Equal(t, "high", result[0].Name)
		assert.Equal(t, "outbound", result[0].Direction)
		assert.Equal(t, ProviderNATS, result[0].Provider)
		assert.Equal(t, "nats://localhost:4222", result[0].Address)
		assert.Equal(t, "token", result[0].AuthType)
		assert.Equal(t, "crossguard.high", result[0].Subject)
		assert.True(t, result[0].FileTransferEnabled)
		assert.Equal(t, "allow", result[0].FileFilterMode)
		assert.Equal(t, ".pdf", result[0].FileFilterTypes)
		assert.Equal(t, "json", result[0].MessageFormat)

		// Inbound
		assert.Equal(t, "low", result[1].Name)
		assert.Equal(t, "inbound", result[1].Direction)
	})

	t.Run("strips sensitive Azure fields", func(t *testing.T) {
		outbound := []ConnectionConfig{
			{
				Name:     "az-conn",
				Provider: ProviderAzureQueue,
				AzureQueue: &AzureQueueProviderConfig{
					QueueServiceURL:   "https://test.queue.core.windows.net",
					BlobServiceURL:    "https://test.blob.core.windows.net",
					AccountName:       "test",
					AccountKey:        "super-secret",
					QueueName:         "my-queue",
					BlobContainerName: "my-container",
				},
			},
		}

		result := redactConnections(outbound, nil)
		require.Len(t, result, 1)
		assert.Equal(t, "my-queue", result[0].QueueName)
		assert.Empty(t, result[0].Address)
		assert.Empty(t, result[0].Subject)
	})

	t.Run("empty inputs", func(t *testing.T) {
		result := redactConnections(nil, nil)
		assert.Empty(t, result)
	})

	t.Run("preserves safe fields without NATS or Azure config", func(t *testing.T) {
		outbound := []ConnectionConfig{
			{
				Name:          "plain",
				Provider:      ProviderNATS,
				MessageFormat: "xml",
			},
		}
		result := redactConnections(outbound, nil)
		require.Len(t, result, 1)
		assert.Equal(t, "plain", result[0].Name)
		assert.Equal(t, "xml", result[0].MessageFormat)
		assert.Empty(t, result[0].Address)
		assert.Empty(t, result[0].QueueName)
	})
}

// -------------------------------------------------------------------
// initTeamForCrossGuard
// -------------------------------------------------------------------

func TestInitTeamForCrossGuard(t *testing.T) {
	conn := store.TeamConnection{Direction: "outbound", Connection: "high"}

	t.Run("team not found returns 404", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(nil, &model.AppError{Message: "not found"})

		team, alreadyLinked, svcErr := p.initTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, team)
		assert.False(t, alreadyLinked)
		require.NotNil(t, svcErr)
		assert.Equal(t, 404, svcErr.Status)
	})

	t.Run("GetTeamConnections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, errors.New("db error")
		}

		team, alreadyLinked, svcErr := p.initTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, team)
		assert.False(t, alreadyLinked)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("already linked returns true", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{conn}, nil
		}

		team, alreadyLinked, svcErr := p.initTeamForCrossGuard(testUser(), "team-id", conn)
		assert.NotNil(t, team)
		assert.True(t, alreadyLinked)
		assert.Nil(t, svcErr)
	})

	t.Run("AddTeamConnection error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{}, nil
		}
		kvs.addTeamConnectionFn = func(teamID string, c store.TeamConnection) error {
			return errors.New("write error")
		}

		team, alreadyLinked, svcErr := p.initTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, team)
		assert.False(t, alreadyLinked)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("first connection adds to initialized teams list", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		api.On("GetChannelByName", "team-id", model.DefaultChannelName, false).Return(
			&model.Channel{Id: "town-square-id"}, nil,
		)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&model.Post{}, nil)

		callCount := 0
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{}, nil
			}
			return []store.TeamConnection{conn}, nil
		}
		kvs.addTeamConnectionFn = func(teamID string, c store.TeamConnection) error {
			return nil
		}

		addCalled := false
		kvs.addInitializedTeamIDFn = func(id string) error {
			addCalled = true
			assert.Equal(t, "team-id", id)
			return nil
		}

		team, alreadyLinked, svcErr := p.initTeamForCrossGuard(testUser(), "team-id", conn)
		assert.NotNil(t, team)
		assert.False(t, alreadyLinked)
		assert.Nil(t, svcErr)
		assert.True(t, addCalled, "AddInitializedTeamID should have been called")
	})

	t.Run("posts announcement to town-square", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		api.On("GetChannelByName", "team-id", model.DefaultChannelName, false).Return(
			&model.Channel{Id: "town-square-id"}, nil,
		)

		var postedMsg string
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			post := args.Get(0).(*model.Post)
			postedMsg = post.Message
		}).Return(&model.Post{}, nil)

		callCount := 0
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{}, nil
			}
			return []store.TeamConnection{conn}, nil
		}
		kvs.addTeamConnectionFn = func(string, store.TeamConnection) error { return nil }
		kvs.addInitializedTeamIDFn = func(string) error { return nil }

		_, _, svcErr := p.initTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, svcErr)
		assert.Contains(t, postedMsg, "outbound:high")
		assert.Contains(t, postedMsg, "@testuser")
	})

	t.Run("town-square not found does not error", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		api.On("GetChannelByName", "team-id", model.DefaultChannelName, false).Return(
			nil, &model.AppError{Message: "not found"},
		)

		callCount := 0
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{}, nil
			}
			return []store.TeamConnection{conn}, nil
		}
		kvs.addTeamConnectionFn = func(string, store.TeamConnection) error { return nil }
		kvs.addInitializedTeamIDFn = func(string) error { return nil }

		team, alreadyLinked, svcErr := p.initTeamForCrossGuard(testUser(), "team-id", conn)
		assert.NotNil(t, team)
		assert.False(t, alreadyLinked)
		assert.Nil(t, svcErr)
	})

	t.Run("re-read error after add returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)

		callCount := 0
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{}, nil
			}
			return nil, errors.New("re-read error")
		}
		kvs.addTeamConnectionFn = func(string, store.TeamConnection) error { return nil }

		team, alreadyLinked, svcErr := p.initTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, team)
		assert.False(t, alreadyLinked)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("AddInitializedTeamID error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)

		callCount := 0
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{}, nil
			}
			return []store.TeamConnection{conn}, nil
		}
		kvs.addTeamConnectionFn = func(string, store.TeamConnection) error { return nil }
		kvs.addInitializedTeamIDFn = func(string) error { return errors.New("list error") }

		team, alreadyLinked, svcErr := p.initTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, team)
		assert.False(t, alreadyLinked)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("second connection does not call AddInitializedTeamID", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		secondConn := store.TeamConnection{Direction: "inbound", Connection: "high"}

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		api.On("GetChannelByName", "team-id", model.DefaultChannelName, false).Return(
			&model.Channel{Id: "town-square-id"}, nil,
		)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&model.Post{}, nil)

		existingConn := store.TeamConnection{Direction: "outbound", Connection: "high"}
		callCount := 0
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{existingConn}, nil
			}
			return []store.TeamConnection{existingConn, secondConn}, nil
		}
		kvs.addTeamConnectionFn = func(string, store.TeamConnection) error { return nil }

		addCalled := false
		kvs.addInitializedTeamIDFn = func(string) error {
			addCalled = true
			return nil
		}

		team, alreadyLinked, svcErr := p.initTeamForCrossGuard(testUser(), "team-id", secondConn)
		assert.NotNil(t, team)
		assert.False(t, alreadyLinked)
		assert.Nil(t, svcErr)
		assert.False(t, addCalled, "AddInitializedTeamID should not be called when team already has connections")
	})
}

// -------------------------------------------------------------------
// teardownTeamForCrossGuard
// -------------------------------------------------------------------

func TestTeardownTeamForCrossGuard(t *testing.T) {
	conn := store.TeamConnection{Direction: "outbound", Connection: "high"}

	t.Run("team not found returns 404", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(nil, &model.AppError{Message: "not found"})

		team, svcErr := p.teardownTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, team)
		require.NotNil(t, svcErr)
		assert.Equal(t, 404, svcErr.Status)
	})

	t.Run("GetTeamConnections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, errors.New("db error")
		}

		team, svcErr := p.teardownTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, team)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("no existing connections returns team nil error", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{}, nil
		}

		team, svcErr := p.teardownTeamForCrossGuard(testUser(), "team-id", conn)
		assert.NotNil(t, team)
		assert.Nil(t, svcErr)
	})

	t.Run("connection not in list returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "low"}}, nil
		}

		team, svcErr := p.teardownTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, team)
		require.NotNil(t, svcErr)
		assert.Equal(t, 400, svcErr.Status)
		assert.Contains(t, svcErr.Message, "not linked")
	})

	t.Run("RemoveTeamConnection error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{conn}, nil
		}
		kvs.removeTeamConnectionFn = func(string, store.TeamConnection) error {
			return errors.New("remove error")
		}

		team, svcErr := p.teardownTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, team)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("last connection removed calls RemoveInitializedTeamID", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		api.On("GetChannelByName", "team-id", model.DefaultChannelName, false).Return(
			&model.Channel{Id: "town-square-id"}, nil,
		)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&model.Post{}, nil)

		callCount := 0
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{conn}, nil
			}
			return []store.TeamConnection{}, nil
		}
		kvs.removeTeamConnectionFn = func(string, store.TeamConnection) error { return nil }

		removeCalled := false
		kvs.removeInitializedTeamIDFn = func(id string) error {
			removeCalled = true
			assert.Equal(t, "team-id", id)
			return nil
		}

		team, svcErr := p.teardownTeamForCrossGuard(testUser(), "team-id", conn)
		assert.NotNil(t, team)
		assert.Nil(t, svcErr)
		assert.True(t, removeCalled, "RemoveInitializedTeamID should have been called")
	})

	t.Run("posts teardown message", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		api.On("GetChannelByName", "team-id", model.DefaultChannelName, false).Return(
			&model.Channel{Id: "town-square-id"}, nil,
		)

		var postedMsg string
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Run(func(args mock.Arguments) {
			post := args.Get(0).(*model.Post)
			postedMsg = post.Message
		}).Return(&model.Post{}, nil)

		callCount := 0
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{conn}, nil
			}
			return []store.TeamConnection{}, nil
		}
		kvs.removeTeamConnectionFn = func(string, store.TeamConnection) error { return nil }
		kvs.removeInitializedTeamIDFn = func(string) error { return nil }

		_, svcErr := p.teardownTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, svcErr)
		assert.Contains(t, postedMsg, "outbound:high")
		assert.Contains(t, postedMsg, "@testuser")
		assert.Contains(t, postedMsg, "All channel relays")
	})

	t.Run("partial teardown does not remove initialized team", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		otherConn := store.TeamConnection{Direction: "inbound", Connection: "high"}
		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		api.On("GetChannelByName", "team-id", model.DefaultChannelName, false).Return(
			&model.Channel{Id: "town-square-id"}, nil,
		)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&model.Post{}, nil)

		callCount := 0
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{conn, otherConn}, nil
			}
			return []store.TeamConnection{otherConn}, nil
		}
		kvs.removeTeamConnectionFn = func(string, store.TeamConnection) error { return nil }

		removeCalled := false
		kvs.removeInitializedTeamIDFn = func(string) error {
			removeCalled = true
			return nil
		}

		team, svcErr := p.teardownTeamForCrossGuard(testUser(), "team-id", conn)
		assert.NotNil(t, team)
		assert.Nil(t, svcErr)
		assert.False(t, removeCalled, "RemoveInitializedTeamID should not be called when connections remain")
	})

	t.Run("re-read error after remove returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)

		callCount := 0
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{conn}, nil
			}
			return nil, errors.New("re-read error")
		}
		kvs.removeTeamConnectionFn = func(string, store.TeamConnection) error { return nil }

		team, svcErr := p.teardownTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, team)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("RemoveInitializedTeamID error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)

		callCount := 0
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{conn}, nil
			}
			return []store.TeamConnection{}, nil
		}
		kvs.removeTeamConnectionFn = func(string, store.TeamConnection) error { return nil }
		kvs.removeInitializedTeamIDFn = func(string) error { return errors.New("list error") }

		team, svcErr := p.teardownTeamForCrossGuard(testUser(), "team-id", conn)
		assert.Nil(t, team)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})
}

// -------------------------------------------------------------------
// initChannelForCrossGuard
// -------------------------------------------------------------------

func TestInitChannelForCrossGuard(t *testing.T) {
	conn := store.TeamConnection{Direction: "outbound", Connection: "high"}

	t.Run("channel not found returns 404", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetChannel", "chan-id").Return(nil, &model.AppError{Message: "not found"})

		ch, alreadyLinked, svcErr := p.initChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.Nil(t, ch)
		assert.False(t, alreadyLinked)
		require.NotNil(t, svcErr)
		assert.Equal(t, 404, svcErr.Status)
	})

	t.Run("team not initialized auto-initializes", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		api.On("GetChannelByName", "team-id", model.DefaultChannelName, false).Return(
			&model.Channel{Id: "town-square-id"}, nil,
		)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&model.Post{}, nil)
		api.On("UpdateChannel", mock.AnythingOfType("*model.Channel")).Return(channel, nil)

		teamCallCount := 0
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			teamCallCount++
			if teamCallCount <= 2 {
				return []store.TeamConnection{}, nil
			}
			return []store.TeamConnection{conn}, nil
		}
		kvs.addTeamConnectionFn = func(string, store.TeamConnection) error { return nil }
		kvs.addInitializedTeamIDFn = func(string) error { return nil }

		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{}, nil
		}
		kvs.addChannelConnectionFn = func(string, store.TeamConnection) error { return nil }

		ch, alreadyLinked, svcErr := p.initChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.NotNil(t, ch)
		assert.False(t, alreadyLinked)
		assert.Nil(t, svcErr)
	})

	t.Run("already linked returns idempotent", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)

		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{conn}, nil
		}
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{conn}, nil
		}

		ch, alreadyLinked, svcErr := p.initChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.NotNil(t, ch)
		assert.True(t, alreadyLinked)
		assert.Nil(t, svcErr)
	})

	t.Run("GetChannelConnections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)

		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{conn}, nil
		}
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, errors.New("db error")
		}

		ch, alreadyLinked, svcErr := p.initChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.Nil(t, ch)
		assert.False(t, alreadyLinked)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("AddChannelConnection error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)

		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{conn}, nil
		}
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{}, nil
		}
		kvs.addChannelConnectionFn = func(string, store.TeamConnection) error {
			return errors.New("write error")
		}

		ch, alreadyLinked, svcErr := p.initChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.Nil(t, ch)
		assert.False(t, alreadyLinked)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("publishes WebSocket event and updates header", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogOnly(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		updatedChannel := *channel
		updatedChannel.Header = crossguardHeaderPrefix + channel.Header

		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("UpdateChannel", mock.AnythingOfType("*model.Channel")).Return(&updatedChannel, nil)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&model.Post{}, nil)

		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{conn}, nil
		}

		chanCallCount := 0
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			chanCallCount++
			if chanCallCount == 1 {
				return []store.TeamConnection{}, nil
			}
			return []store.TeamConnection{conn}, nil
		}
		kvs.addChannelConnectionFn = func(string, store.TeamConnection) error { return nil }

		wsCalled := false
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			wsCalled = true
			payload := args.Get(1).(map[string]any)
			assert.Equal(t, "chan-id", payload["channel_id"])
			assert.Equal(t, "outbound:high", payload["connections"])
		}).Return()

		ch, alreadyLinked, svcErr := p.initChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.NotNil(t, ch)
		assert.False(t, alreadyLinked)
		assert.Nil(t, svcErr)
		assert.True(t, wsCalled, "WebSocket event should have been published")
	})

	t.Run("re-read error after add returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)

		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{conn}, nil
		}

		chanCallCount := 0
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			chanCallCount++
			if chanCallCount == 1 {
				return []store.TeamConnection{}, nil
			}
			return nil, errors.New("re-read error")
		}
		kvs.addChannelConnectionFn = func(string, store.TeamConnection) error { return nil }

		ch, alreadyLinked, svcErr := p.initChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.Nil(t, ch)
		assert.False(t, alreadyLinked)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})
}

// -------------------------------------------------------------------
// teardownChannelForCrossGuard
// -------------------------------------------------------------------

func TestTeardownChannelForCrossGuard(t *testing.T) {
	conn := store.TeamConnection{Direction: "outbound", Connection: "high"}

	t.Run("channel not found returns 404", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetChannel", "chan-id").Return(nil, &model.AppError{Message: "not found"})

		ch, svcErr := p.teardownChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.Nil(t, ch)
		require.NotNil(t, svcErr)
		assert.Equal(t, 404, svcErr.Status)
	})

	t.Run("no connections returns channel nil error", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{}, nil
		}

		ch, svcErr := p.teardownChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.NotNil(t, ch)
		assert.Nil(t, svcErr)
	})

	t.Run("connection not in list returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "low"}}, nil
		}

		ch, svcErr := p.teardownChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.Nil(t, ch)
		require.NotNil(t, svcErr)
		assert.Equal(t, 400, svcErr.Status)
		assert.Contains(t, svcErr.Message, "not linked")
	})

	t.Run("RemoveChannelConnection error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{conn}, nil
		}
		kvs.removeChannelConnectionFn = func(string, store.TeamConnection) error {
			return errors.New("remove error")
		}

		ch, svcErr := p.teardownChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.Nil(t, ch)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("last connection removed deletes and removes header", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		channel.Header = crossguardHeaderPrefix + "original header"
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("UpdateChannel", mock.AnythingOfType("*model.Channel")).Run(func(args mock.Arguments) {
			ch := args.Get(0).(*model.Channel)
			assert.Equal(t, "original header", ch.Header)
		}).Return(channel, nil)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&model.Post{}, nil)

		callCount := 0
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{conn}, nil
			}
			return []store.TeamConnection{}, nil
		}
		kvs.removeChannelConnectionFn = func(string, store.TeamConnection) error { return nil }

		deleteCalled := false
		kvs.deleteChannelConnectionsFn = func(id string) error {
			deleteCalled = true
			assert.Equal(t, "chan-id", id)
			return nil
		}

		ch, svcErr := p.teardownChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.NotNil(t, ch)
		assert.Nil(t, svcErr)
		assert.True(t, deleteCalled, "DeleteChannelConnections should have been called")
	})

	t.Run("partial teardown publishes remaining connections", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogOnly(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		otherConn := store.TeamConnection{Direction: "inbound", Connection: "high"}
		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&model.Post{}, nil)

		callCount := 0
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{conn, otherConn}, nil
			}
			return []store.TeamConnection{otherConn}, nil
		}
		kvs.removeChannelConnectionFn = func(string, store.TeamConnection) error { return nil }

		wsCalled := false
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			wsCalled = true
			payload := args.Get(1).(map[string]any)
			assert.Equal(t, "inbound:high", payload["connections"])
		}).Return()

		ch, svcErr := p.teardownChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.NotNil(t, ch)
		assert.Nil(t, svcErr)
		assert.True(t, wsCalled, "WebSocket event should have been published for remaining connections")
	})

	t.Run("re-read error after remove returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)

		callCount := 0
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{conn}, nil
			}
			return nil, errors.New("re-read error")
		}
		kvs.removeChannelConnectionFn = func(string, store.TeamConnection) error { return nil }

		ch, svcErr := p.teardownChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.Nil(t, ch)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("DeleteChannelConnections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)

		callCount := 0
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			callCount++
			if callCount == 1 {
				return []store.TeamConnection{conn}, nil
			}
			return []store.TeamConnection{}, nil
		}
		kvs.removeChannelConnectionFn = func(string, store.TeamConnection) error { return nil }
		kvs.deleteChannelConnectionsFn = func(string) error {
			return errors.New("delete error")
		}

		ch, svcErr := p.teardownChannelForCrossGuard(testUser(), "chan-id", conn)
		assert.Nil(t, ch)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})
}

// -------------------------------------------------------------------
// getTeamStatus
// -------------------------------------------------------------------

func TestGetTeamStatus(t *testing.T) {
	t.Run("team not found returns 404", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(nil, &model.AppError{Message: "not found"})

		resp, svcErr := p.getTeamStatus("team-id", nil)
		assert.Nil(t, resp)
		require.NotNil(t, svcErr)
		assert.Equal(t, 404, svcErr.Status)
	})

	t.Run("GetTeamConnections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, errors.New("db error")
		}

		resp, svcErr := p.getTeamStatus("team-id", nil)
		assert.Nil(t, resp)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("correctly identifies linked and orphaned connections", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)

		// Team has outbound:high linked, plus an orphaned connection not in config
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "outbound", Connection: "removed-conn"},
			}, nil
		}

		resp, svcErr := p.getTeamStatus("team-id", nil)
		require.Nil(t, svcErr)
		require.NotNil(t, resp)
		assert.Equal(t, "team-id", resp.TeamID)
		assert.Equal(t, "Test Team", resp.TeamDisplayName)
		assert.True(t, resp.Initialized)
		assert.Len(t, resp.LinkedConnections, 2)

		// Find each status entry
		statusMap := make(map[string]ConnectionStatus)
		for _, s := range resp.Connections {
			statusMap[s.Direction+":"+s.Name] = s
		}

		// outbound:high is linked and in config
		outHigh, ok := statusMap["outbound:high"]
		require.True(t, ok)
		assert.True(t, outHigh.Linked)
		assert.False(t, outHigh.Orphaned)

		// inbound:high is in config but not linked
		inHigh, ok := statusMap["inbound:high"]
		require.True(t, ok)
		assert.False(t, inHigh.Linked)
		assert.False(t, inHigh.Orphaned)

		// outbound:removed-conn is linked but not in config (orphaned)
		orphaned, ok := statusMap["outbound:removed-conn"]
		require.True(t, ok)
		assert.True(t, orphaned.Linked)
		assert.True(t, orphaned.Orphaned)
	})

	t.Run("no connections means not initialized", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{}, nil
		}

		resp, svcErr := p.getTeamStatus("team-id", nil)
		require.Nil(t, svcErr)
		require.NotNil(t, resp)
		assert.False(t, resp.Initialized)
		assert.Empty(t, resp.LinkedConnections)
	})
}

// -------------------------------------------------------------------
// getChannelStatus
// -------------------------------------------------------------------

func TestGetChannelStatus(t *testing.T) {
	conn := store.TeamConnection{Direction: "outbound", Connection: "high"}

	t.Run("channel not found returns 404", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		api.On("GetChannel", "chan-id").Return(nil, &model.AppError{Message: "not found"})

		resp, svcErr := p.getChannelStatus("chan-id")
		assert.Nil(t, resp)
		require.NotNil(t, svcErr)
		assert.Equal(t, 404, svcErr.Status)
	})

	t.Run("DM channel returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		dmChannel := &model.Channel{Id: "dm-id", Type: model.ChannelTypeDirect}
		api.On("GetChannel", "dm-id").Return(dmChannel, nil)

		resp, svcErr := p.getChannelStatus("dm-id")
		assert.Nil(t, resp)
		require.NotNil(t, svcErr)
		assert.Equal(t, 400, svcErr.Status)
		assert.Contains(t, svcErr.Message, "direct or group")
	})

	t.Run("group channel returns 400", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		groupChannel := &model.Channel{Id: "group-id", Type: model.ChannelTypeGroup}
		api.On("GetChannel", "group-id").Return(groupChannel, nil)

		resp, svcErr := p.getChannelStatus("group-id")
		assert.Nil(t, resp)
		require.NotNil(t, svcErr)
		assert.Equal(t, 400, svcErr.Status)
	})

	t.Run("happy path merges team and channel connections", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		team := testTeam()
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(team, nil)

		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{conn}, nil
		}
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				conn,
				{Direction: "inbound", Connection: "high"},
			}, nil
		}

		resp, svcErr := p.getChannelStatus("chan-id")
		require.Nil(t, svcErr)
		require.NotNil(t, resp)
		assert.Equal(t, "chan-id", resp.ChannelID)
		assert.Equal(t, "Test Channel", resp.ChannelDisplayName)
		assert.Equal(t, "Test Team", resp.TeamName)

		statusMap := make(map[string]ConnectionStatus)
		for _, s := range resp.TeamConnections {
			statusMap[s.Direction+":"+s.Name] = s
		}

		// outbound:high is channel-linked
		outHigh := statusMap["outbound:high"]
		assert.True(t, outHigh.Linked)

		// inbound:high is from team but not channel-linked
		inHigh := statusMap["inbound:high"]
		assert.False(t, inHigh.Linked)
	})

	t.Run("GetChannelConnections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(testTeam(), nil)

		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, errors.New("db error")
		}

		resp, svcErr := p.getChannelStatus("chan-id")
		assert.Nil(t, resp)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("GetTeamConnections error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(testTeam(), nil)

		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{}, nil
		}
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, errors.New("db error")
		}

		resp, svcErr := p.getChannelStatus("chan-id")
		assert.Nil(t, resp)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("team not found returns 404", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		channel := testChannel()
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(nil, &model.AppError{Message: "not found"})

		resp, svcErr := p.getChannelStatus("chan-id")
		assert.Nil(t, resp)
		require.NotNil(t, svcErr)
		assert.Equal(t, 404, svcErr.Status)
	})
}

// -------------------------------------------------------------------
// getGlobalStatus
// -------------------------------------------------------------------

func TestGetGlobalStatus(t *testing.T) {
	t.Run("GetInitializedTeamIDs error returns 500", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		kvs.getInitializedTeamIDsFn = func() ([]string, error) {
			return nil, errors.New("db error")
		}

		resp, svcErr := p.getGlobalStatus()
		assert.Nil(t, resp)
		require.NotNil(t, svcErr)
		assert.Equal(t, 500, svcErr.Status)
	})

	t.Run("unknown team uses placeholder", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		kvs.getInitializedTeamIDsFn = func() ([]string, error) {
			return []string{"bad-team-id"}, nil
		}

		api.On("GetTeam", "bad-team-id").Return(nil, &model.AppError{Message: "not found"})

		resp, svcErr := p.getGlobalStatus()
		require.Nil(t, svcErr)
		require.NotNil(t, resp)
		require.Len(t, resp.Teams, 1)
		assert.Equal(t, "(unknown)", resp.Teams[0].DisplayName)
		assert.Equal(t, "(error)", resp.Teams[0].TeamName)
		assert.Equal(t, "bad-team-id", resp.Teams[0].TeamID)
	})

	t.Run("happy path with teams and connections", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		kvs.getInitializedTeamIDsFn = func() ([]string, error) {
			return []string{"team-id"}, nil
		}

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			}, nil
		}

		resp, svcErr := p.getGlobalStatus()
		require.Nil(t, svcErr)
		require.NotNil(t, resp)
		require.Len(t, resp.Teams, 1)
		assert.Equal(t, "team-id", resp.Teams[0].TeamID)
		assert.Equal(t, "Test Team", resp.Teams[0].DisplayName)
		assert.Len(t, resp.Teams[0].LinkedConnections, 1)

		// Connections should be the redacted config connections
		assert.Len(t, resp.Connections, 2) // one outbound, one inbound from defaultTestConfig
		assert.Empty(t, resp.Warnings)
	})

	t.Run("bad config adds warnings", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			OutboundConnections: "not-json",
			InboundConnections:  "not-json",
		}

		kvs.getInitializedTeamIDsFn = func() ([]string, error) {
			return []string{}, nil
		}

		resp, svcErr := p.getGlobalStatus()
		require.Nil(t, svcErr)
		require.NotNil(t, resp)
		assert.Len(t, resp.Warnings, 2)
	})

	t.Run("empty state", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		kvs.getInitializedTeamIDsFn = func() ([]string, error) {
			return []string{}, nil
		}

		resp, svcErr := p.getGlobalStatus()
		require.Nil(t, svcErr)
		require.NotNil(t, resp)
		assert.Empty(t, resp.Teams)
		assert.Len(t, resp.Connections, 2)
	})

	t.Run("team connections error is non-fatal", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.configuration = defaultTestConfig()

		kvs.getInitializedTeamIDsFn = func() ([]string, error) {
			return []string{"team-id"}, nil
		}

		api.On("GetTeam", "team-id").Return(testTeam(), nil)
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, errors.New("conn error")
		}

		resp, svcErr := p.getGlobalStatus()
		require.Nil(t, svcErr)
		require.NotNil(t, resp)
		require.Len(t, resp.Teams, 1)
		assert.Equal(t, "Test Team", resp.Teams[0].DisplayName)
		assert.Nil(t, resp.Teams[0].LinkedConnections)
	})
}

// ---------------------------------------------------------------------------
// Additional helper function tests
// ---------------------------------------------------------------------------

func TestAddCrossguardHeaderPrefix(t *testing.T) {
	t.Run("adds prefix", func(t *testing.T) {
		result := addCrossguardHeaderPrefix("My Channel")
		assert.Equal(t, crossguardHeaderPrefix+"My Channel", result)
	})
	t.Run("already prefixed is idempotent", func(t *testing.T) {
		prefixed := addCrossguardHeaderPrefix("My Channel")
		result := addCrossguardHeaderPrefix(prefixed)
		assert.Equal(t, prefixed, result)
	})
	t.Run("empty string gets prefix", func(t *testing.T) {
		result := addCrossguardHeaderPrefix("")
		assert.Equal(t, crossguardHeaderPrefix, result)
	})
}

func TestRemoveCrossguardHeaderPrefix(t *testing.T) {
	t.Run("removes prefix", func(t *testing.T) {
		result := removeCrossguardHeaderPrefix(crossguardHeaderPrefix + "My Channel")
		assert.Equal(t, "My Channel", result)
	})
	t.Run("not prefixed returns as-is", func(t *testing.T) {
		result := removeCrossguardHeaderPrefix("Plain Header")
		assert.Equal(t, "Plain Header", result)
	})
}

func TestPublishChannelConnectionUpdate(t *testing.T) {
	t.Run("empty connections", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, _ := setupTestPluginWithRouter(api)
		api.On("PublishWebSocketEvent", "channel_connections_updated", mock.Anything, mock.Anything).Return()
		p.publishChannelConnectionUpdate("ch1", nil)
		api.AssertCalled(t, "PublishWebSocketEvent", "channel_connections_updated",
			map[string]any{"channel_id": "ch1", "connections": ""},
			mock.Anything)
	})
	t.Run("multiple connections", func(t *testing.T) {
		api := &plugintest.API{}
		mockLogCalls(api)
		p, _ := setupTestPluginWithRouter(api)
		api.On("PublishWebSocketEvent", "channel_connections_updated", mock.Anything, mock.Anything).Return()
		conns := []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
			{Direction: "inbound", Connection: "low"},
		}
		p.publishChannelConnectionUpdate("ch1", conns)
		api.AssertCalled(t, "PublishWebSocketEvent", "channel_connections_updated",
			map[string]any{"channel_id": "ch1", "connections": "outbound:high,inbound:low"},
			mock.Anything)
	})
}

func TestGetAllConnectionNames(t *testing.T) {
	t.Run("returns both inbound and outbound", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		p.configuration = &configuration{
			OutboundConnections: `[{"name":"out-conn","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"test"}}]`,
			InboundConnections:  `[{"name":"in-conn","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"test"}}]`,
		}

		names := p.getAllConnectionNames()

		require.Len(t, names, 2)
		assert.Equal(t, "outbound", names[0].Direction)
		assert.Equal(t, "out-conn", names[0].Connection)
		assert.Equal(t, "inbound", names[1].Direction)
		assert.Equal(t, "in-conn", names[1].Connection)
	})

	t.Run("parse error for outbound still returns inbound", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		p.configuration = &configuration{
			OutboundConnections: "invalid json",
			InboundConnections:  `[{"name":"in-conn","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"test"}}]`,
		}

		names := p.getAllConnectionNames()

		require.Len(t, names, 1)
		assert.Equal(t, "inbound", names[0].Direction)
		assert.Equal(t, "in-conn", names[0].Connection)
	})

	t.Run("empty config returns nil", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		p.configuration = &configuration{}

		names := p.getAllConnectionNames()
		assert.Nil(t, names)
	})
}

func TestGetConnectionMap(t *testing.T) {
	t.Run("returns map keyed by direction:name", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		p.configuration = &configuration{
			OutboundConnections: `[{"name":"out-conn","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"test"},"file_transfer_enabled":true}]`,
			InboundConnections:  `[{"name":"in-conn","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"test"}}]`,
		}

		m := p.getConnectionMap()

		require.Len(t, m, 2)
		outConn, ok := m["outbound:out-conn"]
		require.True(t, ok)
		assert.Equal(t, "out-conn", outConn.Name)
		assert.True(t, outConn.FileTransferEnabled)

		inConn, ok := m["inbound:in-conn"]
		require.True(t, ok)
		assert.Equal(t, "in-conn", inConn.Name)
	})

	t.Run("parse error returns partial map", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		p.configuration = &configuration{
			OutboundConnections: "invalid json",
			InboundConnections:  `[{"name":"in-conn","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"test"}}]`,
		}

		m := p.getConnectionMap()

		require.Len(t, m, 1)
		_, ok := m["inbound:in-conn"]
		assert.True(t, ok)
	})
}

func TestGetGlobalStatus_ConfigParseWarnings(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)

	kvs.getInitializedTeamIDsFn = func() ([]string, error) {
		return nil, nil
	}

	p.configuration = &configuration{
		OutboundConnections: "invalid json",
		InboundConnections:  "also invalid",
	}

	resp, svcErr := p.getGlobalStatus()
	require.Nil(t, svcErr)
	require.NotNil(t, resp)
	assert.Len(t, resp.Warnings, 2)
}

// ---------------------------------------------------------------------------
// Additional service edge-case tests (new)
// ---------------------------------------------------------------------------

func TestInitTeamForCrossGuard_TeamNotFound(t *testing.T) {
	api := &plugintest.API{}
	mockLogCalls(api)
	p, _ := setupTestPluginWithRouter(api)
	p.configuration = defaultTestConfig()

	api.On("GetTeam", "bad-team-id").Return(nil, &model.AppError{Message: "not found"})

	conn := store.TeamConnection{Direction: "outbound", Connection: "high"}
	team, alreadyLinked, svcErr := p.initTeamForCrossGuard(testUser(), "bad-team-id", conn)
	assert.Nil(t, team)
	assert.False(t, alreadyLinked)
	require.NotNil(t, svcErr)
	assert.Equal(t, 404, svcErr.Status)
	assert.Contains(t, svcErr.Message, "team not found")
}

func TestInitTeamForCrossGuard_AddConnectionFails(t *testing.T) {
	api := &plugintest.API{}
	mockLogCalls(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.configuration = defaultTestConfig()

	api.On("GetTeam", "team-id").Return(testTeam(), nil)
	kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{}, nil
	}
	kvs.addTeamConnectionFn = func(teamID string, c store.TeamConnection) error {
		return errors.New("write error")
	}

	conn := store.TeamConnection{Direction: "outbound", Connection: "high"}
	team, alreadyLinked, svcErr := p.initTeamForCrossGuard(testUser(), "team-id", conn)
	assert.Nil(t, team)
	assert.False(t, alreadyLinked)
	require.NotNil(t, svcErr)
	assert.Equal(t, 500, svcErr.Status)
}

func TestTeardownTeamForCrossGuard_NotLinked(t *testing.T) {
	api := &plugintest.API{}
	mockLogCalls(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.configuration = defaultTestConfig()

	api.On("GetTeam", "team-id").Return(testTeam(), nil)
	kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{{Direction: "inbound", Connection: "low"}}, nil
	}

	conn := store.TeamConnection{Direction: "outbound", Connection: "high"}
	_, svcErr := p.teardownTeamForCrossGuard(testUser(), "team-id", conn)
	require.NotNil(t, svcErr)
	assert.Equal(t, 400, svcErr.Status)
	assert.Contains(t, svcErr.Message, "not linked")
}

func TestTeardownChannelForCrossGuard_RemovesHeaderPrefix(t *testing.T) {
	api := &plugintest.API{}
	mockLogCalls(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.configuration = defaultTestConfig()

	channel := testChannel()
	channel.Header = crossguardHeaderPrefix + "my header"
	api.On("GetChannel", "chan-id").Return(channel, nil)

	conn := store.TeamConnection{Direction: "outbound", Connection: "high"}

	// Return the connection as existing, then empty after removal.
	removeCallCount := 0
	kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
		removeCallCount++
		if removeCallCount == 1 {
			return []store.TeamConnection{conn}, nil
		}
		return nil, nil
	}
	kvs.removeChannelConnectionFn = func(channelID string, c store.TeamConnection) error {
		return nil
	}
	kvs.deleteChannelConnectionsFn = func(channelID string) error {
		return nil
	}

	var updatedChannel *model.Channel
	api.On("UpdateChannel", mock.AnythingOfType("*model.Channel")).Run(func(args mock.Arguments) {
		updatedChannel = args.Get(0).(*model.Channel)
	}).Return(&model.Channel{}, nil)
	api.On("CreatePost", mock.Anything).Return(&model.Post{}, nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Return()

	_, svcErr := p.teardownChannelForCrossGuard(testUser(), "chan-id", conn)
	assert.Nil(t, svcErr)
	require.NotNil(t, updatedChannel)
	assert.Equal(t, "my header", updatedChannel.Header, "header prefix should be removed")
}

func TestGetGlobalStatus_MixedTeams(t *testing.T) {
	api := &plugintest.API{}
	mockLogCalls(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.configuration = defaultTestConfig()

	kvs.getInitializedTeamIDsFn = func() ([]string, error) {
		return []string{"good-team", "bad-team"}, nil
	}

	goodTeam := &model.Team{Id: "good-team", Name: "good", DisplayName: "Good Team"}
	api.On("GetTeam", "good-team").Return(goodTeam, nil)
	api.On("GetTeam", "bad-team").Return(nil, &model.AppError{Message: "not found"})

	resp, svcErr := p.getGlobalStatus()
	require.Nil(t, svcErr)
	require.NotNil(t, resp)
	require.Len(t, resp.Teams, 2)

	assert.Equal(t, "Good Team", resp.Teams[0].DisplayName)
	assert.Equal(t, "(unknown)", resp.Teams[1].DisplayName)
	assert.Equal(t, "(error)", resp.Teams[1].TeamName)
}

func TestGetChannelStatus_DMChannel(t *testing.T) {
	api := &plugintest.API{}
	mockLogCalls(api)
	p, _ := setupTestPluginWithRouter(api)
	p.configuration = defaultTestConfig()

	dmChannel := &model.Channel{Id: "dm-chan-id", Type: model.ChannelTypeDirect, TeamId: ""}
	api.On("GetChannel", "dm-chan-id").Return(dmChannel, nil)

	resp, svcErr := p.getChannelStatus("dm-chan-id")
	assert.Nil(t, resp)
	require.NotNil(t, svcErr)
	assert.Equal(t, 400, svcErr.Status)
	assert.Contains(t, svcErr.Message, "direct or group")
}

func TestApiError_Error(t *testing.T) {
	e := &apiError{Message: "something went wrong", Status: 500}
	assert.Equal(t, "something went wrong", e.Error())
}
