package search

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// clock is a time a test moves by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Unix(1_700_000_000, 0)} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fake is a source a test scripts, counting how often it was asked.
type fake struct {
	calls atomic.Int32
	src   source
}

func hit(n string) Hit { return Hit{Title: n, URL: "https://" + n + ".test/", Snippet: n} }

// answers is a source that returns hits at once.
func answers(name string, hits ...Hit) *fake {
	f := &fake{}
	f.src = source{name: name, provider: name, fetch: func(context.Context, Query) ([]Hit, error) {
		f.calls.Add(1)
		return hits, nil
	}}
	return f
}

// fails is a source that returns err at once.
func fails(name string, err error) *fake {
	f := &fake{}
	f.src = source{name: name, provider: name, fetch: func(context.Context, Query) ([]Hit, error) {
		f.calls.Add(1)
		return nil, err
	}}
	return f
}

// hangs is a source that answers only when it is cancelled.
func hangs(name string) *fake {
	f := &fake{}
	f.src = source{name: name, provider: name, fetch: func(ctx context.Context, _ Query) ([]Hit, error) {
		f.calls.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	return f
}

// testMeta is an engine over scripted sources with no budgets, asking all of
// them: the tests of fanout and budgets set those themselves.
func testMeta(c *clock, fakes ...*fake) *meta {
	m := &meta{
		health: newBreaker(c.now), cache: newCache(c.now), buckets: map[string]*bucket{},
		sourceTimeout: time.Second, grace: 20 * time.Millisecond, patience: time.Second, ceiling: 2 * time.Second,
		queueMax: 20 * time.Millisecond, tokenWait: 20 * time.Millisecond, quorum: 3, fanout: 99,
	}
	for _, f := range fakes {
		m.sources = append(m.sources, f.src)
	}
	return m
}

func TestMetaFusesWhatTheSourcesReturn(t *testing.T) {
	m := testMeta(newClock(),
		answers("a", hit("one"), hit("two"), hit("three")),
		answers("b", hit("two"), hit("four")),
		answers("c", hit("two"), hit("one")),
	)
	res, err := m.Search(bg, Query{Text: "q"})
	if err != nil || res.Note != "" || len(res.Blocked) != 0 {
		t.Fatalf("res %+v err %v", res, err)
	}
	var got []string
	for _, h := range res.Results {
		got = append(got, fmt.Sprintf("%s:%d", h.Title, h.Engines))
	}
	if want := []string{"two:3", "one:2", "four:1", "three:1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v want %v", got, want)
	}
}

func TestMetaDoesNotWaitForAStuckSource(t *testing.T) {
	stuck := hangs("stuck")
	m := testMeta(newClock(), answers("a", hit("one")), answers("b", hit("two")), answers("c", hit("three")), stuck)
	start := time.Now()
	res, err := m.Search(bg, Query{Text: "q"})
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("waited %v for a stuck source", took)
	}
	if err != nil || len(res.Results) != 3 {
		t.Fatalf("res %+v err %v", res, err)
	}
	// It was cut short because the search was done: that is no verdict on it.
	if len(res.Blocked) != 0 || m.health.open("stuck") {
		t.Fatalf("a source cancelled by an early return was blamed: %v", res.Blocked)
	}
}

// Fewer sources than the quorum have answered, but there are results: after
// its patience the search returns with them.
func TestMetaPatience(t *testing.T) {
	stuck := hangs("stuck")
	m := testMeta(newClock(), answers("a", hit("one")), stuck)
	m.patience = 40 * time.Millisecond
	start := time.Now()
	res, _ := m.Search(bg, Query{Text: "q"})
	if took := time.Since(start); took > 400*time.Millisecond {
		t.Fatalf("waited %v with results in hand", took)
	}
	if len(res.Results) != 1 || len(res.Blocked) != 0 || m.health.open("stuck") {
		t.Fatalf("res %+v", res)
	}

	// With nothing in hand it keeps waiting, and the first results end it.
	late := &fake{}
	late.src = source{name: "late", provider: "late", fetch: func(context.Context, Query) ([]Hit, error) {
		time.Sleep(120 * time.Millisecond)
		return []Hit{hit("two")}, nil
	}}
	m = testMeta(newClock(), answers("empty"), late, hangs("stuck"))
	m.patience = 40 * time.Millisecond
	start = time.Now()
	res, _ = m.Search(bg, Query{Text: "q"})
	if took := time.Since(start); took < 100*time.Millisecond || took > 500*time.Millisecond {
		t.Fatalf("took %v", took)
	}
	if len(res.Results) != 1 {
		t.Fatalf("res %+v", res)
	}
}

