package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExampleFiles_Roundtrip verifies that every XML example under
// schema/examples/ unmarshals via the model package and has the expected
// envelope Type and payload shape.
func TestExampleFiles_Roundtrip(t *testing.T) {
	cases := []struct {
		file        string
		wantType    string
		wantPayload string
	}{
		{"01_post_simple.xml", "crossguard_post", "post"},
		{"02_post_markdown_rich.xml", "crossguard_post", "post"},
		{"03_post_code_blocks.xml", "crossguard_post", "post"},
		{"04_post_table_and_links.xml", "crossguard_post", "post"},
		{"05_post_thread_reply.xml", "crossguard_post", "post"},
		{"06_post_incident_report.xml", "crossguard_post", "post"},
		{"07_update_edited_post.xml", "crossguard_update", "post"},
		{"08_delete.xml", "crossguard_delete", "delete"},
		{"09_reaction_add.xml", "crossguard_reaction_add", "reaction"},
		{"10_reaction_remove.xml", "crossguard_reaction_remove", "reaction"},
		{"11_reaction_custom_emoji.xml", "crossguard_reaction_add", "reaction"},
		{"12_test.xml", "crossguard_test", "test"},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join("..", "..", "schema", "examples", tc.file)
			data, err := os.ReadFile(path) //nolint:gosec // path built from hardcoded test-case filename
			require.NoError(t, err)

			require.Equal(t, FormatXML, DetectFormat(data))

			env, err := Unmarshal(data, FormatXML)
			require.NoError(t, err)
			require.Equal(t, tc.wantType, env.Type)
			require.NotEmpty(t, env.Timestamp)

			switch tc.wantPayload {
			case "post":
				require.NotNil(t, env.PostMessage)
				require.Nil(t, env.DeleteMessage)
				require.Nil(t, env.ReactionMessage)
				require.Nil(t, env.TestMessage)
				require.NotEmpty(t, env.PostMessage.PostID)
				require.NotEmpty(t, env.PostMessage.MessageText)
			case "delete":
				require.NotNil(t, env.DeleteMessage)
				require.Nil(t, env.PostMessage)
				require.Nil(t, env.ReactionMessage)
				require.Nil(t, env.TestMessage)
				require.NotEmpty(t, env.DeleteMessage.PostID)
			case "reaction":
				require.NotNil(t, env.ReactionMessage)
				require.Nil(t, env.PostMessage)
				require.Nil(t, env.DeleteMessage)
				require.Nil(t, env.TestMessage)
				require.NotEmpty(t, env.ReactionMessage.EmojiName)
			case "test":
				require.NotNil(t, env.TestMessage)
				require.Nil(t, env.PostMessage)
				require.Nil(t, env.DeleteMessage)
				require.Nil(t, env.ReactionMessage)
				require.NotEmpty(t, env.TestMessage.ID)
			}
		})
	}
}

// TestExampleFiles_Symmetric verifies full marshal/unmarshal symmetry against
// every XML example on disk:
//
//  1. Unmarshal -> Marshal -> Unmarshal produces the same Envelope (struct-level fixed point).
//  2. Marshal is deterministic: marshaling the same Envelope twice yields the same bytes.
//  3. The on-disk file is exactly model.Marshal(env) + "\n". The examples are
//     therefore byte-for-byte identical to what the plugin emits on the wire.
func TestExampleFiles_Symmetric(t *testing.T) {
	dir := filepath.Join("..", "..", "schema", "examples")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".xml") {
			files = append(files, e.Name())
		}
	}
	require.NotEmpty(t, files, "no example XML files found under %s", dir)

	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			orig, err := os.ReadFile(path) //nolint:gosec // path built from hardcoded dir + readdir entry
			require.NoError(t, err)

			// 1. Struct-level fixed point: Unmarshal(Marshal(Unmarshal(file))) == Unmarshal(file).
			env1, err := Unmarshal(orig, FormatXML)
			require.NoError(t, err)

			data2, err := Marshal(env1, FormatXML)
			require.NoError(t, err)

			env2, err := Unmarshal(data2, FormatXML)
			require.NoError(t, err)
			require.Equal(t, env1, env2, "Unmarshal(Marshal(env)) must equal env")

			// 2. Byte-level determinism: Marshal is a pure function of the Envelope.
			data3, err := Marshal(env2, FormatXML)
			require.NoError(t, err)
			require.Equal(t, string(data2), string(data3), "Marshal must be deterministic for the same Envelope")

			// 3. On-disk form matches Marshal(env) + "\n".
			//    The trailing newline is a file-on-disk convention; Marshal itself does not
			//    emit one. Asserting byte-for-byte catches any drift (hand-edits, indentation
			//    sneaking back in, CDATA reintroduction, whitespace changes).
			expected := string(data2) + "\n"
			require.Equal(t, expected, string(orig),
				"on-disk file must equal model.Marshal(env) + \"\\n\". "+
					"If this fails, re-run the conversion: Unmarshal the file then Marshal back with a trailing newline.")
		})
	}
}
