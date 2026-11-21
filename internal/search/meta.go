package search

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"
)

// Speed is the point: a search never waits for a stuck source.
const (
	// sourceTimeout is how long one source may take.
	sourceTimeout = 4 * time.Second
	// Once quorum providers have answered, the rest get grace and no more.
	quorum = 3
	grace  = 300 * time.Millisecond
	// After patience a search that has web results returns with them rather
	// than wait for sources that may never bring the quorum.
	patience = 1200 * time.Millisecond
	// ceiling is the longest a search takes, whatever the sources do.
	ceiling = 6 * time.Second
)

// Being a good guest is the other point: a site that is asked too often
// blocks the address for everyone.
const (
	// fanout is how many scraped sites one search asks at most. It asks the
	// site with the most budget left, and more only while those are idle (a
	// full budget), so a lone search hears several voices and a burst of
	// searches spreads thin instead of spending every site on the first few.
	// The sites not asked are a reserve: one is asked for each that fails.
	// A query of a batch asks one: the batch is the burst.
	fanout = 2
	// queueMax is how long a search queues for a scraped site when every
	// budget is spent, and queueBatch how long a query of a batch does.
	// Waiting for a real answer costs the model less than a failed search
	// and another turn, and a batch is what the Bot is told to send. It
	// comes before the ceiling, which times the sources, not our own manners.
	queueMax   = 20 * time.Second
	queueBatch = 60 * time.Second
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

	sourceTimeout, grace, patience, ceiling, queueMax, queueBatch, tokenWait, jitter time.Duration
	quorum, fanout                                                                   int
}

func newMeta(s Settings) *meta {
	return &meta{
		sources: pickSources(sources, s),
		health:  sharedHealth, cache: sharedCache, buckets: sharedBuckets,
		sourceTimeout: sourceTimeout, grace: grace, patience: patience, ceiling: ceiling,
		queueMax: queueMax, queueBatch: queueBatch, tokenWait: tokenWait, jitter: jitter, quorum: quorum, fanout: fanout,
	}
}

// pickSources is the sources a search asks. With no `sources` setting that is
// every one that answers a plain request; a list keeps the ones it names, by
// source or provider name, which is also how an opt-in source is turned on (a
// list that names none is read as no list). A source that needs a key is
// asked only once the key is set.
func pickSources(all []source, s Settings) []source {
	var out []source
	for _, src := range all {
		if on, _ := sourceOn(src, s, all); on {
			if src.keyed != nil {
				src.fetch = src.keyed(strings.TrimSpace(s[src.key]))
			}
			out = append(out, src)
		}
	}
	return out
}

// sourceOn reports whether src is asked under these settings, and why not.
func sourceOn(src source, s Settings, all []source) (on bool, why string) {
	want := map[string]bool{}
	for _, n := range strings.Split(s["sources"], ",") {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			want[n] = true
		}
	}
	named := false
	for _, a := range all {
		named = named || want[a.name] || want[a.provider]
	}
	switch {
	case named && !want[src.name] && !want[src.provider]:
		return false, "not in the sources list"
	case !named && src.optIn:
		return false, "off unless named in the sources list: it answers plain requests with a script challenge"
	case src.keyed != nil && strings.TrimSpace(s[src.key]) == "":
		return false, "needs its API key"
	}
	return true, ""
}

func (m *meta) Search(ctx context.Context, q Query) (Result, error) {
	q = cleanQuery(q)
	if q.Text == "" {
		return Result{}, fmt.Errorf("query required")
	}
	// The search is detached from its caller: it is bounded by its queue and
	// the ceiling, and another caller may be waiting on the same flight.
	f, err := m.cache.do(ctx, cacheKey(q), func() found { return m.run(context.WithoutCancel(ctx), q) })
	if err != nil {
		return Result{}, err
	}
	return f.result(q.Max), nil
}

// result is f as a caller reads it: cut to max, and with a note whenever the
// hits are not what a working web search would have answered.
func (f found) result(max int) Result {
	res := Result{Results: f.hits, Blocked: f.blocked, Busy: f.busy}
	if len(res.Results) > max {
		res.Results = res.Results[:max]
	}
	if res.Results == nil {
		res.Results = []Hit{}
	}
	var missing []string
	if len(f.blocked) > 0 {
		missing = append(missing, "blocked, rate-limited or timed out: "+strings.Join(f.blocked, ", "))
	}
	if len(f.busy) > 0 {
		missing = append(missing, "busy: "+strings.Join(f.busy, ", "))
	}
	why := strings.Join(missing, "; ")
	switch {
	case f.stale > 0:
		res.Note = fmt.Sprintf("No search engine could answer right now (%s). These results are from %d minutes ago.", why, max1(int(f.stale.Minutes())))
	case len(res.Results) == 0 && len(f.blocked) == 0 && len(f.busy) > 0:
		res.Note = fmt.Sprintf("Search is busy: this server's budget with every search engine is spent for the moment (%s). "+
			"This does not mean nothing exists. Search again in about %d seconds.", why, max1(int(f.retry.Seconds()+0.999)))
	case len(res.Results) == 0 && why != "":
		res.Note = "No search engine could answer (" + why +
			"). This does not mean nothing exists. Do not repeat the search at once: the engines need a minute or more."
	case f.thin:
		res.Note = "Only the reference sources answered; the web search engines did not (" + why +
			"). This is not a full web search: do not conclude that nothing else exists."
	}
	return res
}

