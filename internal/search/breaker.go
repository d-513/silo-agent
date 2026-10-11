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
)

// breaker is the health of every source: one that failed is skipped until its
// backoff has passed, so a blocked engine costs a search nothing. The first
// search after the backoff is the probe; an answer resets it.
type breaker struct {
	mu    sync.Mutex
	now   func() time.Time
	state map[string]*health
}

type health struct {
	fails int
	until time.Time
}

func newBreaker(now func() time.Time) *breaker {
	return &breaker{now: now, state: map[string]*health{}}
}

// open reports whether name is inside its backoff and must be skipped.
func (b *breaker) open(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.state[name]
	return h != nil && b.now().Before(h.until)
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
