package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/wire"
)

// TestParseWireFormat covers the case-insensitive, whitespace-trimmed
// mapping of config strings to WireFormat values, plus the rejected
// "unrecognized" case. Empty defaults to XML; xml/json (any case)
// resolve; anything else errors.
func TestParseWireFormat(t *testing.T) {
	cases := []struct {
		in      string
		want    WireFormat
		wantErr bool
	}{
		{"", FormatXML, false},
		{"xml", FormatXML, false},
		{"XML", FormatXML, false},
		{"  xml  ", FormatXML, false},
		{"json", FormatJSON, false},
		{"JSON", FormatJSON, false},
		{"  Json ", FormatJSON, false},
		{"yaml", FormatXML, true},
		{"binary", FormatXML, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseWireFormat(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

// TestMarshalEnvelopeJSON_HasNoXMLHeader confirms that JSON output is
// compact JSON with no leading XML header bytes. The wire-archive
// validator dispatches on file extension, and the bytes themselves
// must be parseable as JSON.
func TestMarshalEnvelopeJSON_HasNoXMLHeader(t *testing.T) {
	env := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeTest,
		ConnName:    "nats-low-to-high",
		Timestamp:   "2026-05-23T10:00:00Z",
		TeamName:    "nats-low-to-high",
		ChannelName: "nats-low-to-high",
		TestID:      "abc123aaaaaaaaaaaaaaaaaaaa",
	}
	data, err := MarshalEnvelope(env, FormatJSON)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(data), "{"), "JSON envelope must start with '{'")
	assert.False(t, strings.Contains(string(data), "<?xml"), "JSON envelope must not contain an XML header")
	// Sanity: parses as JSON.
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "test", got["type"])
}

// TestUnmarshalEnvelope_AutoDetect covers the format-detection rule:
// first non-whitespace byte selects the decoder, with UTF-8 BOM
// stripping. Every supported entry path is exercised, plus the
// catch-all "unrecognized" rejection.
func TestUnmarshalEnvelope_AutoDetect(t *testing.T) {
	xmlBytes := `<?xml version="1.0" encoding="UTF-8"?><CrossGuardEnvelope version="1" type="test"><ConnName>c</ConnName><Timestamp>2026-05-23T10:00:00Z</Timestamp><TeamName>c</TeamName><ChannelName>c</ChannelName><TestID>abc123aaaaaaaaaaaaaaaaaaaa</TestID></CrossGuardEnvelope>`
	jsonBytes := `{"version":1,"type":"test","ConnName":"c","Timestamp":"2026-05-23T10:00:00Z","TeamName":"c","ChannelName":"c","TestID":"abc123aaaaaaaaaaaaaaaaaaaa"}`

	bom := []byte{0xEF, 0xBB, 0xBF}

	cases := []struct {
		name       string
		data       []byte
		wantFormat WireFormat
		wantErr    error
	}{
		{"bare XML", []byte(xmlBytes), FormatXML, nil},
		{"bare JSON", []byte(jsonBytes), FormatJSON, nil},
		{"XML with BOM", append(append([]byte{}, bom...), []byte(xmlBytes)...), FormatXML, nil},
		{"JSON with BOM", append(append([]byte{}, bom...), []byte(jsonBytes)...), FormatJSON, nil},
		{"JSON with leading whitespace", []byte("  \n\t" + jsonBytes), FormatJSON, nil},
		{"XML with BOM + whitespace", append(append([]byte{}, bom...), []byte("\n  "+xmlBytes)...), FormatXML, nil},

		{"empty", []byte(""), FormatXML, ErrUnrecognizedFormat},
		{"only BOM", bom, FormatXML, ErrUnrecognizedFormat},
		{"only whitespace", []byte("  \n\t"), FormatXML, ErrUnrecognizedFormat},
		{"leading letter", []byte("hello"), FormatXML, ErrUnrecognizedFormat},
		{"leading digit", []byte("0"), FormatXML, ErrUnrecognizedFormat},
		{"leading bracket", []byte("[1,2,3]"), FormatXML, ErrUnrecognizedFormat},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, format, err := UnmarshalEnvelope(tc.data)
			if tc.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tc.wantErr), "want ErrUnrecognizedFormat, got %v", err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, env)
			assert.Equal(t, tc.wantFormat, format)
			assert.Equal(t, "test", env.Type)
			assert.Equal(t, "abc123aaaaaaaaaaaaaaaaaaaa", env.TestID)
		})
	}
}

