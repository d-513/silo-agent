package search

import (
	"sync"
	"time"
)

// A source that refuses (a block, a captcha, a rate limit) is left alone for
// backoffStart, doubling with every refusal in a row up to backoffMax. One
// that said "too many requests" is left alone for at least backoffLimited, or
// as long as it asked: every request to a rate-limited site tends to renew
// the limit.
//
// A source that merely stumbled (a timeout, a network error, a 500) says
// nothing about how the site sees us: it rests stumbleStart, doubling to
// stumbleMax, so a slow uplink or a blip does not shut search for half an
// hour.
const (
	backoffStart   = 30 * time.Second
	backoffLimited = 2 * time.Minute
	backoffMax     = 30 * time.Minute
	stumbleStart   = 15 * time.Second
	stumbleMax     = 2 * time.Minute
	// probeStale is when a probe that never reported is given up on, so a
	// lost one does not keep the source shut.
	probeStale = time.Minute
)

// breaker is the health of every source: one that failed is skipped until its
// rest has passed, so a blocked engine costs a search nothing. After the rest
// one search is the probe (enter); the others go on skipping it until the
// probe answers, so a burst does not all hit a site that just blocked us. An
// answer resets it.
type breaker struct {
	mu    sync.Mutex
	now   func() time.Time
	state map[string]*health
}

type health struct {
	// fails counts refusals in a row, soft the stumbles in a row.
	fails, soft int
	until       time.Time
	// probing is when the probe in flight started; zero when none is.
	probing time.Time
	// reason is why it last failed; failedAt and okAt are when it last
	// failed and last answered. They are for the operator's status page.
	reason         string
	failedAt, okAt time.Time
}

func (h *health) probeLive(now time.Time) bool {
	return !h.probing.IsZero() && now.Sub(h.probing) < probeStale
}

func newBreaker(now func() time.Time) *breaker {
	return &breaker{now: now, state: map[string]*health{}}
}

func (b *breaker) of(name string) *health {
	h := b.state[name]
	if h == nil {
		h = &health{}
		b.state[name] = h
	}
	return h
}

// open reports whether name must be skipped: inside its rest, or past it
// with a probe in flight.
func (b *breaker) open(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.state[name]
	now := b.now()
	return h != nil && (now.Before(h.until) || h.probeLive(now))
}

// enter is asking name now: true for a healthy source, or for the one search
// that becomes the probe of a source past its rest. The probe ends with ok,
// fail, stumble or release.
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
	if h.fails > 0 || h.soft > 0 {
		h.probing = now
	}
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

// fail records a refusal. atLeast is the shortest the source must be left
// alone, when the refusal itself says so.
func (b *breaker) fail(name string, atLeast time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.of(name)
	h.fails++
	h.probing = time.Time{}
	wait := backoffMax
	if h.fails <= 7 {
		wait = min(backoffStart<<(h.fails-1), backoffMax)
	}
	h.failedAt = b.now()
	h.until = h.failedAt.Add(min(max(wait, atLeast), backoffMax))
}

// stumble records a failure that is no refusal.
func (b *breaker) stumble(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.of(name)
	h.soft++
	h.probing = time.Time{}
	wait := stumbleMax
	if h.soft <= 4 {
		wait = min(stumbleStart<<(h.soft-1), stumbleMax)
	}
	h.failedAt = b.now()
	if until := h.failedAt.Add(wait); until.After(h.until) {
		h.until = until
	}
}

// blame notes why name last failed.
func (b *breaker) blame(name, reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.of(name).reason = reason
}

func (b *breaker) ok(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.of(name)
	*h = health{okAt: b.now(), reason: h.reason, failedAt: h.failedAt}
}

// look is a copy of name's health, for the status page.
func (b *breaker) look(name string) (h health, resting bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p := b.state[name]; p != nil {
		h = *p
	}
	return h, b.now().Before(h.until)
}
