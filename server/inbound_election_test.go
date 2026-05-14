package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// electorKVStore is a minimal in-memory KV double for the elector that
// records every Acquire/Release call and lets tests script the
// AcquireOrRenewInboundLease return values per call.
type electorKVStore struct {
	*testKVStore

	mu        sync.Mutex
	holder    string    // current lease holder; "" means unheld
	expiresAt time.Time // when the lease expires (only meaningful if holder != "")
	now       func() time.Time

	// scripted return values; when non-nil, override the natural CAS logic
	// on the next AcquireOrRenewInboundLease call. After consumption the
	// script is cleared.
	scriptedAcquireErr  error
	scriptedAcquireFunc func(nodeID string) (acquired, renewed bool, currentHolder string, err error)

	// instrumentation: counts of operations
	acquireCalls atomic.Int64
	renewCalls   atomic.Int64
	releaseCalls atomic.Int64
	lastTTL      time.Duration
}

func newElectorKVStore() *electorKVStore {
	return &electorKVStore{
		testKVStore: newTestKVStore(),
		now:         time.Now,
	}
}

func (s *electorKVStore) currentHolder() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holder != "" && s.now().After(s.expiresAt) {
		s.holder = ""
	}
	return s.holder
}

func (s *electorKVStore) setHolder(nodeID string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.holder = nodeID
	s.expiresAt = s.now().Add(ttl)
}

func (s *electorKVStore) AcquireOrRenewInboundLease(_, nodeID string, ttl time.Duration) (bool, bool, string, error) {
	s.mu.Lock()
	s.lastTTL = ttl
	scriptedFunc := s.scriptedAcquireFunc
	scriptedErr := s.scriptedAcquireErr
	s.scriptedAcquireFunc = nil
	s.scriptedAcquireErr = nil
	s.mu.Unlock()

	if scriptedFunc != nil {
		return scriptedFunc(nodeID)
	}
	if scriptedErr != nil {
		return false, false, "", scriptedErr
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Lapse expired holder.
	if s.holder != "" && s.now().After(s.expiresAt) {
		s.holder = ""
	}

	switch s.holder {
	case "":
		s.holder = nodeID
		s.expiresAt = s.now().Add(ttl)
		s.acquireCalls.Add(1)
		return true, false, "", nil
	case nodeID:
		s.expiresAt = s.now().Add(ttl)
		s.renewCalls.Add(1)
		return false, true, nodeID, nil
	default:
		return false, false, s.holder, nil
	}
}

func (s *electorKVStore) ReleaseInboundLease(_, nodeID string) error {
	s.releaseCalls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holder == nodeID {
		s.holder = ""
		s.expiresAt = time.Time{}
	}
	return nil
}

// electorProvider records Subscribe and Close calls for elector tests.
type electorProvider struct {
	mockQueueProvider

	mu         sync.Mutex
	subscribes atomic.Int64
	closes     atomic.Int64
	subCtx     context.Context
	handler    func([]byte) error
	subErr     error
}

func newElectorProvider() *electorProvider {
	p := &electorProvider{}
	p.subscribeFn = func(ctx context.Context, handler func([]byte) error) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.subscribes.Add(1)
		p.subCtx = ctx
		p.handler = handler
		return p.subErr
	}
	p.closeFn = func() error {
		p.closes.Add(1)
		return nil
	}
	return p
}

// newElectorTestPlugin builds a Plugin with the elector KV store and a
// permissive log API. Returns the plugin, the KV store, and a cleanup
// function the test must defer.
func newElectorTestPlugin(t *testing.T, nodeID string) (*Plugin, *electorKVStore, *plugintest.API, func()) {
	t.Helper()
	api := &plugintest.API{}
	stubLogs(api)
	api.On("PublishPluginClusterEvent", mock.Anything, mock.Anything).Return(nil).Maybe()

	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	p.nodeID = nodeID
	kvs := newElectorKVStore()
	p.kvstore = kvs
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	return p, kvs, api, func() { cancel() }
}

// runOneTick invokes tickOnce directly and waits for any resulting
// startSubscribe to finish setting state. Returns nothing; callers inspect
// the elector and KV state.
func runOneTick(t *testing.T, e *inboundElector) {
	t.Helper()
	e.tickOnce(context.Background())
}

func TestElectorAcquireOnEmptyLease(t *testing.T) {
	p, kvs, _, cleanup := newElectorTestPlugin(t, "node-A")
	defer cleanup()

	prov := newElectorProvider()
	e := newInboundElector(p, "conn1", prov)

	runOneTick(t, e)

	assert.Equal(t, "node-A", kvs.currentHolder(), "lease should be held by this node")
	assert.True(t, e.holding(), "elector should report itself active")
	assert.EqualValues(t, 1, prov.subscribes.Load(), "Subscribe called exactly once")
	assert.EqualValues(t, 0, prov.closes.Load(), "Close not called yet")
	assert.Equal(t, inboundLeaseTTL, kvs.lastTTL, "TTL should match the constant")
}

