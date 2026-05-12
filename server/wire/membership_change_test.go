package wire

import (
	"encoding/xml"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipChangeFromModelNil(t *testing.T) {
	assert.Nil(t, MembershipChangeFromModel(nil))
}

func TestMembershipChangeToModelNil(t *testing.T) {
	var m *MembershipChange
	assert.Nil(t, m.ToModel())
}

func TestMembershipChangeFromModelPopulated(t *testing.T) {
	in := &mmModel.MembershipChangeMsg{
		ChannelId:  "ch01",
		UserId:     "u01",
		IsAdd:      true,
		RemoteId:   "remote-1",
		ChangeTime: 1700000000000,
	}
	got := MembershipChangeFromModel(in)
	require.NotNil(t, got)
	assert.Equal(t, in.ChannelId, got.ChannelId)
	assert.Equal(t, in.UserId, got.UserId)
	assert.Equal(t, in.IsAdd, got.IsAdd)
	assert.Equal(t, in.RemoteId, got.RemoteId)
	assert.Equal(t, in.ChangeTime, got.ChangeTime)
}

func TestMembershipChangeRoundTripThroughXML(t *testing.T) {
	original := &mmModel.MembershipChangeMsg{
		ChannelId:  "ch01",
		UserId:     "u01",
		IsAdd:      false,
		RemoteId:   "remote-1",
		ChangeTime: 1700000000000,
	}

	wired := MembershipChangeFromModel(original)
	data, err := xml.Marshal(wired)
	require.NoError(t, err)

	// The wire element name drops the "Msg" suffix.
	assert.Contains(t, string(data), "<MembershipChange>")
	assert.NotContains(t, string(data), "<MembershipChangeMsg>")

	var decoded MembershipChange
	require.NoError(t, xml.Unmarshal(data, &decoded))
	got := decoded.ToModel()
	require.NotNil(t, got)
	assert.Equal(t, original.ChannelId, got.ChannelId)
	assert.Equal(t, original.UserId, got.UserId)
	assert.Equal(t, original.IsAdd, got.IsAdd)
	assert.Equal(t, original.RemoteId, got.RemoteId)
	assert.Equal(t, original.ChangeTime, got.ChangeTime)
}

func TestMembershipChangeRoundTripViaSyncMsg(t *testing.T) {
	in := &SyncMsg{
		Id:        "sm1",
		ChannelId: "ch01",
		MembershipChanges: []*MembershipChange{
			{ChannelId: "ch01", UserId: "u01", IsAdd: true, ChangeTime: 1700000000000},
		},
	}
	data, err := xml.Marshal(in)
	require.NoError(t, err)
	assert.Contains(t, string(data), "<MembershipChanges><MembershipChange>")
	assert.NotContains(t, string(data), "<MembershipChangeMsg>")

	var got SyncMsg
	require.NoError(t, xml.Unmarshal(data, &got))
	require.Len(t, got.MembershipChanges, 1)
	assert.Equal(t, "u01", got.MembershipChanges[0].UserId)
	assert.True(t, got.MembershipChanges[0].IsAdd)
}
