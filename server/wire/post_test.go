package wire

import (
	"encoding/xml"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostPropsFromModelEmpty(t *testing.T) {
	assert.Nil(t, PostPropsFromModel(nil))
	assert.Nil(t, PostPropsFromModel(mmModel.StringInterface{}))
	assert.Nil(t, PostPropsFromModel(mmModel.StringInterface{"unknown": "x"}))
	assert.Nil(t, PostPropsFromModel(mmModel.StringInterface{
		"force_notification": true,
		"attachments":        "stuff",
	}), "dropped keys must not surface as non-nil props")
}

func TestPostPropsFromModelWhitelistedKeys(t *testing.T) {
	in := mmModel.StringInterface{
		"from_webhook":             true,
		"from_bot":                 true,
		"from_plugin":              true,
		"from_oauth_app":           true,
		"override_username":        "Alice via Webhook",
		"override_icon_url":        "https://example.com/icon.png",
		"override_icon_emoji":      ":bell:",
		"webhook_display_name":     "Notifier",
		"addedUserId":              "u02",
		"deleteBy":                 "u03",
		"add_channel_member":       "u04",
		"mentionHighlightDisabled": true,
		"disable_group_highlight":  true,
		"ai_generated_by":          "u05",
		"ai_generated_by_username": "ai-bot",
		"attachments":              "should drop",
		"force_notification":       true,
	}
	out := PostPropsFromModel(in)
	require.NotNil(t, out)
	assert.True(t, out.FromWebhook)
	assert.True(t, out.FromBot)
	assert.True(t, out.FromPlugin)
	assert.True(t, out.FromOAuthApp)
	assert.Equal(t, "Alice via Webhook", out.OverrideUsername)
	assert.Equal(t, "https://example.com/icon.png", out.OverrideIconURL)
	assert.Equal(t, "bell", out.OverrideIconEmoji, "wire form strips :colons: per EmojiNameType")
	assert.Equal(t, "Notifier", out.WebhookDisplayName)
	assert.Equal(t, "u02", out.AddedUserId)
	assert.Equal(t, "u03", out.DeleteBy)
	assert.Equal(t, "u04", out.AddChannelMember)
	assert.True(t, out.MentionHighlightDisabled)
	assert.True(t, out.DisableGroupHighlight)
	assert.Equal(t, "u05", out.AIGeneratedByUserId)
	assert.Equal(t, "ai-bot", out.AIGeneratedByUsername)
}

func TestPostPropsFromModelStringBool(t *testing.T) {
	// Upstream sometimes stores bool-like flags as the string "true".
	in := mmModel.StringInterface{
		"from_webhook": "true",
	}
	out := PostPropsFromModel(in)
	require.NotNil(t, out)
	assert.True(t, out.FromWebhook)
}

func TestPostPropsToModelRoundTrip(t *testing.T) {
	in := mmModel.StringInterface{
		"from_webhook":        true,
		"override_username":   "Alice",
		"override_icon_url":   "https://example.com/icon.png",
		"override_icon_emoji": ":wave:",
	}
	props := PostPropsFromModel(in)
	out := props.ToModel()
	assert.Equal(t, in, out)
}

func TestPostPropsOverrideIconEmojiCanonicalization(t *testing.T) {
	// Mattermost stores override_icon_emoji as ":name:"; the constrained
	// wire schema's EmojiNameType pattern admits only the bare name.
	// FromModel strips a single leading/trailing colon; ToModel re-adds
	// them so upstream sees the original shape.
	cases := []struct {
		upstream  string
		wantWire  string
		roundTrip string
	}{
		{":white_check_mark:", "white_check_mark", ":white_check_mark:"},
		{"wave", "wave", ":wave:"},
		{":wave", "wave", ":wave:"},
		{"wave:", "wave", ":wave:"},
	}
	for _, tc := range cases {
		t.Run(tc.upstream, func(t *testing.T) {
			in := mmModel.StringInterface{"override_icon_emoji": tc.upstream}
			props := PostPropsFromModel(in)
			require.NotNil(t, props)
			assert.Equal(t, tc.wantWire, props.OverrideIconEmoji)
			out := props.ToModel()
			assert.Equal(t, tc.roundTrip, out["override_icon_emoji"])
		})
	}
}

func TestPostFromModelNil(t *testing.T) {
	assert.Nil(t, PostFromModel(nil))
}

func TestPostToModelNil(t *testing.T) {
	var p *Post
	assert.Nil(t, p.ToModel())
}

func TestPostFromModelDropsDerivedFields(t *testing.T) {
	following := true
	in := &mmModel.Post{
		Id:            "p01",
		CreateAt:      1700000000,
		UpdateAt:      1700000001,
		UserId:        "u01",
		ChannelId:     "ch01",
		Message:       "hello",
		MessageSource: "raw hello",
		PendingPostId: "client-side-pending-id",
		ReplyCount:    5,
		LastReplyAt:   1700000200,
		IsFollowing:   &following,
		Participants: []*mmModel.User{
			{Id: "u02"},
		},
	}
	got := PostFromModel(in)
	require.NotNil(t, got)
	assert.Equal(t, "hello", got.Message)

	// Confirm dropped fields do not appear on the wire.
	data, err := xml.Marshal(got)
	require.NoError(t, err)
	out := string(data)
	for _, dropped := range []string{
		"MessageSource", "raw hello", "PendingPostId", "client-side-pending-id",
		"ReplyCount", "LastReplyAt", "IsFollowing", "Participants",
	} {
		assert.NotContains(t, out, dropped, "wire format must not carry %s", dropped)
	}
}

func TestPostRoundTripThroughXML(t *testing.T) {
	rid := "remote-1"
	original := &mmModel.Post{
		Id:           "p01",
		CreateAt:     1700000000,
		UpdateAt:     1700000001,
		EditAt:       0,
		DeleteAt:     0,
		UserId:       "u01",
		ChannelId:    "ch01",
		RootId:       "",
		OriginalId:   "",
		Message:      "hello world",
		Type:         "",
		Hashtags:     "#hello",
		FileIds:      mmModel.StringArray{"f01", "f02"},
		HasReactions: true,
		RemoteId:     &rid,
		IsPinned:     true,
		Props: mmModel.StringInterface{
			"from_webhook":      true,
			"override_username": "Alice",
		},
	}

	wired := PostFromModel(original)
	data, err := xml.Marshal(wired)
	require.NoError(t, err)

	var decoded Post
	require.NoError(t, xml.Unmarshal(data, &decoded))
	got := decoded.ToModel()
	require.NotNil(t, got)
	assert.Equal(t, original.Id, got.Id)
	assert.Equal(t, original.CreateAt, got.CreateAt)
	assert.Equal(t, original.UpdateAt, got.UpdateAt)
	assert.Equal(t, original.UserId, got.UserId)
	assert.Equal(t, original.ChannelId, got.ChannelId)
	assert.Equal(t, original.Message, got.Message)
	assert.Equal(t, original.Hashtags, got.Hashtags)
	assert.Equal(t, []string(original.FileIds), []string(got.FileIds))
	assert.True(t, got.HasReactions)
	assert.True(t, got.IsPinned)
	require.NotNil(t, got.RemoteId)
	assert.Equal(t, *original.RemoteId, *got.RemoteId)

	gotProps := got.GetProps()
	assert.Equal(t, true, gotProps["from_webhook"])
	assert.Equal(t, "Alice", gotProps["override_username"])
}

func TestPostRoundTripSystemMessage(t *testing.T) {
	original := &mmModel.Post{
		Id:        "p02",
		CreateAt:  1700000000,
		UpdateAt:  1700000001,
		UserId:    "u01",
		ChannelId: "ch01",
		Type:      "system_add_to_channel",
		Message:   "added user",
		Props: mmModel.StringInterface{
			"addedUserId":        "u02",
			"add_channel_member": "u02",
		},
	}

	data, err := xml.Marshal(PostFromModel(original))
	require.NoError(t, err)

	var decoded Post
	require.NoError(t, xml.Unmarshal(data, &decoded))
	got := decoded.ToModel()
	assert.Equal(t, "system_add_to_channel", got.Type)
	gotProps := got.GetProps()
	assert.Equal(t, "u02", gotProps["addedUserId"])
	assert.Equal(t, "u02", gotProps["add_channel_member"])
}
