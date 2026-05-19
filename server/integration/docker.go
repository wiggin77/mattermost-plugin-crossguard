//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// DockerCompose wraps `docker compose` invocations against the dev compose
// file. The integration tests need to run mmctl --local inside the
// mattermost-a / mattermost-b containers for operations that have no
// Client4 equivalent (e.g. creating users/teams without admin signup).
type DockerCompose struct {
	// File is the compose file path, relative to the repo root.
	File string
}

// ExecResult captures the combined output of a docker compose exec call.
type ExecResult struct {
	Stdout string
	Stderr string
	Err    error
}

// Combined returns Stdout + Stderr for diagnostic messages.
func (r ExecResult) Combined() string {
	if r.Stderr == "" {
		return r.Stdout
	}
	if r.Stdout == "" {
		return r.Stderr
	}
	return r.Stdout + "\n" + r.Stderr
}

// Exec runs `docker compose -f <File> exec -T <container> <args...>` and
// captures stdout, stderr, and the command error. It does not call t.Fatal
// on a non-zero exit because some commands (mmctl team create) signal
// "already exists" via exit 1; callers decide whether that is fatal.
func (d *DockerCompose) Exec(t *testing.T, container string, args ...string) ExecResult {
	t.Helper()
	full := append([]string{"compose", "-f", d.File, "exec", "-T", container}, args...)
	// Test-only helper invoking docker compose with caller-supplied args.
	cmd := exec.Command("docker", full...) //nolint:gosec // G204: args come from test code, not external input

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return ExecResult{
		Stdout: strings.TrimRight(stdout.String(), "\n"),
		Stderr: strings.TrimRight(stderr.String(), "\n"),
		Err:    err,
	}
}

// MMCtl runs `mmctl --local <args...>` inside the given container. It
// returns the parsed result. The caller is responsible for inspecting r.Err
// and r.Stderr when the operation is allowed to fail (e.g. "already exists").
func (d *DockerCompose) MMCtl(t *testing.T, container string, args ...string) ExecResult {
	t.Helper()
	full := append([]string{"mmctl", "--local"}, args...)
	return d.Exec(t, container, full...)
}

// IsAlreadyExists reports whether the result indicates an idempotent
// "already exists" failure from mmctl. mmctl phrasing has shifted across
// versions, so we match on common substrings rather than exit code alone.
func IsAlreadyExists(r ExecResult) bool {
	if r.Err == nil {
		return false
	}
	combined := strings.ToLower(r.Combined())
	for _, needle := range []string{
		"already exists",
		"already a member",
		"a team with that name",
		"a channel with that name",
		"a user with that username",
		"a user with that email",
	} {
		if strings.Contains(combined, needle) {
			return true
		}
	}
	return false
}

// MMCtlOrAlreadyExists runs mmctl and ignores "already exists" style errors.
// Any other failure fails the test.
func (d *DockerCompose) MMCtlOrAlreadyExists(t *testing.T, container string, args ...string) ExecResult {
	t.Helper()
	r := d.MMCtl(t, container, args...)
	if r.Err != nil && !IsAlreadyExists(r) {
		t.Fatalf("mmctl %s on %s failed: %v\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), container, r.Err, r.Stdout, r.Stderr)
	}
	return r
}

// Cp runs `docker compose -f <File> cp <src> <dst>` and returns the
// combined stdout/stderr plus any error. Either src or dst may use the
// `service:path` form to copy between the host and a container; the
// other side must be a host path. Used by helpers that need to read
// files inside the dev containers, which are distroless and have no
// shell or coreutils available for in-container scripting.
func (d *DockerCompose) Cp(t *testing.T, src, dst string) ExecResult {
	t.Helper()
	// Test-only helper invoking docker compose with caller-supplied args.
	cmd := exec.Command("docker", "compose", "-f", d.File, "cp", src, dst) //nolint:gosec // G204: args come from test code, not external input

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return ExecResult{
		Stdout: strings.TrimRight(stdout.String(), "\n"),
		Stderr: strings.TrimRight(stderr.String(), "\n"),
		Err:    err,
	}
}

// PluginEnable enables the plugin in the given container.
func (d *DockerCompose) PluginEnable(t *testing.T, container, pluginID string) {
	t.Helper()
	r := d.MMCtl(t, container, "plugin", "enable", pluginID)
	if r.Err != nil {
		t.Fatalf("plugin enable %s on %s failed: %v\n%s",
			pluginID, container, r.Err, r.Combined())
	}
}

// PluginDisable disables the plugin in the given container. It tolerates
// "plugin is not enabled" style errors because a fresh container may not
// have the plugin enabled yet.
func (d *DockerCompose) PluginDisable(t *testing.T, container, pluginID string) {
	t.Helper()
	r := d.MMCtl(t, container, "plugin", "disable", pluginID)
	if r.Err == nil {
		return
	}
	lower := strings.ToLower(r.Combined())
	if strings.Contains(lower, "not enabled") || strings.Contains(lower, "not running") {
		return
	}
	t.Fatalf("plugin disable %s on %s failed: %v\n%s",
		pluginID, container, r.Err, r.Combined())
}

// Sprintf wraps fmt.Sprintf so other helpers in the package can format
// command arguments without an extra import.
func Sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
