package main

import (
	"bytes"
	"os"
	"os/exec"
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
		{"21_metadata_orphan_reaction.xml", metadataOrphanReactionEnvelope},
		{"22_system_add_to_channel.xml", systemAddToChannelEnvelope},
	}
}

// examplesDir resolves the schema/examples directory relative to this test
// file. Joining a constant base directory with a fixed filename from
// exampleCases is safe; gosec G304 does not apply.
func examplesDir() string { return filepath.Join("..", "schema", "examples") }

// requireXmllint resolves xmllint or fails the test. The schema gate is
// mandatory: every test run on every machine must validate fixtures
// against schema/crossguard.xsd. Mac ships xmllint pre-installed; Linux
// developers install libxml2-utils; CI does the same.
func requireXmllint(t *testing.T) string {
	t.Helper()
	xmllint, err := exec.LookPath("xmllint")
	if err != nil {
		t.Fatalf("xmllint not on PATH; install libxml2-utils (the schema gate is mandatory)")
	}
	return xmllint
}

// validateAgainstSchema pipes data into xmllint --schema crossguard.xsd
// and fails the test on any validation error. Used by both the freshly
// marshalled bytes (in TestExampleFiles) and the on-disk fixtures (in
// TestExampleFilesValidateAgainstSchema) so neither path can silently
// admit a non-conforming envelope.
func validateAgainstSchema(t *testing.T, xmllint string, data []byte) {
	t.Helper()
	schema := filepath.Join("..", "schema", "crossguard.xsd")
	cmd := exec.Command(xmllint, "--noout", "--schema", schema, "-") //nolint:gosec // xmllint is resolved via LookPath; schema path is a constant under the repo
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("schema validation failed:\n%s", string(out))
	}
}

// TestExampleFiles compares each schema/examples/*.xml file to the bytes
// produced by MarshalEnvelope on the canonical struct for that scenario,
// AND validates those freshly produced bytes against schema/crossguard.xsd.
// Validation runs whether UPDATE_EXAMPLES is set or not so a builder
// change that produces a non-conforming envelope cannot be silently
// regenerated onto disk.
// Set UPDATE_EXAMPLES=1 to rewrite the files from the current code output;
// otherwise the test asserts byte-for-byte equality against the on-disk
// fixtures.
func TestExampleFiles(t *testing.T) {
	xmllint := requireXmllint(t)
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

			validateAgainstSchema(t, xmllint, want)

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

// TestExampleFilesValidateAgainstSchema runs every on-disk example
// fixture through xmllint against schema/crossguard.xsd. This catches
// any case where a fixture was modified by hand or by an out-of-band
// tool without going through the regeneration path; TestExampleFiles
// covers the regeneration path itself.
func TestExampleFilesValidateAgainstSchema(t *testing.T) {
	xmllint := requireXmllint(t)
	dir := examplesDir()
	for _, tc := range exampleCases() {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tc.file)) //nolint:gosec // example fixture path is a constant under the repo
			require.NoError(t, err)
			validateAgainstSchema(t, xmllint, data)
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

	// timelineEpoch is the shared sender-session ID for the 01-12 timeline
	// (one sender process emitting 11 sync_msgs plus 1 connectivity ping).
	// Examples 13-20 are independent scenarios and each defines its own
	// epoch in its builder so reviewers can see that the receiver treats
	// them as separate sender sessions.
	timelineEpoch = "epoch01aaaaaaaaaaaaaaaaaaa"
)

func alice() *mmModel.User {
	remoteID := "remoteloaaaaaaaaaaaaaaaaaa"
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
	remoteID := "remoteloaaaaaaaaaaaaaaaaaa"
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
		SyncMsg:     wire.SyncMsgFromModel(msg, wire.NewRecordingLogger()),
	}
}

// withSeq stamps the timeline epoch and a per-envelope sequence on the
// envelope. Used by the 01-11 timeline so a reviewer can read the fixtures
// top-to-bottom as one ordered sender session.
func withSeq(env *TransportEnvelope, seq uint64) *TransportEnvelope {
	env.Epoch = timelineEpoch
	env.Sequence = seq
	return env
}

