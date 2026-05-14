package main

import (
	"context"
	"sync"
	"testing"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/testreorder"
)

// senderProcess simulates the sender side of the relay: it carries an
// epoch and a per-(connName, channelID) seq counter that resets on
// Restart, mirroring what Plugin.OnActivate does in production. It
// publishes via a testreorder.Wrapper, so callers can inject reorder /
// loss without touching the production transport.
//
// Sender lives in this file so each integration test stays focused on
// the failure mode it asserts.
type senderProcess struct {
	connName string
	epoch    string

	seqMu sync.Mutex
	seqs  map[string]uint64 // (connName + "\x00" + channelID) -> next seq

	wrapped *testreorder.Wrapper
}

func newSenderProcess(connName string, wrapped *testreorder.Wrapper) *senderProcess {
	return &senderProcess{
		connName: connName,
		epoch:    mmModel.NewId(),
		seqs:     make(map[string]uint64),
		wrapped:  wrapped,
	}
}

// Restart simulates a sender Plugin going through OnActivate again:
// new epoch, counter wiped. Used by the sender-restart integration
// test to reproduce the production poc1/poc2 stall.
func (s *senderProcess) Restart() {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	s.epoch = mmModel.NewId()
	s.seqs = make(map[string]uint64)
}

// SendSyncMsg fans out msg into post + metadata envelopes (via the
// production buildOutboundEnvelopes), stamps each with epoch +
// per-(connName, channelID) seq, and publishes through the wrapper.
// Returns the envelopes that were sent, in order, so the test can
// reason about exact bytes-on-the-wire.
func (s *senderProcess) SendSyncMsg(t *testing.T, channelID string, msg *mmModel.SyncMsg) []*TransportEnvelope {
	t.Helper()
	if msg.ChannelId == "" {
		msg.ChannelId = channelID
	}
	template := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeSyncMsg,
		ConnName:    s.connName,
		Epoch:       s.epoch,
		TeamName:    "team-test",
		ChannelName: "channel-test",
	}
	envs := buildOutboundEnvelopes(template, msg)
	for _, env := range envs {
		key := s.connName + "\x00" + channelID
		s.seqMu.Lock()
		s.seqs[key]++
		env.Sequence = s.seqs[key]
		env.Epoch = s.epoch
		s.seqMu.Unlock()
		data, err := MarshalEnvelope(env)
		require.NoError(t, err)
		require.NoError(t, s.wrapped.Publish(context.Background(), data))
	}
	return envs
}

// receiverProcess holds the inbound sequencer + a dispatched-envelope
// log. Used by integration tests to assert that every sent envelope
// reaches the dispatch step despite reorder / loss in transit.
type receiverProcess struct {
	connName string
	seq      *inboundSequencer
	api      *plugintest.API

	mu         sync.Mutex
	dispatched []*TransportEnvelope
}

func newReceiverProcess(connName string, gapTimeout time.Duration) *receiverProcess {
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
	rp := &receiverProcess{
		connName: connName,
		seq:      newInboundSequencer(api, gapTimeout, 1000, 50*1024*1024),
		api:      api,
	}
	rp.seq.bytesPerEnvelope = func(*TransportEnvelope) int { return 100 }
	return rp
}

// handle is the NATS callback installed by Subscribe(). It unmarshals
// the envelope, runs it through Admit, and records dispatched output.
func (r *receiverProcess) handle(data []byte) {
	env, err := UnmarshalEnvelope(data)
	if err != nil {
		// Malformed envelope; in production this would be audited as
		// InboundUnmarshalFailed. For the integration tests we just
		// drop it; the assertion downstream will catch the resulting
		// missing dispatch.
		return
	}
	ready := r.seq.Admit(r.connName, env)
	r.mu.Lock()
	r.dispatched = append(r.dispatched, ready...)
	r.mu.Unlock()
}

// Snapshot returns a copy of dispatched envelopes in arrival order.
func (r *receiverProcess) Snapshot() []*TransportEnvelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*TransportEnvelope, len(r.dispatched))
	copy(out, r.dispatched)
	return out
}

// DrainGapTimeouts forces any held envelopes past their gap deadline
// out of the sequencer. Returns the count released for assertions.
func (r *receiverProcess) DrainGapTimeouts() int {
	released := r.seq.TickGapDeadlines(time.Now().Add(time.Hour))
	r.mu.Lock()
	r.dispatched = append(r.dispatched, released...)
	r.mu.Unlock()
	return len(released)
}

