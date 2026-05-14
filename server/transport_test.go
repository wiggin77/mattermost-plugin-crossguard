package main

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/wire"
)

func TestTransportEnvelopeMarshalRoundTrip(t *testing.T) {
	env := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    "conn-a",
		Timestamp:   "2026-05-11T10:00:00Z",
		Epoch:       "epoch01aaaaaaaaaaaaaaaaaaa",
		Sequence:    42,
		TeamName:    "team-a",
		ChannelName: "channel-a",
		SyncMsg: wire.SyncMsgFromModel(&mmModel.SyncMsg{
			Id:        "sm1",
			ChannelId: "ch1",
			Users: map[string]*mmModel.User{
				"u1": {Id: "u1", Username: "alice", Roles: "system_user", UpdateAt: 100},
			},
			Posts: []*mmModel.Post{
				{Id: "p1", ChannelId: "ch1", Message: "hello", UpdateAt: 200},
			},
			Reactions: []*mmModel.Reaction{
				{UserId: "u1", PostId: "p1", EmojiName: "thumbsup", UpdateAt: 250},
			},
		}),
	}

	data, err := MarshalEnvelope(env)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(data), xml.Header))

	got, err := UnmarshalEnvelope(data)
	require.NoError(t, err)

	assert.Equal(t, env.Version, got.Version)
	assert.Equal(t, env.Type, got.Type)
	assert.Equal(t, env.ConnName, got.ConnName)
	assert.Equal(t, env.Timestamp, got.Timestamp)
	assert.Equal(t, env.Epoch, got.Epoch)
	assert.Equal(t, env.Sequence, got.Sequence)
	assert.Equal(t, env.TeamName, got.TeamName)
	assert.Equal(t, env.ChannelName, got.ChannelName)

	require.NotNil(t, got.SyncMsg)
	assert.Equal(t, env.SyncMsg.Id, got.SyncMsg.Id)
	assert.Equal(t, env.SyncMsg.ChannelId, got.SyncMsg.ChannelId)
	require.Len(t, got.SyncMsg.Users, 1)
	assert.Equal(t, "alice", got.SyncMsg.Users["u1"].Username)
	require.NotNil(t, got.SyncMsg.Post)
	assert.Equal(t, "hello", got.SyncMsg.Post.Message)
	require.Len(t, got.SyncMsg.Reactions, 1)
	assert.Equal(t, "thumbsup", got.SyncMsg.Reactions[0].EmojiName)
}

func TestTransportEnvelopeEpochSequenceOmittedWhenZero(t *testing.T) {
	env := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    "conn-a",
		Timestamp:   "2026-05-11T10:00:00Z",
		TeamName:    "team-a",
		ChannelName: "channel-a",
		SyncMsg: wire.SyncMsgFromModel(&mmModel.SyncMsg{
			Id:        "sm1",
			ChannelId: "ch1",
		}),
	}

	data, err := MarshalEnvelope(env)
	require.NoError(t, err)

	assert.NotContains(t, string(data), "<Epoch>")
	assert.NotContains(t, string(data), "<Sequence>")

	got, err := UnmarshalEnvelope(data)
	require.NoError(t, err)
	assert.Empty(t, got.Epoch)
	assert.Zero(t, got.Sequence)
}

func TestTransportEnvelopeDefaultsTimestamp(t *testing.T) {
	env := &TransportEnvelope{Type: TransportTypeTest, TestID: "x"}
	_, err := MarshalEnvelope(env)
	require.NoError(t, err)
	require.NotEmpty(t, env.Timestamp, "MarshalEnvelope should default Timestamp when empty")
	_, err = time.Parse(time.RFC3339, env.Timestamp)
	require.NoError(t, err, "defaulted Timestamp must be RFC 3339")
}

func TestTransportEnvelopePreservesTimestamp(t *testing.T) {
	want := "2026-01-02T03:04:05Z"
	env := &TransportEnvelope{Type: TransportTypeTest, TestID: "x", Timestamp: want}
	_, err := MarshalEnvelope(env)
	require.NoError(t, err)
	assert.Equal(t, want, env.Timestamp)
}