// withIndependentSession stamps an independent (per-example) epoch and
// sequence. Used by examples 13-20 to show that receivers treat each as a
// separate sender session.
func withIndependentSession(env *TransportEnvelope, epoch string, seq uint64) *TransportEnvelope {
	env.Epoch = epoch
	env.Sequence = seq
	return env
}

func postSimpleEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm01aaaaaaaaaaaaaaaaaaaaaa",
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
	return withSeq(baseEnvelope("nats-low-to-high", "general", msg), 1)
}

func postMarkdownRichEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm02aaaaaaaaaaaaaaaaaaaaaa",
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
	return withSeq(baseEnvelope("nats-low-to-high", "general", msg), 2)
}

func postCodeBlocksEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm03aaaaaaaaaaaaaaaaaaaaaa",
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
	return withSeq(baseEnvelope("nats-low-to-high", "general", msg), 3)
}

func postTableAndLinksEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm04aaaaaaaaaaaaaaaaaaaaaa",
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
	return withSeq(baseEnvelope("nats-low-to-high", "general", msg), 4)
}

func postThreadReplyEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm05aaaaaaaaaaaaaaaaaaaaaa",
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
	return withSeq(baseEnvelope("nats-low-to-high", "general", msg), 5)
}

func postIncidentReportEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm06aaaaaaaaaaaaaaaaaaaaaa",
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
	return withSeq(baseEnvelope("nats-low-to-high", "general", msg), 6)
}

func postUpdateEnvelope() *TransportEnvelope {
	// Edited post: OriginalId points at the post id that this version
	// supersedes (Mattermost creates a new post row on edit and chains
	// it to the predecessor via OriginalId). Exercises <OriginalId>.
	msg := &mmModel.SyncMsg{
		Id:        "sm07aaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice()},
		Posts: []*mmModel.Post{{
			Id:         postID2,
			CreateAt:   1712956800000,
			UpdateAt:   1712957000000,
			EditAt:     1712957000000,
			UserId:     userA,
			ChannelId:  chanID,
			OriginalId: postID1,
			Message:    "Morning team. Standup moved to 9:45 in the upstairs room.",
		}},
	}
	return withSeq(baseEnvelope("nats-low-to-high", "general", msg), 7)
}

func postDeleteEnvelope() *TransportEnvelope {
	// Deleted post: DeleteAt is set, and Props.DeleteBy records the user
	// who issued the delete (typically the author, or a moderator).
	// Exercises <Props><DeleteBy>.
	msg := &mmModel.SyncMsg{
		Id:        "sm08aaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: chanID,
		Posts: []*mmModel.Post{{
			Id:        postID1,
			CreateAt:  1712956800000,
			UpdateAt:  1712957100000,
			DeleteAt:  1712957100000,
			UserId:    userA,
			ChannelId: chanID,
			Props: mmModel.StringInterface{
				"deleteBy": userA,
			},
		}},
	}
	return withSeq(baseEnvelope("nats-low-to-high", "general", msg), 8)
}

func reactionAddEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm09aaaaaaaaaaaaaaaaaaaaaa",
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
	return withSeq(baseEnvelope("nats-low-to-high", "general", msg), 9)
}

func reactionRemoveEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm10aaaaaaaaaaaaaaaaaaaaaa",
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
	return withSeq(baseEnvelope("nats-low-to-high", "general", msg), 10)
}

func reactionCustomEmojiEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm11aaaaaaaaaaaaaaaaaaaaaa",
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
	return withSeq(baseEnvelope("nats-low-to-high", "general", msg), 11)
}

