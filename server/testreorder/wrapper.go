// Package testreorder provides a QueueProvider wrapper that injects
// deterministic reordering and loss faults for integration tests. The
// wrapper is for test use only and lives in its own package so the
// production build never imports it. It exists to drive the per-transport
// regression tests described in
// implementation-plans/26-05-13-03-test-coverage-followups.md.
//
// The wrapper implements the QueueProvider interface defined in the
// parent package by satisfying the same method set. Tests construct it
// with an underlying real provider (e.g. an embedded NATS provider) and
// a fault Config; the wrapper delegates everything except Publish to the
// underlying provider, and intercepts Publish to inject the configured
// fault.
package testreorder

import (
	"context"
	"math/rand"
	"sync"
)

// Mode names the fault injection strategy.
type Mode int

const (
	// ModePassthrough delivers every Publish to the wrapped provider
	// without alteration. Useful as a "no-fault" baseline.
	ModePassthrough Mode = iota

	// ModeSwapAdjacent buffers one publish at a time and, with
	// probability P, swaps the new publish with the previously
	// buffered one. The net effect is adjacent-pair reordering: the
	// receiver sees envelopes (1, 2, 3) arrive as (1, 3, 2) or
	// (2, 1, 3) with rate P each.
	ModeSwapAdjacent

	// ModeDelayOneInN holds every Nth publish for K Flush() calls
	// before releasing. Caller drives the clock by invoking Flush()
	// between publishes (typically once per test step).
	ModeDelayOneInN

	// ModeLoss drops with probability P. No buffer; the publish
	// returns nil but never reaches the wrapped provider.
	ModeLoss
)

// Provider is the minimal QueueProvider surface the wrapper exposes.
// Tests cast the wrapper to whatever QueueProvider interface they need;
// since the wrapper has the same method set as the real providers, the
// type assertion succeeds. Defined here to avoid an import cycle with
// the parent package.
type Provider interface {
	Publish(ctx context.Context, data []byte) error
	Subscribe(ctx context.Context, handler func(data []byte) error) error
	UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error
	WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error
	MaxMessageSize() int
	IsConnected() bool
	Close() error
}

// Config configures the fault injection. Probability fields are in the
// range [0.0, 1.0]. EveryN and HoldFor are used only by ModeDelayOneInN.
// Seed makes runs deterministic; tests should fix it (e.g., 0xC0FFEE).
type Config struct {
	Mode Mode

	// Probability of injecting the fault on a given Publish. Applies
	// to ModeSwapAdjacent and ModeLoss. Zero means never; one means
	// always.
	Probability float64

	// EveryN: hold the Nth publish (1-indexed counter). Applies only
	// to ModeDelayOneInN. Zero is treated as 1 (every publish held).
	EveryN int

	// HoldFor: how many Flush() calls each held publish must endure
	// before release. Applies only to ModeDelayOneInN.
	HoldFor int

	// Seed for deterministic faults. Zero seeds time-based.
	Seed int64
}

// Wrapper wraps a Provider and injects publishing faults per Config.
// Methods are goroutine-safe.
type Wrapper struct {
	inner Provider
	cfg   Config

	mu    sync.Mutex
	rng   *rand.Rand
	count int // publish counter for ModeDelayOneInN

	// swapBuf holds the previously-publish-deferred payload for
	// ModeSwapAdjacent. nil when no payload is pending swap.
	swapBuf []byte

	// delayed holds publishes deferred for ModeDelayOneInN, each with
	// the remaining Flush count.
	delayed []*delayedPublish
}

type delayedPublish struct {
	ctx       context.Context
	payload   []byte
	remaining int
}

// New constructs a Wrapper around inner. The returned value satisfies
// the same QueueProvider method set as the underlying provider.
func New(inner Provider, cfg Config) *Wrapper {
	src := rand.NewSource(cfg.Seed)
	if cfg.Seed == 0 {
		src = rand.NewSource(0xC0FFEE)
	}
	return &Wrapper{
		inner: inner,
		cfg:   cfg,
		rng:   rand.New(src), //nolint:gosec // fault injection wrapper for tests only
	}
}

// Publish either delivers to the underlying provider, holds in the
// swap buffer / delay queue, or drops, depending on the configured Mode.
// Always returns nil unless the underlying Publish returns an error.
func (w *Wrapper) Publish(ctx context.Context, data []byte) error {
	switch w.cfg.Mode {
	case ModePassthrough:
		return w.inner.Publish(ctx, data)
	case ModeLoss:
		w.mu.Lock()
		drop := w.rng.Float64() < w.cfg.Probability
		w.mu.Unlock()
		if drop {
			return nil
		}
		return w.inner.Publish(ctx, data)
	case ModeSwapAdjacent:
		return w.publishSwapAdjacent(ctx, data)
	case ModeDelayOneInN:
		return w.publishDelayOneInN(ctx, data)
	}
	return w.inner.Publish(ctx, data)
}