func called(fakes ...*fake) (n int) {
	for _, f := range fakes {
		n += int(f.calls.Load())
	}
	return n
}

// A search asks every API but only `fanout` of the scraped sites.
func TestMetaFanout(t *testing.T) {
	api := answers("api", hit("zero"))
	api.src.api = true
	a, b, c, d := answers("a", hit("one")), answers("b", hit("two")), answers("c", hit("three")), answers("d", hit("four"))
	m := testMeta(newClock(), api, a, b, c, d)
	m.fanout = 2
	res, _ := m.Search(bg, Query{Text: "q"})
	if n := called(a, b, c, d); n != 2 || api.calls.Load() != 1 {
		t.Fatalf("%d scraped sites asked, want 2 (api asked %d times)", n, api.calls.Load())
	}
	// The sites that were not needed are not reported as missing.
	if len(res.Results) != 3 || len(res.Blocked) != 0 {
		t.Fatalf("res %+v", res)
	}
}

// A scraped site that fails is replaced from the reserve, so a block costs
// the search nothing.
func TestMetaReserveReplacesAFailure(t *testing.T) {
	c := newClock()
	bad, good := fails("bad", errBlocked), answers("good", hit("one"))
	m := testMeta(c, bad, good)
	m.fanout = 1
	// The fuller budget is asked first.
	m.buckets["bad"] = newBucket(c.now, limit{every: time.Minute, burst: 3})
	m.buckets["good"] = newBucket(c.now, limit{every: time.Minute, burst: 2})
	res, _ := m.Search(bg, Query{Text: "q"})
	if bad.calls.Load() != 1 || good.calls.Load() != 1 {
		t.Fatalf("calls bad %d good %d", bad.calls.Load(), good.calls.Load())
	}
	if len(res.Results) != 1 || !reflect.DeepEqual(res.Blocked, []string{"bad"}) || res.Note != "" {
		t.Fatalf("res %+v", res)
	}
}

// Searches spread over the sites by budget, and a site with no turn left is
// skipped, which is not held against it.
func TestMetaBudgets(t *testing.T) {
	c := newClock()
	a, b := answers("a", hit("one")), answers("b", hit("two"))
	m := testMeta(c, a, b)
	m.fanout = 1
	m.buckets["a"] = newBucket(c.now, limit{every: time.Minute, burst: 1})
	m.buckets["b"] = newBucket(c.now, limit{every: time.Minute, burst: 1})
	for _, text := range []string{"first", "second"} {
		if res, _ := m.Search(bg, Query{Text: text}); len(res.Results) != 1 || len(res.Blocked) != 0 {
			t.Fatalf("%s: %+v", text, res)
		}
	}
	if a.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Fatalf("two searches asked a %d and b %d times, want one each", a.calls.Load(), b.calls.Load())
	}
	// Both budgets are spent and the next turn is a minute off.
	res, _ := m.Search(bg, Query{Text: "third"})
	if called(a, b) != 2 || len(res.Results) != 0 || !reflect.DeepEqual(res.Blocked, []string{"a", "b"}) {
		t.Fatalf("res %+v after %d calls", res, called(a, b))
	}
	if !strings.Contains(res.Note, "rate-limited") || m.health.open("a") || m.health.open("b") {
		t.Fatalf("note %q, a open %v", res.Note, m.health.open("a"))
	}
	// A minute later they have a turn again (and the failed search was not cached).
	c.add(time.Minute)
	if res, _ = m.Search(bg, Query{Text: "third"}); len(res.Results) != 1 {
		t.Fatalf("after the refill: %+v", res)
	}
}

