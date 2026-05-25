package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ecmaRegexp adapts dlclark/regexp2 to jsonschema/v6's Regexp
// interface. JSON Schema mandates ECMA 262 regex semantics, and the
// crossguard schema uses repeat counts up to {0,1536} (matching the
// upstream Mattermost Roles column limit). Go's stdlib regexp/syntax
// caps repeat counts at 1000, which would block the schema from
// loading. regexp2 is a Go port of .NET's regex with ECMA-style
// semantics and no such cap.
type ecmaRegexp struct{ re *regexp2.Regexp }

func (e *ecmaRegexp) MatchString(s string) bool {
	ok, err := e.re.MatchString(s)
	return err == nil && ok
}

func (e *ecmaRegexp) String() string { return e.re.String() }

func ecmaCompile(pattern string) (jsonschema.Regexp, error) {
	re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	return &ecmaRegexp{re: re}, nil
}

// jsonExampleCase pairs a JSON filename with the same envelope builder
// used by the XML example test. The two sets must produce equivalent
// decoded values so the wire-archive validator's JSON-to-XML round
// trip remains lossless.
type jsonExampleCase struct {
	jsonFile string
	xmlFile  string
	env      func() *TransportEnvelope
}

// jsonExampleCases mirrors the XML fixture set one-for-one. The
// schema/examples directory is the authoritative reference handed to
// compliance and security reviewers approving each wire format, so a
// reviewer of the JSON spec must see every element shape the XML
// reviewer sees. Never pare this list down to a subset.
func jsonExampleCases() []jsonExampleCase {
	return []jsonExampleCase{
		{"01_post_simple.json", "01_post_simple.xml", postSimpleEnvelope},
		{"02_post_markdown_rich.json", "02_post_markdown_rich.xml", postMarkdownRichEnvelope},
		{"03_post_code_blocks.json", "03_post_code_blocks.xml", postCodeBlocksEnvelope},
		{"04_post_table_and_links.json", "04_post_table_and_links.xml", postTableAndLinksEnvelope},
		{"05_post_thread_reply.json", "05_post_thread_reply.xml", postThreadReplyEnvelope},
		{"06_post_incident_report.json", "06_post_incident_report.xml", postIncidentReportEnvelope},
		{"07_update_edited_post.json", "07_update_edited_post.xml", postUpdateEnvelope},
		{"08_delete.json", "08_delete.xml", postDeleteEnvelope},
		{"09_reaction_add.json", "09_reaction_add.xml", reactionAddEnvelope},
		{"10_reaction_remove.json", "10_reaction_remove.xml", reactionRemoveEnvelope},
		{"11_reaction_custom_emoji.json", "11_reaction_custom_emoji.xml", reactionCustomEmojiEnvelope},
		{"12_test.json", "12_test.xml", testEnvelope},
		{"13_membership_change_join.json", "13_membership_change_join.xml", membershipChangeJoinEnvelope},
		{"14_membership_change_leave.json", "14_membership_change_leave.xml", membershipChangeLeaveEnvelope},
		{"15_status_dnd.json", "15_status_dnd.xml", statusDndEnvelope},
		{"16_post_acknowledgement.json", "16_post_acknowledgement.xml", postAcknowledgementEnvelope},
		{"17_mention_transforms.json", "17_mention_transforms.xml", mentionTransformsEnvelope},
		{"18_post_with_props_webhook.json", "18_post_with_props_webhook.xml", postWithPropsWebhookEnvelope},
		{"19_user_with_timezone_and_props.json", "19_user_with_timezone_and_props.xml", userWithTimezoneAndPropsEnvelope},
		{"20_bot_user.json", "20_bot_user.xml", botUserEnvelope},
		{"21_metadata_orphan_reaction.json", "21_metadata_orphan_reaction.xml", metadataOrphanReactionEnvelope},
		{"22_system_add_to_channel.json", "22_system_add_to_channel.xml", systemAddToChannelEnvelope},
	}
}

// TestExampleFilesJSON marshals each in-code envelope to JSON and
// compares it to the on-disk fixture. Set UPDATE_EXAMPLES_JSON=1 to
// regenerate the files from the current code output; otherwise the
// test asserts byte-for-byte equality.
//
// The XML and JSON example sets ship the same fixtures one-for-one so
// compliance reviewers approving each wire format see the same surface.
// UPDATE_EXAMPLES regenerates the XML set, UPDATE_EXAMPLES_JSON
// regenerates the JSON set.
func TestExampleFilesJSON(t *testing.T) {
	update := os.Getenv("UPDATE_EXAMPLES_JSON") == "1"
	dir := examplesDir()

	for _, tc := range jsonExampleCases() {
		t.Run(tc.jsonFile, func(t *testing.T) {
			env := tc.env()
			data, err := json.Marshal(env)
			require.NoError(t, err)
			want := make([]byte, 0, len(data)+1)
			want = append(want, data...)
			want = append(want, '\n')

			path := filepath.Join(dir, tc.jsonFile)
			if update {
				require.NoError(t, os.WriteFile(path, want, 0o600)) //nolint:gosec // example fixtures, deterministic input
				return
			}

			got, err := os.ReadFile(path) //nolint:gosec // example fixture path is a constant under the repo
			require.NoError(t, err)
			assert.Equal(t, string(want), string(got), "regenerate with UPDATE_EXAMPLES_JSON=1 if the format change is intentional")
		})
	}
}

