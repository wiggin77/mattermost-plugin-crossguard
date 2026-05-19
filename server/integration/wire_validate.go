//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// wireValidateEnv enables the wire-envelope validation gate when set
// to "1". Off by default so the integration suite remains fast for
// developers iterating on functional behavior; CI and the compliance
// review path set it to mandate schema conformance for every outbound
// envelope the plugin emits.
const wireValidateEnv = "CROSSGUARD_WIRE_VALIDATE"

// archiveDirInContainer must match docker-compose.dev.yml's
// CROSSGUARD_ENVELOPE_ARCHIVE_DIR env var on both mattermost-a and
// mattermost-b. The plugin writes outbound envelopes here; the test
// runner reads them back via `docker compose cp` and validates each
// against schema/crossguard.xsd. The path is a host bind mount so
// the plugin process and the docker-host view stay in sync.
const archiveDirInContainer = "/mattermost/wire-archive"

// WireValidateEnabled reports whether the wire-validation mode is
// active for this test run.
func WireValidateEnabled() bool {
	return os.Getenv(wireValidateEnv) == "1"
}

// snapshotWireArchive records, per server, the set of envelope
// filenames currently in the container's archive. Used at NewHarness
// time when CROSSGUARD_WIRE_VALIDATE=1 so validateWireArchive can
// filter out envelopes from any previous tests in this `go test` run
// and only inspect what was added during this harness's lifetime.
//
// Implementation copies the archive out to a host temp dir via
// `docker compose cp` (the dev container images are distroless and
// have no shell or coreutils for in-container scripting). t.TempDir
// auto-cleans the temp dirs at test end.
func (h *Harness) snapshotWireArchive(t *testing.T) {
	t.Helper()
	if h.preWireArchive == nil {
		h.preWireArchive = make(map[string]map[string]bool)
	}
	for _, s := range []Server{h.A, h.B} {
		h.preWireArchive[s.Container] = h.readArchiveNames(t, s.Container)
	}
}

// validateWireArchive copies each server's archive to a fresh host
// temp dir, walks it, skips envelopes that were already present at
// snapshot time, and runs xmllint against schema/crossguard.xsd on
// each new file. Fails the test on any validation error.
//
// Fails the test if zero new envelopes were observed across both
// servers: a passing run that never produced a wire envelope is
// almost certainly a plumbing bug (missing tmpfs mount, missing env
// var, missing publish), not a successful test of an envelope-less
// scenario.
func (h *Harness) validateWireArchive(t *testing.T) {
	t.Helper()
	xmllint, err := exec.LookPath("xmllint")
	if err != nil {
		t.Fatalf("xmllint not on PATH; required for %s=1", wireValidateEnv)
	}
	schema := RepoPath(t, "schema", "crossguard.xsd")

	var total, failed int
	for _, s := range []Server{h.A, h.B} {
		dir := h.copyArchiveToHost(t, s.Container)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read archive dump for %s: %v", s.Container, err)
		}
		pre := h.preWireArchive[s.Container]
		for _, e := range entries {
			if pre[e.Name()] {
				continue
			}
			total++
			path := filepath.Join(dir, e.Name())
			cmd := exec.Command(xmllint, "--noout", "--schema", schema, path) //nolint:gosec // paths derived from test-controlled temp dir and repo-anchored schema constant
			out, valErr := cmd.CombinedOutput()
			if valErr != nil {
				contents, _ := os.ReadFile(path) //nolint:gosec // path is the same temp-dir-rooted path we just validated
				t.Errorf("envelope %s on %s failed schema validation:\n%s--- envelope ---\n%s",
					e.Name(), s.Container, string(out), string(contents))
				failed++
			}
		}
	}
	if total == 0 {
		t.Errorf("%s=1 set but no envelopes were archived during this test; check tmpfs mount and CROSSGUARD_ENVELOPE_ARCHIVE_DIR in docker-compose.dev.yml", wireValidateEnv)
		return
	}
	t.Logf("wire-validate: %d envelope(s) validated against schema/crossguard.xsd, %d failure(s)", total, failed)
}

// readArchiveNames copies the container's archive to a fresh host
// temp dir and returns the set of filenames found. Returns an empty
// set when the archive is missing or empty.
func (h *Harness) readArchiveNames(t *testing.T, container string) map[string]bool {
	t.Helper()
	dir := h.copyArchiveToHost(t, container)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// dir is a t.TempDir(); failure to read it is a test-harness
		// problem, not a missing-archive problem.
		t.Fatalf("read archive dump for %s: %v", container, err)
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

// copyArchiveToHost copies /mattermost/wire-archive/* from the
// container to a fresh host temp dir and returns the host path. The
// trailing "/." on the source asks docker to copy the directory's
// contents (not the directory itself) into the destination, so the
// destination ends up flat: one file per envelope, no nested
// wire-archive/ subdir.
//
// docker cp fails when the source directory does not exist. We log
// the failure but do not fail the test: the caller sees an empty
// host dir, which it interprets as "no envelopes archived yet" —
// which is the correct semantics. The dev compose file mounts the
// tmpfs unconditionally so the dir is always present in practice.
func (h *Harness) copyArchiveToHost(t *testing.T, container string) string {
	t.Helper()
	dst := t.TempDir()
	r := h.Compose.Cp(t, container+":"+archiveDirInContainer+"/.", dst)
	if r.Err != nil {
		t.Logf("docker cp %s:%s/. -> %s: %v (%s)", container, archiveDirInContainer, dst, r.Err, r.Combined())
	}
	return dst
}