func testEnvelope() *TransportEnvelope {
	// Test envelopes carry the sender's current Epoch (so the receiver can
	// correlate the ping against an ongoing sync_msg session and detect a
	// restart) but never a Sequence (no channel scope). TeamName and
	// ChannelName are populated with the connection name (matching the
	// production buildTestEnvelope behavior) so the envelope satisfies
	// the constrained wire-schema's SlugType requirement on those fields.
	return &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeTest,
		ConnName:    "nats-low-to-high",
		Timestamp:   fixedTimestamp,
		Epoch:       timelineEpoch,
		TeamName:    "nats-low-to-high",
		ChannelName: "nats-low-to-high",
		TestID:      "testid12aaaaaaaaaaaaaaaaaa",
	}
}

// 13: MembershipChange (join). Carries the channel join event without
// any post payload, exercising the <MembershipChanges> container.
func membershipChangeJoinEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm13aaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userB: bob()},
		MembershipChanges: []*mmModel.MembershipChangeMsg{{
			ChannelId:  chanID,
			UserId:     userB,
			IsAdd:      true,
			RemoteId:   "remoteloaaaaaaaaaaaaaaaaaa",
			ChangeTime: 1712957500000,
		}},
	}
	return withIndependentSession(baseEnvelope("nats-low-to-high", "general", msg), "epoch13aaaaaaaaaaaaaaaaaaa", 1)
}

// 14: MembershipChange (leave). Exercises IsAdd=false. Users map is
// intentionally empty since the framework does not require the leaving
// user's profile to accompany the membership event.
func membershipChangeLeaveEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm14aaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: chanID,
		MembershipChanges: []*mmModel.MembershipChangeMsg{{
			ChannelId:  chanID,
			UserId:     userB,
			IsAdd:      false,
			RemoteId:   "remoteloaaaaaaaaaaaaaaaaaa",
			ChangeTime: 1712957600000,
		}},
	}
	return withIndependentSession(baseEnvelope("nats-low-to-high", "general", msg), "epoch14aaaaaaaaaaaaaaaaaaa", 1)
}

// 15: Status (DND with end time). Exercises the <Statuses> container.
// Note: ActiveChannel is intentionally dropped at the wire layer even
// though the upstream Status has one; verify it does not appear here.
func statusDndEnvelope() *TransportEnvelope {
	msg := &mmModel.SyncMsg{
		Id:        "sm15aaaaaaaaaaaaaaaaaaaaaa",
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
	return withIndependentSession(baseEnvelope("nats-low-to-high", "general", msg), "epoch15aaaaaaaaaaaaaaaaaaa", 1)
}

// 16: PostAcknowledgement. Exercises the <Acknowledgements> container.
func postAcknowledgementEnvelope() *TransportEnvelope {
	return withIndependentSession(baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm16aaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: chanID,
		Acknowledgements: []*mmModel.PostAcknowledgement{{
			UserId:         userA,
			PostId:         postID1,
			AcknowledgedAt: 1712957800000,
			ChannelId:      chanID,
		}},
	}), "epoch16aaaaaaaaaaaaaaaaaaa", 1)
}

// 17: MentionTransforms. Exercises the <MentionTransforms> sorted-key
// map serialization.
func mentionTransformsEnvelope() *TransportEnvelope {
	return withIndependentSession(baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm17aaaaaaaaaaaaaaaaaaaaaa",
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
	}), "epoch17aaaaaaaaaaaaaaaaaaa", 1)
}

