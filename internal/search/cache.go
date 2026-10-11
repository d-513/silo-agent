package search

import (
	"strings"
	"sync"
	"time"
)

// Agents re-run the same search a lot, so an answer is kept for cacheTTL.
const (
	cacheTTL = 5 * time.Minute
	cacheMax = 500
)

// found is what one search of the sources produced, before it is cut to the
// number of results a caller asked for.
type found struct {
	hits    []Hit
	blocked []string
}

// usable reports whether f may be served again: an answer that is empty
// because sources were blocked is not one.
func (f found) usable() bool { return len(f.hits) > 0 || len(f.blocked) == 0 }

type cache struct {
	mu      sync.Mutex
	now     func() time.Time
	items   map[string]cached
	flights map[string]*flight
}

type cached struct {
	found
	at time.Time
}

// flight is a search in progress that later callers of the same key wait for.
type flight struct {
	done chan struct{}
	res  found
}

func newCache(now func() time.Time) *cache {
	return &cache{now: now, items: map[string]cached{}, flights: map[string]*flight{}}
}

func cacheKey(q Query) string {
	text := strings.ToLower(strings.Join(strings.Fields(q.Text), " "))
	return q.Language + "\x00" + q.Country + "\x00" + text
}

// do answers key from the cache, joins a search for it that is already
// running, or runs fn.
func (c *cache) do(key string, fn func() found) found {
	c.mu.Lock()
	if it, ok := c.items[key]; ok && c.now().Sub(it.at) < cacheTTL {
		c.mu.Unlock()
		return it.found
	}
	if f := c.flights[key]; f != nil {
		c.mu.Unlock()
		<-f.done
		return f.res
	}
	f := &flight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()

	f.res = fn()

	c.mu.Lock()
	delete(c.flights, key)
	if f.res.usable() {
		c.put(key, f.res)
	}
	c.mu.Unlock()
	close(f.done)
	return f.res
}

func (c *cache) put(key string, res found) {
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
	c.items[key] = cached{found: res, at: now}
}
