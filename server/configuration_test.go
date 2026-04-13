package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
)

func TestParseConnections(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		conns, err := parseConnections("")
		require.NoError(t, err)
		assert.Nil(t, conns)
	})

	t.Run("empty array string returns nil", func(t *testing.T) {
		conns, err := parseConnections("[]")
		require.NoError(t, err)
		assert.Nil(t, conns)
	})

	t.Run("whitespace only returns nil", func(t *testing.T) {
		conns, err := parseConnections("   ")
		require.NoError(t, err)
		assert.Nil(t, conns)
	})

	t.Run("valid JSON parses correctly", func(t *testing.T) {
		input := `[{"name":"test","provider":"nats","nats":{"address":"nats://localhost:4222","subject":"crossguard.test","tls_enabled":false,"auth_type":"none","token":"","username":"","password":"","client_cert":"","client_key":"","ca_cert":""}}]`
		conns, err := parseConnections(input)
		require.NoError(t, err)
		require.Len(t, conns, 1)
		assert.Equal(t, "test", conns[0].Name)
		assert.Equal(t, "nats://localhost:4222", conns[0].NATS.Address)
		assert.Equal(t, "crossguard.test", conns[0].NATS.Subject)
	})

	t.Run("multiple connections parse correctly", func(t *testing.T) {
		input := `[{"name":"first","provider":"nats","nats":{"address":"nats://host1:4222","subject":"crossguard.sub1","auth_type":"none"}},{"name":"second","provider":"nats","nats":{"address":"nats://host2:4222","subject":"crossguard.sub2","auth_type":"token","token":"mytoken"}}]`
		conns, err := parseConnections(input)
		require.NoError(t, err)
		require.Len(t, conns, 2)
		assert.Equal(t, "first", conns[0].Name)
		assert.Equal(t, "second", conns[1].Name)
		assert.Equal(t, "token", conns[1].NATS.AuthType)
		assert.Equal(t, "mytoken", conns[1].NATS.Token)
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		_, err := parseConnections("{not valid json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse connections")
	})
}