func TestTransportEnvelopeMarshalTest(t *testing.T) {
	env := &TransportEnvelope{
		Type:   TransportTypeTest,
		TestID: "abc123",
	}
	data, err := MarshalEnvelope(env)
	require.NoError(t, err)
	got, err := UnmarshalEnvelope(data)
	require.NoError(t, err)
	assert.Equal(t, "test", got.Type)
	assert.Equal(t, "abc123", got.TestID)
	assert.Nil(t, got.SyncMsg)
}

func TestFanoutSinglePostNoMetadata(t *testing.T) {
	template := &TransportEnvelope{
		Version:  1,
		Type:     TransportTypeSyncMsg,
		ConnName: "low-to-high",
	}
	msg := &mmModel.SyncMsg{
		Id:        "sm-1",
		ChannelId: "ch",
		Users: map[string]*mmModel.User{
			"u1": {Id: "u1", Username: "alice"},
		},
		Posts: []*mmModel.Post{{Id: "p1", UserId: "u1", Message: "hello"}},
	}
	envs := buildOutboundEnvelopes(template, msg)
	require.Len(t, envs, 1)
	require.NotNil(t, envs[0].SyncMsg.Post)
	assert.Equal(t, "p1", envs[0].SyncMsg.Post.Id)
	require.Len(t, envs[0].SyncMsg.Users, 1)
	assert.Contains(t, envs[0].SyncMsg.Users, "u1")
}

func TestFanoutMultiPostWithReactions(t *testing.T) {
	template := &TransportEnvelope{Version: 1, Type: TransportTypeSyncMsg, ConnName: "c"}
	msg := &mmModel.SyncMsg{
		Id:        "sm",
		ChannelId: "ch",
		Users: map[string]*mmModel.User{
			"u1": {Id: "u1", Username: "alice"},
			"u2": {Id: "u2", Username: "bob"},
		},
		Posts: []*mmModel.Post{
			{Id: "p1", UserId: "u1", Message: "first"},
			{Id: "p2", UserId: "u2", Message: "second"},
		},
		Reactions: []*mmModel.Reaction{
			{UserId: "u2", PostId: "p1", EmojiName: "thumbsup"},
		},
	}
	envs := buildOutboundEnvelopes(template, msg)
	require.Len(t, envs, 2, "two posts produce two envelopes; reaction rides with its post")

	// First post envelope carries p1 + its reaction + the author.
	assert.Equal(t, "p1", envs[0].SyncMsg.Post.Id)
	require.Len(t, envs[0].SyncMsg.Reactions, 1)
	assert.Equal(t, "thumbsup", envs[0].SyncMsg.Reactions[0].EmojiName)
	assert.Contains(t, envs[0].SyncMsg.Users, "u1", "post envelope inlines the post's author only")
	assert.NotContains(t, envs[0].SyncMsg.Users, "u2", "non-author users do not ride with the post")

	// Second post envelope carries p2 with no reaction.
	assert.Equal(t, "p2", envs[1].SyncMsg.Post.Id)
	assert.Empty(t, envs[1].SyncMsg.Reactions)
	assert.Contains(t, envs[1].SyncMsg.Users, "u2")
}

func TestFanoutMetadataOnly(t *testing.T) {
	template := &TransportEnvelope{Version: 1, Type: TransportTypeSyncMsg, ConnName: "c"}
	msg := &mmModel.SyncMsg{
		Id:        "sm",
		ChannelId: "ch",
		Users: map[string]*mmModel.User{
			"u1": {Id: "u1", Username: "alice"},
		},
		MembershipChanges: []*mmModel.MembershipChangeMsg{
			{ChannelId: "ch", UserId: "u1", IsAdd: true, ChangeTime: 12345},
		},
	}
	envs := buildOutboundEnvelopes(template, msg)
	require.Len(t, envs, 1, "membership-only sync produces one metadata envelope")
	assert.Nil(t, envs[0].SyncMsg.Post, "metadata envelope has no <Post>")
	require.Len(t, envs[0].SyncMsg.MembershipChanges, 1)
	assert.Contains(t, envs[0].SyncMsg.Users, "u1")
}

