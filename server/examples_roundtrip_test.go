package main

import (
	"os"
	"path/filepath"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/wire"
)

// fixedTimestamp keeps the example files byte-stable across runs. Real
// envelopes default Timestamp to time.Now in MarshalEnvelope; examples set
// it explicitly so the encoded bytes are deterministic.
const fixedTimestamp = "2026-05-11T10:00:00Z"

// exampleCase pairs a filename with a builder for the envelope.
type exampleCase struct {
	file string
	env  func() *TransportEnvelope
}

func exampleCases() []exampleCase {
	return []exampleCase{
		{"01_post_simple.xml", postSimpleEnvelope},
		{"02_post_markdown_rich.xml", postMarkdownRichEnvelope},
		{"03_post_code_blocks.xml", postCodeBlocksEnvelope},
		{"04_post_table_and_links.xml", postTableAndLinksEnvelope},
		{"05_post_thread_reply.xml", postThreadReplyEnvelope},
		{"06_post_incident_report.xml", postIncidentReportEnvelope},
		{"07_update_edited_post.xml", postUpdateEnvelope},
		{"08_delete.xml", postDeleteEnvelope},
		{"09_reaction_add.xml", reactionAddEnvelope},
		{"10_reaction_remove.xml", reactionRemoveEnvelope},
		{"11_reaction_custom_emoji.xml", reactionCustomEmojiEnvelope},
		{"12_test.xml", testEnvelope},
		{"13_membership_change_join.xml", membershipChangeJoinEnvelope},
		{"14_membership_change_leave.xml", membershipChangeLeaveEnvelope},
		{"15_status_dnd.xml", statusDndEnvelope},
		{"16_post_acknowledgement.xml", postAcknowledgementEnvelope},
		{"17_mention_transforms.xml", mentionTransformsEnvelope},
		{"18_post_with_props_webhook.xml", postWithPropsWebhookEnvelope},
		{"19_user_with_timezone_and_props.xml", userWithTimezoneAndPropsEnvelope},
		{"20_bot_user.xml", botUserEnvelope},
	}
}

// examplesDir resolves the schema/examples directory relative to this test
// file. Joining a constant base directory with a fixed filename from
// exampleCases is safe; gosec G304 does not apply.
func examplesDir() string { return filepath.Join("..", "schema", "examples") }

// TestExampleFiles compares each schema/examples/*.xml file to the bytes
// produced by MarshalEnvelope on the canonical struct for that scenario.
// Set UPDATE_EXAMPLES=1 to rewrite the files from the current code output;
// otherwise the test asserts byte-for-byte equality.
func TestExampleFiles(t *testing.T) {
	update := os.Getenv("UPDATE_EXAMPLES") == "1"
	dir := examplesDir()

	for _, tc := range exampleCases() {
		t.Run(tc.file, func(t *testing.T) {
			env := tc.env()
			data, err := MarshalEnvelope(env)
			require.NoError(t, err)
			want := make([]byte, 0, len(data)+1)
			want = append(want, data...)
			want = append(want, '\n')

			path := filepath.Join(dir, tc.file)
			if update {
				require.NoError(t, os.WriteFile(path, want, 0o600)) //nolint:gosec // example fixtures, deterministic input
				return
			}

			got, err := os.ReadFile(path) //nolint:gosec // example fixture path is a constant under the repo
			require.NoError(t, err)
			assert.Equal(t, string(want), string(got), "regenerate with UPDATE_EXAMPLES=1 if format change is intentional")
		})
	}
}

// TestExampleFilesRoundTrip ensures every example file unmarshals back into
// a TransportEnvelope and that the envelope-level routing/audit fields are
// preserved (these are the fields compliance monitoring relies on).
func TestExampleFilesRoundTrip(t *testing.T) {
	dir := examplesDir()
	for _, tc := range exampleCases() {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tc.file)) //nolint:gosec // example fixture path is a constant under the repo
			require.NoError(t, err)
			env, err := UnmarshalEnvelope(data)
			require.NoError(t, err)

			assert.Equal(t, 1, env.Version)
			assert.NotEmpty(t, env.Type)
			assert.NotEmpty(t, env.Timestamp)
			assert.NotEmpty(t, env.ConnName)

			if env.Type == TransportTypeTest {
				assert.NotEmpty(t, env.TestID)
				assert.Nil(t, env.SyncMsg)
				return
			}
			require.NotNil(t, env.SyncMsg)
			assert.NotEmpty(t, env.SyncMsg.Id)
			assert.NotEmpty(t, env.SyncMsg.ChannelId)
		})
	}
}

