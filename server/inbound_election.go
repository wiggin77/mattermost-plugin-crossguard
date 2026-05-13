package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

// Lease ticker cadence and TTL. Renewal happens at every tick from the
// holder; non-holders use the same tick to check the lease for vacancy.
// The 3x ratio between TTL and ticker leaves two missed renewals of
// headroom before handoff fires, so a single slow KV write never causes
// churn. See implementation-plans/26-05-13-01-envelope-sequence-and-epoch.md
// for the election design.
const (
	inboundLeaseTicker = 15 * time.Second
	inboundLeaseTTL    = 45 * time.Second
)

// Cluster event broadcast on graceful step-down to shorten failover.
// Receivers tick once immediately upon receipt, so a sibling can pick up
// the lease in sub-second time when the holder shuts down cleanly.
const clusterEventInboundStepdown = "active_inbound_stepdown"

// stepdownEvent is the JSON payload carried by clusterEventInboundStepdown.
type stepdownEvent struct {
	ConnName string `json:"conn_name"`
	NodeID   string `json:"node_id"`
}

// inboundElector runs a single goroutine per inbound connection. The
// goroutine wakes on a ticker (or on a cluster step-down event for the
// same connection) and runs the lease state machine. Subscribe and
// Unsubscribe are called exactly once per actual leadership transition.
type inboundElector struct {
	p        *Plugin
	connName string
	provider QueueProvider

	mu         sync.Mutex
	active     bool
	wakeNow    chan struct{} // buffered size 1; signals the goroutine to tick immediately
	stopped    chan struct{}
	cancelSub  context.CancelFunc
	subscribed bool
}

// newInboundElector constructs an elector but does not start its goroutine.
// Call run() in a new goroutine to begin the lease loop.
func newInboundElector(p *Plugin, connName string, provider QueueProvider) *inboundElector {
	return &inboundElector{
		p:        p,
		connName: connName,
		provider: provider,
		wakeNow:  make(chan struct{}, 1),
		stopped:  make(chan struct{}),
	}
}

// run is the elector's main loop. It runs until ctx is cancelled or stop()
// is called. On exit it releases the lease (if held) and unsubscribes.
func (e *inboundElector) run(ctx context.Context) {
	defer close(e.stopped)
	// Try once immediately so a freshly-activated plugin can subscribe
	// without waiting one full ticker interval.
	e.tickOnce(ctx)

	ticker := time.NewTicker(inboundLeaseTicker)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.shutdown()
			return
		case <-e.wakeNow:
			e.tickOnce(ctx)
		case <-ticker.C:
			e.tickOnce(ctx)
		}
	}
}

// tickOnce runs one iteration of the lease state machine. The outcomes
// (acquire, renew, lose, observe-sibling-holder) are encoded in the
// AcquireOrRenewInboundLease return values; the elector translates them
// into Subscribe / Unsubscribe transitions.
func (e *inboundElector) tickOnce(ctx context.Context) {
	acquired, renewed, holder, err := e.p.kvstore.AcquireOrRenewInboundLease(
		e.connName, e.p.nodeID, inboundLeaseTTL,
	)
	if err != nil {
		// CAS failures are common when racing siblings; only the holder
		// goes from "I have it" to "lost it" via the renewed=false branch
		// below. A plain error means the KV is unreachable. Log at warn
		// and try again next tick.
		if e.holding() {
			e.p.API.LogWarn("Inbound lease renewal failed",
				"error_code", errcode.InboundActiveLeaseRenewalFailed,
				"conn_name", e.connName, "error", err.Error())
		} else {
			e.p.API.LogWarn("Inbound lease acquire failed",
				"error_code", errcode.InboundActiveLeaseAcquireFailed,
				"conn_name", e.connName, "error", err.Error())
		}
		return
	}

	switch {
	case acquired:
		e.p.API.LogInfo("Inbound active-node elected",
			"error_code", errcode.InboundActiveNodeElected,
			"conn_name", e.connName, "node_id", e.p.nodeID)
		e.startSubscribe(ctx)
	case renewed:
		// Steady state. Already subscribed. No transition.
	case holder == "":
		// Empty current holder with neither acquired nor renewed means a
		// CAS race resolved in someone else's favor in the same tick.
		// Try again next tick.
	default:
		// Sibling holds the lease. If we were previously holding it
		// (e.g., partition + sibling took over), step down.
		if e.holding() {
			e.p.API.LogWarn("Inbound lease lost; another node holds it",
				"error_code", errcode.InboundActiveLeaseLost,
				"conn_name", e.connName, "new_holder", holder)
			e.stopSubscribe()
		}
	}
}

