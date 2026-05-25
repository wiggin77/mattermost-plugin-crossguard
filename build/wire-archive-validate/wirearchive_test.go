package wirearchive

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJSONToXML_AllFixtures is the end-to-end gate for the
// JSON-archive validation path used by
// `make docker-integration-test-validate-wire`. For every
// JSON envelope fixture shipped under schema/examples/, the test
// asserts that the helper converts the file to XML and that the
// resulting XML validates against schema/crossguard.xsd.
//
// The chain it exercises is:
//
//	JSON bytes -> envelope (wire.SyncMsg as inner) -> XML bytes -> xmllint
//
// This is the same chain the integration harness runs against each
// archived .json envelope when CROSSGUARD_WIRE_VALIDATE=1. A failure
// here is a strong signal that the JSON-archive validation path is
// broken (either the helper's envelope mirror has drifted from the
// production TransportEnvelope, or the wire types' JSON/XML
// marshalers produce non-equivalent shapes).
func TestJSONToXML_AllFixtures(t *testing.T) {
	xmllint, err := exec.LookPath("xmllint")
	if err != nil {
		t.Fatalf("xmllint not on PATH; install libxml2-utils")
	}
	examplesDir := filepath.Join("..", "..", "schema", "examples")
	schema := filepath.Join("..", "..", "schema", "crossguard.xsd")

	entries, err := os.ReadDir(examplesDir)
	require.NoError(t, err)

	var jsonCount int
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		jsonCount++
		t.Run(e.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(examplesDir, e.Name())) //nolint:gosec // path is rooted at the repo's schema/examples dir
			require.NoError(t, err)

			xmlBytes, err := JSONToXML(data)
			require.NoError(t, err, "JSONToXML failed for %s", e.Name())

			cmd := exec.Command(xmllint, "--noout", "--schema", schema, "-") //nolint:gosec // xmllint resolved via LookPath; schema is a repo-anchored constant
			cmd.Stdin = bytes.NewReader(xmlBytes)
			out, valErr := cmd.CombinedOutput()
			assert.NoError(t, valErr,
				"%s round-tripped to XML failed XSD validation:\n%s--- re-encoded XML ---\n%s",
				e.Name(), string(out), string(xmlBytes))
		})
	}

	// Belt and suspenders: a passing run with zero fixtures would be a
	// silent regression that the fixtures dir was not found.
	require.NotZero(t, jsonCount, "no JSON fixtures found under schema/examples/")
}

// TestJSONToXML_RejectsGarbage confirms that the helper surfaces a
// non-nil error for bytes that are not valid JSON envelopes. The
// integration harness reports this as a test failure (with the file's
// contents in the error message), so the helper must not swallow
// decode errors.
func TestJSONToXML_RejectsGarbage(t *testing.T) {
	_, err := JSONToXML([]byte("not json at all"))
	assert.Error(t, err)
}

// TestJSONToXML_EmitsXMLHeader confirms the helper's output starts
// with the standard XML processing instruction; xmllint accepts both
// header-prefixed and bare documents, but the production XML archive
// path emits the header and the helper must match so the validator
// has identical inputs from both paths.
func TestJSONToXML_EmitsXMLHeader(t *testing.T) {
	in := []byte(`{
		"version": 1,
		"type": "test",
		"ConnName": "c",
		"Timestamp": "2026-05-23T10:00:00Z",
		"TeamName": "c",
		"ChannelName": "c",
		"TestID": "abc123aaaaaaaaaaaaaaaaaaaa"
	}`)
	out, err := JSONToXML(in)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(out, []byte("<?xml")), "expected XML header prefix; got: %s", string(out))
}