// With every budget spent a search queues for the next turn rather than fail.
func TestMetaQueuesForATurn(t *testing.T) {
	a := answers("a", hit("one"))
	m := testMeta(newClock(), a)
	m.queueMax = time.Second
	m.buckets["a"] = newBucket(time.Now, limit{every: 80 * time.Millisecond, burst: 1})
	_, _ = m.Search(bg, Query{Text: "first"})
	start := time.Now()
	res, _ := m.Search(bg, Query{Text: "second"})
	if took := time.Since(start); a.calls.Load() != 2 || len(res.Results) != 1 || took < 40*time.Millisecond {
		t.Fatalf("calls %d res %+v took %v", a.calls.Load(), res, took)
	}
}

// A lone search asks several idle sites; searches that follow closely ask
// one each, so a burst does not spend every site on its first searches.
func TestMetaAsksMoreSitesOnlyWhileTheyAreIdle(t *testing.T) {
	c := newClock()
	a, b := answers("a", hit("one")), answers("b", hit("two"))
	m := testMeta(c, a, b)
	m.fanout = 2
	for _, n := range []string{"a", "b"} {
		m.buckets[n] = newBucket(c.now, limit{every: time.Minute, burst: 2})
	}
	if res, _ := m.Search(bg, Query{Text: "first"}); called(a, b) != 2 || len(res.Results) != 2 {
		t.Fatalf("a lone search asked %d sites: %+v", called(a, b), res)
	}
	if res, _ := m.Search(bg, Query{Text: "second"}); called(a, b) != 3 || len(res.Results) != 1 {
		t.Fatalf("the next search asked %d more sites: %+v", called(a, b)-2, res)
	}
	// After a quiet spell both are idle again.
	c.add(2 * time.Minute)
	if _, _ = m.Search(bg, Query{Text: "third"}); called(a, b) != 5 {
		t.Fatalf("calls %d", called(a, b))
	}
}

// A replacement's turn reserved for a search that ended before it came is
// given back.
func TestMetaGivesBackAnUnusedTurn(t *testing.T) {
	c := newClock()
	bad, slow := fails("bad", errBlocked), answers("slow", hit("late"))
	var all []*fake
	for _, n := range []string{"x", "y", "z"} {
		f := answers(n, hit(n))
		f.src.api = true
		all = append(all, f)
	}
	m := testMeta(c, append(all, bad, slow)...)
	m.fanout = 1
	m.tokenWait = time.Second
	// "bad" has the fuller budget and is asked first; "slow" is its
	// replacement, 300 ms away from its turn.
	m.buckets["bad"] = newBucket(c.now, limit{every: time.Minute, burst: 3})
	b := newBucket(time.Now, limit{every: 300 * time.Millisecond, burst: 1})
	b.take(0)
	m.buckets["slow"] = b
	// Three APIs make the quorum long before that turn comes.
	if res, _ := m.Search(bg, Query{Text: "q"}); len(res.Results) != 3 || slow.calls.Load() != 0 {
		t.Fatalf("res %+v calls %d", res, slow.calls.Load())
	}
	time.Sleep(50 * time.Millisecond)
	if lvl := b.level(); lvl < 0 {
		t.Fatalf("the unused turn was kept: level %.2f", lvl)
	}
}

// A site that says "too many requests" is left alone for longer than one
// that merely failed, and for as long as it asked.
func TestMetaRateLimitedSourceRests(t *testing.T) {
	c := newClock()
	limited := fails("limited", &refused{status: 429, limited: true})
	asked := fails("asked", &refused{status: 429, limited: true, after: 10 * time.Minute})
	forbidden := fails("forbidden", &refused{status: 403})
	m := testMeta(c, limited, asked, forbidden)
	res, _ := m.Search(bg, Query{Text: "q"})
	if !reflect.DeepEqual(res.Blocked, []string{"asked", "forbidden", "limited"}) {
		t.Fatalf("blocked %v", res.Blocked)
	}
	c.add(backoffStart + time.Second)
	if m.health.open("forbidden") || !m.health.open("limited") || !m.health.open("asked") {
		t.Fatal("after 30 s only the plain failure should be asked again")
	}
	c.add(backoffLimited)
	if m.health.open("limited") || !m.health.open("asked") {
		t.Fatal("after 2 min the rate-limited site is asked again, but not the one that asked for 10")
	}
	c.add(10 * time.Minute)
	if m.health.open("asked") {
		t.Fatal("Retry-After has passed")
	}
}

