package main

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	require.Len(t, got.SyncMsg.Posts, 1)
	assert.Equal(t, "hello", got.SyncMsg.Posts[0].Message)
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

func TestSplitTransportEnvelopeFits(t *testing.T) {
	env := &TransportEnvelope{
		Type: TransportTypeSyncMsg,
		SyncMsg: wire.SyncMsgFromModel(&mmModel.SyncMsg{
			Id:        "sm",
			ChannelId: "ch",
			Posts: []*mmModel.Post{
				{Id: "p1", Message: "short"},
			},
		}),
	}
	parts, err := splitTransportEnvelope(env, 1<<20)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Same(t, env, parts[0])
}

func TestSplitTransportEnvelopeNoLimit(t *testing.T) {
	env := &TransportEnvelope{
		Type: TransportTypeSyncMsg,
		SyncMsg: wire.SyncMsgFromModel(&mmModel.SyncMsg{
			Id:        "sm",
			ChannelId: "ch",
			Posts: []*mmModel.Post{
				{Id: "p1", Message: strings.Repeat("x", 10000)},
			},
		}),
	}
	parts, err := splitTransportEnvelope(env, 0)
	require.NoError(t, err)
	require.Len(t, parts, 1)
}

func TestSplitTransportEnvelopeMultiplePosts(t *testing.T) {
	posts := make([]*mmModel.Post, 10)
	for i := range posts {
		posts[i] = &mmModel.Post{
			Id:      mmModel.NewId(),
			Message: strings.Repeat("a", 200),
		}
	}
	reactions := []*mmModel.Reaction{
		{UserId: "u1", PostId: posts[0].Id, EmojiName: "tada"},
		{UserId: "u1", PostId: posts[5].Id, EmojiName: "ok_hand"},
	}

	env := &TransportEnvelope{
		Type: TransportTypeSyncMsg,
		SyncMsg: wire.SyncMsgFromModel(&mmModel.SyncMsg{
			Id:        "sm",
			ChannelId: "ch",
			Users:     map[string]*mmModel.User{"u1": {Id: "u1", Username: "alice"}},
			Posts:     posts,
			Reactions: reactions,
		}),
	}

	full, err := MarshalEnvelope(env)
	require.NoError(t, err)
	maxSize := len(full) / 3

	parts, err := splitTransportEnvelope(env, maxSize)
	require.NoError(t, err)
	require.Greater(t, len(parts), 1, "envelope should split into multiple parts")

	totalPosts := 0
	totalReactions := 0
	for _, part := range parts {
		require.NotNil(t, part.SyncMsg)
		totalPosts += len(part.SyncMsg.Posts)
		totalReactions += len(part.SyncMsg.Reactions)
		assert.Equal(t, env.SyncMsg.Users, part.SyncMsg.Users, "users map should be in every split")
	}
	assert.Equal(t, len(posts), totalPosts)
	assert.Equal(t, len(reactions), totalReactions)
}

func TestSplitTransportEnvelopeUsersOnly(t *testing.T) {
	env := &TransportEnvelope{
		Type: TransportTypeSyncMsg,
		SyncMsg: wire.SyncMsgFromModel(&mmModel.SyncMsg{
			Id:        "sm",
			ChannelId: "ch",
			Users:     map[string]*mmModel.User{"u1": {Id: "u1", Username: "alice"}},
		}),
	}
	parts, err := splitTransportEnvelope(env, 10)
	require.NoError(t, err)
	require.Len(t, parts, 1, "users-only sync should not be split")
}

func TestSplitTransportEnvelopeSinglePostOversize(t *testing.T) {
	env := &TransportEnvelope{
		Type: TransportTypeSyncMsg,
		SyncMsg: wire.SyncMsgFromModel(&mmModel.SyncMsg{
			Id:        "sm",
			ChannelId: "ch",
			Posts: []*mmModel.Post{
				{Id: "p1", Message: strings.Repeat("y", 5000)},
			},
		}),
	}
	parts, err := splitTransportEnvelope(env, 100)
	require.NoError(t, err)
	require.Len(t, parts, 1, "single post that exceeds the limit cannot be split")
}

func TestSplitTransportEnvelopeUTF8Safety(t *testing.T) {
	posts := make([]*mmModel.Post, 5)
	for i := range posts {
		posts[i] = &mmModel.Post{
			Id:      mmModel.NewId(),
			Message: strings.Repeat("中文", 50),
		}
	}
	env := &TransportEnvelope{
		Type: TransportTypeSyncMsg,
		SyncMsg: wire.SyncMsgFromModel(&mmModel.SyncMsg{
			Id:        "sm",
			ChannelId: "ch",
			Posts:     posts,
		}),
	}

	full, err := MarshalEnvelope(env)
	require.NoError(t, err)
	parts, err := splitTransportEnvelope(env, len(full)/2)
	require.NoError(t, err)
	for _, part := range parts {
		for _, p := range part.SyncMsg.Posts {
			assert.True(t, utf8.ValidString(p.Message), "split post must remain valid UTF-8")
		}
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
