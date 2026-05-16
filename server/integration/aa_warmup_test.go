//go:build integration

// =============================================================================
// TEMPORARY FILE: DELETE WHEN THE UPSTREAM FIX LANDS
// =============================================================================
//
// This entire file is a workaround for a race in the Mattermost server's
// shared-channels framework. Once the server-side fix ships in the dev
// container image and the bug is closed, delete this file. No other
// changes are required: the other integration tests will start in
// steady state without it.
//
// Upstream bug: `App.RegisterPluginForSharedChannels` in
// `server/channels/app/remote_cluster.go` calls `rcService.PingNow(rc)`
// synchronously inside the call, before the plugin's caller has had a
// chance to record the returned RemoteId. The plugin's
// `OnSharedChannelsPing` hook therefore cannot resolve the remote and
// returns false, leaving `LastPingAt = 0`. The remote is reported
// offline by `rc.IsOnline()` until the next periodic `pingLoop`
// iteration (~`PingFreq` = 1 minute). Empirically the recovery window
// can extend to ~3 minutes when sync tasks queued during the offline
// period hit MaxRetries and the framework re-attempts only on later
// events.
//
// Tracking: a Jira ticket has been filed for the upstream fix. Search
// the repo for "TEMPORARY-RegisterPlugin-PingRace" to find every
// callsite (this file is the only one as of this writing).
//
// File-name prefix: the file is named "aa_..." so the Go test runner
// processes it before every other *_test.go file in this package
// (tests are visited in file-name order, then by declaration order
// within a file). This concentrates the entire race wait in one
// dedicated test instead of distributing it across whichever test
// happens to run first alphabetically.
//
// =============================================================================

package integration

import (
	"testing"
	"time"
)

// warmupSleep is sized at slightly over two PingFreq cycles (default
// PingFreq = 1 minute) plus a small margin for the framework's
// pingAllNow loop, plugin OnActivate, and provider warmup time.
//
// Why a sleep and not a posted-message warmup: the framework's sync
// queue retries failed sync attempts up to `MaxRetries` (3) with a
// short delay (`NotifyMinimumDelay` = 2s plus internal backoff). A
// post made while all remotes are still offline can exhaust its
// retries before any remote comes back online; the framework then
// drops the post permanently. A warmup that posts and waits for relay
// therefore times out unreliably. A simple sleep is deterministic.
const warmupSleep = 130 * time.Second

// TestAARemoteWarmup_RegisterPluginPingRace is a TEMPORARY workaround
// for the upstream race described at the top of this file. It runs
// before every other test in this package (file-name sort), absorbs
// the framework's PingNow-during-registration cold-start window with
// a sleep, and then exits. Once the upstream fix lands, delete this
// file -- there are no callers in other tests.
//
// Search tag: TEMPORARY-RegisterPlugin-PingRace
func TestAARemoteWarmup_RegisterPluginPingRace(t *testing.T) {
	t.Logf("TEMPORARY: sleeping %s to absorb the upstream "+
		"RegisterPluginForSharedChannels/PingNow race. Delete this "+
		"test (aa_warmup_test.go) once the framework fix is in the "+
		"dev image. See file header for details.", warmupSleep)
	time.Sleep(warmupSleep)
}