func TestBucket(t *testing.T) {
	c := newClock()
	b := newBucket(c.now, limit{every: 6 * time.Second, burst: 2})
	for i := range 2 {
		if wait, ok := b.take(0); !ok || wait != 0 {
			t.Fatalf("turn %d of the burst: wait %v ok %v", i+1, wait, ok)
		}
	}
	if _, ok := b.take(time.Second); ok {
		t.Fatal("the next turn is 6 s off: a 1 s wait should be refused")
	}
	// A turn that is close enough is reserved, and the one after it is further.
	c.add(5 * time.Second)
	if wait, ok := b.take(1500 * time.Millisecond); !ok || wait.Round(time.Millisecond) != time.Second {
		t.Fatalf("wait %v ok %v", wait, ok)
	}
	if _, ok := b.take(1500 * time.Millisecond); ok {
		t.Fatal("a second search should not get the same turn")
	}
	b.giveBack()
	if wait, ok := b.take(1500 * time.Millisecond); !ok || wait.Round(time.Millisecond) != time.Second {
		t.Fatalf("after giving the turn back: wait %v ok %v", wait, ok)
	}
	// A long quiet spell fills it to the burst and no further.
	c.add(time.Hour)
	if lvl := b.level(); lvl != 2 {
		t.Fatalf("level %.2f", lvl)
	}
}

// Every scraped site has a budget; a new one must not ship without.
func TestScrapedSourcesAreLimited(t *testing.T) {
	for _, s := range sources {
		if !s.api && s.limit.every == 0 {
			t.Fatalf("%s is scraped without a limit", s.name)
		}
		if (s.limit.every > 0) != (sharedBuckets[s.name] != nil) {
			t.Fatalf("%s: limit and bucket disagree", s.name)
		}
	}
}

func TestMetaCeiling(t *testing.T) {
	m := testMeta(newClock(), answers("a", hit("one")), hangs("x"), hangs("y"))
	m.ceiling = 60 * time.Millisecond
	start := time.Now()
	res, _ := m.Search(bg, Query{Text: "q"})
	if took := time.Since(start); took > 400*time.Millisecond {
		t.Fatalf("the ceiling did not hold: %v", took)
	}
	if len(res.Results) != 1 || !reflect.DeepEqual(res.Blocked, []string{"x", "y"}) || res.Note != "" {
		t.Fatalf("res %+v", res)
	}
}

func TestMetaSourceTimeoutOpensTheBreaker(t *testing.T) {
	slow := hangs("slow")
	m := testMeta(newClock(), answers("a", hit("one")), slow)
	m.sourceTimeout = 30 * time.Millisecond
	res, _ := m.Search(bg, Query{Text: "q"})
	if !reflect.DeepEqual(res.Blocked, []string{"slow"}) || !m.health.open("slow") {
		t.Fatalf("blocked %v open %v", res.Blocked, m.health.open("slow"))
	}
	// The next search leaves it alone and still says it is missing.
	res, _ = m.Search(bg, Query{Text: "other"})
	if slow.calls.Load() != 1 || !reflect.DeepEqual(res.Blocked, []string{"slow"}) {
		t.Fatalf("calls %d blocked %v", slow.calls.Load(), res.Blocked)
	}
}

func TestMetaBlockedIsNotNothingFound(t *testing.T) {
	c := newClock()
	a, b := fails("a", errBlocked), fails("b", errors.New("HTTP 500"))
	m := testMeta(c, a, b)
	res, err := m.Search(bg, Query{Text: "q"})
	if err != nil || len(res.Results) != 0 || res.Results == nil {
		t.Fatalf("res %+v err %v", res, err)
	}
	if !reflect.DeepEqual(res.Blocked, []string{"a", "b"}) || !strings.Contains(res.Note, "does not mean nothing exists") || !strings.Contains(res.Note, "a, b") {
		t.Fatalf("res %+v", res)
	}
	// Not cached: once the sources are back the same query is answered.
	c.add(backoffStart + time.Second)
	m.sources = []source{answers("a", hit("one")).src}
	if res, _ = m.Search(bg, Query{Text: "q"}); len(res.Results) != 1 || res.Note != "" {
		t.Fatalf("after recovery %+v", res)
	}
}