// startSubscribe transitions from non-holder to holder. Idempotent: a
// second call while already subscribed is a no-op.
func (e *inboundElector) startSubscribe(parentCtx context.Context) {
	e.mu.Lock()
	if e.subscribed {
		e.mu.Unlock()
		return
	}
	subCtx, cancel := context.WithCancel(parentCtx)
	e.cancelSub = cancel
	e.subscribed = true
	e.active = true
	e.mu.Unlock()

	handler := e.p.handleInboundMessage(e.connName)
	if err := e.provider.Subscribe(subCtx, handler); err != nil {
		e.p.API.LogError("Failed to subscribe inbound after acquiring lease",
			"error_code", errcode.InboundActiveSubscribeFailed,
			"conn_name", e.connName, "error", err.Error())
		// Roll back the subscribed flag so the next tick can retry. Do
		// not release the lease: we still hold it in KV until TTL
		// expires, and retrying Subscribe is the right thing.
		e.mu.Lock()
		e.subscribed = false
		e.active = false
		e.cancelSub = nil
		e.mu.Unlock()
		cancel()
		return
	}
}

// stopSubscribe transitions from holder to non-holder. Cancels the
// provider's subscription context and clears state. Idempotent.
func (e *inboundElector) stopSubscribe() {
	e.mu.Lock()
	if !e.subscribed {
		e.mu.Unlock()
		return
	}
	cancel := e.cancelSub
	e.cancelSub = nil
	e.subscribed = false
	e.active = false
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if err := e.provider.Close(); err != nil {
		e.p.API.LogWarn("Provider Close after losing lease returned error",
			"error_code", errcode.InboundActiveUnsubscribed,
			"conn_name", e.connName, "error", err.Error())
	} else {
		e.p.API.LogInfo("Inbound active-node stepped down",
			"error_code", errcode.InboundActiveNodeSteppedDown,
			"conn_name", e.connName, "node_id", e.p.nodeID)
	}
}

func (e *inboundElector) holding() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.active
}

// shutdown is called when the elector's context is cancelled. Releases
// the lease and unsubscribes if currently holding.
func (e *inboundElector) shutdown() {
	if e.holding() {
		// Broadcast a step-down event so siblings can acquire immediately
		// rather than waiting for the TTL. Best effort: lossy delivery
		// is acceptable since the TTL is the backstop.
		e.publishStepdown()
		if err := e.p.kvstore.ReleaseInboundLease(e.connName, e.p.nodeID); err != nil {
			e.p.API.LogWarn("Failed to release inbound lease on shutdown",
				"error_code", errcode.InboundActiveLeaseRenewalFailed,
				"conn_name", e.connName, "error", err.Error())
		}
		e.stopSubscribe()
	}
}

// publishStepdown announces a graceful step-down to peers. Failure is
// non-fatal; the TTL will eventually clear the lease either way.
func (e *inboundElector) publishStepdown() {
	payload, err := json.Marshal(stepdownEvent{
		ConnName: e.connName,
		NodeID:   e.p.nodeID,
	})
	if err != nil {
		// Marshaling a fixed-shape struct cannot fail in practice; if it
		// somehow does we just skip the broadcast.
		return
	}
	if err := e.p.API.PublishPluginClusterEvent(
		model.PluginClusterEvent{Id: clusterEventInboundStepdown, Data: payload},
		model.PluginClusterEventSendOptions{SendType: model.PluginClusterEventSendTypeReliable},
	); err != nil {
		e.p.API.LogWarn("Failed to publish inbound step-down cluster event",
			"error_code", errcode.InboundActiveStepdownEventPubErr,
			"conn_name", e.connName, "error", err.Error())
	}
}

// poke wakes the elector for an immediate tick. Used by the cluster-event
// handler so siblings react in sub-second time on graceful step-down
// rather than waiting for the next 15s tick.
func (e *inboundElector) poke() {
	select {
	case e.wakeNow <- struct{}{}:
	default:
		// Already pending wake; coalesce.
	}
}

// handleStepdownEvent is called from OnPluginClusterEvent. It looks up
// the matching elector by connName and pokes it.
func (p *Plugin) handleStepdownEvent(payload []byte) {
	var ev stepdownEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		// Don't log noisily on malformed events from a different plugin
		// version; the event channel is shared.
		return
	}
	if ev.NodeID == p.nodeID {
		// Self-broadcast (in some Mattermost versions a node receives its
		// own cluster events). Ignore.
		return
	}
	p.API.LogDebug("Received inbound step-down event from peer",
		"error_code", errcode.InboundActiveStepdownEventRecv,
		"conn_name", ev.ConnName, "from_node", ev.NodeID)

	p.inboundMu.RLock()
	for i := range p.inboundConns {
		if p.inboundConns[i].name == ev.ConnName && p.inboundConns[i].elector != nil {
			p.inboundConns[i].elector.poke()
			break
		}
	}
	p.inboundMu.RUnlock()
}