func TestElectorRenewWhenHolder(t *testing.T) {
	p, kvs, _, cleanup := newElectorTestPlugin(t, "node-A")
	defer cleanup()

	prov := newElectorProvider()
	e := newInboundElector(p, "conn1", prov)

	runOneTick(t, e) // acquire
	require.True(t, e.holding())

	prevSubs := prov.subscribes.Load()
	prevReleases := kvs.releaseCalls.Load()

	runOneTick(t, e) // renew
	runOneTick(t, e) // renew

	assert.Equal(t, "node-A", kvs.currentHolder(), "still holds lease")
	assert.True(t, e.holding())
	assert.Equal(t, prevSubs, prov.subscribes.Load(), "no additional Subscribe on renewals")
	assert.EqualValues(t, prevReleases, kvs.releaseCalls.Load(), "no Release on renew")
	assert.GreaterOrEqual(t, kvs.renewCalls.Load(), int64(2), "renew counter advanced")
}

func TestElectorObservesSiblingHolder(t *testing.T) {
	p, kvs, _, cleanup := newElectorTestPlugin(t, "node-A")
	defer cleanup()

	// Pre-seed the lease as held by sibling node-B.
	kvs.setHolder("node-B", inboundLeaseTTL)

	prov := newElectorProvider()
	e := newInboundElector(p, "conn1", prov)

	runOneTick(t, e)

	assert.Equal(t, "node-B", kvs.currentHolder(), "sibling still holds the lease")
	assert.False(t, e.holding(), "elector remains inactive")
	assert.EqualValues(t, 0, prov.subscribes.Load(), "no Subscribe while sibling holds it")
	assert.EqualValues(t, 0, prov.closes.Load(), "no Close either")
}

func TestElectorAcquiresAfterTTLExpiry(t *testing.T) {
	p, kvs, _, cleanup := newElectorTestPlugin(t, "node-A")
	defer cleanup()

	// Sibling node-B held the lease but it has expired (simulate by
	// rolling the now() function backward in time after seeding).
	kvs.setHolder("node-B", inboundLeaseTTL)
	kvs.mu.Lock()
	kvs.expiresAt = time.Now().Add(-time.Second) // already expired
	kvs.mu.Unlock()

	prov := newElectorProvider()
	e := newInboundElector(p, "conn1", prov)

	runOneTick(t, e)

	assert.Equal(t, "node-A", kvs.currentHolder(), "we should now hold the lease after sibling expiry")
	assert.True(t, e.holding(), "elector should be active")
	assert.EqualValues(t, 1, prov.subscribes.Load(), "Subscribe called once after acquire")
}

func TestElectorTwoNodeRace(t *testing.T) {
	// Two electors pointing at the same KV store race for an empty lease.
	// Only one should acquire; no flapping over 50 ticks.
	pA, kvs, _, cleanupA := newElectorTestPlugin(t, "node-A")
	defer cleanupA()

	// Reuse the same KV instance for the second node by overriding pB.kvstore.
	apiB := &plugintest.API{}
	stubLogs(apiB)
	apiB.On("PublishPluginClusterEvent", mock.Anything, mock.Anything).Return(nil).Maybe()
	pB := &Plugin{}
	pB.SetAPI(apiB)
	pB.botUserID = "bot-user-id"
	pB.nodeID = "node-B"
	pB.kvstore = kvs
	ctxB, cancelB := context.WithCancel(context.Background())
	pB.ctx = ctxB
	pB.cancel = cancelB
	defer cancelB()

	provA := newElectorProvider()
	provB := newElectorProvider()
	eA := newInboundElector(pA, "conn1", provA)
	eB := newInboundElector(pB, "conn1", provB)

	// First tick: both race. Whoever wins keeps the lease for the rest
	// of the test since neither expires within these ticks.
	for range 50 {
		runOneTick(t, eA)
		runOneTick(t, eB)
	}

	holder := kvs.currentHolder()
	require.Contains(t, []string{"node-A", "node-B"}, holder, "exactly one node should hold the lease")

	if holder == "node-A" {
		assert.True(t, eA.holding())
		assert.False(t, eB.holding())
		assert.EqualValues(t, 1, provA.subscribes.Load(), "winner subscribes exactly once, no flap")
		assert.EqualValues(t, 0, provB.subscribes.Load(), "loser never subscribes")
	} else {
		assert.True(t, eB.holding())
		assert.False(t, eA.holding())
		assert.EqualValues(t, 1, provB.subscribes.Load(), "winner subscribes exactly once, no flap")
		assert.EqualValues(t, 0, provA.subscribes.Load(), "loser never subscribes")
	}
}