func TestConfigurationValidate(t *testing.T) {
	t.Run("empty config is valid", func(t *testing.T) {
		cfg := &configuration{}
		assert.NoError(t, cfg.validate())
	})

	t.Run("empty arrays are valid", func(t *testing.T) {
		cfg := &configuration{
			InboundConnections:  "[]",
			OutboundConnections: "[]",
		}
		assert.NoError(t, cfg.validate())
	})

	t.Run("valid connections pass validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "conn1", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.test", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{
			InboundConnections: string(data),
		}
		assert.NoError(t, cfg.validate())
	})

	t.Run("missing name fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.test", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("missing address fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", NATS: &NATSProviderConfig{Address: "", Subject: "crossguard.test", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "address is required")
	})

	t.Run("missing subject fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "subject is required")
	})

	t.Run("subject without required prefix fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "bad.prefix.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "subject must start with")
	})

	t.Run("duplicate names fail validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "dup", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://host1:4222", Subject: "crossguard.sub1", AuthType: "none"}},
			{Name: "dup", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://host2:4222", Subject: "crossguard.sub2", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate name")
	})

	t.Run("invalid auth_type fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "invalid"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "auth_type must be")
	})

	t.Run("token auth without token fails", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "token", Token: ""}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token is required")
	})

	t.Run("credentials auth without username fails", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "credentials", Username: "", Password: "pass"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "username is required")
	})

	t.Run("credentials auth without password fails", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "credentials", Username: "user", Password: ""}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "password is required")
	})

	t.Run("valid credentials auth passes", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "credentials", Username: "user", Password: "pass"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("valid token auth passes", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "token", Token: "mytoken"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("name with uppercase letters fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "MyConn", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("name with spaces fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "my conn", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("name with special characters fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "my_conn!", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("valid name with hyphens passes", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "my-nats-conn", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("name with leading hyphen fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "-leading", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("name with trailing hyphen fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "trailing-", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("name with consecutive hyphens fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "my--conn", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("empty message_format defaults to json and passes", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", MessageFormat: "", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{OutboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("xml message_format passes", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", MessageFormat: "xml", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{OutboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("invalid message_format fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", MessageFormat: "yaml", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{OutboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "message_format must be")
	})

	t.Run("malformed JSON reports error", func(t *testing.T) {
		cfg := &configuration{InboundConnections: "not json"}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "inbound connections")
	})

	t.Run("duplicate names across inbound and outbound fail validation", func(t *testing.T) {
		inbound := []ConnectionConfig{
			{Name: "shared-name", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://host1:4222", Subject: "crossguard.sub1", AuthType: "none"}},
		}
		outbound := []ConnectionConfig{
			{Name: "shared-name", Provider: "nats", NATS: &NATSProviderConfig{Address: "nats://host2:4222", Subject: "crossguard.sub2", AuthType: "none"}},
		}
		inData, _ := json.Marshal(inbound)
		outData, _ := json.Marshal(outbound)
		cfg := &configuration{
			InboundConnections:  string(inData),
			OutboundConnections: string(outData),
		}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate name")
	})

	t.Run("errors from both directions are aggregated", func(t *testing.T) {
		cfg := &configuration{
			InboundConnections:  "not json",
			OutboundConnections: "also not json",
		}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "inbound")
		assert.Contains(t, err.Error(), "outbound")
	})
}

func TestIsUsernameLookupEnabled(t *testing.T) {
	t.Run("nil defaults to true", func(t *testing.T) {
		cfg := &configuration{}
		assert.True(t, cfg.isUsernameLookupEnabled())
	})

	t.Run("explicitly true", func(t *testing.T) {
		cfg := &configuration{UsernameLookup: new(true)}
		assert.True(t, cfg.isUsernameLookupEnabled())
	})

	t.Run("explicitly false", func(t *testing.T) {
		cfg := &configuration{UsernameLookup: new(false)}
		assert.False(t, cfg.isUsernameLookupEnabled())
	})
}

func TestIsRestrictedToSystemAdmins(t *testing.T) {
	t.Run("nil defaults to false", func(t *testing.T) {
		cfg := &configuration{}
		assert.False(t, cfg.isRestrictedToSystemAdmins())
	})

	t.Run("explicitly true", func(t *testing.T) {
		cfg := &configuration{RestrictToSystemAdmins: new(true)}
		assert.True(t, cfg.isRestrictedToSystemAdmins())
	})

	t.Run("explicitly false", func(t *testing.T) {
		cfg := &configuration{RestrictToSystemAdmins: new(false)}
		assert.False(t, cfg.isRestrictedToSystemAdmins())
	})
}

func TestIsTeamAdminRequestsAllowed(t *testing.T) {
	t.Run("nil defaults to false", func(t *testing.T) {
		cfg := &configuration{}
		assert.False(t, cfg.isTeamAdminRequestsAllowed())
	})

	t.Run("explicitly true", func(t *testing.T) {
		cfg := &configuration{AllowTeamAdminRequests: new(true)}
		assert.True(t, cfg.isTeamAdminRequestsAllowed())
	})

	t.Run("explicitly false", func(t *testing.T) {
		cfg := &configuration{AllowTeamAdminRequests: new(false)}
		assert.False(t, cfg.isTeamAdminRequestsAllowed())
	})
}

func TestIsRequestMode(t *testing.T) {
	t.Run("both enabled", func(t *testing.T) {
		cfg := &configuration{
			RestrictToSystemAdmins: new(true),
			AllowTeamAdminRequests: new(true),
		}
		assert.True(t, cfg.isRequestMode())
	})

	t.Run("restrict only", func(t *testing.T) {
		cfg := &configuration{
			RestrictToSystemAdmins: new(true),
			AllowTeamAdminRequests: new(false),
		}
		assert.False(t, cfg.isRequestMode())
	})

	t.Run("allow only", func(t *testing.T) {
		cfg := &configuration{
			RestrictToSystemAdmins: new(false),
			AllowTeamAdminRequests: new(true),
		}
		assert.False(t, cfg.isRequestMode())
	})

	t.Run("both nil", func(t *testing.T) {
		cfg := &configuration{}
		assert.False(t, cfg.isRequestMode())
	})
}

func TestIsTestMessage(t *testing.T) {
	t.Run("valid test message is detected via JSON", func(t *testing.T) {
		env := &model.Envelope{
			Type:        model.MessageTypeTest,
			Timestamp:   "2026-04-06T12:00:00Z",
			TestMessage: &model.TestMessage{ID: "abc-123"},
		}
		data, err := model.Marshal(env, model.FormatJSON)
		require.NoError(t, err)

		result, ok := isTestMessage(data)
		require.True(t, ok)
		assert.Equal(t, "abc-123", result.ID)
	})

	t.Run("valid test message is detected via XML", func(t *testing.T) {
		env := &model.Envelope{
			Type:        model.MessageTypeTest,
			Timestamp:   "2026-04-06T12:00:00Z",
			TestMessage: &model.TestMessage{ID: "xml-456"},
		}
		data, err := model.Marshal(env, model.FormatXML)
		require.NoError(t, err)

		result, ok := isTestMessage(data)
		require.True(t, ok)
		assert.Equal(t, "xml-456", result.ID)
	})

	t.Run("non-test message type is not detected", func(t *testing.T) {
		env := &model.Envelope{
			Type:        "regular_message",
			Timestamp:   "2026-04-06T12:00:00Z",
			PostMessage: &model.PostMessage{PostID: "p1"},
		}
		data, err := model.Marshal(env, model.FormatJSON)
		require.NoError(t, err)

		result, ok := isTestMessage(data)
		assert.False(t, ok)
		assert.Nil(t, result)
	})

	t.Run("invalid JSON is not detected", func(t *testing.T) {
		result, ok := isTestMessage([]byte("not json"))
		assert.False(t, ok)
		assert.Nil(t, result)
	})

	t.Run("empty type is not detected", func(t *testing.T) {
		env := &model.Envelope{
			Type:        "",
			Timestamp:   "2026-04-06T12:00:00Z",
			TestMessage: &model.TestMessage{ID: "123"},
		}
		data, err := model.Marshal(env, model.FormatJSON)
		require.NoError(t, err)

		result, ok := isTestMessage(data)
		assert.False(t, ok)
		assert.Nil(t, result)
	})
}

func TestIsFileAllowed(t *testing.T) {
	t.Run("no filter mode allows all files", func(t *testing.T) {
		assert.True(t, isFileAllowed("document.pdf", "", ""))
		assert.True(t, isFileAllowed("image.png", "", ""))
		assert.True(t, isFileAllowed("noext", "", ""))
	})

	t.Run("allow mode permits listed types", func(t *testing.T) {
		assert.True(t, isFileAllowed("report.pdf", "allow", ".pdf,.docx,.png"))
		assert.True(t, isFileAllowed("doc.docx", "allow", ".pdf,.docx,.png"))
		assert.True(t, isFileAllowed("image.png", "allow", ".pdf,.docx,.png"))
		assert.False(t, isFileAllowed("script.exe", "allow", ".pdf,.docx,.png"))
		assert.False(t, isFileAllowed("noext", "allow", ".pdf,.docx,.png"))
	})

	t.Run("deny mode blocks listed types", func(t *testing.T) {
		assert.False(t, isFileAllowed("virus.exe", "deny", ".exe,.bat"))
		assert.False(t, isFileAllowed("script.bat", "deny", ".exe,.bat"))
		assert.True(t, isFileAllowed("document.pdf", "deny", ".exe,.bat"))
		assert.True(t, isFileAllowed("image.png", "deny", ".exe,.bat"))
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		assert.True(t, isFileAllowed("REPORT.pdf", "allow", ".PDF,.Docx"))
		assert.True(t, isFileAllowed("doc.DOCX", "allow", ".PDF,.Docx"))
		assert.False(t, isFileAllowed("image.png", "allow", ".PDF,.Docx"))
	})

	t.Run("types without leading dot are normalized", func(t *testing.T) {
		assert.True(t, isFileAllowed("report.pdf", "allow", "pdf,docx"))
		assert.True(t, isFileAllowed("doc.docx", "allow", "pdf,docx"))
		assert.False(t, isFileAllowed("image.png", "allow", "pdf,docx"))
	})

	t.Run("file with no extension in deny mode", func(t *testing.T) {
		assert.True(t, isFileAllowed("Makefile", "deny", ".exe"))
	})

	t.Run("spaces in filter types are trimmed", func(t *testing.T) {
		assert.True(t, isFileAllowed("report.pdf", "allow", " .pdf , .docx "))
		assert.True(t, isFileAllowed("doc.docx", "allow", " .pdf , .docx "))
	})
}

func TestFileFilterValidation(t *testing.T) {
	t.Run("invalid file_filter_mode fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", FileFilterMode: "invalid", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "file_filter_mode must be")
	})

	t.Run("allow mode without types fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", FileFilterMode: "allow", FileFilterTypes: "", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "file_filter_types is required")
	})

	t.Run("deny mode without types fails validation", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", FileFilterMode: "deny", FileFilterTypes: "", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "file_filter_types is required")
	})

	t.Run("allow mode with types passes", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", FileFilterMode: "allow", FileFilterTypes: ".pdf", NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("file transfer enabled without filter passes", func(t *testing.T) {
		conns := []ConnectionConfig{
			{Name: "test", Provider: "nats", FileTransferEnabled: true, NATS: &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"}},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})
}

func TestBuildTestMessage(t *testing.T) {
	t.Run("JSON format", func(t *testing.T) {
		data, msgID, err := buildTestMessage(model.FormatJSON)
		require.NoError(t, err)
		require.NotEmpty(t, msgID)
		require.NotEmpty(t, data)

		env, err := model.Unmarshal(data, model.FormatJSON)
		require.NoError(t, err)
		assert.Equal(t, model.MessageTypeTest, env.Type)
		assert.NotEmpty(t, env.Timestamp)
		require.NotNil(t, env.TestMessage)
		assert.Equal(t, msgID, env.TestMessage.ID)
	})

	t.Run("XML format", func(t *testing.T) {
		data, msgID, err := buildTestMessage(model.FormatXML)
		require.NoError(t, err)
		require.NotEmpty(t, msgID)
		require.NotEmpty(t, data)

		env, err := model.Unmarshal(data, model.FormatXML)
		require.NoError(t, err)
		assert.Equal(t, model.MessageTypeTest, env.Type)
		assert.NotEmpty(t, env.Timestamp)
		require.NotNil(t, env.TestMessage)
		assert.Equal(t, msgID, env.TestMessage.ID)
	})
}

func TestAzureConfigValidation(t *testing.T) {
	validAzureQueue := func() *AzureQueueProviderConfig {
		return &AzureQueueProviderConfig{
			QueueServiceURL: "https://test.queue.core.windows.net",
			BlobServiceURL:  "https://test.blob.core.windows.net",
			AccountName:     "test",
			AccountKey:      "abc",
			QueueName:       "my-queue",
		}
	}

	t.Run("valid azure connection passes", func(t *testing.T) {
		conns := []ConnectionConfig{{
			Name: "az-test", Provider: "azure-queue", AzureQueue: validAzureQueue(),
		}}
		data, _ := json.Marshal(conns)
		assert.NoError(t, (&configuration{OutboundConnections: string(data)}).validate())
	})

	t.Run("azure missing queue_service_url fails", func(t *testing.T) {
		az := validAzureQueue()
		az.QueueServiceURL = ""
		conns := []ConnectionConfig{{Name: "az-test", Provider: "azure-queue", AzureQueue: az}}
		data, _ := json.Marshal(conns)
		err := (&configuration{OutboundConnections: string(data)}).validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "queue_service_url is required")
	})

	t.Run("azure missing account_name fails", func(t *testing.T) {
		az := validAzureQueue()
		az.AccountName = ""
		conns := []ConnectionConfig{{Name: "az-test", Provider: "azure-queue", AzureQueue: az}}
		data, _ := json.Marshal(conns)
		err := (&configuration{OutboundConnections: string(data)}).validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "account_name is required")
	})

	t.Run("azure missing account_key fails", func(t *testing.T) {
		az := validAzureQueue()
		az.AccountKey = ""
		conns := []ConnectionConfig{{Name: "az-test", Provider: "azure-queue", AzureQueue: az}}
		data, _ := json.Marshal(conns)
		err := (&configuration{OutboundConnections: string(data)}).validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "account_key is required")
	})

	t.Run("azure missing queue_name fails", func(t *testing.T) {
		az := validAzureQueue()
		az.QueueName = ""
		conns := []ConnectionConfig{{Name: "az-test", Provider: "azure-queue", AzureQueue: az}}
		data, _ := json.Marshal(conns)
		err := (&configuration{OutboundConnections: string(data)}).validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "queue_name is required")
	})

	t.Run("azure missing blob_container_name with file transfer fails", func(t *testing.T) {
		az := validAzureQueue()
		az.BlobContainerName = ""
		conns := []ConnectionConfig{{
			Name: "az-test", Provider: "azure-queue", FileTransferEnabled: true, AzureQueue: az,
		}}
		data, _ := json.Marshal(conns)
		err := (&configuration{OutboundConnections: string(data)}).validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "blob_container_name is required")
	})

	t.Run("azure with blob_container_name and file transfer passes", func(t *testing.T) {
		az := validAzureQueue()
		az.BlobContainerName = "my-container"
		conns := []ConnectionConfig{{
			Name: "az-test", Provider: "azure-queue", FileTransferEnabled: true, AzureQueue: az,
		}}
		data, _ := json.Marshal(conns)
		assert.NoError(t, (&configuration{OutboundConnections: string(data)}).validate())
	})

	t.Run("azure-queue missing azure_queue config block fails", func(t *testing.T) {
		conns := []ConnectionConfig{
			{
				Name:       "az-test",
				Provider:   "azure-queue",
				AzureQueue: nil,
			},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{OutboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "azure_queue config block is required")
	})

	t.Run("nats missing nats config block fails", func(t *testing.T) {
		conns := []ConnectionConfig{
			{
				Name:     "nats-test",
				Provider: "nats",
				NATS:     nil,
			},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{OutboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nats config block is required")
	})

	t.Run("unknown provider fails", func(t *testing.T) {
		conns := []ConnectionConfig{
			{
				Name:     "bad",
				Provider: "kafka",
			},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{OutboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "provider must be")
	})

	t.Run("mixed nats and azure connections pass", func(t *testing.T) {
		conns := []ConnectionConfig{
			{
				Name:     "nats-conn",
				Provider: "nats",
				NATS:     &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.test", AuthType: "none"},
			},
			{
				Name:       "az-conn",
				Provider:   "azure-queue",
				AzureQueue: validAzureQueue(),
			},
		}
		data, _ := json.Marshal(conns)
		assert.NoError(t, (&configuration{OutboundConnections: string(data)}).validate())
	})
}

func TestAzureBlobConfigValidation(t *testing.T) {
	validAzureBlob := func() *AzureBlobProviderConfig {
		return &AzureBlobProviderConfig{
			ServiceURL:        "https://test.blob.core.windows.net",
			AccountName:       "test",
			AccountKey:        "abc",
			BlobContainerName: "my-container",
		}
	}
	validAzureQueue := func() *AzureQueueProviderConfig {
		return &AzureQueueProviderConfig{
			QueueServiceURL: "https://test.queue.core.windows.net",
			BlobServiceURL:  "https://test.blob.core.windows.net",
			AccountName:     "test",
			AccountKey:      "abc",
			QueueName:       "my-queue",
		}
	}

	t.Run("valid azure-blob connection passes", func(t *testing.T) {
		conns := []ConnectionConfig{{
			Name: "blob-test", Provider: "azure-blob", AzureBlob: validAzureBlob(),
		}}
		data, _ := json.Marshal(conns)
		assert.NoError(t, (&configuration{OutboundConnections: string(data)}).validate())
	})

	t.Run("azure-blob with valid flush interval passes", func(t *testing.T) {
		ab := validAzureBlob()
		ab.FlushIntervalSeconds = 30
		conns := []ConnectionConfig{{Name: "blob-test", Provider: "azure-blob", AzureBlob: ab}}
		data, _ := json.Marshal(conns)
		assert.NoError(t, (&configuration{OutboundConnections: string(data)}).validate())
	})

	t.Run("azure-blob missing service_url fails", func(t *testing.T) {
		ab := validAzureBlob()
		ab.ServiceURL = ""
		conns := []ConnectionConfig{{Name: "blob-test", Provider: "azure-blob", AzureBlob: ab}}
		data, _ := json.Marshal(conns)
		err := (&configuration{OutboundConnections: string(data)}).validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "service_url is required")
	})

	t.Run("azure-blob missing account_name fails", func(t *testing.T) {
		ab := validAzureBlob()
		ab.AccountName = ""
		conns := []ConnectionConfig{{Name: "blob-test", Provider: "azure-blob", AzureBlob: ab}}
		data, _ := json.Marshal(conns)
		err := (&configuration{OutboundConnections: string(data)}).validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "account_name is required")
	})

	t.Run("azure-blob missing account_key fails", func(t *testing.T) {
		ab := validAzureBlob()
		ab.AccountKey = ""
		conns := []ConnectionConfig{{Name: "blob-test", Provider: "azure-blob", AzureBlob: ab}}
		data, _ := json.Marshal(conns)
		err := (&configuration{OutboundConnections: string(data)}).validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "account_key is required")
	})

	t.Run("azure-blob missing blob_container_name fails", func(t *testing.T) {
		ab := validAzureBlob()
		ab.BlobContainerName = ""
		conns := []ConnectionConfig{{Name: "blob-test", Provider: "azure-blob", AzureBlob: ab}}
		data, _ := json.Marshal(conns)
		err := (&configuration{OutboundConnections: string(data)}).validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "blob_container_name is required")
	})

	t.Run("azure-blob flush_interval below minimum fails", func(t *testing.T) {
		ab := validAzureBlob()
		ab.FlushIntervalSeconds = 3
		conns := []ConnectionConfig{{Name: "blob-test", Provider: "azure-blob", AzureBlob: ab}}
		data, _ := json.Marshal(conns)
		err := (&configuration{OutboundConnections: string(data)}).validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "flush_interval_seconds must be at least 5")
	})

	t.Run("azure-blob missing config block fails", func(t *testing.T) {
		conns := []ConnectionConfig{{Name: "blob-test", Provider: "azure-blob", AzureBlob: nil}}
		data, _ := json.Marshal(conns)
		err := (&configuration{OutboundConnections: string(data)}).validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "azure_blob config block is required")
	})

	t.Run("mixed providers including azure-blob pass", func(t *testing.T) {
		conns := []ConnectionConfig{
			{
				Name:     "nats-conn",
				Provider: "nats",
				NATS:     &NATSProviderConfig{Address: "nats://localhost:4222", Subject: "crossguard.test", AuthType: "none"},
			},
			{Name: "az-conn", Provider: "azure-queue", AzureQueue: validAzureQueue()},
			{Name: "blob-conn", Provider: "azure-blob", AzureBlob: validAzureBlob()},
		}
		data, _ := json.Marshal(conns)
		assert.NoError(t, (&configuration{OutboundConnections: string(data)}).validate())
	})
}