// integrationHarness ties an embedded NATS server, a wrapped sender
// provider, and a receiver process together. Tests construct one,
// drive sender publishes, then assert on the receiver's dispatched
// log. The harness exposes Drain() so tests can flush any held-back
// payloads in the wrapper or gap-timeout pending in the sequencer at
// teardown.
type integrationHarness struct {
	addr     string
	subject  string
	connName string

	innerSenderConn   *nats.Conn
	innerReceiverConn *nats.Conn
	senderProvider    *natsProvider
	wrapped           *testreorder.Wrapper
	sender            *senderProcess
	receiver          *receiverProcess
}

// newIntegrationHarness spins up an embedded NATS server and wires
// a sender → wrapper → receiver pipeline on the given subject and
// connection name. wrapperCfg controls the fault injection (use
// ModePassthrough for no-fault baseline runs).
func newIntegrationHarness(t *testing.T, subject, connName string, wrapperCfg testreorder.Config, gapTimeout time.Duration) *integrationHarness {
	t.Helper()
	addr := startEmbeddedNATS(t)

	// Sender side: a dedicated nats.Conn → natsProvider for publishing.
	senderConn, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	t.Cleanup(func() { senderConn.Close() })

	senderProv := &natsProvider{nc: senderConn, subject: subject}
	wrapped := testreorder.New(senderProv, wrapperCfg)

	// Receiver side: a dedicated nats.Conn that subscribes to the same
	// subject. We avoid the natsProvider Subscribe path because it
	// returns synchronously after installing the callback; using
	// nc.Subscribe directly gives us a tighter shutdown.
	recvConn, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	require.NoError(t, err)
	t.Cleanup(func() { recvConn.Close() })

	receiver := newReceiverProcess(connName, gapTimeout)
	sub, err := recvConn.Subscribe(subject, func(msg *nats.Msg) {
		receiver.handle(msg.Data)
	})
	require.NoError(t, err)
	// Flush ensures the SUB protocol message has reached the server
	// before we start publishing. Without this, a fast publisher races
	// the subscription registration and the receiver misses early
	// messages.
	require.NoError(t, recvConn.Flush())
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	sender := newSenderProcess(connName, wrapped)
	return &integrationHarness{
		addr:              addr,
		subject:           subject,
		connName:          connName,
		innerSenderConn:   senderConn,
		innerReceiverConn: recvConn,
		senderProvider:    senderProv,
		wrapped:           wrapped,
		sender:            sender,
		receiver:          receiver,
	}
}

// WaitForDispatchCount blocks until the receiver has dispatched at
// least `want` envelopes, or fails the test after timeout.
func (h *integrationHarness) WaitForDispatchCount(t *testing.T, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(h.receiver.Snapshot()) >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d dispatched envelopes; got %d",
		want, len(h.receiver.Snapshot()))
}

// makePost returns a single-post SyncMsg keyed by id. The author is
// always "u-author" with a User row included so post envelopes inline
// the author per buildOutboundEnvelopes.
func makePost(id string, body string) *mmModel.SyncMsg {
	return &mmModel.SyncMsg{
		Id:        "sm-" + id,
		ChannelId: "ch-test",
		Users: map[string]*mmModel.User{
			"u-author": {Id: "u-author", Username: "alice"},
		},
		Posts: []*mmModel.Post{
			{Id: id, UserId: "u-author", Message: body},
		},
	}
}

// makePostWithChannel makes a single-post SyncMsg on a specific channel.
func makePostWithChannel(id, channel, body string) *mmModel.SyncMsg {
	m := makePost(id, body)
	m.ChannelId = channel
	m.Posts[0].ChannelId = channel
	return m
}

// extractPostIDs walks dispatched envelopes and returns the set of post
// IDs that arrived. Metadata-only envelopes are skipped.
func extractPostIDs(envs []*TransportEnvelope) map[string]struct{} {
	out := make(map[string]struct{})
	for _, env := range envs {
		if env.SyncMsg == nil || env.SyncMsg.Post == nil {
			continue
		}
		out[env.SyncMsg.Post.Id] = struct{}{}
	}
	return out
}
