package wire

import (
	"encoding/xml"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

func TestUserPropsFromModelEmpty(t *testing.T) {
	log := NewRecordingLogger()
	assert.Nil(t, UserPropsFromModel(nil, log, "u01"))
	assert.Nil(t, UserPropsFromModel(mmModel.StringMap{}, log, "u01"))
	assert.Nil(t, UserPropsFromModel(mmModel.StringMap{"unknown": "x"}, log, "u01"),
		"unknown keys must produce nil, not an empty Props element")
	assert.Empty(t, log.Warns, "empty input must not emit audit events")
	assert.Empty(t, log.Errors, "empty input must not emit audit events")
}

func TestUserPropsFromModelWhitelistedKeys(t *testing.T) {
	in := mmModel.StringMap{
		"customStatus":     `{"emoji":":wave:","text":"hi"}`,
		"RemoteUsername":   "alice",
		"RemoteEmail":      "alice@source.example.com",
		"OriginalRemoteId": "remote-source",
		"unknown":          "should be dropped",
		"force_notify":     "should be dropped",
	}
	log := NewRecordingLogger()
	out := UserPropsFromModel(in, log, "u01")
	require.NotNil(t, out)
	assert.Equal(t, `{"emoji":":wave:","text":"hi"}`, out.CustomStatus)
	assert.Equal(t, "alice", out.RemoteUsername)
	assert.Equal(t, "alice@source.example.com", out.RemoteEmail)
	assert.Equal(t, "remote-source", out.OriginalRemoteId)
	assert.Empty(t, log.Warns, "all whitelisted props conform; no audit events expected")
	assert.Empty(t, log.Errors, "all whitelisted props conform; no audit events expected")
}

func TestUserPropsFromModelDropsNonConformingRemoteUsername(t *testing.T) {
	// A RemoteUsername value with '@' fails the UsernameType pattern.
	// The prop key is dropped; the user record stays (the caller
	// retains the rest of the Props). Warn-level audit event is
	// emitted so operators see the drop.
	in := mmModel.StringMap{
		"RemoteUsername":   "alice@source",
		"OriginalRemoteId": "remote-source",
	}
	log := NewRecordingLogger()
	out := UserPropsFromModel(in, log, "u01")
	require.NotNil(t, out)
	assert.Empty(t, out.RemoteUsername, "non-conforming RemoteUsername must be dropped")
	assert.Equal(t, "remote-source", out.OriginalRemoteId, "other props are unaffected")
	require.Len(t, log.Warns, 1)
	assert.Equal(t, errcode.WireUserPropDroppedNonConforming, log.Warns[0].Code)
	assert.Empty(t, log.Errors)
}

func TestUserPropsFromModelDropsNonConformingRemoteEmail(t *testing.T) {
	in := mmModel.StringMap{
		"RemoteEmail":      "o'brien@company.com",
		"OriginalRemoteId": "remote-source",
	}
	log := NewRecordingLogger()
	out := UserPropsFromModel(in, log, "u01")
	require.NotNil(t, out)
	assert.Empty(t, out.RemoteEmail, "non-conforming RemoteEmail must be dropped")
	assert.Equal(t, "remote-source", out.OriginalRemoteId)
	require.Len(t, log.Warns, 1)
	assert.Equal(t, errcode.WireUserPropDroppedNonConforming, log.Warns[0].Code)
	assert.Empty(t, log.Errors)
}

func TestUserPropsToModelRoundTrip(t *testing.T) {
	in := mmModel.StringMap{
		"customStatus":     "status",
		"RemoteUsername":   "user",
		"RemoteEmail":      "user@source.example.com",
		"OriginalRemoteId": "remote-source",
	}
	log := NewRecordingLogger()
	wp := UserPropsFromModel(in, log, "u01")
	require.NotNil(t, wp)
	out := wp.ToModel()
	assert.Equal(t, in, out)
	assert.Empty(t, log.Warns)
	assert.Empty(t, log.Errors)
}

func TestUserFromModelNil(t *testing.T) {
	log := NewRecordingLogger()
	assert.Nil(t, UserFromModel(nil, log))
	assert.Empty(t, log.Warns)
	assert.Empty(t, log.Errors)
}

func TestUserToModelNil(t *testing.T) {
	var u *User
	assert.Nil(t, u.ToModel())
}

// ----- Username ladder coverage (P2.4) -----

func TestUserFromModelUsernameAsIs(t *testing.T) {
	in := &mmModel.User{
		Id:       "u01",
		Username: "alice",
		Email:    "alice@example.com",
	}
	log := NewRecordingLogger()
	got := UserFromModel(in, log)
	require.NotNil(t, got)
	assert.Equal(t, "alice", got.Username)
	assert.Empty(t, log.Warns, "ladder step 1 (as-is) must not log")
	assert.Empty(t, log.Errors)
}

func TestUserFromModelUsernameResolvedViaRemoteProp(t *testing.T) {
	// Ladder step 2: colon-bearing Username with a conforming
	// RemoteUsername prop. This is the normal path for users that
	// originated on another server and are being synced outbound;
	// no log is expected.
	in := &mmModel.User{
		Id:       "u01",
		Username: "alice:remoteABCDEFGHIJKLMNOPQR",
		Email:    "alice@example.com",
		Props:    mmModel.StringMap{"RemoteUsername": "alice"},
	}
	log := NewRecordingLogger()
	got := UserFromModel(in, log)
	require.NotNil(t, got)
	assert.Equal(t, "alice", got.Username, "wire Username sourced from RemoteUsername prop")
	assert.Empty(t, log.Warns, "remote-prop resolution is the normal path; no audit event")
	assert.Empty(t, log.Errors)
}

func TestUserFromModelUsernameTruncatedAtColon(t *testing.T) {
	// Ladder step 3: colon-bearing Username with missing
	// RemoteUsername prop. Truncate at colon and warn.
	in := &mmModel.User{
		Id:       "u01",
		Username: "alice:remoteABCDEFGHIJKLMNOPQR",
		Email:    "alice@example.com",
	}
	log := NewRecordingLogger()
	got := UserFromModel(in, log)
	require.NotNil(t, got)
	assert.Equal(t, "alice", got.Username, "wire Username is the colon-prefix")
	require.Len(t, log.Warns, 1)
	assert.Equal(t, errcode.WireUsernameTruncatedAtColon, log.Warns[0].Code)
	assert.Empty(t, log.Errors)
}

func TestUserFromModelUsernameTruncatedWhenRemotePropAlsoBad(t *testing.T) {
	// Ladder step 3 also fires when RemoteUsername is present but
	// itself non-conforming. The prop is dropped (warn) and the
	// truncation path runs (additional warn).
	in := &mmModel.User{
		Id:       "u01",
		Username: "alice:remoteABCDEFGHIJKLMNOPQR",
		Email:    "alice@example.com",
		Props:    mmModel.StringMap{"RemoteUsername": "alice@multihop"},
	}
	log := NewRecordingLogger()
	got := UserFromModel(in, log)
	require.NotNil(t, got)
	assert.Equal(t, "alice", got.Username)
	codes := log.WarnCodes()
	assert.Contains(t, codes, errcode.WireUsernameTruncatedAtColon)
	assert.Contains(t, codes, errcode.WireUserPropDroppedNonConforming)
	assert.Empty(t, log.Errors)
}

func TestUserFromModelDropsWhenUsernameUnrecoverable(t *testing.T) {
	cases := []struct {
		name  string
		uname string
	}{
		{"leading_colon", ":alice"},
		{"truncate_yields_invalid", "Bad:remoteABC"}, // candidate "Bad" still fails (uppercase)
		{"no_colon_no_match", "BadCaps"},
		{"with_space", "with space"},
		{"empty", ""},
		{"leading_underscore", "_alice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &mmModel.User{Id: "u01", Username: tc.uname, Email: "alice@example.com"}
			log := NewRecordingLogger()
			got := UserFromModel(in, log)
			assert.Nil(t, got, "non-recoverable username drops the user record")
			require.Len(t, log.Errors, 1)
			assert.Equal(t, errcode.WireUserDroppedNonConformingUsername, log.Errors[0].Code)
		})
	}
}

