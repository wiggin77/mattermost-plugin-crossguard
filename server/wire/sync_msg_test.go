package wire

import (
	"encoding/xml"
	"strings"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserMapMarshalSortedByKey(t *testing.T) {
	sm := &SyncMsg{
		Id:        "sm1",
		ChannelId: "ch1",
		Users: UserMap{
			"u02": {Id: "u02", Username: "bob"},
			"u01": {Id: "u01", Username: "alice"},
			"u03": {Id: "u03", Username: "carol"},
		},
	}
	data, err := xml.Marshal(sm)
	require.NoError(t, err)
	out := string(data)

	// Element order in output must be u01 < u02 < u03.
	idx01 := strings.Index(out, `id="u01"`)
	idx02 := strings.Index(out, `id="u02"`)
	idx03 := strings.Index(out, `id="u03"`)
	require.Greater(t, idx01, -1)
	require.Greater(t, idx02, -1)
	require.Greater(t, idx03, -1)
	assert.Less(t, idx01, idx02)
	assert.Less(t, idx02, idx03)
}

func TestUserMapEmittedShape(t *testing.T) {
	sm := &SyncMsg{
		Id:        "sm1",
		ChannelId: "ch1",
		Users: UserMap{
			"u01": {Id: "u01", Username: "alice"},
		},
	}
	data, err := xml.Marshal(sm)
	require.NoError(t, err)
	out := string(data)

	// No upstream-style <UserEntry> wrapper around each user.
	assert.NotContains(t, out, "<UserEntry")
	// Each user has an id attribute on its <User> element directly.
	assert.Contains(t, out, `<Users><User id="u01">`)
}

func TestUserMapOmittedWhenEmpty(t *testing.T) {
	sm := &SyncMsg{Id: "sm1", ChannelId: "ch1"}
	data, err := xml.Marshal(sm)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "<Users>")
}

func TestUserMapRoundTrip(t *testing.T) {
	in := &SyncMsg{
		Id:        "sm1",
		ChannelId: "ch1",
		Users: UserMap{
			"u01": {Id: "u01", Username: "alice", Email: "alice@example.com"},
			"u02": {Id: "u02", Username: "bob", Email: "bob@example.com"},
		},
	}
	data, err := xml.Marshal(in)
	require.NoError(t, err)

	var got SyncMsg
	require.NoError(t, xml.Unmarshal(data, &got))
	require.Len(t, got.Users, 2)
	assert.Equal(t, "alice", got.Users["u01"].Username)
	assert.Equal(t, "alice@example.com", got.Users["u01"].Email)
	assert.Equal(t, "bob", got.Users["u02"].Username)
}

func TestMentionTransformsMarshalSortedByKey(t *testing.T) {
	sm := &SyncMsg{
		Id:        "sm1",
		ChannelId: "ch1",
		MentionTransforms: MentionTransforms{
			"@bob":   "@bob.remote",
			"@alice": "@alice.remote",
		},
	}
	data, err := xml.Marshal(sm)
	require.NoError(t, err)
	out := string(data)

	idxA := strings.Index(out, `key="@alice"`)
	idxB := strings.Index(out, `key="@bob"`)
	require.Greater(t, idxA, -1)
	require.Greater(t, idxB, -1)
	assert.Less(t, idxA, idxB)
}

func TestMentionTransformsOmittedWhenEmpty(t *testing.T) {
	sm := &SyncMsg{Id: "sm1", ChannelId: "ch1"}
	data, err := xml.Marshal(sm)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "<MentionTransforms")
}

func TestMentionTransformsRoundTrip(t *testing.T) {
	in := &SyncMsg{
		Id:        "sm1",
		ChannelId: "ch1",
		MentionTransforms: MentionTransforms{
			"@alice": "@alice.remote",
			"@bob":   "@bob.remote",
		},
	}
	data, err := xml.Marshal(in)
	require.NoError(t, err)

	var got SyncMsg
	require.NoError(t, xml.Unmarshal(data, &got))
	require.Len(t, got.MentionTransforms, 2)
	assert.Equal(t, "@alice.remote", got.MentionTransforms["@alice"])
	assert.Equal(t, "@bob.remote", got.MentionTransforms["@bob"])
}

func TestSyncMsgFromModelNil(t *testing.T) {
	assert.Nil(t, SyncMsgFromModel(nil))
}

func TestSyncMsgToModelNil(t *testing.T) {
	var m *SyncMsg
	assert.Nil(t, m.ToModel())
}

