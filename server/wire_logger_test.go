package main

import (
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/wire"
)

// TestPluginWireLoggerForwardsToAPI verifies the adapter forwards
// Warn/Error events to the plugin.API with the schema-grade K/V
// payload: error_code first, then the adapter's base context, then
// the event's own pairs.
func TestPluginWireLoggerForwardsToAPI(t *testing.T) {
	api := &plugintest.API{}
	defer api.AssertExpectations(t)

	api.On("LogWarn",
		"wire-layer validation event",
		"error_code", errcode.WireUsernameTruncatedAtColon,
		"conn_name", "nats-low-to-high",
		"channel_id", "ch1",
		"user_id", "u01",
	).Once()
	api.On("LogError",
		"wire-layer validation event",
		"error_code", errcode.WireUserDroppedNonConformingEmail,
		"conn_name", "nats-low-to-high",
		"channel_id", "ch1",
		"user_id", "u02",
	).Once()

	log := newPluginWireLogger(api,
		"conn_name", "nats-low-to-high",
		"channel_id", "ch1",
	)
	log.Warn(errcode.WireUsernameTruncatedAtColon, "user_id", "u01")
	log.Error(errcode.WireUserDroppedNonConformingEmail, "user_id", "u02")
}

// TestWithPostContextAppendsPostID verifies the per-post context
// derivation: a post-aware logger carries conn/channel context plus
// post_id, so a single wire-layer log already correlates the drop
// with the post that triggered it.
func TestWithPostContextAppendsPostID(t *testing.T) {
	api := &plugintest.API{}
	defer api.AssertExpectations(t)

	api.On("LogError",
		"wire-layer validation event",
		"error_code", errcode.WireUserDroppedNonConformingUsername,
		"conn_name", "c",
		"channel_id", "ch1",
		"post_id", "p01",
		"user_id", "u01",
	).Once()

	base := newPluginWireLogger(api, "conn_name", "c", "channel_id", "ch1")
	postLog := withPostContext(base, "p01")
	postLog.Error(errcode.WireUserDroppedNonConformingUsername, "user_id", "u01")
}

// TestWithPostContextPassThroughForForeignLogger verifies that a
// non-pluginWireLogger (e.g., a wire.RecordingLogger in tests) is
// returned unchanged. The wire package's recording logger has its
// own context-free API, so re-wrapping it would not add value and
// could mask semantics.
func TestWithPostContextPassThroughForForeignLogger(t *testing.T) {
	rec := wire.NewRecordingLogger()
	got := withPostContext(rec, "p01")
	assert.Same(t, rec, got)
}

// TestBuildOutboundEnvelopesAuditsDroppedUser exercises the full
// audit-log path: a sync msg with a user whose Email fails the
// EmailType pattern. buildOutboundEnvelopes should still ship the
// post (one envelope) but record an Error with post_id correlation.
func TestBuildOutboundEnvelopesAuditsDroppedUser(t *testing.T) {
	api := &plugintest.API{}
	defer api.AssertExpectations(t)

	// The post envelope is still built. UserFromModel returns nil for
	// the non-conforming user, the wire layer logs an Error, and the
	// post is shipped with no inline user (receiver resolves identity
	// through its own flow). Verify the audit log fires.
	api.On("LogError",
		"wire-layer validation event",
		"error_code", errcode.WireUserDroppedNonConformingEmail,
		"conn_name", "c",
		"channel_id", "ch1",
		"post_id", "p01",
		"user_id", "u01",
	).Once()

	template := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    "c",
		TeamName:    "team-a",
		ChannelName: "channel-a",
	}
	msg := &mmModel.SyncMsg{
		Id:        "sm1",
		ChannelId: "ch1",
		Users: map[string]*mmModel.User{
			"u01": {Id: "u01", Username: "alice", Email: "o'brien@bad.com"}, // apostrophe -> non-conforming
		},
		Posts: []*mmModel.Post{
			{Id: "p01", UserId: "u01", Message: "hello", ChannelId: "ch1"},
		},
	}

	log := newPluginWireLogger(api, "conn_name", "c", "channel_id", "ch1")
	envs := buildOutboundEnvelopes(log, template, msg)
	require.Len(t, envs, 1, "post still ships even though user was dropped")
	require.NotNil(t, envs[0].SyncMsg.Post)
	assert.Equal(t, "p01", envs[0].SyncMsg.Post.Id)
	assert.NotContains(t, envs[0].SyncMsg.Users, "u01",
		"non-conforming user must not appear in the wire envelope")
}

// TestBuildOutboundEnvelopesConformingUsersNoAudit verifies the
// negative case: conforming inputs must not emit any wire-layer audit
// events.
func TestBuildOutboundEnvelopesConformingUsersNoAudit(t *testing.T) {
	api := &plugintest.API{}
	// Strict: no LogWarn / LogError expected.
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything).Maybe()
	defer api.AssertExpectations(t)

	template := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    "c",
		TeamName:    "team-a",
		ChannelName: "channel-a",
	}
	msg := &mmModel.SyncMsg{
		Id:        "sm1",
		ChannelId: "ch1",
		Users: map[string]*mmModel.User{
			"u01": {Id: "u01", Username: "alice", Email: "alice@example.test"},
		},
		Posts: []*mmModel.Post{
			{Id: "p01", UserId: "u01", Message: "hi", ChannelId: "ch1"},
		},
	}

	log := newPluginWireLogger(api, "conn_name", "c")
	envs := buildOutboundEnvelopes(log, template, msg)
	require.Len(t, envs, 1)
	require.NotNil(t, envs[0].SyncMsg.Post)
	require.Contains(t, envs[0].SyncMsg.Users, "u01")
}
