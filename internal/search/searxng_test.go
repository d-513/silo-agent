package search

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const searxngAnswer = `{"query":"go context","number_of_results":0,"results":[
 {"url":"https://go.dev/blog/context","title":"Go Concurrency  Patterns: Context","content":"In Go servers, each <b>incoming</b> request…","engines":["duckduckgo","brave","startpage"]},
 {"url":"https://go.dev/blog/context/","title":"The same page","content":"again","engines":["bing"]},
 {"url":"https://pkg.go.dev/context","title":"","content":"Package context","engine":"google"}],
 "unresponsive_engines":[["qwant","timeout"]]}`

func TestSearXNG(t *testing.T) {
	real := sharedCache
	sharedCache = newCache(newClock().now)
	t.Cleanup(func() { sharedCache = real })
	p := serve(t, map[string]page{"searx.test/search": body(searxngAnswer)})
	eng, err := New(SearXNG, Settings{"url": " http://searx.test/ "})
	if err != nil {
		t.Fatal(err)
	}
	res, err := eng.Search(bg, Query{Text: "go context", Language: "pl"})
	if err != nil || len(res.Results) != 2 || res.Note != "" || len(res.Blocked) != 0 {
		t.Fatalf("res %+v err %v", res, err)
	}
	want := Hit{Title: "Go Concurrency Patterns: Context", URL: "https://go.dev/blog/context", Snippet: "In Go servers, each incoming request…", Engines: 3}
	if res.Results[0] != want || res.Results[1].Title != "https://pkg.go.dev/context" || res.Results[1].Engines != 1 {
		t.Fatalf("hits %+v", res.Results)
	}
	rq := p.requests("searx.test")[0]
	if rq.url != "http://searx.test/search" || rq.query.Get("format") != "json" || rq.query.Get("language") != "pl-PL" || !rq.keyed {
		t.Fatalf("request %+v", rq)
	}
	// Answered again from the cache.
	if _, _ = eng.Search(bg, Query{Text: "go context", Language: "pl"}); len(p.requests("searx.test")) != 1 {
		t.Fatal("the same query was asked twice")
	}

	// No results because its engines did not answer is not "nothing exists".
	serve(t, map[string]page{"searx.test": body(`{"results":[],"unresponsive_engines":[["google","CAPTCHA"],["brave","timeout"]]}`)})
	res, err = eng.Search(bg, Query{Text: "other"})
	if err != nil || len(res.Results) != 0 || !reflect.DeepEqual(res.Blocked, []string{"google", "brave"}) || !strings.Contains(res.Note, "does not mean nothing exists") {
		t.Fatalf("res %+v err %v", res, err)
	}
	// A true empty answer has no note.
	serve(t, map[string]page{"searx.test": body(`{"results":[],"unresponsive_engines":[]}`)})
	if res, err = eng.Search(bg, Query{Text: "zzqqxx"}); err != nil || res.Note != "" || res.Results == nil {
		t.Fatalf("res %+v err %v", res, err)
	}

	// What an operator has to fix is said in words.
	for status, want := range map[int]string{http.StatusForbidden: "search.formats", http.StatusTooManyRequests: "limiter", http.StatusBadGateway: "HTTP 502"} {
		serve(t, map[string]page{"searx.test": {status: status}})
		if _, err := eng.Search(bg, Query{Text: "q" + want}); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("HTTP %d: %v", status, err)
		}
	}
	serve(t, map[string]page{"searx.test": body(`<html>not searxng</html>`)})
	if _, err := eng.Search(bg, Query{Text: "html"}); err == nil || !strings.Contains(err.Error(), "did not answer a search") {
		t.Fatalf("a page: %v", err)
	}
	none, _ := New(SearXNG, nil)
	if _, err := none.Search(bg, Query{Text: "q"}); err == nil || !strings.Contains(err.Error(), "search.searxng.url") {
		t.Fatalf("no address: %v", err)
	}
}

func TestStatus(t *testing.T) {
	c := newClock()
	real := sharedHealth
	sharedHealth = newBreaker(c.now)
	t.Cleanup(func() { sharedHealth = real })
	sharedHealth.ok("duckduckgo")
	sharedHealth.blame("yahoo", "rate-limited (HTTP 429)")
	sharedHealth.fail("yahoo", 0)
	by := map[string]SourceStatus{}
	for _, st := range Status(Settings{"sources": "duckduckgo,yahoo,brave"}) {
		by[st.Name] = st
	}
	if st := by["duckduckgo"]; !st.On || st.State != "ok" || st.LastOK.IsZero() {
		t.Fatalf("duckduckgo %+v", st)
	}
	if st := by["yahoo"]; st.State != "resting" || st.Reason != "rate-limited (HTTP 429)" || !st.Until.Equal(c.now().Add(backoffStart)) {
		t.Fatalf("yahoo %+v", st)
	}
	if st := by["brave"]; !st.On || st.State != "" {
		t.Fatalf("brave %+v", st)
	}
	if st := by["seznam"]; st.On || !strings.Contains(st.Off, "sources list") {
		t.Fatalf("seznam %+v", st)
	}
	// By default the walled sites are off and say why, and Marginalia wants its key.
	by = map[string]SourceStatus{}
	for _, st := range Status(nil) {
		by[st.Name] = st
	}
	if by["brave"].On || !strings.Contains(by["brave"].Off, "challenge") || by["marginalia"].On || !strings.Contains(by["marginalia"].Off, "key") || !by["seznam"].On {
		t.Fatalf("defaults %+v", by)
	}
	c.add(backoffStart)
	for _, st := range Status(nil) {
		if st.Name == "yahoo" && st.State != "failing" {
			t.Fatalf("after its rest: %+v", st)
		}
	}

	id, s := Configured("searxng", func(k string) string { return map[string]string{"search.searxng.url": " http://x "}[k] })
	if id != SearXNG || s["url"] != "http://x" {
		t.Fatalf("configured %s %v", id, s)
	}
	if id, s = Configured("nope", func(string) string { return "" }); id != Builtin || len(s) != 0 {
		t.Fatalf("configured %s %v", id, s)
	}
}
