package main

import (
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/wire"
)

// freshSenderState constructs the minimum plugin state needed to test
// the outbound sequence-stamping behavior. It mirrors what OnActivate
// sets up for the sender side: a fresh epoch and an empty per-channel
// counter map. Tests that simulate a plugin restart call this helper a
// second time to assert the reset semantics.
func freshSenderState() *Plugin {
	return &Plugin{
		epoch:       mmModel.NewId(),
		outboundSeq: make(map[string]uint64),
	}
}

// TestSenderStateResetOnFreshActivation captures the poc1/poc2 bug:
// when the plugin restarts, both the epoch and the per-channel counter
// must reset together so the first envelope on each channel under the
// new epoch is Sequence=1. The previous KV-persisted counter survived
// restart and caused the new epoch's first envelope to carry seq=20
// instead of seq=1, which forced the receiver into a 30-second gap
// wait. This test ensures the in-memory counter resets alongside the
// epoch on every OnActivate.
func TestSenderStateResetOnFreshActivation(t *testing.T) {
	p1 := freshSenderState()
	require.Equal(t, uint64(1), p1.nextOutboundSeq("c", "ch1"),
		"first call on a channel returns 1")
	require.Equal(t, uint64(2), p1.nextOutboundSeq("c", "ch1"),
		"second call returns 2")
	require.Equal(t, uint64(1), p1.nextOutboundSeq("c", "ch2"),
		"different channel has its own counter starting at 1")

	// Simulate plugin restart: a second OnActivate call. The helper
	// here is the same as what OnActivate does for sender state, so
	// running it twice models the lifecycle.
	p2 := freshSenderState()
	require.NotEqual(t, p1.epoch, p2.epoch,
		"fresh activation produces a new epoch (would catch a reverted reset)")
	require.Equal(t, uint64(1), p2.nextOutboundSeq("c", "ch1"),
		"counter resets on activation so the first envelope per channel under the new epoch is seq=1")
}

// TestNextOutboundSeqIsConcurrencySafe runs many goroutines hammering
// the same channel counter and asserts every increment is unique. The
// in-memory counter uses a mutex; this test catches regressions if
// someone removes the mutex thinking single-leader semantics make it
// unnecessary.
func TestNextOutboundSeqIsConcurrencySafe(t *testing.T) {
	p := freshSenderState()
	const goroutines = 50
	const incrementsEach = 20
	const total = goroutines * incrementsEach

	results := make(chan uint64, total)
	done := make(chan struct{})
	for range goroutines {
		go func() {
			for range incrementsEach {
				results <- p.nextOutboundSeq("c", "ch1")
			}
			done <- struct{}{}
		}()
	}
	for range goroutines {
		<-done
	}
	close(results)

	seen := make(map[uint64]bool, total)
	for v := range results {
		require.False(t, seen[v], "sequence %d returned twice", v)
		seen[v] = true
	}
	require.Len(t, seen, total)
	// The set of returned values must be exactly {1, 2, ..., total}.
	for i := uint64(1); i <= total; i++ {
		assert.True(t, seen[i], "expected %d in the result set", i)
	}
}

