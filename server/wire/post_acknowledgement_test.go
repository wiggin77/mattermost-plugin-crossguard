package wire

import (
	"encoding/xml"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostAcknowledgementFromModelNil(t *testing.T) {
	assert.Nil(t, PostAcknowledgementFromModel(nil))
}

func TestPostAcknowledgementToModelNil(t *testing.T) {
	var a *PostAcknowledgement
	assert.Nil(t, a.ToModel())
}

func TestPostAcknowledgementFromModelPopulated(t *testing.T) {
	rid := "remote-1"
	in := &mmModel.PostAcknowledgement{
		UserId:         "u01",
		PostId:         "p01",
		AcknowledgedAt: 1700000000000,
		ChannelId:      "ch01",
		RemoteId:       &rid,
	}
	got := PostAcknowledgementFromModel(in)
	require.NotNil(t, got)
	assert.Equal(t, in.UserId, got.UserId)
	assert.Equal(t, in.PostId, got.PostId)
	assert.Equal(t, in.AcknowledgedAt, got.AcknowledgedAt)
	assert.Equal(t, in.ChannelId, got.ChannelId)
	assert.Equal(t, rid, got.RemoteId)
}

func TestPostAcknowledgementFromModelEmptyRemoteId(t *testing.T) {
	in := &mmModel.PostAcknowledgement{
		UserId:         "u01",
		PostId:         "p01",
		AcknowledgedAt: 1700000000000,
		ChannelId:      "ch01",
	}
	got := PostAcknowledgementFromModel(in)
	require.NotNil(t, got)
	assert.Empty(t, got.RemoteId)
}

func TestPostAcknowledgementRoundTripThroughXML(t *testing.T) {
	rid := "remote-1"
	original := &mmModel.PostAcknowledgement{
		UserId:         "u01",
		PostId:         "p01",
		AcknowledgedAt: 1700000000000,
		ChannelId:      "ch01",
		RemoteId:       &rid,
	}

	wired := PostAcknowledgementFromModel(original)

	data, err := xml.MarshalIndent(wired, "", "  ")
	require.NoError(t, err)
	assert.Contains(t, string(data), "<UserId>u01</UserId>")
	assert.Contains(t, string(data), "<RemoteId>remote-1</RemoteId>")

	var decoded PostAcknowledgement
	require.NoError(t, xml.Unmarshal(data, &decoded))

	roundTripped := decoded.ToModel()
	require.NotNil(t, roundTripped)
	assert.Equal(t, original.UserId, roundTripped.UserId)
	assert.Equal(t, original.PostId, roundTripped.PostId)
	assert.Equal(t, original.AcknowledgedAt, roundTripped.AcknowledgedAt)
	assert.Equal(t, original.ChannelId, roundTripped.ChannelId)
	require.NotNil(t, roundTripped.RemoteId)
	assert.Equal(t, *original.RemoteId, *roundTripped.RemoteId)
}

func TestPostAcknowledgementRoundTripEmptyRemoteId(t *testing.T) {
	original := &mmModel.PostAcknowledgement{
		UserId:         "u01",
		PostId:         "p01",
		AcknowledgedAt: 1700000000000,
		ChannelId:      "ch01",
	}
	wired := PostAcknowledgementFromModel(original)

	data, err := xml.Marshal(wired)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "<RemoteId>")

	var decoded PostAcknowledgement
	require.NoError(t, xml.Unmarshal(data, &decoded))
	roundTripped := decoded.ToModel()
	require.NotNil(t, roundTripped)
	assert.Nil(t, roundTripped.RemoteId)
}

// TestPostAcknowledgementRoundTripViaSyncMsg verifies that the
// acknowledgement type composes correctly into the SyncMsg envelope
// as <Acknowledgements><PostAcknowledgement>...</PostAcknowledgement></Acknowledgements>.
func TestPostAcknowledgementRoundTripViaSyncMsg(t *testing.T) {
	in := &SyncMsg{
		Id:        "sm1",
		ChannelId: "ch01",
		Acknowledgements: []*PostAcknowledgement{
			{UserId: "u01", PostId: "p01", AcknowledgedAt: 1700000000000, ChannelId: "ch01", RemoteId: "remote-1"},
		},
	}
	data, err := xml.Marshal(in)
	require.NoError(t, err)
	assert.Contains(t, string(data), "<Acknowledgements><PostAcknowledgement>")

	var got SyncMsg
	require.NoError(t, xml.Unmarshal(data, &got))
	require.Len(t, got.Acknowledgements, 1)
	assert.Equal(t, "u01", got.Acknowledgements[0].UserId)
	assert.Equal(t, "remote-1", got.Acknowledgements[0].RemoteId)
}
