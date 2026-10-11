package search

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"
)

// Speed is the point: a search never waits for a stuck source.
const (
	// sourceTimeout is how long one source may take.
	sourceTimeout = 2500 * time.Millisecond
	// Once quorum providers have answered, the rest get grace and no more.
	quorum = 3
	grace  = 300 * time.Millisecond
	// After patience a search that has any results returns with them rather
	// than wait for sources that may never bring the quorum.
	patience = 1200 * time.Millisecond
	// ceiling is the longest a search takes, whatever the sources do.
	ceiling = 4 * time.Second
)

// Being a good guest is the other point: a site that is asked too often
// blocks the address for everyone.
const (
	// fanout is how many scraped sites one search asks at most. It asks the
	// site with the most budget left, and more only while those are idle (a
	// full budget), so a lone search hears several voices and a burst of
	// searches spreads thin instead of spending every site on the first few.
	// The sites not asked are a reserve: one is asked for each that fails.
	fanout = 2
	// queueMax is how long a search queues for a scraped site when every
	// budget is spent. Waiting a few seconds for a real answer costs the
	// model less than a failed search and another turn. It comes before the
	// ceiling, which times the sources, not our own manners.
	queueMax = 8 * time.Second
	// tokenWait is how long a failed site's replacement may be waited for.
	tokenWait = 1500 * time.Millisecond
	// jitter is added to a wait, so requests to one site under load are not
	// evenly spaced.
	jitter = 400 * time.Millisecond
)

// Package state: the sources' health and budgets and the answers outlive an
// engine value (one is built per call).
var (
	sharedHealth  = newBreaker(time.Now)
	sharedCache   = newCache(time.Now)
	sharedBuckets = map[string]*bucket{}
)

func init() {
	for _, s := range sources {
		if s.limit.every > 0 {
			sharedBuckets[s.name] = newBucket(time.Now, s.limit)
		}
	}
}

// meta is the Built-in Scraper: it asks the APIs and a few healthy scraped
// sites at once, returns when enough have answered, and fuses their lists.
type meta struct {
	sources []source
	health  *breaker
	cache   *cache
	buckets map[string]*bucket

	sourceTimeout, grace, patience, ceiling, queueMax, tokenWait, jitter time.Duration
	quorum, fanout                                                       int
}

func newMeta(s Settings) *meta {
	return &meta{
		sources: pickSources(sources, s["sources"]),
		health:  sharedHealth, cache: sharedCache, buckets: sharedBuckets,
		sourceTimeout: sourceTimeout, grace: grace, patience: patience, ceiling: ceiling,
		queueMax: queueMax, tokenWait: tokenWait, jitter: jitter, quorum: quorum, fanout: fanout,
	}
}

