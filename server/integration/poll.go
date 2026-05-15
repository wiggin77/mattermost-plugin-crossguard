//go:build integration

package integration

import (
	"testing"
	"time"
)

// PollInterval is the cadence used by Eventually between attempts. It matches
// the 1s sleeps in the shell tests being replaced.
const PollInterval = 1 * time.Second

// Eventually polls fn at PollInterval until it returns ok == true, the
// timeout elapses, or t.Failed() goes true. On success it returns the value
// captured by fn. On timeout it calls t.Fatalf with msg, so callers should
// describe what they were waiting for.
//
// fn should be cheap and side-effect free. Errors inside fn (e.g. HTTP
// failures during initial server warmup) should be reflected by returning
// ok=false, not by failing the test directly; transient errors are normal
// and should not abort the poll.
func Eventually[T any](t *testing.T, timeout time.Duration, msg string, fn func() (T, bool)) T {
	t.Helper()
	deadline := time.Now().Add(timeout)
	// First attempt before sleeping; the shell tests usually saw the
	// relay arrive between 1 and 3 polls, and burning an extra second
	// up front adds nothing.
	if v, ok := fn(); ok {
		return v
	}
	tick := time.NewTicker(PollInterval)
	defer tick.Stop()
	for {
		<-tick.C
		if v, ok := fn(); ok {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("Eventually timed out after %s: %s", timeout, msg)
			var zero T
			return zero
		}
	}
}
