package wire

import (
	"encoding/xml"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPropsFromModelEmpty(t *testing.T) {
	assert.Nil(t, UserPropsFromModel(nil))
	assert.Nil(t, UserPropsFromModel(mmModel.StringMap{}))
	assert.Nil(t, UserPropsFromModel(mmModel.StringMap{"unknown": "x"}),
		"unknown keys must produce nil, not an empty Props element")
}

func TestUserPropsFromModelWhitelistedKeys(t *testing.T) {
	in := mmModel.StringMap{
		"customStatus":     `{"emoji":":wave:","text":"hi"}`,
		"RemoteUsername":   "alice@source",
		"RemoteEmail":      "alice@source.example.com",
		"OriginalRemoteId": "remote-source",
		"unknown":          "should be dropped",
		"force_notify":     "should be dropped",
	}
	out := UserPropsFromModel(in)
	require.NotNil(t, out)
	assert.Equal(t, `{"emoji":":wave:","text":"hi"}`, out.CustomStatus)
	assert.Equal(t, "alice@source", out.RemoteUsername)
	assert.Equal(t, "alice@source.example.com", out.RemoteEmail)
	assert.Equal(t, "remote-source", out.OriginalRemoteId)
}

func TestUserPropsToModelRoundTrip(t *testing.T) {
	in := mmModel.StringMap{
		"customStatus":     "status",
		"RemoteUsername":   "user@source",
		"RemoteEmail":      "user@source.example.com",
		"OriginalRemoteId": "remote-source",
	}
	wp := UserPropsFromModel(in)
	require.NotNil(t, wp)
	out := wp.ToModel()
	assert.Equal(t, in, out)
}

func TestUserFromModelNil(t *testing.T) {
	assert.Nil(t, UserFromModel(nil))
}

func TestUserToModelNil(t *testing.T) {
	var u *User
	assert.Nil(t, u.ToModel())
}

func TestUserFromModelDropsSanitizedFields(t *testing.T) {
	rid := "remote-1"
	in := &mmModel.User{
		Id:                     "u01",
		Username:               "alice",
		Email:                  "alice@example.com",
		AuthService:            "ldap",
		EmailVerified:          true,
		AllowMarketing:         true,
		NotifyProps:            mmModel.StringMap{"desktop": "all"},
		LastPasswordUpdate:     999,
		LastPictureUpdate:      888,
		FailedAttempts:         3,
		MfaActive:              true,
		LastActivityAt:         1700000000,
		TermsOfServiceId:       "tos-1",
		TermsOfServiceCreateAt: 1700000001,
		DisableWelcomeEmail:    true,
		LastLogin:              1700000002,
		RemoteId:               &rid,
	}
	got := UserFromModel(in)
	require.NotNil(t, got)
	assert.Equal(t, "u01", got.Id)
	assert.Equal(t, "alice", got.Username)
	assert.Equal(t, "alice@example.com", got.Email)
	assert.Equal(t, rid, got.RemoteId)

	// Confirm dropped fields do not appear on the wire.
	data, err := xml.Marshal(got)
	require.NoError(t, err)
	out := string(data)
	for _, dropped := range []string{
		"AuthService", "EmailVerified", "AllowMarketing", "NotifyProps",
		"LastPasswordUpdate", "LastPictureUpdate", "FailedAttempts", "MfaActive",
		"LastActivityAt", "TermsOfServiceId", "TermsOfServiceCreateAt",
		"DisableWelcomeEmail", "LastLogin",
	} {
		assert.NotContains(t, out, dropped, "wire format must not carry %s", dropped)
	}
}

func TestUserRoundTripThroughXML(t *testing.T) {
	rid := "remote-1"
	original := &mmModel.User{
		Id:                "u01",
		CreateAt:          1700000000,
		UpdateAt:          1700000001,
		DeleteAt:          0,
		Username:          "alice",
		Email:             "alice@example.com",
		Nickname:          "ally",
		FirstName:         "Alice",
		LastName:          "Anderson",
		Position:          "Engineer",
		Roles:             "system_user",
		Locale:            "en",
		Timezone:          mmModel.StringMap{"automaticTimezone": "America/New_York"},
		Props:             mmModel.StringMap{"customStatus": "status", "RemoteUsername": "alice@source"},
		RemoteId:          &rid,
		IsBot:             false,
		BotDescription:    "",
		BotLastIconUpdate: 0,
	}

	data, err := xml.Marshal(UserFromModel(original))
	require.NoError(t, err)

	var decoded User
	require.NoError(t, xml.Unmarshal(data, &decoded))
	got := decoded.ToModel()
	require.NotNil(t, got)
	assert.Equal(t, original.Id, got.Id)
	assert.Equal(t, original.CreateAt, got.CreateAt)
	assert.Equal(t, original.UpdateAt, got.UpdateAt)
	assert.Equal(t, original.Username, got.Username)
	assert.Equal(t, original.Email, got.Email)
	assert.Equal(t, original.Nickname, got.Nickname)
	assert.Equal(t, original.FirstName, got.FirstName)
	assert.Equal(t, original.LastName, got.LastName)
	assert.Equal(t, original.Position, got.Position)
	assert.Equal(t, original.Roles, got.Roles)
	assert.Equal(t, original.Locale, got.Locale)
	assert.Equal(t, original.Timezone, got.Timezone)
	assert.Equal(t, original.Props, got.Props)
	require.NotNil(t, got.RemoteId)
	assert.Equal(t, *original.RemoteId, *got.RemoteId)
}

func TestUserRoundTripBotUser(t *testing.T) {
	original := &mmModel.User{
		Id:                "bot01",
		Username:          "deploybot",
		Email:             "deploybot@example.com",
		Roles:             "system_user",
		IsBot:             true,
		BotDescription:    "Deploys things",
		BotLastIconUpdate: 1700000000,
	}
	data, err := xml.Marshal(UserFromModel(original))
	require.NoError(t, err)

	var decoded User
	require.NoError(t, xml.Unmarshal(data, &decoded))
	got := decoded.ToModel()
	assert.True(t, got.IsBot)
	assert.Equal(t, "Deploys things", got.BotDescription)
	assert.Equal(t, int64(1700000000), got.BotLastIconUpdate)
}