func TestMetaNothingFound(t *testing.T) {
	a := answers("a")
	m := testMeta(newClock(), a, answers("b"))
	res, err := m.Search(bg, Query{Text: "zzqqxx"})
	if err != nil || len(res.Results) != 0 || res.Note != "" || len(res.Blocked) != 0 {
		t.Fatalf("res %+v err %v", res, err)
	}
	// A true empty answer is an answer: it is cached.
	_, _ = m.Search(bg, Query{Text: "zzqqxx"})
	if a.calls.Load() != 1 {
		t.Fatalf("asked %d times", a.calls.Load())
	}
}

func TestMetaCache(t *testing.T) {
	c := newClock()
	a := answers("a", hit("one"), hit("two"), hit("three"))
	m := testMeta(c, a)
	if res, _ := m.Search(bg, Query{Text: "Go  Context", Max: 2}); len(res.Results) != 2 {
		t.Fatalf("max not applied: %+v", res)
	}
	// Same words, other spelling and another size: served from the cache.
	if res, _ := m.Search(bg, Query{Text: " go context ", Max: 3}); len(res.Results) != 3 {
		t.Fatalf("cached answer cut to the first caller's size: %+v", res)
	}
	if a.calls.Load() != 1 {
		t.Fatalf("asked %d times, want 1", a.calls.Load())
	}
	// Another language is another search.
	_, _ = m.Search(bg, Query{Text: "go context", Language: "pl"})
	if a.calls.Load() != 2 {
		t.Fatalf("language shared a cache entry")
	}
	c.add(cacheTTL + time.Second)
	_, _ = m.Search(bg, Query{Text: "go context"})
	if a.calls.Load() != 3 {
		t.Fatalf("expired entry served")
	}
}

func TestMetaSharesOneFlight(t *testing.T) {
	release := make(chan struct{})
	f := &fake{}
	f.src = source{name: "a", provider: "a", fetch: func(context.Context, Query) ([]Hit, error) {
		f.calls.Add(1)
		<-release
		return []Hit{hit("one")}, nil
	}}
	m := testMeta(newClock(), f)
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if res, _ := m.Search(bg, Query{Text: "q"}); len(res.Results) != 1 {
				t.Errorf("res %+v", res)
			}
		}()
	}
	time.Sleep(30 * time.Millisecond)
	close(release)
	wg.Wait()
	if f.calls.Load() != 1 {
		t.Fatalf("asked %d times for one query", f.calls.Load())
	}
}

func TestMetaSkipsSourcesThatCannotServeTheQuery(t *testing.T) {
	en := answers("en", hit("one"))
	en.src.skip = notEnglish
	m := testMeta(newClock(), en, answers("a", hit("two")))
	res, _ := m.Search(bg, Query{Text: "q", Language: "pl"})
	if en.calls.Load() != 0 || len(res.Results) != 1 || len(res.Blocked) != 0 {
		t.Fatalf("calls %d res %+v", en.calls.Load(), res)
	}
}

func TestMetaQuery(t *testing.T) {
	if _, err := testMeta(newClock(), answers("a")).Search(bg, Query{Text: "  "}); err == nil {
		t.Fatal("an empty query should fail")
	}
	got := cleanQuery(Query{Text: " x ", Language: "PL-pl", Country: "../etc", Max: 99})
	if want := (Query{Text: "x", Language: "pl", Max: MaxResultsCap}); got != want {
		t.Fatalf("cleaned %+v", got)
	}
	if got := cleanQuery(Query{Text: "x", Language: "polish", Country: "PL"}); got.Language != "" || got.Country != "pl" || got.Max != DefaultMaxResults {
		t.Fatalf("cleaned %+v", got)
	}
}

func TestPickSources(t *testing.T) {
	all := []source{{name: "a", provider: "p"}, {name: "b", provider: "p"}, {name: "c", provider: "c"}}
	names := func(list string) string {
		var out []string
		for _, s := range pickSources(all, list) {
			out = append(out, s.name)
		}
		return strings.Join(out, ",")
	}
	for list, want := range map[string]string{"": "a,b,c", " C , a": "a,c", "p": "a,b", "nope": "a,b,c"} {
		if got := names(list); got != want {
			t.Fatalf("%q: %s want %s", list, got, want)
		}
	}
	// Every real source is on the list the admin page shows.
	if d, _ := Lookup(Builtin); len(d.Settings) != 1 || !strings.Contains(d.Settings[0].Description, "marginalia") {
		t.Fatalf("descriptor %+v", d)
	}
}