func TestParseConnections_AzureBlobProvider(t *testing.T) {
	raw := `[{"name":"blob-test","provider":"azure-blob","azure_blob":{"service_url":"https://test.blob.core.windows.net","account_name":"test","account_key":"abc","blob_container_name":"mycontainer","flush_interval_seconds":45}}]`
	conns, err := parseConnections(raw)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.Equal(t, ProviderAzureBlob, conns[0].Provider)
	require.NotNil(t, conns[0].AzureBlob)
	assert.Equal(t, "https://test.blob.core.windows.net", conns[0].AzureBlob.ServiceURL)
	assert.Equal(t, "test", conns[0].AzureBlob.AccountName)
	assert.Equal(t, "abc", conns[0].AzureBlob.AccountKey)
	assert.Equal(t, "mycontainer", conns[0].AzureBlob.BlobContainerName)
	assert.Equal(t, 45, conns[0].AzureBlob.FlushIntervalSeconds)
}

func TestParseFilterTypes_MultipleCommas(t *testing.T) {
	result := parseFilterTypes(",,pdf,,docx,,")
	assert.Equal(t, []string{".pdf", ".docx"}, result)
}

func TestParseFilterTypes_WhitespaceHandling(t *testing.T) {
	result := parseFilterTypes("  .pdf , docx , .PNG ")
	assert.Equal(t, []string{".pdf", ".docx", ".png"}, result)
}