// ----- Email validation coverage (P2.6) -----

func TestUserFromModelDropsWhenEmailNonConforming(t *testing.T) {
	cases := []struct {
		name  string
		email string
	}{
		{"apostrophe_local", "o'brien@company.com"},
		{"unicode_local", "user@münchen.de"},
		{"underscore_domain", "user@my_company.internal"},
		{"ip_literal_host", "user@[192.0.2.1]"},
		{"empty_email", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &mmModel.User{Id: "u01", Username: "alice", Email: tc.email}
			log := NewRecordingLogger()
			got := UserFromModel(in, log)
			assert.Nil(t, got, "non-conforming email drops the user record")
			require.Len(t, log.Errors, 1)
			assert.Equal(t, errcode.WireUserDroppedNonConformingEmail, log.Errors[0].Code)
		})
	}
}

func TestUserFromModelAcceptsConformingEmails(t *testing.T) {
	cases := []string{
		"firstname.lastname@company.com",
		"user+tag@example.org",
		"a_b-c@sub.example.io",
	}
	for _, email := range cases {
		t.Run(email, func(t *testing.T) {
			in := &mmModel.User{Id: "u01", Username: "alice", Email: email}
			log := NewRecordingLogger()
			got := UserFromModel(in, log)
			require.NotNil(t, got)
			assert.Equal(t, email, got.Email)
			assert.Empty(t, log.Warns)
			assert.Empty(t, log.Errors)
		})
	}
}

// ----- Existing coverage, updated to RecordingLogger -----

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
	log := NewRecordingLogger()
	got := UserFromModel(in, log)
	require.NotNil(t, got)
	assert.Equal(t, "u01", got.Id)
	assert.Equal(t, "alice", got.Username)
	assert.Equal(t, "alice@example.com", got.Email)
	assert.Equal(t, rid, got.RemoteId)
	assert.Empty(t, log.Warns)
	assert.Empty(t, log.Errors)

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
		Props:             mmModel.StringMap{"customStatus": "status", "RemoteUsername": "alice"},
		RemoteId:          &rid,
		IsBot:             false,
		BotDescription:    "",
		BotLastIconUpdate: 0,
	}

	log := NewRecordingLogger()
	data, err := xml.Marshal(UserFromModel(original, log))
	require.NoError(t, err)
	assert.Empty(t, log.Warns, "conforming inputs must not emit audit events")
	assert.Empty(t, log.Errors)

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
	log := NewRecordingLogger()
	data, err := xml.Marshal(UserFromModel(original, log))
	require.NoError(t, err)
	assert.Empty(t, log.Warns)
	assert.Empty(t, log.Errors)

	var decoded User
	require.NoError(t, xml.Unmarshal(data, &decoded))
	got := decoded.ToModel()
	assert.True(t, got.IsBot)
	assert.Equal(t, "Deploys things", got.BotDescription)
	assert.Equal(t, int64(1700000000), got.BotLastIconUpdate)
}
