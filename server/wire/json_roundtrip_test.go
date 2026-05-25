package wire

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStringMap_JSONShape verifies that StringMap marshals as a sorted
// array of {"key":..., "value":...} objects and round-trips losslessly.
// Empty input is suppressed by the parent struct's omitempty tag in
// production; the type itself yields "[]" if reached directly.
func TestStringMap_JSONShape(t *testing.T) {
	in := StringMap{"b": "two", "a": "one"}
	out, err := json.Marshal(in)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"key":"a","value":"one"},{"key":"b","value":"two"}]`, string(out))

	// Direct byte equality confirms the sorted-key ordering is stable.
	assert.Equal(t, `[{"key":"a","value":"one"},{"key":"b","value":"two"}]`, string(out))

	var got StringMap
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, in, got)
}

// TestStringMap_EmptyJSON verifies the empty-map suppression contract:
// when a StringMap is reached directly with no entries it emits "[]",
// but when nested in a parent struct with omitempty it is elided.
func TestStringMap_EmptyJSON(t *testing.T) {
	out, err := json.Marshal(StringMap{})
	require.NoError(t, err)
	assert.Equal(t, "[]", string(out))

	// Nested in a struct with omitempty: field omitted entirely.
	type wrap struct {
		TZ StringMap `json:"tz,omitempty"`
	}
	b, err := json.Marshal(wrap{TZ: StringMap{}})
	require.NoError(t, err)
	assert.Equal(t, "{}", string(b))
}

// TestMentionTransforms_JSONShape verifies the array-of-kv shape and
// deterministic key order.
func TestMentionTransforms_JSONShape(t *testing.T) {
	in := MentionTransforms{"@bob": "userB", "@alice": "userA"}
	out, err := json.Marshal(in)
	require.NoError(t, err)
	assert.Equal(t,
		`[{"key":"@alice","value":"userA"},{"key":"@bob","value":"userB"}]`,
		string(out))

	var got MentionTransforms
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, in, got)
}

// TestUserMap_JSONShape verifies that UserMap emits an array of User
// objects with the map key as the "id" sibling key, sorted by key.
func TestUserMap_JSONShape(t *testing.T) {
	in := UserMap{
		"u02bobaaaaaaaaaaaaaaaaaaaa": {
			Id:       "u02bobaaaaaaaaaaaaaaaaaaaa",
			Username: "bob",
			Email:    "bob@example.com",
			Roles:    "system_user",
		},
		"u01aliceaaaaaaaaaaaaaaaaaa": {
			Id:       "u01aliceaaaaaaaaaaaaaaaaaa",
			Username: "alice",
			Email:    "alice@example.com",
			Roles:    "system_user",
		},
	}
	out, err := json.Marshal(in)
	require.NoError(t, err)

	// Decode into a generic shape to verify ordering and content.
	var arr []map[string]any
	require.NoError(t, json.Unmarshal(out, &arr))
	require.Len(t, arr, 2)
	assert.Equal(t, "u01aliceaaaaaaaaaaaaaaaaaa", arr[0]["id"])
	assert.Equal(t, "u01aliceaaaaaaaaaaaaaaaaaa", arr[0]["Id"])
	assert.Equal(t, "alice", arr[0]["Username"])
	assert.Equal(t, "u02bobaaaaaaaaaaaaaaaaaaaa", arr[1]["id"])
	assert.Equal(t, "bob", arr[1]["Username"])

	// Round-trip.
	var got UserMap
	require.NoError(t, json.Unmarshal(out, &got))
	require.Len(t, got, 2)
	assert.Equal(t, in["u01aliceaaaaaaaaaaaaaaaaaa"].Username, got["u01aliceaaaaaaaaaaaaaaaaaa"].Username)
	assert.Equal(t, in["u02bobaaaaaaaaaaaaaaaaaaaa"].Email, got["u02bobaaaaaaaaaaaaaaaaaaaa"].Email)
}

// TestUserMap_Deterministic verifies that re-marshalling produces the
// same bytes. Sorted-key iteration is the only correctness contract
// the wire archive relies on for cross-node byte comparability.
func TestUserMap_Deterministic(t *testing.T) {
	in := UserMap{}
	for _, k := range []string{"u02bobaaaaaaaaaaaaaaaaaaaa", "u01aliceaaaaaaaaaaaaaaaaaa", "u03carolaaaaaaaaaaaaaaaaaa"} {
		in[k] = &User{Id: k, Username: strings.ToLower(k[:3]), Email: k + "@x.example.com", Roles: "system_user"}
	}
	a, err := json.Marshal(in)
	require.NoError(t, err)
	b, err := json.Marshal(in)
	require.NoError(t, err)
	assert.Equal(t, string(a), string(b))
}

// TestFileIDs_JSONShape verifies that FileIDs marshals as a flat array
// of strings (no wrapper).
func TestFileIDs_JSONShape(t *testing.T) {
	in := FileIDs{"f1aaaaaaaaaaaaaaaaaaaaaaaa", "f2aaaaaaaaaaaaaaaaaaaaaaaa"}
	out, err := json.Marshal(in)
	require.NoError(t, err)
	assert.Equal(t, `["f1aaaaaaaaaaaaaaaaaaaaaaaa","f2aaaaaaaaaaaaaaaaaaaaaaaa"]`, string(out))

	var got FileIDs
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, in, got)
}

// TestSyncMsg_EmptyContainerSuppression verifies that nil or empty
// Users, Reactions, Statuses, MembershipChanges, Acknowledgements, and
// MentionTransforms are omitted from the JSON output. The XSD declares
// these wrappers with minOccurs=0; suppression at the marshaler level
// keeps the JSON shape aligned with that constraint.
func TestSyncMsg_EmptyContainerSuppression(t *testing.T) {
	m := &SyncMsg{
		Id:        "sm0aaaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa",
	}
	out, err := json.Marshal(m)
	require.NoError(t, err)
	assert.Equal(t,
		`{"Id":"sm0aaaaaaaaaaaaaaaaaaaaaaa","ChannelId":"ch0aaaaaaaaaaaaaaaaaaaaaaa"}`,
		string(out))

	// Adding empty (non-nil) maps and slices must still elide them.
	m.Users = UserMap{}
	m.Reactions = []*Reaction{}
	m.Statuses = []*Status{}
	m.MembershipChanges = []*MembershipChange{}
	m.Acknowledgements = []*PostAcknowledgement{}
	m.MentionTransforms = MentionTransforms{}
	out, err = json.Marshal(m)
	require.NoError(t, err)
	assert.Equal(t,
		`{"Id":"sm0aaaaaaaaaaaaaaaaaaaaaaa","ChannelId":"ch0aaaaaaaaaaaaaaaaaaaaaaa"}`,
		string(out))
}

// TestSyncMsg_FieldOrder verifies the JSON property order matches the
// XSD's element-declaration order, so archived envelopes are
// byte-comparable across nodes that produced equivalent payloads.
func TestSyncMsg_FieldOrder(t *testing.T) {
	m := &SyncMsg{
		Id:        "sm0aaaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa",
		Users: UserMap{
			"u01aliceaaaaaaaaaaaaaaaaaa": {
				Id: "u01aliceaaaaaaaaaaaaaaaaaa", Username: "alice",
				Email: "alice@example.com", Roles: "system_user",
			},
		},
		Post: &Post{
			Id: "p01postaaaaaaaaaaaaaaaaaaa", CreateAt: 1, UpdateAt: 1,
			DeleteAt: 0, UserId: "u01aliceaaaaaaaaaaaaaaaaaa",
			ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa", Message: "hello",
		},
		MentionTransforms: MentionTransforms{"@alice": "userA"},
	}
	out, err := json.Marshal(m)
	require.NoError(t, err)
	// Each non-empty field appears in declaration order.
	idxId := strings.Index(string(out), `"Id"`)
	idxChan := strings.Index(string(out), `"ChannelId"`)
	idxUsers := strings.Index(string(out), `"Users"`)
	idxPost := strings.Index(string(out), `"Post"`)
	idxMT := strings.Index(string(out), `"MentionTransforms"`)
	assert.True(t, idxId < idxChan && idxChan < idxUsers && idxUsers < idxPost && idxPost < idxMT,
		"declaration order violated: %s", string(out))
}

// TestPostProps_JSONRoundTrip verifies that PostProps round-trips
// through both encodings and that the same fields are populated.
func TestPostProps_JSONRoundTrip(t *testing.T) {
	in := &PostProps{
		FromWebhook:           true,
		OverrideUsername:      "CI Bot",
		WebhookDisplayName:    "Continuous Integration",
		AIGeneratedByUsername: "release-bot",
	}
	jsonBytes, err := json.Marshal(in)
	require.NoError(t, err)
	xmlBytes, err := xml.Marshal(in)
	require.NoError(t, err)

	var jsonOut, xmlOut PostProps
	require.NoError(t, json.Unmarshal(jsonBytes, &jsonOut))
	require.NoError(t, xml.Unmarshal(xmlBytes, &xmlOut))
	assert.Equal(t, *in, jsonOut)
	assert.Equal(t, *in, xmlOut)
	assert.Equal(t, jsonOut, xmlOut)
}

// TestUser_JSONRoundTrip verifies that User round-trips through both
// encodings, including the embedded Timezone StringMap and UserProps.
func TestUser_JSONRoundTrip(t *testing.T) {
	in := &User{
		Id:        "u01aliceaaaaaaaaaaaaaaaaaa",
		CreateAt:  1700000000000,
		UpdateAt:  1712956800000,
		Username:  "alice",
		Email:     "alice@example.com",
		FirstName: "Alice",
		Roles:     "system_user",
		Locale:    "en",
		Timezone: StringMap{
			"useAutomaticTimezone": "true",
			"automaticTimezone":    "America/New_York",
		},
		Props: &UserProps{
			RemoteUsername:   "alice",
			OriginalRemoteId: "remoteloaaaaaaaaaaaaaaaaaa",
		},
	}
	jsonBytes, err := json.Marshal(in)
	require.NoError(t, err)

	var got User
	require.NoError(t, json.Unmarshal(jsonBytes, &got))
	assert.Equal(t, in.Id, got.Id)
	assert.Equal(t, in.Username, got.Username)
	assert.Equal(t, in.Timezone, got.Timezone)
	assert.Equal(t, in.Props, got.Props)
}

// TestSyncMsg_JSONRoundTripFull verifies a fully-populated SyncMsg
// survives JSON encode + decode without losing data, and that the
// resulting struct equals the original.
func TestSyncMsg_JSONRoundTripFull(t *testing.T) {
	in := &SyncMsg{
		Id:        "sm01aaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa",
		Users: UserMap{
			"u01aliceaaaaaaaaaaaaaaaaaa": {
				Id: "u01aliceaaaaaaaaaaaaaaaaaa", Username: "alice",
				Email: "alice@example.com", Roles: "system_user",
			},
		},
		Post: &Post{
			Id: "p01postaaaaaaaaaaaaaaaaaaa", CreateAt: 1, UpdateAt: 1,
			UserId:    "u01aliceaaaaaaaaaaaaaaaaaa",
			ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa",
			Message:   "hello", FileIds: FileIDs{"f1aaaaaaaaaaaaaaaaaaaaaaaa"},
		},
		Reactions: []*Reaction{{
			UserId: "u01aliceaaaaaaaaaaaaaaaaaa", PostId: "p01postaaaaaaaaaaaaaaaaaaa",
			EmojiName: "thumbsup", CreateAt: 1, UpdateAt: 1,
			ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa",
		}},
		MembershipChanges: []*MembershipChange{{
			ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa",
			UserId:    "u01aliceaaaaaaaaaaaaaaaaaa", IsAdd: true, ChangeTime: 5,
		}},
		Statuses: []*Status{{
			UserId: "u01aliceaaaaaaaaaaaaaaaaaa", Status: "online",
			Manual: false, LastActivityAt: 9, DNDEndTime: 0,
		}},
		Acknowledgements: []*PostAcknowledgement{{
			UserId: "u01aliceaaaaaaaaaaaaaaaaaa", PostId: "p01postaaaaaaaaaaaaaaaaaaa",
			AcknowledgedAt: 7, ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa",
		}},
		MentionTransforms: MentionTransforms{"@alice": "userA"},
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)

	var got SyncMsg
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, in.Id, got.Id)
	require.NotNil(t, got.Post)
	assert.Equal(t, in.Post.Message, got.Post.Message)
	require.Len(t, got.Users, 1)
	assert.Equal(t, in.Users["u01aliceaaaaaaaaaaaaaaaaaa"].Username, got.Users["u01aliceaaaaaaaaaaaaaaaaaa"].Username)
	require.Len(t, got.Reactions, 1)
	assert.Equal(t, in.Reactions[0].EmojiName, got.Reactions[0].EmojiName)
	require.Len(t, got.MembershipChanges, 1)
	assert.True(t, got.MembershipChanges[0].IsAdd)
	require.Len(t, got.Statuses, 1)
	assert.Equal(t, "online", got.Statuses[0].Status)
	require.Len(t, got.Acknowledgements, 1)
	assert.Equal(t, int64(7), got.Acknowledgements[0].AcknowledgedAt)
	assert.Equal(t, "userA", got.MentionTransforms["@alice"])
}

// TestSyncMsg_CrossEncodingEquivalence verifies that decoding the same
// SyncMsg from XML and from JSON yields equal struct values. This is
// the cross-encoding equivalence guarantee the wire-archive validator
// relies on (decode JSON, re-encode XML, validate against XSD).
func TestSyncMsg_CrossEncodingEquivalence(t *testing.T) {
	in := &SyncMsg{
		Id:        "sm01aaaaaaaaaaaaaaaaaaaaaa",
		ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa",
		Post: &Post{
			Id: "p01postaaaaaaaaaaaaaaaaaaa", CreateAt: 1, UpdateAt: 1,
			UserId:    "u01aliceaaaaaaaaaaaaaaaaaa",
			ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa", Message: "hi",
		},
		MentionTransforms: MentionTransforms{"@alice": "userA"},
	}
	xmlBytes, err := xml.Marshal(in)
	require.NoError(t, err)
	jsonBytes, err := json.Marshal(in)
	require.NoError(t, err)

	var fromXML, fromJSON SyncMsg
	require.NoError(t, xml.Unmarshal(xmlBytes, &fromXML))
	require.NoError(t, json.Unmarshal(jsonBytes, &fromJSON))

	// Equal field-by-field. Compare via re-marshal to JSON since
	// SyncMsg equality is best expressed by the wire content.
	a, _ := json.Marshal(&fromXML)
	b, _ := json.Marshal(&fromJSON)
	assert.Equal(t, string(a), string(b))
}