func max1(n int) int { return max(n, 1) }

type answer struct {
	src  source
	hits []Hit
	err  error
}

// take reserves s's next turn if it comes within maxWait; refused, it says
// how far off the turn is.
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
	// A provider counts as failed only when none of its sources answered,
	// and as busy when it was not asked because its turn was too far off.
	failed, busy, answered := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var retry time.Duration
	tooFar := func(s source, wait time.Duration) {
		busy[s.provider] = true
		if retry == 0 || wait < retry {
			retry = wait
		}
	}
	var apis, reserve []source
	scraped := false
	for _, s := range m.sources {
		if s.skip != nil && s.skip(q) {
			continue
		}
		scraped = scraped || !s.api
		switch {
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
	queueMax, fanout := m.queueMax, m.fanout
	if q.batch {
		queueMax, fanout = m.queueBatch, 1
	}
	var chosen []source
	var queue time.Duration
	for len(reserve) > 0 && len(chosen) == 0 {
		s := reserve[0]
		reserve = reserve[1:]
		wait, ok := m.take(s, queueMax)
		if !ok {
			tooFar(s, wait)
			continue
		}
		chosen, queue = append(chosen, s), wait
	}
	if len(chosen) > 0 && queue == 0 {
		var rest []source
		for _, s := range reserve {
			if len(chosen) < fanout && m.idle(s) {
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
	pending := map[string]source{}
	start := func(s source, wait time.Duration) {
		pending[s.name] = s
		go func() { ch <- m.ask(ctx, s, q, wait) }()
	}
	// next asks the first site of the reserve whose turn is near.
	next := func() {
		for len(reserve) > 0 {
			s := reserve[0]
			reserve = reserve[1:]
			wait, ok := m.take(s, m.tokenWait)
			if ok {
				start(s, wait)
				return
			}
			tooFar(s, wait)
		}
	}
	for _, s := range apis {
		if wait, ok := m.take(s, 0); ok {
			start(s, 0)
		} else {
			tooFar(s, wait)
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
	hits, web := 0, false
	// enough is having results worth returning early with: any, unless a
	// web search site is still to answer and only the reference APIs have.
	enough := func() bool {
		if hits == 0 {
			return false
		}
		if web {
			return true
		}
		for _, s := range pending {
			if !s.api {
				return false
			}
		}
		return true
	}
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
			} else {
				answered[a.src.provider] = true
				lists = append(lists, ranked{provider: a.src.provider, hits: a.hits})
				hits += len(a.hits)
				web = web || !a.src.api
				if graceC == nil && len(answered) >= m.quorum {
					t := time.NewTimer(m.grace)
					defer t.Stop()
					graceC = t.C
				}
			}
			if patienceC == nil && enough() {
				break wait
			}
		case <-graceC:
			break wait
		case <-patienceC:
			if enough() {
				break wait
			}
			// Not yet: the next answer that makes it enough ends the wait.
			patienceC = nil
		case <-ctx.Done():
			// The ceiling: what has not answered by now is too slow.
			for _, s := range pending {
				failed[s.provider] = true
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
	for p := range busy {
		if !answered[p] && !failed[p] {
			out.busy = append(out.busy, p)
		}
	}
	sort.Strings(out.blocked)
	sort.Strings(out.busy)
	if len(out.busy) > 0 {
		out.retry = retry
	}
	out.thin = len(out.hits) > 0 && scraped && !web && len(out.blocked)+len(out.busy) > 0
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
	// Past a rest only one search probes the site; the others treat it as
	// still resting, and the turn goes back.
	if !m.health.enter(s.name) {
		if b := m.buckets[s.name]; b != nil {
			b.giveBack()
		}
		return answer{src: s, err: errResting}
	}
	sctx, cancel := context.WithTimeout(ctx, m.sourceTimeout)
	defer cancel()
	hits, err := s.fetch(sctx, q)
	switch {
	case ctx.Err() != nil:
		m.health.release(s.name)
		if err == nil {
			err = ctx.Err()
		}
	case err != nil:
		m.health.blame(s.name, reasonOf(err, sctx))
		// Only a refusal is a verdict on how the site sees us.
		if errors.Is(err, errBlocked) {
			var atLeast time.Duration
			if after, limited := rateLimited(err); limited {
				atLeast = max(backoffLimited, after)
			}
			m.health.fail(s.name, atLeast)
		} else {
			m.health.stumble(s.name)
		}
	default:
		m.health.ok(s.name)
	}
	return answer{src: s, hits: hits, err: err}
}

// reasonOf is a failure in a few words, for the status page.
func reasonOf(err error, sctx context.Context) string {
	var r *refused
	switch {
	case errors.As(err, &r) && r.limited:
		return fmt.Sprintf("rate-limited (HTTP %d)", r.status)
	case errors.As(err, &r):
		return fmt.Sprintf("refused (HTTP %d)", r.status)
	case errors.Is(err, errBlocked):
		return "answered a challenge or a page without results"
	case sctx.Err() != nil:
		return "timed out"
	}
	return clip(err.Error(), 120)
}

// errResting is a source skipped because another search is probing it.
var errResting = errors.New("resting after a failure while another search tries it")

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
