package main

import (
	"slices"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

// cursorLoader returns the persisted (epoch, nextExpected) for a
// per-(connName, channelID) cursor. Returns ("", 0, nil) when no
// checkpoint is present. Used by the sequencer to resume after
// leadership handoff (Phase 5).
type cursorLoader func(connName, channelID string) (epoch string, nextExpected uint64, err error)

// inboundSequencer maintains per-(remoteID, channelID) cursors and a
// bounded reorder buffer for inbound sync_msg envelopes. It does not
// dispatch envelopes itself; callers receive the ready batch from Admit
// or from TickGapDeadlines and route them to the existing inbound
// handler.
//
// Envelopes carry (Epoch, Sequence) stamped by the sender (Phase 1).
// The sequencer rejects duplicates, holds gaps until either the
// missing seq fills in or the per-channel deadline expires, and resets
// the cursor on epoch change. Test envelopes are not sequenced; only
// the Epoch is compared for restart-detection audit logging.
//
// Concurrency: the sequencer is goroutine-safe via a single mutex. In
// production it is fed by a single inbound dispatch goroutine per
// active node (Phase 2), so contention is low. TickGapDeadlines is
// safe to call from a separate goroutine.
type inboundSequencer struct {
	api plugin.API

	gapTimeout       time.Duration
	bufferMaxCount   int
	bufferMaxBytes   int
	bytesPerEnvelope func(*TransportEnvelope) int
	loadCursor       cursorLoader

	mu      sync.Mutex
	states  map[seqKey]*seqState
	testEps map[string]string // connName -> last-observed test envelope epoch
}

type seqKey struct {
	connName  string
	channelID string
}

type seqState struct {
	epoch        string
	nextExpected uint64

	// buffer holds envelopes with seq > nextExpected, keyed by seq.
	// gapStart is the first deadline observed since the gap was opened.
	buffer     map[uint64]*bufferedEnvelope
	bufferSize int // total bytes in the buffer (approximate, sum of envelope.size)
	gapStart   time.Time
}

type bufferedEnvelope struct {
	env  *TransportEnvelope
	size int
}

// newInboundSequencer constructs a sequencer with the given knobs. The
// size function lets tests inject a deterministic byte count.
func newInboundSequencer(api plugin.API, gapTimeout time.Duration, maxCount, maxBytes int) *inboundSequencer {
	if gapTimeout <= 0 {
		gapTimeout = sequencerDefaultGapTimeoutSeconds * time.Second
	}
	if maxCount <= 0 {
		maxCount = sequencerDefaultBufferMaxEnvelopes
	}
	if maxBytes <= 0 {
		maxBytes = sequencerDefaultBufferMaxBytes
	}
	return &inboundSequencer{
		api:              api,
		gapTimeout:       gapTimeout,
		bufferMaxCount:   maxCount,
		bufferMaxBytes:   maxBytes,
		bytesPerEnvelope: estimateEnvelopeBytes,
		states:           make(map[seqKey]*seqState),
		testEps:          make(map[string]string),
	}
}

// Admit feeds an envelope through the sequencer state machine and
// returns the envelopes ready to dispatch (in order). The caller is
// responsible for actually dispatching them. Returning a nil/empty
// slice means the envelope was buffered (gap), discarded (duplicate or
// missing fields), or non-sequenceable (test/attachment/profile_image
// pass through unchanged via the second return value being the input
// envelope wrapped in a single-element slice).
//
// Special cases:
//   - Non-sync_msg envelopes are returned as-is, with an audit log for
//     test envelopes carrying an Epoch.
//   - Envelopes missing Epoch or Sequence are returned as-is with an
//     audit (`InboundSeqMissingFields`) so Phase 1/2 senders interop
//     until they're upgraded.
//
// The connName parameter identifies the inbound connection; the
// channelID is derived from env.SyncMsg.ChannelId for sync_msg
// envelopes after rewriting (or it can be passed in by the caller; we
// use env.SyncMsg.ChannelId here for simplicity).
func (s *inboundSequencer) Admit(connName string, env *TransportEnvelope) []*TransportEnvelope {
	if env == nil {
		return nil
	}

	// Non-sync_msg envelopes pass through. For test envelopes, compare
	// the Epoch against the most recently seen one and log a restart
	// detection event for operator visibility.
	if env.Type != TransportTypeSyncMsg {
		if env.Type == TransportTypeTest && env.Epoch != "" {
			s.observeTestEpoch(connName, env.Epoch)
		}
		return []*TransportEnvelope{env}
	}

	// Missing Epoch or Sequence: pass through with audit. This keeps
	// the receiver working against pre-Phase-1 senders during a staged
	// rollout. Epoch may be present without Sequence in theory but
	// never in practice for sync_msg; treat both as required.
	if env.Epoch == "" || env.Sequence == 0 {
		s.api.LogWarn("Inbound sync_msg missing Epoch or Sequence; sequencer bypassed",
			"error_code", errcode.InboundSeqMissingFields,
			"conn_name", connName, "epoch_present", env.Epoch != "",
			"sequence_present", env.Sequence != 0)
		return []*TransportEnvelope{env}
	}

	if env.SyncMsg == nil || env.SyncMsg.ChannelId == "" {
		// Shouldn't happen; defensive.
		return []*TransportEnvelope{env}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := seqKey{connName: connName, channelID: env.SyncMsg.ChannelId}
	state, ok := s.states[key]
	if !ok {
		state = s.initStateForChannel(connName, key, env.Epoch)
	}

	// Epoch change: dispatch any old-epoch buffered envelopes (they're
	// valid data, just from a previous sender session - the receiver
	// is content-keyed and doesn't care about epoch), then reset the
	// cursor for the new session. The receiver adopts the incoming
	// envelope's Sequence as the new baseline rather than insisting
	// the new epoch start at 1: the sender process is a fresh
	// conversation, and the receiver has no basis to predict what
	// starting seq it will use. Monotonicity within the new epoch is
	// what matters; the absolute starting value is opaque.
	var ready []*TransportEnvelope
	if env.Epoch != state.epoch {
		ready = s.handleEpochChange(connName, key, state, env.Epoch, env.Sequence)
	}

	// Duplicate of an already-dispatched seq: discard. The receiver is
	// idempotent so redispatching is safe, but it is wasted work; we
	// keep the discard as a pure performance optimization. (Duplicates
	// never entered the queue, so this is not a "remove from queue"
	// case where best-effort dispatch applies.)
	if env.Sequence < state.nextExpected {
		s.api.LogWarn("Inbound sync_msg duplicate suppressed",
			"error_code", errcode.InboundSeqDuplicate,
			"conn_name", connName, "channel_id", key.channelID,
			"epoch", env.Epoch, "seq", env.Sequence,
			"next_expected", state.nextExpected)
		return ready
	}

	// Out-of-order: buffer the envelope. When the buffer exceeds its
	// caps (count or bytes), bufferEnvelope returns the full buffer
	// drained in seq order rather than dropping anything: leaving the
	// buffer is always "best effort to dispatch" so the receiver can
	// salvage envelopes that may not actually depend on the missing
	// predecessors.
	if env.Sequence > state.nextExpected {
		if drained := s.bufferEnvelope(connName, key, state, env); len(drained) > 0 {
			return append(ready, drained...)
		}
		return ready
	}

	// In-order arrival: dispatch this envelope plus any contiguous
	// buffered envelopes that now follow it.
	ready = append(ready, env)
	state.nextExpected = env.Sequence + 1
	ready = append(ready, s.drainContiguous(state)...)

	// If we drained any buffered envelopes, log a gap-filled audit;
	// otherwise log a plain in-order at debug to avoid log spam.
	if len(ready) > 1 {
		s.api.LogInfo("Inbound sync_msg gap filled",
			"error_code", errcode.InboundSeqGapFilled,
			"conn_name", connName, "channel_id", key.channelID,
			"epoch", env.Epoch, "filled_count", len(ready),
			"new_next_expected", state.nextExpected)
	} else {
		s.api.LogDebug("Inbound sync_msg in order",
			"error_code", errcode.InboundSeqInOrder,
			"conn_name", connName, "channel_id", key.channelID,
			"epoch", env.Epoch, "seq", env.Sequence)
	}

	return ready
}

// drainContiguous pulls buffered envelopes starting at nextExpected
// and continuing while each successive seq is present, advancing the
// cursor as it goes. Caller holds s.mu.
func (s *inboundSequencer) drainContiguous(state *seqState) []*TransportEnvelope {
	var drained []*TransportEnvelope
	for {
		buffered, has := state.buffer[state.nextExpected]
		if !has {
			break
		}
		drained = append(drained, buffered.env)
		state.bufferSize -= buffered.size
		delete(state.buffer, state.nextExpected)
		state.nextExpected++
	}
	if len(state.buffer) == 0 {
		state.gapStart = time.Time{}
	}
	return drained
}

// initStateForChannel constructs a fresh seqState for a channel,
// preferring a checkpoint loaded via loadCursor (Phase 5) when one is
// available and matches the incoming epoch. A mismatched checkpoint
// epoch is overwritten with the incoming epoch; the cursor reset
// flows through the normal epoch-change logic on the next Admit step
// when the in-memory state.epoch differs.
func (s *inboundSequencer) initStateForChannel(connName string, key seqKey, incomingEpoch string) *seqState {
	state := &seqState{
		epoch:        incomingEpoch,
		nextExpected: 1,
		buffer:       make(map[uint64]*bufferedEnvelope),
	}
	if s.loadCursor != nil {
		if epoch, next, err := s.loadCursor(connName, key.channelID); err == nil && epoch != "" {
			state.epoch = epoch
			state.nextExpected = next
			s.api.LogInfo("Inbound sequencer cursor loaded from checkpoint",
				"error_code", errcode.InboundSeqCheckpointLoaded,
				"conn_name", connName, "channel_id", key.channelID,
				"epoch", epoch, "next_expected", next)
		} else if err != nil {
			s.api.LogWarn("Inbound sequencer cursor checkpoint load failed",
				"error_code", errcode.InboundSeqCheckpointLoadFailed,
				"conn_name", connName, "channel_id", key.channelID, "error", err.Error())
		}
	}
	s.states[key] = state
	s.api.LogInfo("Inbound sequencer initialized for channel",
		"error_code", errcode.InboundSeqInOrder,
		"conn_name", connName, "channel_id", key.channelID,
		"epoch", state.epoch, "next_expected", state.nextExpected)
	return state
}

// observeTestEpoch records the epoch seen on a test envelope and
// audits a restart event when the epoch differs from the last-known
// epoch for the connection. The state lives outside the per-channel
// sequencer because tests have no channel scope.
func (s *inboundSequencer) observeTestEpoch(connName, epoch string) {
	s.mu.Lock()
	prev, seen := s.testEps[connName]
	s.testEps[connName] = epoch
	s.mu.Unlock()

	if seen && prev != epoch {
		s.api.LogInfo("Inbound test envelope reports sender epoch change",
			"error_code", errcode.InboundSeqTestEpochChange,
			"conn_name", connName, "old_epoch", prev, "new_epoch", epoch)
		return
	}
	s.api.LogDebug("Inbound test envelope epoch observed",
		"error_code", errcode.InboundSeqTestEpochMatch,
		"conn_name", connName, "epoch", epoch)
}

// handleEpochChange resets the cursor when an envelope carrying a new
// epoch arrives. Any envelopes buffered for the old epoch are returned
// so the caller can dispatch them: the receiver is keyed by content
// ID, not by epoch, so old-epoch envelopes still apply and should not
// be dropped just because the sender restarted.
//
// The cursor is set to the incoming envelope's Sequence (not to 1):
// the sender's per-channel counter strategy across restarts is its own
// concern, and the receiver has no basis to predict what starting seq
// the new epoch will use. Subsequent envelopes within the new epoch
// must arrive monotonically from this baseline; the absolute starting
// value is opaque to the receiver and that is fine.
//
// Stale-epoch arrivals (env.Epoch < state.epoch by some ordering) are
// not detectable here without persisted epoch history; we treat any
// epoch change as forward progress and accept the new envelope.
func (s *inboundSequencer) handleEpochChange(connName string, key seqKey, state *seqState, newEpoch string, incomingSeq uint64) []*TransportEnvelope {
	var released []*TransportEnvelope
	if len(state.buffer) > 0 {
		seqs := make([]uint64, 0, len(state.buffer))
		for k := range state.buffer {
			seqs = append(seqs, k)
		}
		slices.Sort(seqs)

		s.api.LogWarn("Inbound sequencer epoch change; dispatching old-epoch buffered envelopes before reset",
			"error_code", errcode.InboundSeqEpochReset,
			"conn_name", connName, "channel_id", key.channelID,
			"old_epoch", state.epoch, "new_epoch", newEpoch,
			"released_count", len(seqs),
			"released_seq_min", seqs[0], "released_seq_max", seqs[len(seqs)-1])

		released = make([]*TransportEnvelope, 0, len(seqs))
		for _, k := range seqs {
			released = append(released, state.buffer[k].env)
		}
	}

	s.api.LogInfo("Inbound sequencer epoch reset",
		"error_code", errcode.InboundSeqEpochReset,
		"conn_name", connName, "channel_id", key.channelID,
		"old_epoch", state.epoch, "new_epoch", newEpoch,
		"new_baseline_seq", incomingSeq)

	state.epoch = newEpoch
	state.nextExpected = incomingSeq
	state.buffer = make(map[uint64]*bufferedEnvelope)
	state.bufferSize = 0
	state.gapStart = time.Time{}
	return released
}

// bufferEnvelope holds an out-of-order envelope. When the buffer
// exceeds its count or byte caps, returns the entire held buffer
// drained in seq order (giving up on the gap) so the receiver can
// best-effort process them. Returns an empty slice when the envelope
// simply queued without overflow.
//
// The "drain everything on overflow" semantic matches the time-based
// gap-timeout path in TickGapDeadlines: in both cases the sequencer
// has decided the gap will not fill and the right thing is to ship
// what we have rather than drop data.
func (s *inboundSequencer) bufferEnvelope(connName string, key seqKey, state *seqState, env *TransportEnvelope) []*TransportEnvelope {
	size := s.bytesPerEnvelope(env)

	// Duplicate buffered seq: discard.
	if _, dup := state.buffer[env.Sequence]; dup {
		s.api.LogWarn("Inbound sync_msg duplicate of buffered seq suppressed",
			"error_code", errcode.InboundSeqDuplicate,
			"conn_name", connName, "channel_id", key.channelID,
			"epoch", env.Epoch, "seq", env.Sequence)
		return nil
	}

	// First buffered envelope opens the gap timer.
	if len(state.buffer) == 0 {
		state.gapStart = time.Now()
		s.api.LogWarn("Inbound sync_msg gap detected",
			"error_code", errcode.InboundSeqGapDetected,
			"conn_name", connName, "channel_id", key.channelID,
			"epoch", env.Epoch, "next_expected", state.nextExpected,
			"received_seq", env.Sequence)
	}

	state.buffer[env.Sequence] = &bufferedEnvelope{env: env, size: size}
	state.bufferSize += size

	if len(state.buffer) > s.bufferMaxCount || state.bufferSize > s.bufferMaxBytes {
		cause := "count"
		if state.bufferSize > s.bufferMaxBytes && len(state.buffer) <= s.bufferMaxCount {
			cause = "bytes"
		}
		return s.drainBufferOnOverflow(connName, key, state, cause)
	}
	return nil
}

// drainBufferOnOverflow gives up on the current gap. Dispatches every
// held envelope in seq order, advances nextExpected past the highest
// held, and clears the buffer. Caller appends the returned slice into
// the Admit result so the receiver gets a chance to apply each one;
// per-entity failures (e.g., reaction without post) are reported in
// the receiver's SyncResponse and logged via joinSyncErrors, which is
// strictly no worse than dropping at the sequencer.
func (s *inboundSequencer) drainBufferOnOverflow(connName string, key seqKey, state *seqState, cause string) []*TransportEnvelope {
	seqs := make([]uint64, 0, len(state.buffer))
	for k := range state.buffer {
		seqs = append(seqs, k)
	}
	slices.Sort(seqs)

	missingCount := uint64(0)
	if seqs[0] > state.nextExpected {
		missingCount = seqs[0] - state.nextExpected
	}
	s.api.LogError("Inbound sequencer buffer overflow; dispatching held envelopes out of order",
		"error_code", errcode.InboundSeqBufferOverflow,
		"conn_name", connName, "channel_id", key.channelID,
		"epoch", state.epoch,
		"cause", cause,
		"missing_from", state.nextExpected, "missing_to", seqs[0]-1,
		"missing_count", missingCount,
		"dispatched_count", len(seqs))

	released := make([]*TransportEnvelope, 0, len(seqs))
	for _, k := range seqs {
		released = append(released, state.buffer[k].env)
	}
	state.buffer = make(map[uint64]*bufferedEnvelope)
	state.bufferSize = 0
	state.nextExpected = seqs[len(seqs)-1] + 1
	state.gapStart = time.Time{}
	return released
}

// TickGapDeadlines walks every per-channel state and advances past
// gaps whose deadline has expired. Returns the envelopes that should
// be dispatched (those that were buffered behind a now-abandoned
// gap). The caller dispatches them; the sequencer has already audited
// the lost seq range via InboundSeqGapTimeout.
//
// Intended to be called from a 1s ticker.
func (s *inboundSequencer) TickGapDeadlines(now time.Time) []*TransportEnvelope {
	var released []*TransportEnvelope

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, state := range s.states {
		if state.gapStart.IsZero() || now.Sub(state.gapStart) < s.gapTimeout {
			continue
		}
		// Deadline expired. Drain the buffered envelopes in sequence
		// order and emit one audit covering the lost range.
		seqs := make([]uint64, 0, len(state.buffer))
		for k := range state.buffer {
			seqs = append(seqs, k)
		}
		slices.Sort(seqs)

		s.api.LogError("Inbound sequencer gap timed out; advancing past missing seqs",
			"error_code", errcode.InboundSeqGapTimeout,
			"conn_name", key.connName, "channel_id", key.channelID,
			"epoch", state.epoch,
			"missing_from", state.nextExpected, "missing_to", seqs[0]-1,
			"missing_count", seqs[0]-state.nextExpected,
			"released_count", len(seqs))

		for _, k := range seqs {
			buffered := state.buffer[k]
			released = append(released, buffered.env)
			delete(state.buffer, k)
			state.bufferSize -= buffered.size
		}
		state.nextExpected = seqs[len(seqs)-1] + 1
		state.gapStart = time.Time{}
	}

	return released
}

// estimateEnvelopeBytes is the default byte estimator for buffered
// envelopes. It returns the marshaled XML size as a stable reference
// point regardless of the wire format the original envelope arrived
// in: the buffer-size accounting needs deterministic numbers across
// nodes and across format flips, and the XSD remains the authoritative
// shape contract. An error in marshaling falls back to a per-envelope
// overhead. The estimator is replaceable in tests via the
// bytesPerEnvelope field.
func estimateEnvelopeBytes(env *TransportEnvelope) int {
	data, err := MarshalEnvelope(env, FormatXML)
	if err != nil {
		return 1024
	}
	return len(data)
}

// sequencerCursorEntry is one (connName, channelID, epoch, nextExpected)
// row used by Snapshot/Restore so callers can persist and reload cursors
// without taking the sequencer lock externally.
type sequencerCursorEntry struct {
	ConnName     string
	ChannelID    string
	Epoch        string
	NextExpected uint64
}

// Snapshot returns the current cursors for every tracked channel.
// Buffered envelopes are intentionally not included: they live only in
// memory and are abandoned on leadership handoff (the new leader will
// re-receive them from the transport when the at-least-once provider
// redelivers, or audit-log the lost range under
// InboundSeqGapTimeout).
func (s *inboundSequencer) Snapshot() []sequencerCursorEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]sequencerCursorEntry, 0, len(s.states))
	for k, st := range s.states {
		out = append(out, sequencerCursorEntry{
			ConnName:     k.connName,
			ChannelID:    k.channelID,
			Epoch:        st.epoch,
			NextExpected: st.nextExpected,
		})
	}
	return out
}

// Restore installs an (epoch, nextExpected) cursor for the named
// channel without dispatching anything. Used on lease acquisition so
// the resuming node continues where the previous active node left
// off. Idempotent: calling with an existing key overwrites the
// in-memory state.
func (s *inboundSequencer) Restore(connName, channelID, epoch string, nextExpected uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[seqKey{connName: connName, channelID: channelID}] = &seqState{
		epoch:        epoch,
		nextExpected: nextExpected,
		buffer:       make(map[uint64]*bufferedEnvelope),
	}
	s.api.LogInfo("Inbound sequencer cursor restored from checkpoint",
		"error_code", errcode.InboundSeqCheckpointLoaded,
		"conn_name", connName, "channel_id", channelID,
		"epoch", epoch, "next_expected", nextExpected)
}
