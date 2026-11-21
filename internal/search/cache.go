package search

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Agents re-run the same search a lot, so an answer is kept for cacheTTL. It
// is held on to for staleTTL: when no engine can answer, an answer from a
// while ago, said to be one, beats none.
const (
	cacheTTL = 5 * time.Minute
	staleTTL = time.Hour
	cacheMax = 500
)

// found is what one search of the sources produced, before it is cut to the
// number of results a caller asked for.
type found struct {
	hits []Hit
	// blocked are the providers that refused, failed or timed out; busy the
	// ones not asked because their next turn was too far off, and retry how
	// far the nearest of those turns is.
	blocked, busy []string
	retry         time.Duration
	// thin marks hits that came from the reference APIs alone while every
	// web search site failed or was busy.
	thin bool
	// stale is the age of hits served from an answer past cacheTTL because
	// nothing could answer now.
	stale time.Duration
}

// usable reports whether f may be served again: an answer that is empty, or
// only the encyclopedia's, because sources were blocked or busy is not one.
func (f found) usable() bool {
	return !f.thin && f.stale == 0 && (len(f.hits) > 0 || (len(f.blocked) == 0 && len(f.busy) == 0))
}

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
// running, or starts fn. The search runs on its own: a caller that gives up
// (ctx) leaves it to finish for the others and for the cache. A search that
// could not answer is given the last answer for key, if one is not too old.
func (c *cache) do(ctx context.Context, key string, fn func() found) (found, error) {
	c.mu.Lock()
	if it, ok := c.items[key]; ok && c.now().Sub(it.at) < cacheTTL {
		c.mu.Unlock()
		return it.found, nil
	}
	f := c.flights[key]
	if f == nil {
		f = &flight{done: make(chan struct{})}
		c.flights[key] = f
		go func() {
			res := fn()
			c.mu.Lock()
			delete(c.flights, key)
			if res.usable() {
				c.put(key, res)
			} else if it, ok := c.items[key]; ok && len(res.hits) == 0 {
				if age := c.now().Sub(it.at); age < staleTTL {
					res.hits, res.stale = it.hits, age
				}
			}
			c.mu.Unlock()
			f.res = res
			close(f.done)
		}()
	}
	c.mu.Unlock()
	select {
	case <-f.done:
		return f.res, nil
	case <-ctx.Done():
		return found{}, ctx.Err()
	}
}

func (c *cache) put(key string, res found) {
	now := c.now()
	if len(c.items) >= cacheMax {
		oldest, at := "", now
		for k, it := range c.items {
			if now.Sub(it.at) >= staleTTL {
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