// ----------------------------------------------------------------------
// Example builders. Each returns a fully-populated TransportEnvelope for
// one scenario. Identifiers are fixed strings so the encoded bytes are
// stable run to run.
// ----------------------------------------------------------------------

const (
	chanID  = "ch0aaaaaaaaaaaaaaaaaaaaaaa"
	teamID  = "tm0aaaaaaaaaaaaaaaaaaaaaaa"
	userA   = "u01aliceaaaaaaaaaaaaaaaaaa"
	userB   = "u02bobaaaaaaaaaaaaaaaaaaaa"
	userBot = "u03botaaaaaaaaaaaaaaaaaaaa"
	postID1 = "p01postaaaaaaaaaaaaaaaaaaa"
	postID2 = "p02postaaaaaaaaaaaaaaaaaaa"
	postID3 = "p03postaaaaaaaaaaaaaaaaaaa"
)

func alice() *mmModel.User {
	remoteID := "remote-low"
	return &mmModel.User{
		Id:        userA,
		CreateAt:  1700000000000,
		UpdateAt:  1712956800000,
		Username:  "alice",
		Email:     "alice@server-a.example.com",
		Nickname:  "Alice",
		FirstName: "Alice",
		LastName:  "Smith",
		Roles:     "system_user",
		Locale:    "en",
		RemoteId:  &remoteID,
	}
}

func bob() *mmModel.User {
	remoteID := "remote-low"
	return &mmModel.User{
		Id:        userB,
		CreateAt:  1700000000000,
		UpdateAt:  1712956800000,
		Username:  "bob",
		Email:     "bob@server-a.example.com",
		Nickname:  "Bob",
		FirstName: "Bob",
		LastName:  "Jones",
		Roles:     "system_user",
		Locale:    "en",
		RemoteId:  &remoteID,
	}
}

func baseEnvelope(connName, channelName string, msg *mmModel.SyncMsg) *TransportEnvelope {
	return &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    connName,
		Timestamp:   fixedTimestamp,
		TeamName:    "team-a",
		ChannelName: channelName,
		SyncMsg:     wire.SyncMsgFromModel(msg),
	}
}

func postSimpleEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-01",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice()},
		Posts: []*mmModel.Post{{
			Id:        postID1,
			CreateAt:  1712956800000,
			UpdateAt:  1712956800000,
			UserId:    userA,
			ChannelId: chanID,
			Message:   "Morning team. Standup at 9:30 in the usual room, see you there.",
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

func postMarkdownRichEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-02",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice()},
		Posts: []*mmModel.Post{{
			Id:        postID1,
			CreateAt:  1712956800000,
			UpdateAt:  1712956800000,
			UserId:    userA,
			ChannelId: chanID,
			Message:   "**Release notes** for `v1.2.3`:\n\n- New <Tag>-style filter on inbound\n- *Italic* and **bold** rendering checked\n- Quote: > stay focused on the migration\n\nSee #release-eng for follow-ups.",
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

func postCodeBlocksEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-03",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice()},
		Posts: []*mmModel.Post{{
			Id:        postID1,
			CreateAt:  1712956800000,
			UpdateAt:  1712956800000,
			UserId:    userA,
			ChannelId: chanID,
			Message:   "Here's the migration shim, note the explicit <Envelope> handling:\n\n```go\nfunc migrate(e *Envelope) error {\n    if e == nil { return nil }\n    return apply(e)\n}\n```",
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

func postTableAndLinksEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-04",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice()},
		Posts: []*mmModel.Post{{
			Id:        postID1,
			CreateAt:  1712956800000,
			UpdateAt:  1712956800000,
			UserId:    userA,
			ChannelId: chanID,
			Message:   "Status board:\n\n| Service | Owner | Link |\n| --- | --- | --- |\n| ingest | alice | https://wiki/ingest |\n| relay | bob | https://wiki/relay |\n\nSee [the runbook](https://runbooks.example.com/relay).",
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

func postThreadReplyEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-05",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice()},
		Posts: []*mmModel.Post{{
			Id:        postID2,
			CreateAt:  1712956900000,
			UpdateAt:  1712956900000,
			UserId:    userA,
			ChannelId: chanID,
			RootId:    postID1,
			Message:   "Replying in thread, sounds good to me.",
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

func postIncidentReportEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-06",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice(), userB: bob()},
		Posts: []*mmModel.Post{{
			Id:        postID1,
			CreateAt:  1712956800000,
			UpdateAt:  1712956800000,
			UserId:    userA,
			ChannelId: chanID,
			Message:   "## Incident write-up\n\n**Summary:** Brief outage of the <Relay> path between low and high domains.\n\n**Timeline:**\n1. 09:14 alert fires\n2. 09:18 oncall paged\n3. 09:32 root cause identified (stale DNS entry)\n4. 09:41 mitigation applied\n5. 09:47 all clear\n\n**Action items:** see thread.",
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

func postUpdateEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-07",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice()},
		Posts: []*mmModel.Post{{
			Id:        postID1,
			CreateAt:  1712956800000,
			UpdateAt:  1712957000000,
			EditAt:    1712957000000,
			UserId:    userA,
			ChannelId: chanID,
			Message:   "Morning team. Standup moved to 9:45 in the upstairs room.",
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

func postDeleteEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-08",
		ChannelId: chanID,
		Posts: []*mmModel.Post{{
			Id:        postID1,
			CreateAt:  1712956800000,
			UpdateAt:  1712957100000,
			DeleteAt:  1712957100000,
			UserId:    userA,
			ChannelId: chanID,
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

func reactionAddEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-09",
		ChannelId: chanID,
		Reactions: []*mmModel.Reaction{{
			UserId:    userA,
			PostId:    postID1,
			EmojiName: "thumbsup",
			CreateAt:  1712957200000,
			UpdateAt:  1712957200000,
			ChannelId: chanID,
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

func reactionRemoveEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-10",
		ChannelId: chanID,
		Reactions: []*mmModel.Reaction{{
			UserId:    userA,
			PostId:    postID1,
			EmojiName: "thumbsup",
			CreateAt:  1712957200000,
			UpdateAt:  1712957300000,
			DeleteAt:  1712957300000,
			ChannelId: chanID,
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

func reactionCustomEmojiEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-11",
		ChannelId: chanID,
		Reactions: []*mmModel.Reaction{{
			UserId:    userA,
			PostId:    postID1,
			EmojiName: "shipit_squirrel",
			CreateAt:  1712957400000,
			UpdateAt:  1712957400000,
			ChannelId: chanID,
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

func testEnvelope() *TransportEnvelope {
	return &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeTest,
		ConnName:    "nats-low-to-high",
		Timestamp:   fixedTimestamp,
		TeamName:    "",
		ChannelName: "",
		TestID:      "test-37io7o7ewliugtoc022jpmyb1e",
	}
}

// 13: MembershipChange (join). Carries the channel join event without
// any post payload, exercising the <MembershipChanges> container.
func membershipChangeJoinEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-13",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userB: bob()},
		MembershipChanges: []*mmModel.MembershipChangeMsg{{
			ChannelId:  chanID,
			UserId:     userB,
			IsAdd:      true,
			RemoteId:   "remote-low",
			ChangeTime: 1712957500000,
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

// 14: MembershipChange (leave). Exercises IsAdd=false. Users map is
// intentionally empty since the framework does not require the leaving
// user's profile to accompany the membership event.
func membershipChangeLeaveEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-14",
		ChannelId: chanID,
		MembershipChanges: []*mmModel.MembershipChangeMsg{{
			ChannelId:  chanID,
			UserId:     userB,
			IsAdd:      false,
			RemoteId:   "remote-low",
			ChangeTime: 1712957600000,
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

// 15: Status (DND with end time). Exercises the <Statuses> container.
// Note: ActiveChannel is intentionally dropped at the wire layer even
// though the upstream Status has one; verify it does not appear here.
func statusDndEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm-15",
		ChannelId: chanID,
		Statuses: []*mmModel.Status{{
			UserId:         userA,
			Status:         "dnd",
			Manual:         true,
			LastActivityAt: 1712957700000,
			ActiveChannel:  "should-not-appear-on-wire",
			DNDEndTime:     1712961300,
		}},
	}
	return baseEnvelope("nats-low-to-high", "general", msg)
}

// 16: PostAcknowledgement. Exercises the <Acknowledgements> container.
func postAcknowledgementEnvelope() *TransportEnvelope {
	return baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm-16",
		ChannelId: chanID,
		Acknowledgements: []*mmModel.PostAcknowledgement{{
			UserId:         userA,
			PostId:         postID1,
			AcknowledgedAt: 1712957800000,
			ChannelId:      chanID,
		}},
	})
}

// 17: MentionTransforms. Exercises the <MentionTransforms> sorted-key
// map serialization.
func mentionTransformsEnvelope() *TransportEnvelope {
	return baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm-17",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice()},
		Posts: []*mmModel.Post{{
			Id:        postID1,
			CreateAt:  1712957900000,
			UpdateAt:  1712957900000,
			UserId:    userA,
			ChannelId: chanID,
			Message:   "Heads up @alice and @bob, please review the relay config change.",
		}},
		MentionTransforms: map[string]string{
			"@alice": userA,
			"@bob":   userB,
		},
	})
}

// 18: Post with typed PostProps (webhook + AI provenance). Exercises
// every PostProps field so the compliance reviewer can see the full
// whitelisted set in action. Drops the upstream "attachments" and
// "force_notification" keys silently.
func postWithPropsWebhookEnvelope() *TransportEnvelope {
	return baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm-18",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice()},
		Posts: []*mmModel.Post{{
			Id:        postID1,
			CreateAt:  1712958000000,
			UpdateAt:  1712958000000,
			UserId:    userA,
			ChannelId: chanID,
			Message:   "Build succeeded for v1.2.3. Deploy window opens at 14:00 UTC.",
			Props: mmModel.StringInterface{
				"from_webhook":             true,
				"from_bot":                 false,
				"override_username":        "CI Bot",
				"override_icon_url":        "https://ci.example.com/icon.png",
				"override_icon_emoji":      ":white_check_mark:",
				"webhook_display_name":     "Continuous Integration",
				"ai_generated_by":          userBot,
				"ai_generated_by_username": "release-summary-bot",
				"mentionHighlightDisabled": true,
				// Dropped upstream keys; must not appear on wire:
				"attachments":        "must not cross",
				"force_notification": true,
				"channel_mentions":   map[string]any{"@here": "drop"},
			},
		}},
	})
}

// 19: User with Timezone (open StringMap) and typed UserProps. Exercises
// the framework-set RemoteUsername / RemoteEmail / OriginalRemoteId
// keys, plus a customStatus payload, plus the Timezone map.
func userWithTimezoneAndPropsEnvelope() *TransportEnvelope {
	remoteID := "remote-low"
	user := &mmModel.User{
		Id:        userA,
		CreateAt:  1700000000000,
		UpdateAt:  1712958100000,
		Username:  "alice",
		Email:     "alice@server-a.example.com",
		Nickname:  "Alice",
		FirstName: "Alice",
		LastName:  "Smith",
		Position:  "Reliability Engineer",
		Roles:     "system_user",
		Locale:    "en",
		Timezone: mmModel.StringMap{
			"useAutomaticTimezone": "true",
			"automaticTimezone":    "America/New_York",
			"manualTimezone":       "",
		},
		Props: mmModel.StringMap{
			"customStatus":     `{"emoji":":coffee:","text":"deep focus"}`,
			"RemoteUsername":   "alice",
			"RemoteEmail":      "alice@server-a.example.com",
			"OriginalRemoteId": "remote-low",
			// Unknown upstream key; must not appear on wire:
			"some_local_setting": "drop me",
		},
		RemoteId: &remoteID,
	}
	return baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm-19",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: user},
	})
}

// 20: Bot user. Exercises IsBot, BotDescription, BotLastIconUpdate, and
// shows that bot users carry no Timezone / Locale / Position.
func botUserEnvelope() *TransportEnvelope {
	bot := &mmModel.User{
		Id:                userBot,
		CreateAt:          1700000000000,
		UpdateAt:          1712958200000,
		Username:          "deploybot",
		Email:             "deploybot@server-a.example.com",
		Roles:             "system_user",
		IsBot:             true,
		BotDescription:    "Posts deployment progress to release channels.",
		BotLastIconUpdate: 1712000000000,
	}
	return baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm-20",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userBot: bot},
		Posts: []*mmModel.Post{{
			Id:        postID1,
			CreateAt:  1712958200000,
			UpdateAt:  1712958200000,
			UserId:    userBot,
			ChannelId: chanID,
			Message:   "Deployment of v1.2.3 to production complete.",
		}},
	})
}