func TestMany(t *testing.T) {
	m := testMeta(newClock(), answers("a", hit("one")))
	res := Many(bg, m, []Query{{Text: "first"}, {Text: ""}, {Text: "third"}})
	if len(res) != 3 || len(res[0].Results) != 1 || len(res[2].Results) != 1 {
		t.Fatalf("res %+v", res)
	}
	if res[1].Results == nil || !strings.Contains(res[1].Note, "query required") {
		t.Fatalf("a failed query should say why: %+v", res[1])
	}
}

func TestBreakerBacksOffExponentially(t *testing.T) {
	c := newClock()
	b := newBreaker(c.now)
	if b.open("x") {
		t.Fatal("a source starts healthy")
	}
	for i, want := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute} {
		b.fail("x", 0)
		c.add(want - time.Second)
		if !b.open("x") {
			t.Fatalf("failure %d: closed before %v", i+1, want)
		}
		c.add(time.Second)
		if b.open("x") {
			t.Fatalf("failure %d: still open after %v", i+1, want)
		}
	}
	for range 20 {
		b.fail("x", time.Hour)
	}
	c.add(backoffMax - time.Second)
	if !b.open("x") {
		t.Fatal("the backoff should reach its cap")
	}
	c.add(time.Second)
	if b.open("x") {
		t.Fatal("the backoff should not pass its cap")
	}
	// An answer resets it: the next failure starts from the beginning.
	b.ok("x")
	b.fail("x", 0)
	c.add(backoffStart)
	if b.open("x") {
		t.Fatal("ok did not reset the backoff")
	}
}

func TestFuse(t *testing.T) {
	got := fuse([]ranked{
		{provider: "ddg", hits: []Hit{
			{Title: "Go", URL: "http://www.go.dev/doc/?utm_source=x#top", Snippet: "short"},
			{Title: "Only ddg", URL: "https://only.test/a"},
		}},
		// A second page of the same provider: no extra voice.
		{provider: "ddg", hits: []Hit{{Title: "Go", URL: "https://go.dev/doc"}}},
		{provider: "brave", hits: []Hit{
			{Title: "Elsewhere", URL: "https://else.test/"},
			{Title: "Go again", URL: "https://go.dev/doc/", Snippet: "a longer snippet"},
			{Title: "", URL: "https://untitled.test/"},
			{Title: "Not a page", URL: "javascript:void(0)"},
		}},
	})
	if len(got) != 3 {
		t.Fatalf("hits %+v", got)
	}
	want := Hit{Title: "Go", URL: "https://go.dev/doc", Snippet: "a longer snippet", Engines: 2}
	if got[0] != want {
		t.Fatalf("first %+v want %+v", got[0], want)
	}
	if got[1].Title != "Elsewhere" || got[1].Engines != 1 || got[2].Title != "Only ddg" {
		t.Fatalf("rest %+v", got[1:])
	}
}

func TestCanonical(t *testing.T) {
	same := []string{
		"https://Example.com/a/b/", "http://www.example.com/a/b", "https://example.com:443/a/b#frag",
		"https://example.com/a/b?utm_campaign=x&fbclid=1",
	}
	for _, u := range same {
		if got := canonical(u); got != "example.com/a/b" {
			t.Fatalf("%s -> %q", u, got)
		}
	}
	if canonical("https://example.com/a?b=2&a=1") != canonical("https://example.com/a?a=1&b=2") {
		t.Fatal("parameter order should not matter")
	}
	if canonical("https://example.com/a?id=1") == canonical("https://example.com/a?id=2") {
		t.Fatal("real parameters are part of the page")
	}
	for _, u := range []string{"", "mailto:x@y.z", "/relative", "ftp://example.com/x"} {
		if canonical(u) != "" {
			t.Fatalf("%q should not be a page", u)
		}
	}
	if got := clip(strings.Repeat("ż", 400), 320); len([]rune(got)) != 320 || !strings.HasSuffix(got, "…") {
		t.Fatalf("clip gave %d runes", len([]rune(got)))
	}
}