func TestElectorStepsDownOnLost(t *testing.T) {
	p, kvs, _, cleanup := newElectorTestPlugin(t, "node-A")
	defer cleanup()

	prov := newElectorProvider()
	e := newInboundElector(p, "conn1", prov)

	runOneTick(t, e)
	require.True(t, e.holding())

	// Simulate a partition: KV now reports a different holder. (This can
	// happen if our network drops and a sibling's TTL acquire wins.)
	kvs.mu.Lock()
	kvs.holder = "node-B"
	kvs.expiresAt = time.Now().Add(inboundLeaseTTL)
	kvs.mu.Unlock()

	runOneTick(t, e)

	assert.False(t, e.holding(), "elector should have stepped down")
	assert.EqualValues(t, 1, prov.closes.Load(), "Close called on step down")
	assert.Equal(t, "node-B", kvs.currentHolder(), "sibling now holds it")
}

func TestElectorGracefulStepdownPoke(t *testing.T) {
	p, kvs, _, cleanup := newElectorTestPlugin(t, "node-A")
	defer cleanup()

	prov := newElectorProvider()
	e := newInboundElector(p, "conn1", prov)

	// Start the elector goroutine.
	done := make(chan struct{})
	go func() {
		e.run(p.ctx)
		close(done)
	}()

	// Wait for the immediate first tick to acquire.
	require.Eventually(t, e.holding, 2*time.Second, 5*time.Millisecond, "elector should acquire on first tick")
	require.EqualValues(t, 1, prov.subscribes.Load())

	// Cause a sibling takeover: change KV holder, then poke. The poke
	// fires an immediate tick that observes the new holder and steps
	// down without waiting for the 15s ticker.
	kvs.mu.Lock()
	kvs.holder = "node-B"
	kvs.expiresAt = time.Now().Add(inboundLeaseTTL)
	kvs.mu.Unlock()

	e.poke()
	require.Eventually(t, func() bool { return !e.holding() }, 2*time.Second, 5*time.Millisecond,
		"elector should step down after poke when sibling holds lease")
	assert.EqualValues(t, 1, prov.closes.Load(), "Close called exactly once on step-down")

	// Tear down.
	p.cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("elector goroutine did not exit")
	}
}

func TestElectorPublishStepdownOnShutdown(t *testing.T) {
	api := &plugintest.API{}
	stubLogs(api)

	// Strict expectation: PublishPluginClusterEvent must be called with the
	// step-down event id and a payload identifying this node.
	var capturedPayload []byte
	api.On("PublishPluginClusterEvent",
		mock.MatchedBy(func(ev model.PluginClusterEvent) bool {
			capturedPayload = ev.Data
			return ev.Id == clusterEventInboundStepdown
		}),
		mock.Anything,
	).Return(nil).Once()

	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	p.nodeID = "node-A"
	kvs := newElectorKVStore()
	p.kvstore = kvs
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	defer cancel()

	prov := newElectorProvider()
	e := newInboundElector(p, "conn-A", prov)

	// Acquire the lease so shutdown has something to release.
	runOneTick(t, e)
	require.True(t, e.holding())

	e.shutdown()

	api.AssertCalled(t, "PublishPluginClusterEvent",
		mock.MatchedBy(func(ev model.PluginClusterEvent) bool {
			return ev.Id == clusterEventInboundStepdown
		}),
		mock.Anything,
	)

	// Verify the payload identifies this connection and node.
	require.NotNil(t, capturedPayload)
	var ev stepdownEvent
	require.NoError(t, json.Unmarshal(capturedPayload, &ev))
	assert.Equal(t, "conn-A", ev.ConnName)
	assert.Equal(t, "node-A", ev.NodeID)

	// Lease released, provider closed.
	assert.Empty(t, kvs.currentHolder(), "lease should be released on shutdown")
	assert.EqualValues(t, 1, prov.closes.Load(), "Close called on shutdown")
	assert.EqualValues(t, 1, kvs.releaseCalls.Load(), "ReleaseInboundLease called once")
}

func TestElectorAcquireErrorPropagated(t *testing.T) {
	// Sanity test: KV errors don't crash the elector and don't change state.
	p, kvs, _, cleanup := newElectorTestPlugin(t, "node-A")
	defer cleanup()

	kvs.scriptedAcquireErr = errors.New("kv unreachable")

	prov := newElectorProvider()
	e := newInboundElector(p, "conn1", prov)

	runOneTick(t, e)

	assert.False(t, e.holding(), "no acquire when KV errors")
	assert.EqualValues(t, 0, prov.subscribes.Load(), "no subscribe on error")
}