// 18: Post with typed PostProps (webhook + AI provenance) plus the
// optional Post-level flags. Exhaustive coverage for compliance review:
// every PostProps key in PostPropsType and every optional Post element
// (Hashtags, FileIds, HasReactions, IsPinned) appears here.
// The provenance flags (from_webhook / from_bot / from_plugin /
// from_oauth_app) are all set true purely for element coverage; a real
// post would set exactly one. Drops the upstream "attachments" and
// "force_notification" keys silently.
func postWithPropsWebhookEnvelope() *TransportEnvelope {
	return withIndependentSession(baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm18aaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice()},
		Posts: []*mmModel.Post{{
			Id:           postID1,
			CreateAt:     1712958000000,
			UpdateAt:     1712958000000,
			UserId:       userA,
			ChannelId:    chanID,
			Message:      "Build succeeded for v1.2.3. Deploy window opens at 14:00 UTC.",
			Hashtags:     "#release #v1.2.3",
			FileIds:      mmModel.StringArray{"fileidaaaaaaaaaaaaaaaaaaaa"},
			HasReactions: true,
			IsPinned:     true,
			Props: mmModel.StringInterface{
				"from_webhook":             true,
				"from_bot":                 true,
				"from_plugin":              true,
				"from_oauth_app":           true,
				"override_username":        "CI Bot",
				"override_icon_url":        "https://ci.example.com/icon.png",
				"override_icon_emoji":      ":white_check_mark:",
				"webhook_display_name":     "Continuous Integration",
				"ai_generated_by":          userBot,
				"ai_generated_by_username": "release-summary-bot",
				"mentionHighlightDisabled": true,
				"disable_group_highlight":  true,
				// Dropped upstream keys; must not appear on wire:
				"attachments":        "must not cross",
				"force_notification": true,
				"channel_mentions":   map[string]any{"@here": "drop"},
			},
		}},
	}), "epoch18aaaaaaaaaaaaaaaaaaa", 1)
}

// 19: User with Timezone (open StringMap) and typed UserProps. Exercises
// the framework-set RemoteUsername / RemoteEmail / OriginalRemoteId
// keys, plus a customStatus payload, plus the Timezone map.
func userWithTimezoneAndPropsEnvelope() *TransportEnvelope {
	remoteID := "remoteloaaaaaaaaaaaaaaaaaa"
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
			"OriginalRemoteId": "remoteloaaaaaaaaaaaaaaaaaa",
			// Unknown upstream key; must not appear on wire:
			"some_local_setting": "drop me",
		},
		RemoteId: &remoteID,
	}
	return withIndependentSession(baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm19aaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: user},
	}), "epoch19aaaaaaaaaaaaaaaaaaa", 1)
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
	return withIndependentSession(baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm20aaaaaaaaaaaaaaaaaaaaaa",
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
	}), "epoch20aaaaaaaaaaaaaaaaaaa", 1)
}

// 21: Metadata envelope. A sync cycle that produced no posts emits one
// envelope with no <Post> element, carrying only the non-post content
// (here, a reaction on a post from a previous sync cycle). Compliance
// content inspection sees an envelope with nothing to reject; metadata
// envelopes always flow.
func metadataOrphanReactionEnvelope() *TransportEnvelope {
	return withIndependentSession(baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm21aaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice()},
		Reactions: []*mmModel.Reaction{{
			UserId:    userA,
			PostId:    postID1, // post is from an earlier sync cycle, not in this batch
			EmojiName: "raised_hands",
			CreateAt:  1712958300000,
			UpdateAt:  1712958300000,
			ChannelId: chanID,
		}},
	}), "epoch21aaaaaaaaaaaaaaaaaaa", 1)
}

// 22: System message (user added to channel). Exercises <Post><Type>
// for system_* markers plus the PostProps that carry system-message
// data (AddedUserId, AddChannelMember). Mattermost renders these as
// "<actor> added <target> to the channel"; the receiver replays the
// system message verbatim by feeding the typed wire form back to
// p.API.ReceiveSharedChannelSyncMsg.
func systemAddToChannelEnvelope() *TransportEnvelope {
	return withIndependentSession(baseEnvelope("nats-low-to-high", "general", &mmModel.SyncMsg{
		Id:        "sm22aaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: chanID,
		Users:     map[string]*mmModel.User{userA: alice(), userB: bob()},
		Posts: []*mmModel.Post{{
			Id:        postID3,
			CreateAt:  1712958400000,
			UpdateAt:  1712958400000,
			UserId:    userA,
			ChannelId: chanID,
			Type:      "system_add_to_channel",
			Message:   "added bob to the channel.",
			Props: mmModel.StringInterface{
				"addedUserId":        userB,
				"add_channel_member": userB,
			},
		}},
	}), "epoch22aaaaaaaaaaaaaaaaaaa", 1)
}
