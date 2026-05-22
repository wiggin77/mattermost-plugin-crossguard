package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withArchiveDir temporarily points envelopeArchiveDir at the given
// directory, runs fn, and restores the previous value on return. The
// process-wide envelopeArchiveDir is initialized from the env var
// once at start; tests that need archival behavior set it explicitly
// rather than relying on the env var so they do not contaminate
// neighboring tests.
func withArchiveDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	prev := envelopeArchiveDir
	envelopeArchiveDir = dir
	defer func() { envelopeArchiveDir = prev }()
	fn()
}

func TestArchiveEnvelopeNoOpWhenDirEmpty(t *testing.T) {
	withArchiveDir(t, "", func() {
		// Must not panic, must not write anything, no return value
		// to assert beyond "did not fail." Using a temp dir's
		// emptiness to confirm no spurious write would be circular;
		// simply call and require no panic.
		archiveEnvelope(&TransportEnvelope{ConnName: "c", Type: TransportTypeSyncMsg}, []byte("<x/>"))
	})
}

func TestArchiveEnvelopeWritesFileWhenDirSet(t *testing.T) {
	dir := t.TempDir()
	withArchiveDir(t, dir, func() {
		env := &TransportEnvelope{ConnName: "nats-low-to-high", Type: TransportTypeSyncMsg}
		archiveEnvelope(env, []byte("<CrossGuardEnvelope/>"))
	})

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected exactly one archived envelope")
	name := entries[0].Name()
	assert.True(t, strings.HasSuffix(name, ".xml"), "filename should end with .xml: %s", name)
	assert.Contains(t, name, "nats-low-to-high", "filename should embed conn name: %s", name)
	assert.Contains(t, name, TransportTypeSyncMsg, "filename should embed envelope type: %s", name)

	got, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // test-local temp dir
	require.NoError(t, err)
	assert.Equal(t, "<CrossGuardEnvelope/>", string(got))
}

func TestArchiveEnvelopeUniqueFilenames(t *testing.T) {
	dir := t.TempDir()
	withArchiveDir(t, dir, func() {
		// Multiple writes in the same nanosecond would collide on
		// the timestamp portion; the seq counter prevents overwrite.
		env := &TransportEnvelope{ConnName: "c", Type: TransportTypeSyncMsg}
		for range 100 {
			archiveEnvelope(env, []byte("<x/>"))
		}
	})
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 100, "all writes should produce distinct files")
}

func TestArchiveEnvelopeSanitizesConnName(t *testing.T) {
	dir := t.TempDir()
	withArchiveDir(t, dir, func() {
		// ConnName is SlugType in production but defensively the
		// archiver should map any unexpected characters to '_' so a
		// pathological value can't escape the archive directory or
		// crash filename construction.
		archiveEnvelope(&TransportEnvelope{ConnName: "weird/../name", Type: "t"}, []byte("<x/>"))
	})
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	name := entries[0].Name()
	assert.NotContains(t, name, "/", "filename must not contain path separators")
	assert.NotContains(t, name, "..", "filename must not contain parent-dir markers")
}

func TestArchiveEnvelopeNilEnvelopeNoOp(t *testing.T) {
	dir := t.TempDir()
	withArchiveDir(t, dir, func() {
		archiveEnvelope(nil, []byte("<x/>"))
	})
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestMarshalEnvelopeArchivesWhenEnabled(t *testing.T) {
	// End-to-end: MarshalEnvelope is the chokepoint that all outbound
	// wire envelopes flow through. When the archive dir is set, every
	// successful marshal should drop a file in the archive.
	dir := t.TempDir()
	withArchiveDir(t, dir, func() {
		env := &TransportEnvelope{
			Version:     1,
			Type:        TransportTypeTest,
			ConnName:    "nats-low-to-high",
			Timestamp:   "2026-05-18T10:00:00Z",
			Epoch:       "epoch01aaaaaaaaaaaaaaaaaaa",
			TeamName:    "nats-low-to-high",
			ChannelName: "nats-low-to-high",
			TestID:      "testid12aaaaaaaaaaaaaaaaaa",
		}
		data, err := MarshalEnvelope(env)
		require.NoError(t, err)
		require.NotEmpty(t, data)
	})
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	got, err := os.ReadFile(filepath.Join(dir, entries[0].Name())) //nolint:gosec // test-local temp dir
	require.NoError(t, err)
	assert.Contains(t, string(got), "<CrossGuardEnvelope")
	assert.Contains(t, string(got), `type="test"`)
}