func TestParseFilterTypes_EmptyString(t *testing.T) {
	result := parseFilterTypes("")
	assert.Nil(t, result)
}

func TestIsFileAllowed_NoExtension(t *testing.T) {
	allowed := isFileAllowed("Makefile", "allow", ".txt")
	assert.False(t, allowed)
}

func TestIsFileAllowed_CaseSensitivity(t *testing.T) {
	allowed := isFileAllowed("REPORT.PDF", "allow", ".pdf")
	assert.True(t, allowed)
}

func TestIsFileAllowed_DoubleExtension(t *testing.T) {
	allowed := isFileAllowed("archive.tar.gz", "allow", ".gz")
	assert.True(t, allowed)
}

func TestIsFileAllowed_EmptyFilename(t *testing.T) {
	// An empty filename has no extension; in allow mode it should not match any type.
	allowed := isFileAllowed("", "allow", ".pdf")
	assert.False(t, allowed)
}

func TestGetConfiguration_NilReturnsEmpty(t *testing.T) {
	p := &Plugin{}
	// configuration field is nil by default.
	cfg := p.getConfiguration()
	require.NotNil(t, cfg)
	assert.Equal(t, &configuration{}, cfg)
}

func TestOnConfigurationChange_LoadError(t *testing.T) {
	api := &plugintest.API{}
	api.On("LoadPluginConfiguration", mock.Anything).Return(fmt.Errorf("storage unavailable"))

	p := &Plugin{}
	p.SetAPI(api)

	err := p.OnConfigurationChange()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load plugin configuration")
	assert.Contains(t, err.Error(), "storage unavailable")
}