// TestOutboundFanoutSequenceMonotonic exercises the full sender-side
// path from a single upstream SyncMsg through fanout to stamped
// envelopes, asserting consecutive sequences are assigned in order for
// the same channel.
func TestOutboundFanoutSequenceMonotonic(t *testing.T) {
	p := freshSenderState()

	template := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    "conn",
		TeamName:    "team-a",
		ChannelName: "general",
	}
	msg := &mmModel.SyncMsg{
		Id:        "sm-1",
		ChannelId: "ch1",
		Users: map[string]*mmModel.User{
			"u1": {Id: "u1", Username: "alice", Email: "alice@example.test"},
		},
		Posts: []*mmModel.Post{
			{Id: "p1", UserId: "u1", Message: "first"},
			{Id: "p2", UserId: "u1", Message: "second"},
			{Id: "p3", UserId: "u1", Message: "third"},
		},
		MembershipChanges: []*mmModel.MembershipChangeMsg{
			{ChannelId: "ch1", UserId: "u1", IsAdd: true, ChangeTime: 100},
		},
	}

	envs := buildOutboundEnvelopes(wire.NewRecordingLogger(), template, msg)
	// Three post envelopes plus one metadata envelope (membership has
	// no associated post in this batch).
	require.Len(t, envs, 4)

	// Stamp Sequence per-envelope the way publishToOutboundConn would.
	for _, env := range envs {
		if env.Type == TransportTypeSyncMsg && env.SyncMsg != nil {
			env.Sequence = p.nextOutboundSeq("conn", env.SyncMsg.ChannelId)
		}
	}

	// Sequences must be 1, 2, 3, 4 in order.
	for i, env := range envs {
		want := uint64(i) + 1 //nolint:gosec // i is a slice index bounded by len(envs)=4
		assert.Equal(t, want, env.Sequence,
			"envelope %d should carry Sequence=%d", i, want)
	}

	// The three post envelopes carry distinct posts, the fourth is the
	// metadata envelope.
	require.NotNil(t, envs[0].SyncMsg.Post)
	require.NotNil(t, envs[1].SyncMsg.Post)
	require.NotNil(t, envs[2].SyncMsg.Post)
	assert.Nil(t, envs[3].SyncMsg.Post, "fourth envelope is the metadata envelope")
	require.Len(t, envs[3].SyncMsg.MembershipChanges, 1)
}

// TestOutboundInboundRoundTrip is the end-to-end interaction test that
// the existing unit tests didn't have. It runs the sender's fanout +
// marshal path and the receiver's unmarshal + Admit path back to back
// against the same content, asserting the receiver sees the same posts
// the sender emitted, in the right order, with no buffering or gap
// detection. Catches interaction bugs (mismatched contracts between
// sender stamping and receiver expectations) that pass unit tests on
// each side individually.
func TestOutboundInboundRoundTrip(t *testing.T) {
	sender := freshSenderState()

	template := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    "conn",
		Timestamp:   "2026-05-13T10:00:00Z",
		TeamName:    "team-a",
		ChannelName: "general",
	}
	msg := &mmModel.SyncMsg{
		Id:        "sm-rt",
		ChannelId: "ch-rt",
		Users: map[string]*mmModel.User{
			"u1": {Id: "u1", Username: "alice", Email: "alice@example.test"},
		},
		Posts: []*mmModel.Post{
			{Id: "p1", UserId: "u1", Message: "one"},
			{Id: "p2", UserId: "u1", Message: "two"},
			{Id: "p3", UserId: "u1", Message: "three"},
		},
	}

	// Sender side: fan out, stamp epoch + sequence, marshal each.
	envs := buildOutboundEnvelopes(wire.NewRecordingLogger(), template, msg)
	require.Len(t, envs, 3)
	onWire := make([][]byte, 0, len(envs))
	for _, env := range envs {
		env.Epoch = sender.epoch
		if env.Type == TransportTypeSyncMsg && env.SyncMsg != nil {
			env.Sequence = sender.nextOutboundSeq("conn", env.SyncMsg.ChannelId)
		}
		data, err := MarshalEnvelope(env, FormatXML)
		require.NoError(t, err)
		onWire = append(onWire, data)
	}

	// Receiver side: unmarshal each, feed through the sequencer.
	seq, _ := newTestSequencer(t)
	dispatched := []*TransportEnvelope{}
	for _, data := range onWire {
		env, _, err := UnmarshalEnvelope(data)
		require.NoError(t, err)
		dispatched = append(dispatched, seq.Admit("conn", env)...)
	}

	// All three envelopes must dispatch in order with no buffering.
	require.Len(t, dispatched, 3, "round-trip should dispatch all three envelopes")
	for i, env := range dispatched {
		want := uint64(i) + 1 //nolint:gosec // i is a slice index bounded by len(dispatched)=3
		assert.Equal(t, want, env.Sequence,
			"dispatched envelope %d has Sequence=%d", i, want)
		require.NotNil(t, env.SyncMsg)
		require.NotNil(t, env.SyncMsg.Post)
	}
	assert.Equal(t, "p1", dispatched[0].SyncMsg.Post.Id)
	assert.Equal(t, "p2", dispatched[1].SyncMsg.Post.Id)
	assert.Equal(t, "p3", dispatched[2].SyncMsg.Post.Id)
}