func TestFanoutPostsPlusOrphanReaction(t *testing.T) {
	template := &TransportEnvelope{Version: 1, Type: TransportTypeSyncMsg, ConnName: "c"}
	msg := &mmModel.SyncMsg{
		Id:        "sm",
		ChannelId: "ch",
		Users: map[string]*mmModel.User{
			"u1": {Id: "u1", Username: "alice"},
			"u2": {Id: "u2", Username: "bob"},
		},
		Posts: []*mmModel.Post{
			{Id: "p1", UserId: "u1", Message: "in this batch"},
		},
		Reactions: []*mmModel.Reaction{
			{UserId: "u2", PostId: "p999", EmojiName: "smile"}, // orphan: p999 not in this batch
		},
	}
	envs := buildOutboundEnvelopes(template, msg)
	require.Len(t, envs, 2, "one post envelope + one metadata envelope for the orphan")
	assert.NotNil(t, envs[0].SyncMsg.Post)
	assert.Empty(t, envs[0].SyncMsg.Reactions, "p1 has no reactions in this batch")
	assert.Nil(t, envs[1].SyncMsg.Post)
	require.Len(t, envs[1].SyncMsg.Reactions, 1)
	assert.Equal(t, "p999", envs[1].SyncMsg.Reactions[0].PostId)
	assert.Contains(t, envs[1].SyncMsg.Users, "u2", "reacting user inlined on metadata envelope")
}

func TestFanoutEmptySyncMsgEmitsBare(t *testing.T) {
	template := &TransportEnvelope{Version: 1, Type: TransportTypeSyncMsg, ConnName: "c"}
	msg := &mmModel.SyncMsg{Id: "sm", ChannelId: "ch"}
	envs := buildOutboundEnvelopes(template, msg)
	require.Len(t, envs, 1, "empty sync msg still emits one envelope so the cursor advances")
	assert.Nil(t, envs[0].SyncMsg.Post)
	assert.Empty(t, envs[0].SyncMsg.Reactions)
	assert.Empty(t, envs[0].SyncMsg.MembershipChanges)
}

func TestFanoutPostsOnlyDistinctUsers(t *testing.T) {
	template := &TransportEnvelope{Version: 1, Type: TransportTypeSyncMsg, ConnName: "c"}
	msg := &mmModel.SyncMsg{
		Id:        "sm",
		ChannelId: "ch",
		Users: map[string]*mmModel.User{
			"u1": {Id: "u1", Username: "alice"},
			"u2": {Id: "u2", Username: "bob"},
			"u3": {Id: "u3", Username: "carol"},
		},
		Posts: []*mmModel.Post{
			{Id: "p1", UserId: "u1", Message: "from alice"},
			{Id: "p2", UserId: "u2", Message: "from bob"},
		},
	}
	envs := buildOutboundEnvelopes(template, msg)
	require.Len(t, envs, 2)

	// Each post envelope inlines only its post's author. carol (u3) is
	// referenced by nothing in this batch so she doesn't appear anywhere.
	require.Len(t, envs[0].SyncMsg.Users, 1)
	assert.Contains(t, envs[0].SyncMsg.Users, "u1")
	assert.NotContains(t, envs[0].SyncMsg.Users, "u2")
	assert.NotContains(t, envs[0].SyncMsg.Users, "u3")

	require.Len(t, envs[1].SyncMsg.Users, 1)
	assert.Contains(t, envs[1].SyncMsg.Users, "u2")
	assert.NotContains(t, envs[1].SyncMsg.Users, "u1")
	assert.NotContains(t, envs[1].SyncMsg.Users, "u3")
}