// TestIsTestMessage_BothFormats confirms that the slash-command and
// NATS file watcher's test-message detection path is format-agnostic
// once UnmarshalEnvelope auto-detects.
func TestIsTestMessage_BothFormats(t *testing.T) {
	env := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeTest,
		ConnName:    "c",
		Timestamp:   "2026-05-23T10:00:00Z",
		TeamName:    "c",
		ChannelName: "c",
		TestID:      "abc123aaaaaaaaaaaaaaaaaaaa",
	}
	for _, f := range []WireFormat{FormatXML, FormatJSON} {
		t.Run(f.String(), func(t *testing.T) {
			data, err := MarshalEnvelope(env, f)
			require.NoError(t, err)
			id, ok := isTestMessage(data)
			require.True(t, ok)
			assert.Equal(t, "abc123aaaaaaaaaaaaaaaaaaaa", id)
		})
	}
}

// TestRoundTripJSON_AllFields verifies a fully-populated envelope
// survives a JSON marshal+unmarshal round trip with no data loss,
// including all SyncMsg containers. The check complements the
// example-fixture tests by exercising fields the example subset omits.
func TestRoundTripJSON_AllFields(t *testing.T) {
	in := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    "low-to-high",
		Timestamp:   "2026-05-23T10:00:00Z",
		Epoch:       "epoch01aaaaaaaaaaaaaaaaaaa",
		Sequence:    42,
		TeamName:    "team-a",
		ChannelName: "general",
		SyncMsg: wire.SyncMsgFromModel(&mmModel.SyncMsg{
			Id:        "sm0aaaaaaaaaaaaaaaaaaaaaaa",
			ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa",
			Users: map[string]*mmModel.User{
				"u01aliceaaaaaaaaaaaaaaaaaa": {
					Id: "u01aliceaaaaaaaaaaaaaaaaaa", Username: "alice",
					Email: "alice@example.com", Roles: "system_user",
				},
			},
			Posts: []*mmModel.Post{{
				Id: "p01postaaaaaaaaaaaaaaaaaaa", CreateAt: 1, UpdateAt: 1,
				UserId:    "u01aliceaaaaaaaaaaaaaaaaaa",
				ChannelId: "ch0aaaaaaaaaaaaaaaaaaaaaaa",
				Message:   "hi",
			}},
			MentionTransforms: map[string]string{"@alice": "userA"},
		}, wire.NewRecordingLogger()),
	}
	data, err := MarshalEnvelope(in, FormatJSON)
	require.NoError(t, err)

	got, format, err := UnmarshalEnvelope(data)
	require.NoError(t, err)
	assert.Equal(t, FormatJSON, format)
	assert.Equal(t, in.Version, got.Version)
	assert.Equal(t, in.Type, got.Type)
	assert.Equal(t, in.Sequence, got.Sequence)
	assert.Equal(t, in.Epoch, got.Epoch)
	require.NotNil(t, got.SyncMsg)
	require.NotNil(t, got.SyncMsg.Post)
	assert.Equal(t, "hi", got.SyncMsg.Post.Message)
	require.Len(t, got.SyncMsg.Users, 1)
	assert.Equal(t, "alice", got.SyncMsg.Users["u01aliceaaaaaaaaaaaaaaaaaa"].Username)
	assert.Equal(t, "userA", got.SyncMsg.MentionTransforms["@alice"])
}

// TestArchiveExtensionMatchesFormat verifies the .xml / .json filename
// suffixes that the wire-archive validator dispatches on. The
// archiver is documented to use the chosen format's identifier as
// the file extension.
func TestArchiveExtensionMatchesFormat(t *testing.T) {
	for _, f := range []WireFormat{FormatXML, FormatJSON} {
		t.Run(f.String(), func(t *testing.T) {
			dir := t.TempDir()
			withArchiveDir(t, dir, func() {
				env := &TransportEnvelope{
					Version:     1,
					Type:        TransportTypeTest,
					ConnName:    "c",
					Timestamp:   "2026-05-23T10:00:00Z",
					TeamName:    "c",
					ChannelName: "c",
					TestID:      "abc123aaaaaaaaaaaaaaaaaaaa",
				}
				_, err := MarshalEnvelope(env, f)
				require.NoError(t, err)
			})
			// One file with the right extension.
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			name := entries[0].Name()
			assert.True(t,
				strings.HasSuffix(name, "."+f.String()),
				"archive filename %q should end with .%s", name, f)
		})
	}
}