// pickSources keeps the sources an operator listed, by source or provider
// name. An empty list, or one that names none, keeps them all.
func pickSources(all []source, list string) []source {
	want := map[string]bool{}
	for _, n := range strings.Split(list, ",") {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			want[n] = true
		}
	}
	var out []source
	for _, s := range all {
		if want[s.name] || want[s.provider] {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}

func (m *meta) Search(ctx context.Context, q Query) (Result, error) {
	q = cleanQuery(q)
	if q.Text == "" {
		return Result{}, fmt.Errorf("query required")
	}
	// The search is detached from its caller: it is bounded by the ceiling,
	// and another caller may be waiting on the same flight.
	f := m.cache.do(cacheKey(q), func() found { return m.run(context.WithoutCancel(ctx), q) })
	res := Result{Results: f.hits, Blocked: f.blocked}
	if len(res.Results) > q.Max {
		res.Results = res.Results[:q.Max]
	}
	if res.Results == nil {
		res.Results = []Hit{}
	}
	if len(res.Results) == 0 && len(res.Blocked) > 0 {
		res.Note = "No search engine could answer (blocked, rate-limited or timed out: " + strings.Join(res.Blocked, ", ") +
			"). This does not mean nothing exists. Do not repeat the search at once: the engines need a minute or more."
	}
	return res, nil
}

type answer struct {
	src  source
	hits []Hit
	err  error
}

// take reserves s's next turn if it comes within maxWait.
func (m *meta) take(s source, maxWait time.Duration) (wait time.Duration, ok bool) {
	b := m.buckets[s.name]
	if b == nil {
		return 0, true
	}
	if wait, ok = b.take(maxWait); ok && wait > 0 && m.jitter > 0 {
		wait += rand.N(m.jitter)
	}
	return wait, ok
}

// idle reports whether s has its whole budget: nobody asked it for a while.
func (m *meta) idle(s source) bool {
	b := m.buckets[s.name]
	return b == nil || b.level() >= b.burst
}

func (m *meta) run(parent context.Context, q Query) found {
	// A provider counts as failed only when none of its sources answered.
	failed, answered := map[string]bool{}, map[string]bool{}
	var apis, reserve []source
	for _, s := range m.sources {
		switch {
		case s.skip != nil && s.skip(q):
		case m.health.open(s.name):
			failed[s.provider] = true
		case s.api:
			apis = append(apis, s)
		default:
			reserve = append(reserve, s)
		}
	}
	// The scraped sites with the most budget left go first, and equals in a
	// random order, so searches spread over the sites.
	rand.Shuffle(len(reserve), func(i, j int) { reserve[i], reserve[j] = reserve[j], reserve[i] })
	level := map[string]float64{}
	for _, s := range reserve {
		if b := m.buckets[s.name]; b != nil {
			level[s.name] = b.level()
		}
	}
	sort.SliceStable(reserve, func(i, j int) bool { return level[reserve[i].name] > level[reserve[j].name] })

	// Which scraped sites this search asks: the first that will take it,
	// queueing for its turn if every budget is spent, and then others only
	// while they are idle.
	var chosen []source
	var queue time.Duration
	for len(reserve) > 0 && len(chosen) == 0 {
		s := reserve[0]
		reserve = reserve[1:]
		wait, ok := m.take(s, m.queueMax)
		if !ok {
			failed[s.provider] = true
			continue
		}
		chosen, queue = append(chosen, s), wait
	}
	if len(chosen) > 0 && queue == 0 {
		var rest []source
		for _, s := range reserve {
			if len(chosen) < m.fanout && m.idle(s) {
				if _, ok := m.take(s, 0); ok {
					chosen = append(chosen, s)
					continue
				}
			}
			rest = append(rest, s)
		}
		reserve = rest
	}
	if queue > 0 {
		select {
		case <-time.After(queue):
		case <-parent.Done():
		}
	}

	ctx, cancel := context.WithTimeout(parent, m.ceiling)
	defer cancel()
	ch := make(chan answer, len(m.sources))
	pending := map[string]string{}
	start := func(s source, wait time.Duration) {
		pending[s.name] = s.provider
		go func() { ch <- m.ask(ctx, s, q, wait) }()
	}
	// next asks the first site of the reserve whose turn is near.
	next := func() {
		for len(reserve) > 0 {
			s := reserve[0]
			reserve = reserve[1:]
			if wait, ok := m.take(s, m.tokenWait); ok {
				start(s, wait)
				return
			}
			failed[s.provider] = true
		}
	}
	for _, s := range apis {
		if _, ok := m.take(s, 0); ok {
			start(s, 0)
		} else {
			failed[s.provider] = true
		}
	}
	for _, s := range chosen {
		start(s, 0)
	}

	var lists []ranked
	var graceC <-chan time.Time
	patient := time.NewTimer(m.patience)
	defer patient.Stop()
	patienceC := patient.C
	hits := 0
wait:
	for len(pending) > 0 {
		select {
		case a := <-ch:
			delete(pending, a.src.name)
			if a.err != nil {
				failed[a.src.provider] = true
				// A scraped site that failed is replaced from the reserve.
				if !a.src.api && ctx.Err() == nil {
					next()
				}
				continue
			}
			answered[a.src.provider] = true
			lists = append(lists, ranked{provider: a.src.provider, hits: a.hits})
			hits += len(a.hits)
			if patienceC == nil && hits > 0 {
				break wait
			}
			if graceC == nil && len(answered) >= m.quorum {
				t := time.NewTimer(m.grace)
				defer t.Stop()
				graceC = t.C
			}
		case <-graceC:
			break wait
		case <-patienceC:
			if hits > 0 {
				break wait
			}
			// Nothing yet: the next source to bring results ends the wait.
			patienceC = nil
		case <-ctx.Done():
			// The ceiling: what has not answered by now is too slow.
			for _, p := range pending {
				failed[p] = true
			}
			break wait
		}
	}

	// Sources answer in any order; the fused order must not depend on it.
	sort.SliceStable(lists, func(i, j int) bool { return lists[i].provider < lists[j].provider })
	out := found{hits: fuse(lists)}
	for p := range failed {
		if !answered[p] {
			out.blocked = append(out.blocked, p)
		}
	}
	sort.Strings(out.blocked)
	return out
}

// ask waits for the source's turn (a replacement's may be a moment off), runs
// it under its own timeout and records its health. A source cut short because
// the search already ended gets no verdict, and one that was never reached
// gets its turn back.
func (m *meta) ask(ctx context.Context, s source, q Query, wait time.Duration) answer {
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			if b := m.buckets[s.name]; b != nil {
				b.giveBack()
			}
			return answer{src: s, err: ctx.Err()}
		}
	}
	sctx, cancel := context.WithTimeout(ctx, m.sourceTimeout)
	defer cancel()
	hits, err := s.fetch(sctx, q)
	switch {
	case ctx.Err() != nil:
		if err == nil {
			err = ctx.Err()
		}
	case err != nil:
		var atLeast time.Duration
		if after, limited := rateLimited(err); limited {
			atLeast = max(backoffLimited, after)
		}
		m.health.fail(s.name, atLeast)
	default:
		m.health.ok(s.name)
	}
	return answer{src: s, hits: hits, err: err}
}

// cleanQuery trims a query and drops a language or country that is not a
// plain code, so nothing a model made up reaches a URL or a cookie.
func cleanQuery(q Query) Query {
	q.Text = strings.TrimSpace(q.Text)
	q.Language = code(q.Language, 2, 3)
	q.Country = code(q.Country, 2, 2)
	q.Max = ClampMax(q.Max)
	return q
}

func code(s string, minLen, maxLen int) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// "pl-PL" and "pl_PL" are common spellings of a language: keep the first part.
	if i := strings.IndexAny(s, "-_"); i > 0 {
		s = s[:i]
	}
	if len(s) < minLen || len(s) > maxLen {
		return ""
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	return s
}