func TestFanoutReactionWithoutPostUser(t *testing.T) {
	// Orphan reaction whose user is not in msg.Users. The metadata
	// envelope should still be built; the user lookup just misses
	// without nil deref.
	template := &TransportEnvelope{Version: 1, Type: TransportTypeSyncMsg, ConnName: "c"}
	msg := &mmModel.SyncMsg{
		Id:        "sm",
		ChannelId: "ch",
		Users:     map[string]*mmModel.User{}, // empty
		Reactions: []*mmModel.Reaction{
			{UserId: "u-unknown", PostId: "p-elsewhere", EmojiName: "smile"},
		},
	}

	require.NotPanics(t, func() {
		envs := buildOutboundEnvelopes(template, msg)
		require.Len(t, envs, 1, "one metadata envelope")
		assert.Nil(t, envs[0].SyncMsg.Post)
		require.Len(t, envs[0].SyncMsg.Reactions, 1)
		assert.Empty(t, envs[0].SyncMsg.Users, "lookup miss is silent")
	})
}

func TestFanoutMembershipPlusOrphanReactionSameUser(t *testing.T) {
	// Membership and orphan reaction both reference u1. The metadata
	// envelope should dedupe the user (Users is a map, not a slice).
	template := &TransportEnvelope{Version: 1, Type: TransportTypeSyncMsg, ConnName: "c"}
	msg := &mmModel.SyncMsg{
		Id:        "sm",
		ChannelId: "ch",
		Users: map[string]*mmModel.User{
			"u1": {Id: "u1", Username: "alice"},
		},
		Reactions: []*mmModel.Reaction{
			{UserId: "u1", PostId: "p-elsewhere", EmojiName: "+1"},
		},
		MembershipChanges: []*mmModel.MembershipChangeMsg{
			{ChannelId: "ch", UserId: "u1", IsAdd: true, ChangeTime: 1},
		},
	}
	envs := buildOutboundEnvelopes(template, msg)
	require.Len(t, envs, 1, "one metadata envelope; no posts")
	require.Len(t, envs[0].SyncMsg.Users, 1, "user deduped across membership + reaction")
	assert.Contains(t, envs[0].SyncMsg.Users, "u1")
}

func TestFanoutNilEntries(t *testing.T) {
	// Defensive: nil entries in Posts/Reactions/Acknowledgements/etc.
	// should be skipped, not panic.
	template := &TransportEnvelope{Version: 1, Type: TransportTypeSyncMsg, ConnName: "c"}
	msg := &mmModel.SyncMsg{
		Id:        "sm",
		ChannelId: "ch",
		Users: map[string]*mmModel.User{
			"u1": {Id: "u1", Username: "alice"},
		},
		Posts: []*mmModel.Post{
			nil,
			{Id: "p1", UserId: "u1", Message: "real"},
			nil,
		},
		Reactions: []*mmModel.Reaction{
			nil,
			{UserId: "u1", PostId: "p1", EmojiName: "ok"},
		},
		Acknowledgements: []*mmModel.PostAcknowledgement{
			nil,
		},
		MembershipChanges: []*mmModel.MembershipChangeMsg{
			nil,
		},
	}

	require.NotPanics(t, func() {
		envs := buildOutboundEnvelopes(template, msg)
		// Exactly one post envelope, no metadata envelope (nil membership
		// is skipped and the reaction is local to p1).
		require.Len(t, envs, 1)
		require.NotNil(t, envs[0].SyncMsg.Post)
		assert.Equal(t, "p1", envs[0].SyncMsg.Post.Id)
		require.Len(t, envs[0].SyncMsg.Reactions, 1)
	})
}

