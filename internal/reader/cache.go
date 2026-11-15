package reader

import (
	"sync"
	"time"
)

// A page read once is kept for cacheTTL: a model pages through a long
// article, asks it a second question, or reads what another Bot just read,
// and none of that should cost the site another request.
const (
	cacheTTL = time.Hour
	cacheMax = 300
	// cacheDocMax is the longest text kept; a longer page is read again.
	cacheDocMax = 2 << 20
)

type cache struct {
	mu      sync.Mutex
	now     func() time.Time
	items   map[string]cached
	flights map[string]*flight
}

type cached struct {
	doc *doc
	at  time.Time
}

// flight is a read in progress that later callers of the same key wait for.
type flight struct {
	done chan struct{}
	doc  *doc
	err  error
}

func newCache(now func() time.Time) *cache {
	return &cache{now: now, items: map[string]cached{}, flights: map[string]*flight{}}
}

// do answers key from the cache (unless fresh), joins a read of it that is
// already running, or runs fn.
func (c *cache) do(key string, fresh bool, fn func() (*doc, error)) (*doc, error) {
	c.mu.Lock()
	if it, ok := c.items[key]; ok && !fresh && c.now().Sub(it.at) < cacheTTL {
		c.mu.Unlock()
		return it.doc, nil
	}
	if f := c.flights[key]; f != nil {
		c.mu.Unlock()
		<-f.done
		return f.doc, f.err
	}
	f := &flight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()

	f.doc, f.err = fn()

	c.mu.Lock()
	delete(c.flights, key)
	if f.err == nil && f.doc.keep() && len(f.doc.text) <= cacheDocMax {
		c.put(key, f.doc)
	}
	c.mu.Unlock()
	close(f.done)
	return f.doc, f.err
}

func (c *cache) put(key string, d *doc) {
	now := c.now()
	if len(c.items) >= cacheMax {
		oldest, at := "", now
		for k, it := range c.items {
			if now.Sub(it.at) >= cacheTTL {
				delete(c.items, k)
			} else if it.at.Before(at) {
				oldest, at = k, it.at
			}
		}
		if len(c.items) >= cacheMax {
			delete(c.items, oldest)
		}
	}
	c.items[key] = cached{doc: d, at: now}
}