// TestExampleFilesJSONValidateAgainstSchema decodes each JSON example,
// re-encodes it as XML through the same TransportEnvelope wire types,
// and pipes the XML through xmllint against schema/crossguard.xsd.
// The round trip is the runtime enforcement of the JSON Schema: any
// JSON envelope that fails the XSD after re-encoding is a compliance
// violation by construction, because the wire types are the only
// path from JSON bytes to XML bytes.
func TestExampleFilesJSONValidateAgainstSchema(t *testing.T) {
	xmllint := requireXmllint(t)
	dir := examplesDir()
	for _, tc := range jsonExampleCases() {
		t.Run(tc.jsonFile, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tc.jsonFile)) //nolint:gosec // example fixture path is a constant under the repo
			require.NoError(t, err)

			var env TransportEnvelope
			require.NoError(t, json.Unmarshal(data, &env))

			xmlBody, err := xml.Marshal(&env)
			require.NoError(t, err)
			out := append([]byte(xml.Header), xmlBody...)
			validateAgainstSchema(t, xmllint, out)
		})
	}
}

// TestJSONSchemaIsFresh regenerates schema/crossguard.schema.json from
// schema/crossguard.xsd via the in-tree generator and asserts it is
// byte-identical to the checked-in file. This is the same gate as
// `make check-json-schema`, surfaced as a Go test so that any test
// run on any machine catches a stale schema.
func TestJSONSchemaIsFresh(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go binary not on PATH: %v", err)
	}
	cmd := exec.Command(goBin, "run", "./build/xsd2jsonschema", "schema/crossguard.xsd") //nolint:gosec // goBin resolved via LookPath; args are constants
	cmd.Dir = ".."
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err = cmd.Run(); err != nil {
		t.Fatalf("xsd2jsonschema failed: %v\n%s", err, out.String())
	}

	onDisk, err := os.ReadFile(filepath.Join("..", "schema", "crossguard.schema.json"))
	require.NoError(t, err)
	if !bytes.Equal(out.Bytes(), onDisk) {
		t.Fatalf("schema/crossguard.schema.json is stale. Run 'make generate-json-schema' and commit the result.")
	}
}

// TestExampleFilesJSONValidateAgainstJSONSchema validates every JSON
// example directly against schema/crossguard.schema.json using a
// JSON Schema validator. This is the JSON-side analogue of the XSD
// gate the XML examples already pass through: it is the standalone
// compliance gate for the JSON wire format, independent of the XML
// round-trip in TestExampleFilesJSONValidateAgainstSchema. If the
// xsd2jsonschema generator drifts from the XSD's semantics in a
// way that lets an example pass the XML re-encode but fail the
// JSON Schema (or vice versa), this gate catches it. Together the
// two gates pin both encodings to their respective compliance
// artifacts.
func TestExampleFilesJSONValidateAgainstJSONSchema(t *testing.T) {
	schemaPath := filepath.Join("..", "schema", "crossguard.schema.json")
	schemaBytes, err := os.ReadFile(schemaPath) //nolint:gosec // schema path is a constant under the repo
	require.NoError(t, err)
	var schemaDoc any
	require.NoError(t, json.Unmarshal(schemaBytes, &schemaDoc))

	c := jsonschema.NewCompiler()
	c.UseRegexpEngine(ecmaCompile)
	const schemaURL = "https://crossguard.local/crossguard.schema.json"
	require.NoError(t, c.AddResource(schemaURL, schemaDoc))
	schema, err := c.Compile(schemaURL)
	require.NoError(t, err)

	dir := examplesDir()
	for _, tc := range jsonExampleCases() {
		t.Run(tc.jsonFile, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tc.jsonFile)) //nolint:gosec // example fixture path is a constant under the repo
			require.NoError(t, err)
			var instance any
			require.NoError(t, json.Unmarshal(data, &instance))
			if err := schema.Validate(instance); err != nil {
				t.Fatalf("JSON Schema validation failed for %s:\n%v", tc.jsonFile, err)
			}
		})
	}
}

// TestExampleFilesJSONXMLEquivalence asserts that each numbered
// fixture is the same envelope in both encodings. The decoded value
// from the JSON file must equal the decoded value from the XML file,
// confirming the JSON encoder and XML encoder treat the data
// identically.
func TestExampleFilesJSONXMLEquivalence(t *testing.T) {
	dir := examplesDir()
	for _, tc := range jsonExampleCases() {
		t.Run(tc.jsonFile, func(t *testing.T) {
			jsonData, err := os.ReadFile(filepath.Join(dir, tc.jsonFile)) //nolint:gosec // example fixture path is a constant under the repo
			require.NoError(t, err)
			xmlData, err := os.ReadFile(filepath.Join(dir, tc.xmlFile)) //nolint:gosec // example fixture path is a constant under the repo
			require.NoError(t, err)

			var fromJSON, fromXML TransportEnvelope
			require.NoError(t, json.Unmarshal(jsonData, &fromJSON))
			require.NoError(t, xml.Unmarshal(xmlData, &fromXML))

			// Compare via JSON re-marshal: the wire-archive validator's
			// runtime guarantee is that the two encoders agree on the
			// data they admit. Comparing structurally via JSON
			// re-marshal of both decoded envelopes is the cleanest way
			// to assert "same data, different encoding".
			a, err := json.Marshal(&fromJSON)
			require.NoError(t, err)
			b, err := json.Marshal(&fromXML)
			require.NoError(t, err)
			assert.JSONEq(t, string(a), string(b))
		})
	}
}
