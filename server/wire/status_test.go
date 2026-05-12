package wire

import (
	"encoding/xml"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusFromModelNil(t *testing.T) {
	assert.Nil(t, StatusFromModel(nil))
}

func TestStatusToModelNil(t *testing.T) {
	var s *Status
	assert.Nil(t, s.ToModel())
}

func TestStatusFromModelPopulated(t *testing.T) {
	in := &mmModel.Status{
		UserId:         "u01",
		Status:         "online",
		Manual:         true,
		LastActivityAt: 1700000000000,
		ActiveChannel:  "ch01",
		DNDEndTime:     1700000900,
	}
	got := StatusFromModel(in)
	require.NotNil(t, got)
	assert.Equal(t, in.UserId, got.UserId)
	assert.Equal(t, in.Status, got.Status)
	assert.Equal(t, in.Manual, got.Manual)
	assert.Equal(t, in.LastActivityAt, got.LastActivityAt)
	assert.Equal(t, in.DNDEndTime, got.DNDEndTime)
}

func TestStatusFromModelDropsActiveChannel(t *testing.T) {
	in := &mmModel.Status{
		UserId:        "u01",
		Status:        "online",
		ActiveChannel: "ch-secret",
	}
	got := StatusFromModel(in)
	require.NotNil(t, got)

	data, err := xml.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "ActiveChannel")
	assert.NotContains(t, string(data), "ch-secret")
}

func TestStatusRoundTripThroughXML(t *testing.T) {
	original := &mmModel.Status{
		UserId:         "u01",
		Status:         "dnd",
		Manual:         true,
		LastActivityAt: 1700000000000,
		ActiveChannel:  "ch01",
		DNDEndTime:     1700000900,
	}

	data, err := xml.Marshal(StatusFromModel(original))
	require.NoError(t, err)

	var decoded Status
	require.NoError(t, xml.Unmarshal(data, &decoded))
	got := decoded.ToModel()
	require.NotNil(t, got)
	assert.Equal(t, original.UserId, got.UserId)
	assert.Equal(t, original.Status, got.Status)
	assert.Equal(t, original.Manual, got.Manual)
	assert.Equal(t, original.LastActivityAt, got.LastActivityAt)
	assert.Equal(t, original.DNDEndTime, got.DNDEndTime)
	assert.Empty(t, got.ActiveChannel)
}