func TestOnConfigurationChange_NoReconnectBeforeActivation(t *testing.T) {
	api := &plugintest.API{}
	api.On("LoadPluginConfiguration", mock.Anything).Return(nil)
	registerLogMocks(api, "LogWarn")

	p := &Plugin{}
	p.SetAPI(api)
	// relaySem is nil (plugin not yet activated), so reconnect methods must not be called.

	err := p.OnConfigurationChange()
	require.NoError(t, err)

	// Verify configuration was set despite relaySem being nil.
	cfg := p.getConfiguration()
	require.NotNil(t, cfg)
}

func TestSetConfiguration_SamePointerLogs(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p := &Plugin{}
	p.SetAPI(api)

	cfg := &configuration{}
	p.configuration = cfg

	// Setting same pointer should log warning and not change config.
	p.setConfiguration(cfg)

	api.AssertCalled(t, "LogWarn", "setConfiguration called with the existing configuration",
		"error_code", errcode.ConfigSameConfigPassed)
}

func TestSetConfiguration_ReplaceConfig(t *testing.T) {
	p := &Plugin{}
	cfg1 := &configuration{OutboundConnections: "old"}
	cfg2 := &configuration{OutboundConnections: "new"}

	p.setConfiguration(cfg1)
	assert.Equal(t, cfg1, p.getConfiguration())

	p.setConfiguration(cfg2)
	assert.Equal(t, cfg2, p.getConfiguration())
}

