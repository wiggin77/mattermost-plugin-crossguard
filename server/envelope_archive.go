package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// envelopeArchiveDirEnv is the env var that, when set in the plugin
// process's environment, causes every marshalled outbound envelope to
// be written to that directory. Used by the integration test suite to
// verify the plugin emits schema-conformant XML across all transports.
// Unset in production; the archiver is a no-op when unset.
//
// The directory must be a host bind mount in docker-compose.dev.yml
// (not a tmpfs); tmpfs mounts at this path are isolated from the
// docker-host view, so the test runner cannot read them back. See
// the volumes: stanza in docker-compose.dev.yml.
const envelopeArchiveDirEnv = "CROSSGUARD_ENVELOPE_ARCHIVE_DIR"

// envelopeArchiveDir is the configured archive directory, read once
// at process start. Empty when the env var is unset, which disables
// the archiver. Read at init so we do not stat the env var on every
// marshal call.
var envelopeArchiveDir = os.Getenv(envelopeArchiveDirEnv)

// envelopeArchiveSeq monotonically increments to guarantee filename
// uniqueness when multiple envelopes are marshalled in the same
// nanosecond. Filenames sort lexicographically by emission order.
var envelopeArchiveSeq atomic.Uint64

// envelopeArchiveFirstErrOnce surfaces the first archive write error
// to stderr (Mattermost captures plugin stderr as a log entry). We
// log only the first error to keep the signal noticeable without
// flooding logs when, e.g., the archive dir is not writable.
var envelopeArchiveFirstErrOnce sync.Once

// archiveEnvelope writes the marshalled envelope bytes to a file under
// envelopeArchiveDir. No-op when the env var is unset. The first
// write error is logged to stderr; subsequent errors are silently
// swallowed. The integration test helper asserts that at least one
// envelope was archived per scenario, which catches a fully broken
// plumbing.
//
// Filename shape: <unix_nano>_<seq6>_<conn>_<type>.<ext> where <ext>
// is "xml" or "json" matching the wire format that produced the
// bytes. The wire-archive validator dispatches on the extension to
// pipe XML directly through xmllint and to round-trip JSON via the
// wire types before validating the re-encoded XML. Both unix_nano
// and seq are zero-padded so a lexicographic sort matches emission
// order even when many envelopes share a nanosecond.
func archiveEnvelope(env *TransportEnvelope, data []byte, format WireFormat) {
	if envelopeArchiveDir == "" || env == nil {
		return
	}
	seq := envelopeArchiveSeq.Add(1)
	name := fmt.Sprintf("%019d_%06d_%s_%s.%s",
		time.Now().UnixNano(), seq,
		archiveSanitizeSegment(env.ConnName),
		archiveSanitizeSegment(env.Type),
		format)
	path := filepath.Join(envelopeArchiveDir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // test instrumentation; container path is mode-permissive on purpose
		envelopeArchiveFirstErrOnce.Do(func() {
			fmt.Fprintf(os.Stderr,
				"crossguard: envelope archive write failed (first occurrence): %v path=%s\n",
				err, path)
		})
	}
}

// archiveSanitizeSegment maps a free-form identifier onto the
// [A-Za-z0-9_-] subset used in filenames. ConnName already conforms
// to SlugType but Type is a TransportEnvelope attribute with the
// enumerated values sync_msg / test / attachment / profile_image,
// all of which are filename-safe already; the sanitizer is defensive.
func archiveSanitizeSegment(s string) string {
	if s == "" {
		return "_"
	}
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-', c == '_':
			b[i] = c
		default:
			b[i] = '_'
		}
	}
	return string(b)
}
