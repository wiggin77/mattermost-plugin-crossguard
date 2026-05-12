package wire

import (
	"encoding/xml"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReactionFromModelNil(t *testing.T) {
	assert.Nil(t, ReactionFromModel(nil))
}

func TestReactionToModelNil(t *testing.T) {
	var r *Reaction
	assert.Nil(t, r.ToModel())
}

func TestReactionFromModelPopulated(t *testing.T) {
	rid := "remote-1"
	in := &mmModel.Reaction{
		UserId:    "u01",
		PostId:    "p01",
		EmojiName: "thumbsup",
		CreateAt:  1700000000000,
		UpdateAt:  1700000000000,
		DeleteAt:  0,
		RemoteId:  &rid,
		ChannelId: "ch01",
	}
	got := ReactionFromModel(in)
	require.NotNil(t, got)
	assert.Equal(t, in.UserId, got.UserId)
	assert.Equal(t, in.PostId, got.PostId)
	assert.Equal(t, in.EmojiName, got.EmojiName)
	assert.Equal(t, in.CreateAt, got.CreateAt)
	assert.Equal(t, in.UpdateAt, got.UpdateAt)
	assert.Equal(t, in.DeleteAt, got.DeleteAt)
	assert.Equal(t, in.ChannelId, got.ChannelId)
	assert.Equal(t, rid, got.RemoteId)
}

func TestReactionRoundTripThroughXML(t *testing.T) {
	rid := "remote-1"
	original := &mmModel.Reaction{
		UserId:    "u01",
		PostId:    "p01",
		EmojiName: "thumbsup",
		CreateAt:  1700000000000,
		UpdateAt:  1700000000001,
		DeleteAt:  0,
		RemoteId:  &rid,
		ChannelId: "ch01",
	}

	data, err := xml.Marshal(ReactionFromModel(original))
	require.NoError(t, err)

	var decoded Reaction
	require.NoError(t, xml.Unmarshal(data, &decoded))
	got := decoded.ToModel()
	require.NotNil(t, got)
	assert.Equal(t, original.UserId, got.UserId)
	assert.Equal(t, original.PostId, got.PostId)
	assert.Equal(t, original.EmojiName, got.EmojiName)
	assert.Equal(t, original.CreateAt, got.CreateAt)
	assert.Equal(t, original.UpdateAt, got.UpdateAt)
	assert.Equal(t, original.DeleteAt, got.DeleteAt)
	assert.Equal(t, original.ChannelId, got.ChannelId)
	require.NotNil(t, got.RemoteId)
	assert.Equal(t, *original.RemoteId, *got.RemoteId)
}

func TestReactionRoundTripEmptyRemoteId(t *testing.T) {
	original := &mmModel.Reaction{
		UserId:    "u01",
		PostId:    "p01",
		EmojiName: "thumbsup",
		CreateAt:  1700000000000,
		UpdateAt:  1700000000000,
		ChannelId: "ch01",
	}
	data, err := xml.Marshal(ReactionFromModel(original))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "<RemoteId>")

	var decoded Reaction
	require.NoError(t, xml.Unmarshal(data, &decoded))
	got := decoded.ToModel()
	assert.Nil(t, got.RemoteId)
}