func TestOnConfigurationChange_WithReconnect(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	// Simulate post-activation state.
	p.relaySem = make(chan struct{}, 50)
	p.fileSem = make(chan struct{}, 32)
	p.inboundCancel = func() {}
	p.configuration = &configuration{}

	api.On("LoadPluginConfiguration", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		// The LoadPluginConfiguration writes to the passed config pointer.
		// Leave it as zero-value config for this test.
	})

	err := p.OnConfigurationChange()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Additional configuration edge-case tests (new)
// ---------------------------------------------------------------------------

func TestParseConnections_AzureQueueProvider(t *testing.T) {
	input := `[{"name":"az-conn","provider":"azure-queue","azure_queue":{"queue_service_url":"https://test.queue.core.windows.net","blob_service_url":"https://test.blob.core.windows.net","account_name":"test","account_key":"abc","queue_name":"myqueue","blob_container_name":"myblob"}}]`
	conns, err := parseConnections(input)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.Equal(t, "az-conn", conns[0].Name)
	assert.Equal(t, ProviderAzureQueue, conns[0].Provider)
	require.NotNil(t, conns[0].AzureQueue)
	assert.Equal(t, "https://test.queue.core.windows.net", conns[0].AzureQueue.QueueServiceURL)
	assert.Equal(t, "https://test.blob.core.windows.net", conns[0].AzureQueue.BlobServiceURL)
	assert.Equal(t, "test", conns[0].AzureQueue.AccountName)
	assert.Equal(t, "abc", conns[0].AzureQueue.AccountKey)
	assert.Equal(t, "myqueue", conns[0].AzureQueue.QueueName)
	assert.Equal(t, "myblob", conns[0].AzureQueue.BlobContainerName)
	assert.Nil(t, conns[0].NATS)
}