func TestFanoutMentionTransformsDuplicatedPerPostEnvelope(t *testing.T) {
	// MentionTransforms is the same map on every post envelope: the
	// fanout duplicates it across envelopes by design (size trade-off
	// documented in plan 02).
	template := &TransportEnvelope{Version: 1, Type: TransportTypeSyncMsg, ConnName: "c"}
	msg := &mmModel.SyncMsg{
		Id:        "sm",
		ChannelId: "ch",
		Users: map[string]*mmModel.User{
			"u1": {Id: "u1", Username: "alice"},
			"u2": {Id: "u2", Username: "bob"},
		},
		Posts: []*mmModel.Post{
			{Id: "p1", UserId: "u1", Message: "first"},
			{Id: "p2", UserId: "u2", Message: "second"},
		},
		MentionTransforms: map[string]string{
			"@oldname": "@newname",
		},
	}
	envs := buildOutboundEnvelopes(template, msg)
	require.Len(t, envs, 2)
	for i, env := range envs {
		require.NotNil(t, env.SyncMsg.MentionTransforms, "envelope %d", i)
		assert.Equal(t, "@newname", env.SyncMsg.MentionTransforms["@oldname"], "envelope %d", i)
	}
}

func TestFanoutSyncMsgIDAndChannelIDPreserved(t *testing.T) {
	// Every emitted envelope inherits Id and ChannelId from the source
	// SyncMsg, regardless of whether it's a post envelope or metadata
	// envelope or the empty-bare case.
	template := &TransportEnvelope{Version: 1, Type: TransportTypeSyncMsg, ConnName: "c"}
	msg := &mmModel.SyncMsg{
		Id:        "sm-source",
		ChannelId: "ch-source",
		Users: map[string]*mmModel.User{
			"u1": {Id: "u1", Username: "alice"},
		},
		Posts: []*mmModel.Post{
			{Id: "p1", UserId: "u1", Message: "x"},
		},
		MembershipChanges: []*mmModel.MembershipChangeMsg{
			{ChannelId: "ch-source", UserId: "u1", IsAdd: true, ChangeTime: 1},
		},
	}
	envs := buildOutboundEnvelopes(template, msg)
	require.Len(t, envs, 2, "one post + one metadata")
	for i, env := range envs {
		assert.Equal(t, "sm-source", env.SyncMsg.Id, "envelope %d Id", i)
		assert.Equal(t, "ch-source", env.SyncMsg.ChannelId, "envelope %d ChannelId", i)
	}
}

func TestBuildSyncResponse(t *testing.T) {
	msg := &mmModel.SyncMsg{
		Users: map[string]*mmModel.User{
			"u1": {UpdateAt: 100},
			"u2": {UpdateAt: 250},
		},
		Posts: []*mmModel.Post{
			{UpdateAt: 300},
			{UpdateAt: 400},
		},
		Reactions: []*mmModel.Reaction{
			{UpdateAt: 350},
		},
		Acknowledgements: []*mmModel.PostAcknowledgement{
			{AcknowledgedAt: 500},
		},
	}
	resp := buildSyncResponse(msg)
	assert.Equal(t, int64(250), resp.UsersLastUpdateAt)
	assert.Equal(t, int64(400), resp.PostsLastUpdateAt)
	assert.Equal(t, int64(350), resp.ReactionsLastUpdateAt)
	assert.Equal(t, int64(500), resp.AcknowledgementsLastUpdateAt)
}

func TestBuildSyncResponseNil(t *testing.T) {
	resp := buildSyncResponse(nil)
	assert.Equal(t, mmModel.SyncResponse{}, resp)
}

func TestTransportEnvelopeXMLReadable(t *testing.T) {
	env := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    "conn",
		TeamName:    "team",
		ChannelName: "channel",
		SyncMsg: wire.SyncMsgFromModel(&mmModel.SyncMsg{
			Id:        "sm1",
			ChannelId: "ch1",
		}),
	}
	data, err := MarshalEnvelope(env)
	require.NoError(t, err)
	out := string(data)
	assert.Contains(t, out, "<CrossGuardEnvelope")
	assert.Contains(t, out, `version="1"`)
	assert.Contains(t, out, `type="sync_msg"`)
	assert.Contains(t, out, "<ConnName>conn</ConnName>")
}
