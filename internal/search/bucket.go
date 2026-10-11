package search

import (
	"sync"
	"time"
)

// limit is how often a site may be asked: one request every `every`, and up
// to `burst` at once after a quiet spell.
type limit struct {
	every time.Duration
	burst int
}

// bucket is a site's token bucket. The Control Plane is one address to a
// search site, whatever number of Bots it serves, so the budget is per site
// and shared by every search.
type bucket struct {
	mu     sync.Mutex
	now    func() time.Time
	every  time.Duration
	burst  float64
	tokens float64
	last   time.Time
}

func newBucket(now func() time.Time, l limit) *bucket {
	return &bucket{now: now, every: l.every, burst: float64(l.burst), tokens: float64(l.burst), last: now()}
}

func (b *bucket) refill() {
	now := b.now()
	if el := now.Sub(b.last); el > 0 {
		b.tokens = min(b.burst, b.tokens+float64(el)/float64(b.every))
	}
	b.last = now
}

// level is how many requests the site could take right now. Searches ask the
// fullest sites first, which is what spreads them over the sites.
func (b *bucket) level() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	return b.tokens
}

// take reserves the site's next turn and answers how long until it comes. It
// refuses when that is further off than maxWait: the search goes on without
// the site rather than queue for it.
func (b *bucket) take(maxWait time.Duration) (wait time.Duration, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	if b.tokens < 1 {
		wait = time.Duration((1 - b.tokens) * float64(b.every))
		if wait > maxWait {
			return 0, false
		}
	}
	b.tokens--
	return wait, true
}

// giveBack returns a turn that was reserved and never used.
func (b *bucket) giveBack() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens = min(b.burst, b.tokens+1)
}