func (w *Wrapper) publishSwapAdjacent(ctx context.Context, data []byte) error {
	w.mu.Lock()
	swap := w.rng.Float64() < w.cfg.Probability
	pending := w.swapBuf
	// We always release the pending one (FIFO) after either swapping
	// with the new one or appending the new one after it.
	if pending == nil {
		// Nothing buffered. If we want to swap with the *next* publish,
		// hold this one back; otherwise pass through.
		if swap {
			w.swapBuf = data
			w.mu.Unlock()
			return nil
		}
		w.mu.Unlock()
		return w.inner.Publish(ctx, data)
	}
	// Something already buffered. Either swap order (publish new
	// before pending) or pass through (publish pending then new).
	w.swapBuf = nil
	w.mu.Unlock()
	if swap {
		if err := w.inner.Publish(ctx, data); err != nil {
			// On error, do not lose the pending payload; try to flush
			// it too. The error from new is the one we surface.
			_ = w.inner.Publish(ctx, pending)
			return err
		}
		return w.inner.Publish(ctx, pending)
	}
	if err := w.inner.Publish(ctx, pending); err != nil {
		_ = w.inner.Publish(ctx, data)
		return err
	}
	return w.inner.Publish(ctx, data)
}

func (w *Wrapper) publishDelayOneInN(ctx context.Context, data []byte) error {
	everyN := w.cfg.EveryN
	if everyN <= 0 {
		everyN = 1
	}
	holdFor := w.cfg.HoldFor
	if holdFor <= 0 {
		holdFor = 1
	}
	w.mu.Lock()
	w.count++
	shouldHold := w.count%everyN == 0
	if shouldHold {
		w.delayed = append(w.delayed, &delayedPublish{
			ctx:       ctx,
			payload:   data,
			remaining: holdFor,
		})
		w.mu.Unlock()
		return nil
	}
	w.mu.Unlock()
	return w.inner.Publish(ctx, data)
}

// Flush advances the virtual clock by one tick. Held publishes with
// remaining == 0 are released to the underlying provider; the held
// queue is otherwise decremented. Returns the number of publishes
// released. Errors during release are silently swallowed since the
// transport-side test harness has no way to surface them to the
// caller; use Probe() if a test needs to observe held-queue state.
func (w *Wrapper) Flush() int {
	w.mu.Lock()
	released := make([]*delayedPublish, 0)
	remaining := w.delayed[:0]
	for _, d := range w.delayed {
		d.remaining--
		if d.remaining <= 0 {
			released = append(released, d)
		} else {
			remaining = append(remaining, d)
		}
	}
	w.delayed = remaining
	// Also flush any pending swap-adjacent buffer so tests can drain at
	// end-of-stream rather than leak the held envelope.
	swapPending := w.swapBuf
	w.swapBuf = nil
	w.mu.Unlock()

	count := 0
	for _, d := range released {
		_ = w.inner.Publish(d.ctx, d.payload)
		count++
	}
	if swapPending != nil {
		_ = w.inner.Publish(context.Background(), swapPending)
		count++
	}
	return count
}

// HeldCount reports the number of payloads currently held back. Useful
// for assertions in tests.
func (w *Wrapper) HeldCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(w.delayed)
	if w.swapBuf != nil {
		n++
	}
	return n
}

// Subscribe delegates to the underlying provider.
func (w *Wrapper) Subscribe(ctx context.Context, handler func(data []byte) error) error {
	return w.inner.Subscribe(ctx, handler)
}

// UploadFile delegates to the underlying provider.
func (w *Wrapper) UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error {
	return w.inner.UploadFile(ctx, key, data, headers)
}

// WatchFiles delegates to the underlying provider.
func (w *Wrapper) WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error {
	return w.inner.WatchFiles(ctx, handler)
}

// MaxMessageSize delegates to the underlying provider.
func (w *Wrapper) MaxMessageSize() int { return w.inner.MaxMessageSize() }

// IsConnected delegates to the underlying provider.
func (w *Wrapper) IsConnected() bool { return w.inner.IsConnected() }

// Close flushes any pending held-back publishes and delegates Close to
// the underlying provider. The flush ensures tests do not see surprise
// "missing publishes" from leftover delays at teardown.
func (w *Wrapper) Close() error {
	w.Flush()
	return w.inner.Close()
}