func TestParseConnections_MissingNATSConfig(t *testing.T) {
	// Provider is "nats" but no nats config block. parseConnections itself succeeds,
	// but validation should catch it.
	input := `[{"name":"bad-conn","provider":"nats"}]`
	conns, err := parseConnections(input)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.Equal(t, "nats", conns[0].Provider)
	assert.Nil(t, conns[0].NATS)

	// Validate should report an error about missing nats config block.
	cfg := &configuration{OutboundConnections: input}
	valErr := cfg.validate()
	require.Error(t, valErr)
	assert.Contains(t, valErr.Error(), "nats config block is required")
}

func TestValidateConnectionList_MultipleErrors(t *testing.T) {
	conns := []ConnectionConfig{
		{Name: "", Provider: "nats"},                  // missing name and missing nats block
		{Name: "valid", Provider: "invalid-provider"}, // bad provider
	}
	data, _ := json.Marshal(conns)
	cfg := &configuration{InboundConnections: string(data)}
	err := cfg.validate()
	require.Error(t, err)
	// Should contain errors for both connections.
	assert.Contains(t, err.Error(), "name is required")
	assert.Contains(t, err.Error(), "provider must be")
}

func TestParseConnections_DefaultProviderNATS(t *testing.T) {
	// Empty/missing provider field should default to "nats".
	input := `[{"name":"default-conn","nats":{"address":"nats://localhost:4222","subject":"crossguard.test"}}]`
	conns, err := parseConnections(input)
	require.NoError(t, err)
	require.Len(t, conns, 1)
	assert.Equal(t, ProviderNATS, conns[0].Provider, "empty provider should default to nats")
	require.NotNil(t, conns[0].NATS)
	assert.Equal(t, "default-conn", conns[0].NATS.Name, "nested name should be populated from parent")
}

func TestValidatePollInterval(t *testing.T) {
	t.Run("zero returns nil (disabled)", func(t *testing.T) {
		assert.Nil(t, validatePollInterval(0, "conn[0]", "poll_interval_seconds"))
	})

	t.Run("negative is rejected", func(t *testing.T) {
		errs := validatePollInterval(-1, "conn[0]", "poll_interval_seconds")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "must be at least 1")
	})

	t.Run("valid values accepted", func(t *testing.T) {
		assert.Nil(t, validatePollInterval(1, "conn[0]", "poll_interval_seconds"))
		assert.Nil(t, validatePollInterval(pollIntervalMaxSeconds, "conn[0]", "poll_interval_seconds"))
	})

	t.Run("above max is rejected", func(t *testing.T) {
		errs := validatePollInterval(pollIntervalMaxSeconds+1, "conn[0]", "poll_interval_seconds")
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0], "must be at most")
	})
}
