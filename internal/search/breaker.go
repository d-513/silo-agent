package search

import (
	"sync"
	"time"
)

// A source that blocks or fails is left alone for backoffStart, doubling with
// every failure in a row up to backoffMax. One that said "too many requests"
// is left alone for at least backoffLimited, or as long as it asked: every
// request to a rate-limited site tends to renew the limit.
const (
	backoffStart   = 30 * time.Second
	backoffLimited = 2 * time.Minute
	backoffMax     = 30 * time.Minute
	// probeStale is when a probe that never reported is given up on, so a
	// lost one does not keep the source shut.
	probeStale = time.Minute
)

// breaker is the health of every source: one that failed is skipped until its
// backoff has passed, so a blocked engine costs a search nothing. After the
// backoff one search is the probe (enter); the others go on skipping it until
// the probe answers, so a burst does not all hit a site that just blocked us.
// An answer resets it.
type breaker struct {
	mu    sync.Mutex
	now   func() time.Time
	state map[string]*health
}

type health struct {
	fails int
	until time.Time
	// probing is when the probe in flight started; zero when none is.
	probing time.Time
}

func (h *health) probeLive(now time.Time) bool {
	return !h.probing.IsZero() && now.Sub(h.probing) < probeStale
}

func newBreaker(now func() time.Time) *breaker {
	return &breaker{now: now, state: map[string]*health{}}
}

// open reports whether name must be skipped: inside its backoff, or past it
// with a probe in flight.
func (b *breaker) open(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.state[name]
	now := b.now()
	return h != nil && (now.Before(h.until) || h.probeLive(now))
}

// enter is asking name now: true for a healthy source, or for the one search
// that becomes the probe of a source past its backoff. The probe ends with
// ok, fail or release.
func (b *breaker) enter(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.state[name]
	if h == nil {
		return true
	}
	now := b.now()
	if now.Before(h.until) || h.probeLive(now) {
		return false
	}
	h.probing = now
	return true
}

// release ends a probe that got no verdict (the search ended first), so the
// next search may probe.
func (b *breaker) release(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if h := b.state[name]; h != nil {
		h.probing = time.Time{}
	}
}

// fail records a failure. atLeast is the shortest the source must be left
// alone, when the failure itself says so.
func (b *breaker) fail(name string, atLeast time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.state[name]
	if h == nil {
		h = &health{}
		b.state[name] = h
	}
	h.fails++
	h.probing = time.Time{}
	wait := backoffMax
	if h.fails <= 7 {
		wait = min(backoffStart<<(h.fails-1), backoffMax)
	}
	h.until = b.now().Add(min(max(wait, atLeast), backoffMax))
}

func (b *breaker) ok(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, name)
}