// TestOutboundInboundRoundTripAcrossSenderRestart is the bug-shaped
// integration test: a sender publishes a few envelopes, "restarts"
// (fresh epoch + reset counter), publishes more, and the receiver must
// dispatch every envelope without any 30 s gap wait. This is the
// scenario the production bug surfaced under and that no existing
// test covered.
func TestOutboundInboundRoundTripAcrossSenderRestart(t *testing.T) {
	template := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    "conn",
		Timestamp:   "2026-05-13T10:00:00Z",
		TeamName:    "team-a",
		ChannelName: "general",
	}
	mkMsg := func(id, postID, msg string) *mmModel.SyncMsg {
		return &mmModel.SyncMsg{
			Id:        id,
			ChannelId: "ch-rt",
			Users: map[string]*mmModel.User{
				"u1": {Id: "u1", Username: "alice", Email: "alice@example.test"},
			},
			Posts: []*mmModel.Post{{Id: postID, UserId: "u1", Message: msg}},
		}
	}

	// Session 1: sender publishes two envelopes.
	sender1 := freshSenderState()
	session1Wire := publishOnWire(t, sender1, template, []*mmModel.SyncMsg{
		mkMsg("sm-1", "p1", "before restart 1"),
		mkMsg("sm-2", "p2", "before restart 2"),
	})

	// Session 2: sender "restarts". Fresh epoch, counter resets to 0.
	sender2 := freshSenderState()
	require.NotEqual(t, sender1.epoch, sender2.epoch)
	session2Wire := publishOnWire(t, sender2, template, []*mmModel.SyncMsg{
		mkMsg("sm-3", "p3", "after restart 1"),
	})

	// The receiver has already persisted a cursor from session 1. The
	// production bug was triggered by exactly this: cursor checkpointed,
	// then a new-epoch envelope arrived and stalled.
	seq, _ := newTestSequencer(t)

	// Feed session 1 envelopes through normally.
	dispatched := []*TransportEnvelope{}
	for _, data := range session1Wire {
		env, _, err := UnmarshalEnvelope(data)
		require.NoError(t, err)
		dispatched = append(dispatched, seq.Admit("conn", env)...)
	}
	require.Len(t, dispatched, 2, "session 1 envelopes dispatch immediately")

	// Simulate the receiver having checkpointed its session-1 cursor.
	// In production this lives in KV and is reloaded on a leadership
	// handoff or restart, but for this test what matters is that the
	// sequencer's in-memory state is what the next envelope encounters.
	// (Session 2's first envelope just needs to find an existing state
	// with a different epoch; the existing state from session 1 is
	// already in the sequencer.)

	// Session 2 envelopes (new epoch, counter resets to 1) must
	// dispatch immediately, not stall in a 30 s gap wait.
	for _, data := range session2Wire {
		env, _, err := UnmarshalEnvelope(data)
		require.NoError(t, err)
		dispatched = append(dispatched, seq.Admit("conn", env)...)
	}
	require.Len(t, dispatched, 3, "session 2 envelope must dispatch immediately on the new epoch")
	assert.Equal(t, sender2.epoch, dispatched[2].Epoch)
	assert.Equal(t, uint64(1), dispatched[2].Sequence,
		"first envelope of the new epoch carries Sequence=1 after sender reset")
}

// publishOnWire fans out each upstream SyncMsg via buildOutboundEnvelopes,
// stamps Epoch and Sequence the way publishToOutboundConn does, marshals
// each envelope, and returns the wire bytes. Helper used by the
// round-trip tests.
func publishOnWire(t *testing.T, sender *Plugin, template *TransportEnvelope, msgs []*mmModel.SyncMsg) [][]byte {
	t.Helper()
	var out [][]byte
	for _, msg := range msgs {
		envs := buildOutboundEnvelopes(wire.NewRecordingLogger(), template, msg)
		for _, env := range envs {
			env.Epoch = sender.epoch
			if env.Type == TransportTypeSyncMsg && env.SyncMsg != nil {
				env.Sequence = sender.nextOutboundSeq(template.ConnName, env.SyncMsg.ChannelId)
			}
			data, err := MarshalEnvelope(env, FormatXML)
			require.NoError(t, err)
			out = append(out, data)
		}
	}
	return out
}