func TestSyncMsgFromModelEmpty(t *testing.T) {
	in := &mmModel.SyncMsg{Id: "sm1", ChannelId: "ch01"}
	got := SyncMsgFromModel(in)
	require.NotNil(t, got)
	assert.Equal(t, "sm1", got.Id)
	assert.Equal(t, "ch01", got.ChannelId)
	assert.Nil(t, got.Users)
	assert.Nil(t, got.Posts)
	assert.Nil(t, got.MentionTransforms)
}

func TestSyncMsgFullRoundTrip(t *testing.T) {
	rid := "remote-1"
	original := &mmModel.SyncMsg{
		Id:        "sm-full",
		ChannelId: "ch01",
		Users: map[string]*mmModel.User{
			"u01": {
				Id:       "u01",
				Username: "alice",
				Email:    "alice@example.com",
				Roles:    "system_user",
				Props:    mmModel.StringMap{"customStatus": "status-x"},
			},
		},
		Posts: []*mmModel.Post{
			{
				Id:        "p01",
				CreateAt:  1700000000,
				UpdateAt:  1700000001,
				UserId:    "u01",
				ChannelId: "ch01",
				Message:   "hello",
				RemoteId:  &rid,
				Props: mmModel.StringInterface{
					"from_webhook":      true,
					"override_username": "Alice",
				},
			},
		},
		Reactions: []*mmModel.Reaction{
			{
				UserId:    "u01",
				PostId:    "p01",
				EmojiName: "thumbsup",
				CreateAt:  1700000000,
				UpdateAt:  1700000000,
				ChannelId: "ch01",
			},
		},
		Statuses: []*mmModel.Status{
			{
				UserId:         "u01",
				Status:         "online",
				LastActivityAt: 1700000000,
			},
		},
		MembershipChanges: []*mmModel.MembershipChangeMsg{
			{
				ChannelId:  "ch01",
				UserId:     "u01",
				IsAdd:      true,
				ChangeTime: 1700000000,
			},
		},
		Acknowledgements: []*mmModel.PostAcknowledgement{
			{
				UserId:         "u01",
				PostId:         "p01",
				AcknowledgedAt: 1700000200,
				ChannelId:      "ch01",
			},
		},
		MentionTransforms: map[string]string{
			"@alice": "@alice.remote",
			"@bob":   "@bob.remote",
		},
	}

	wired := SyncMsgFromModel(original)
	data, err := xml.Marshal(wired)
	require.NoError(t, err)

	var decoded SyncMsg
	require.NoError(t, xml.Unmarshal(data, &decoded))
	got := decoded.ToModel()
	require.NotNil(t, got)

	assert.Equal(t, original.Id, got.Id)
	assert.Equal(t, original.ChannelId, got.ChannelId)

	require.Len(t, got.Users, 1)
	assert.Equal(t, "alice", got.Users["u01"].Username)
	assert.Equal(t, "status-x", got.Users["u01"].Props["customStatus"])

	require.Len(t, got.Posts, 1)
	assert.Equal(t, "hello", got.Posts[0].Message)
	require.NotNil(t, got.Posts[0].RemoteId)
	assert.Equal(t, rid, *got.Posts[0].RemoteId)
	assert.Equal(t, true, got.Posts[0].GetProps()["from_webhook"])

	require.Len(t, got.Reactions, 1)
	assert.Equal(t, "thumbsup", got.Reactions[0].EmojiName)

	require.Len(t, got.Statuses, 1)
	assert.Equal(t, "online", got.Statuses[0].Status)

	require.Len(t, got.MembershipChanges, 1)
	assert.True(t, got.MembershipChanges[0].IsAdd)

	require.Len(t, got.Acknowledgements, 1)
	assert.Equal(t, int64(1700000200), got.Acknowledgements[0].AcknowledgedAt)

	assert.Equal(t, original.MentionTransforms, got.MentionTransforms)
}

func TestSyncMsgDeterministicMarshalling(t *testing.T) {
	build := func() *mmModel.SyncMsg {
		return &mmModel.SyncMsg{
			Id:        "sm1",
			ChannelId: "ch01",
			Users: map[string]*mmModel.User{
				"u03": {Id: "u03", Username: "carol", Roles: "system_user"},
				"u01": {Id: "u01", Username: "alice", Roles: "system_user"},
				"u02": {Id: "u02", Username: "bob", Roles: "system_user"},
			},
			MentionTransforms: map[string]string{
				"@bob":   "@bob.remote",
				"@alice": "@alice.remote",
			},
		}
	}
	// Marshalling the same SyncMsg twice must produce byte-identical
	// output despite Go's randomized map iteration.
	a, err := xml.Marshal(SyncMsgFromModel(build()))
	require.NoError(t, err)
	b, err := xml.Marshal(SyncMsgFromModel(build()))
	require.NoError(t, err)
	assert.Equal(t, string(a), string(b))
}
