package search

import (
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// pages replaces the network for a test: each request is answered by the
// entry whose key is part of its URL, and every request is recorded.
type pages struct {
	mu   sync.Mutex
	seen []request
}

func (p *pages) requests(part string) []request {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []request
	for _, r := range p.seen {
		if strings.Contains(r.url, part) {
			out = append(out, r)
		}
	}
	return out
}

func serve(t *testing.T, answers map[string]page) *pages {
	t.Helper()
	p := &pages{}
	real := fetch
	fetch = func(_ context.Context, rq request) (page, error) {
		p.mu.Lock()
		p.seen = append(p.seen, rq)
		p.mu.Unlock()
		// The longest key wins, so a path can be told from its host.
		best := ""
		for part := range answers {
			if strings.Contains(rq.url, part) && len(part) > len(best) {
				best = part
			}
		}
		if best == "" {
			return page{status: http.StatusNotFound}, nil
		}
		return answers[best], nil
	}
	ddgPages = newBreaker(time.Now)
	startpageSC.value = ""
	t.Cleanup(func() { fetch = real })
	return p
}

func fixture(t *testing.T, name string) page {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return page{status: http.StatusOK, body: b}
}

func body(s string) page { return page{status: http.StatusOK, body: []byte(s)} }

var bg = context.Background()

func TestDuckDuckGoReadsTheHTMLPage(t *testing.T) {
	p := serve(t, map[string]page{"html.duckduckgo.com": fixture(t, "ddg_html.html")})
	hits, err := duckduckgo(bg, Query{Text: "golang context cancellation"})
	if err != nil || len(hits) != 10 {
		t.Fatalf("hits %d err %v", len(hits), err)
	}
	if hits[0].URL != "https://go.dev/doc/database/cancel-operations" || hits[0].Title == "" || hits[0].Snippet == "" {
		t.Fatalf("first hit %+v", hits[0])
	}
	for _, h := range hits {
		if strings.Contains(h.URL, "duckduckgo.com") {
			t.Fatalf("redirect or ad link kept: %s", h.URL)
		}
	}
	rq := p.requests("html.duckduckgo.com")[0]
	if rq.method != http.MethodPost || rq.form.Get("q") != "golang context cancellation" || rq.form.Get("kl") != "wt-wt" {
		t.Fatalf("request %+v", rq)
	}
	if len(p.requests("lite.")) != 0 {
		t.Fatal("the lite page is only for when the html page fails")
	}
}

func TestDuckDuckGoRegion(t *testing.T) {
	p := serve(t, map[string]page{"html.duckduckgo.com": fixture(t, "ddg_html.html")})
	for q, want := range map[Query]string{
		{Text: "x", Language: "pl"}:                "pl-pl",
		{Text: "x", Language: "en"}:                "us-en",
		{Text: "x", Language: "de", Country: "at"}: "at-de",
		{Text: "x", Country: "gb"}:                 "gb-en",
	} {
		if _, err := duckduckgo(bg, q); err != nil {
			t.Fatal(err)
		}
		rqs := p.requests("html.")
		if got := rqs[len(rqs)-1]; got.form.Get("kl") != want || got.lang != q.Language && q.Language != "" {
			t.Fatalf("%+v: kl %q want %q (lang %q)", q, got.form.Get("kl"), want, got.lang)
		}
	}
}

func TestDuckDuckGoFallsBackToLite(t *testing.T) {
	// The html page is broken, not rate-limited.
	p := serve(t, map[string]page{
		"html.duckduckgo.com": {status: http.StatusInternalServerError},
		"lite.duckduckgo.com": fixture(t, "ddg_lite.html"),
	})
	hits, err := duckduckgo(bg, Query{Text: "golang context cancellation"})
	if err != nil || len(hits) != 10 {
		t.Fatalf("hits %d err %v", len(hits), err)
	}
	if hits[0].URL != "https://go.dev/doc/database/cancel-operations" || hits[0].Snippet == "" {
		t.Fatalf("first hit %+v", hits[0])
	}
	// The html page is now in its backoff: the next search goes straight to lite.
	if _, err := duckduckgo(bg, Query{Text: "again"}); err != nil {
		t.Fatal(err)
	}
	if n := len(p.requests("html.")); n != 1 {
		t.Fatalf("html page asked %d times, want 1", n)
	}
}

func TestDuckDuckGoBlocked(t *testing.T) {
	// A 202 is DuckDuckGo's "too many requests". The lite page shares the
	// limit, so it is not asked on top.
	p := serve(t, map[string]page{"duckduckgo.com": {status: http.StatusAccepted, retryAfter: time.Minute}})
	_, err := duckduckgo(bg, Query{Text: "x"})
	if after, limited := rateLimited(err); !errors.Is(err, errBlocked) || !limited || after != time.Minute {
		t.Fatalf("202: %v", err)
	}
	if len(p.seen) != 1 {
		t.Fatalf("%d requests to a rate-limited DuckDuckGo, want 1", len(p.seen))
	}
	// The "select all ducks" challenge served as a 200.
	serve(t, map[string]page{"duckduckgo.com": fixture(t, "ddg_blocked.html")})
	if _, err := duckduckgo(bg, Query{Text: "x"}); !errors.Is(err, errBlocked) {
		t.Fatalf("challenge page: %v", err)
	}
}

func TestDuckDuckGoNoResultsIsNotBlocked(t *testing.T) {
	serve(t, map[string]page{"html.duckduckgo.com": body(`<div class="serp__results"><div class="no-results">No results.</div></div>`)})
	if hits, err := duckduckgo(bg, Query{Text: "zzqqxx"}); err != nil || len(hits) != 0 {
		t.Fatalf("hits %v err %v", hits, err)
	}
	serve(t, map[string]page{
		"html.duckduckgo.com": {status: http.StatusInternalServerError},
		"lite.duckduckgo.com": body(`<form action="/lite/" method="post"><input name="q"></form><table><tr><td>No results.</td></tr></table>`),
	})
	if hits, err := duckduckgo(bg, Query{Text: "zzqqxx"}); err != nil || len(hits) != 0 {
		t.Fatalf("lite: hits %v err %v", hits, err)
	}
}

func TestBrave(t *testing.T) {
	p := serve(t, map[string]page{"search.brave.com": fixture(t, "brave.html")})
	hits, err := brave(bg, Query{Text: "najlepsze pierogi w Krakowie", Language: "pl"})
	if err != nil || len(hits) < 10 {
		t.Fatalf("hits %d err %v", len(hits), err)
	}
	if hits[0].URL != "https://ambalaz.pl/blog/pierogi-krakow" || !strings.Contains(hits[0].Title, "Pierogi w Krakowie") || hits[0].Snippet == "" {
		t.Fatalf("first hit %+v", hits[0])
	}
	rq := p.requests("brave")[0]
	if rq.query.Get("q") != "najlepsze pierogi w Krakowie" || rq.cookies["country"] != "pl" || rq.lang != "pl" {
		t.Fatalf("request %+v", rq)
	}
	serve(t, map[string]page{"search.brave.com": {status: http.StatusTooManyRequests}})
	if _, err := brave(bg, Query{Text: "x"}); !errors.Is(err, errBlocked) {
		t.Fatalf("429: %v", err)
	}
	serve(t, map[string]page{"search.brave.com": body(`<html><body><h1>Verify you are human</h1></body></html>`)})
	if _, err := brave(bg, Query{Text: "x"}); !errors.Is(err, errBlocked) {
		t.Fatalf("challenge: %v", err)
	}
}

const mojeekPage = `<html><body><ul class="results-standard">
<li><a class="ob" href="https://go.dev/blog/context">go.dev</a><h2><a class="title" href="https://go.dev/blog/context">Go Concurrency Patterns: Context</a></h2><p class="s">In Go servers, each incoming request is handled in its own goroutine.</p></li>
<li><h2><a class="title" href="https://pkg.go.dev/context">context package</a></h2><p class="s">Package context defines the Context type.</p></li>
</ul></body></html>`

func TestMojeek(t *testing.T) {
	p := serve(t, map[string]page{"mojeek.com": body(mojeekPage)})
	hits, err := mojeek(bg, Query{Text: "go context", Language: "de", Country: "at"})
	if err != nil || len(hits) != 2 {
		t.Fatalf("hits %v err %v", hits, err)
	}
	want := Hit{Title: "Go Concurrency Patterns: Context", URL: "https://go.dev/blog/context", Snippet: "In Go servers, each incoming request is handled in its own goroutine."}
	if hits[0] != want {
		t.Fatalf("first hit %+v", hits[0])
	}
	if rq := p.requests("mojeek")[0]; rq.cookies["arc"] != "at" || rq.cookies["lb"] != "de" || rq.query.Get("q") != "go context" {
		t.Fatalf("request %+v", rq)
	}
	// Without a region no cookie is sent at all.
	_, _ = mojeek(bg, Query{Text: "go context"})
	if rq := p.requests("mojeek")[1]; len(rq.cookies) != 0 {
		t.Fatalf("cookies %v", rq.cookies)
	}
	serve(t, map[string]page{"mojeek.com": fixture(t, "mojeek_captcha.html")})
	if _, err := mojeek(bg, Query{Text: "x"}); !errors.Is(err, errBlocked) {
		t.Fatalf("captcha: %v", err)
	}
	serve(t, map[string]page{"mojeek.com": body(`<div class="results"><p>No pages found matching: zzqqxx</p></div>`)})
	if hits, err := mojeek(bg, Query{Text: "zzqqxx"}); err != nil || len(hits) != 0 {
		t.Fatalf("no results: %v %v", hits, err)
	}
}

const (
	startpageHomePage = `<html><body><form id="search" action="/sp/search" method="post"><input name="query"><input type="hidden" name="sc" value="tok123"></form></body></html>`
	startpageResults  = `<html><body><section>
<div class="result css-1"><a class="result-title result-link" href="https://go.dev/blog/context"><h2 class="wgl-title">Go Concurrency Patterns: Context</h2></a><p class="description">Each incoming request is handled in its own goroutine.</p></div>
<div class="result css-1"><a href="https://pkg.go.dev/context"><h2>context package</h2></a><p>Package context defines the Context type.</p></div>
<div class="result-sidebar"><p>not a result</p></div>
</section></body></html>`
)

func TestStartpage(t *testing.T) {
	p := serve(t, map[string]page{"startpage.com/sp/search": body(startpageResults), "startpage.com": body(startpageHomePage)})
	hits, err := startpage(bg, Query{Text: "go context", Language: "pl"})
	if err != nil || len(hits) != 2 {
		t.Fatalf("hits %v err %v", hits, err)
	}
	want := Hit{Title: "Go Concurrency Patterns: Context", URL: "https://go.dev/blog/context", Snippet: "Each incoming request is handled in its own goroutine."}
	if hits[0] != want {
		t.Fatalf("first hit %+v", hits[0])
	}
	rq := p.requests("/sp/search")[0]
	if rq.form.Get("sc") != "tok123" || rq.form.Get("query") != "go context" || rq.form.Get("qsr") != "pl_PL" {
		t.Fatalf("form %v", rq.form)
	}
	// The form token is reused: the second search is one request.
	if _, err := startpage(bg, Query{Text: "again"}); err != nil {
		t.Fatal(err)
	}
	if n := len(p.seen); n != 3 {
		t.Fatalf("%d requests for two searches, want 3", n)
	}
}

func TestStartpageBlocked(t *testing.T) {
	serve(t, map[string]page{"startpage.com": fixture(t, "startpage_blocked.html")})
	if _, err := startpage(bg, Query{Text: "x"}); !errors.Is(err, errBlocked) {
		t.Fatalf("blocked home page: %v", err)
	}
	// A results page without results drops the token, so the next search asks for a new one.
	p := serve(t, map[string]page{"startpage.com/sp/search": body(`<html><body>robot?</body></html>`), "startpage.com": body(startpageHomePage)})
	if _, err := startpage(bg, Query{Text: "x"}); !errors.Is(err, errBlocked) {
		t.Fatalf("blocked results: %v", err)
	}
	_, _ = startpage(bg, Query{Text: "x"})
	if n := len(p.seen); n != 4 {
		t.Fatalf("%d requests, want the token fetched again (4)", n)
	}
}

func TestWikipedia(t *testing.T) {
	p := serve(t, map[string]page{"wikipedia.org": fixture(t, "wikipedia.json")})
	hits, err := wikipedia(bg, Query{Text: "pierogi", Language: "pl"})
	if err != nil || len(hits) != 3 {
		t.Fatalf("hits %v err %v", hits, err)
	}
	// In the order Wikipedia ranked them, not the order of its JSON object.
	if hits[0].Title != "Pierogi - Wikipedia" || hits[0].URL != "https://pl.wikipedia.org/wiki/Pierogi" || !strings.HasPrefix(hits[0].Snippet, "Pierogi – ogólnie") {
		t.Fatalf("first hit %+v", hits[0])
	}
	if hits[2].URL != "https://pl.wikipedia.org/wiki/Pierogi_ruskie" {
		t.Fatalf("third hit %+v", hits[2])
	}
	rq := p.requests("wikipedia")[0]
	if !rq.api || !strings.HasPrefix(rq.url, "https://pl.wikipedia.org/") || rq.query.Get("gpssearch") != "pierogi" {
		t.Fatalf("request %+v", rq)
	}
	_, _ = wikipedia(bg, Query{Text: "x"})
	if rq := p.requests("wikipedia")[1]; !strings.HasPrefix(rq.url, "https://en.wikipedia.org/") {
		t.Fatalf("default language: %s", rq.url)
	}

	// A disambiguation page is left out.
	serve(t, map[string]page{"wikipedia.org": fixture(t, "wikipedia_disambiguation.json")})
	hits, err = wikipedia(bg, Query{Text: "mercury"})
	if err != nil || len(hits) != 2 || hits[0].Title != "Freddie Mercury - Wikipedia" {
		t.Fatalf("hits %+v err %v", hits, err)
	}

	serve(t, map[string]page{"wikipedia.org": {status: http.StatusForbidden}})
	if _, err := wikipedia(bg, Query{Text: "x"}); !errors.Is(err, errBlocked) {
		t.Fatalf("403: %v", err)
	}
	serve(t, map[string]page{"wikipedia.org": body("<html>Wikimedia Error</html>")})
	if _, err := wikipedia(bg, Query{Text: "x"}); !errors.Is(err, errBlocked) {
		t.Fatalf("error page: %v", err)
	}
	// No article by that title is a true empty answer.
	serve(t, map[string]page{"wikipedia.org": body(`{"batchcomplete":""}`)})
	if hits, err := wikipedia(bg, Query{Text: "zzqqxx"}); err != nil || len(hits) != 0 {
		t.Fatalf("no results: %v %v", hits, err)
	}
}

func TestMarginalia(t *testing.T) {
	p := serve(t, map[string]page{"marginalia": fixture(t, "marginalia.json")})
	hits, err := marginalia(bg, Query{Text: "golang context cancellation"})
	if err != nil || len(hits) == 0 {
		t.Fatalf("hits %v err %v", hits, err)
	}
	if hits[0].URL != "https://golangbot.com/context-timeout-cancellation/" || hits[0].Snippet == "" {
		t.Fatalf("first hit %+v", hits[0])
	}
	if rq := p.requests("marginalia")[0]; !rq.api || !strings.HasSuffix(rq.url, "/public/search/golang%20context%20cancellation") {
		t.Fatalf("request %+v", rq)
	}
	// The shared public key ran out of quota.
	serve(t, map[string]page{"marginalia": {status: http.StatusServiceUnavailable}})
	if _, err := marginalia(bg, Query{Text: "x"}); !errors.Is(err, errBlocked) {
		t.Fatalf("503: %v", err)
	}
	if !notEnglish(Query{Language: "pl"}) || notEnglish(Query{}) || notEnglish(Query{Language: "en"}) {
		t.Fatal("marginalia is asked for English and unspecified queries only")
	}
}

// TestFetch runs the real client against a local server: the method, the
// query, the form, the cookies and the language all arrive, with the headers
// a browser sends for that kind of navigation.
func TestFetch(t *testing.T) {
	var got *http.Request
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_ = r.ParseForm()
		form = r.PostForm.Encode()
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "1"})
		w.Header().Set("Retry-After", "90")
		// A browser asks for compressed pages, so it has to read them.
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusAccepted)
		zw := gzip.NewWriter(w)
		_, _ = zw.Write([]byte("hello"))
		_ = zw.Close()
	}))
	defer srv.Close()

	p, err := fetch(bg, request{
		method: http.MethodPost, url: srv.URL + "/s", query: map[string][]string{"a": {"1"}},
		form: map[string][]string{"q": {"zażółć"}}, cookies: map[string]string{"arc": "pl"},
		headers: map[string]string{"referer": srv.URL + "/"}, lang: "pl", country: "pl",
	})
	if err != nil || p.status != http.StatusAccepted || string(p.body) != "hello" || len(p.cookies) != 1 || p.retryAfter != 90*time.Second {
		t.Fatalf("page %+v err %v", p, err)
	}
	if got.Method != http.MethodPost || got.URL.RawQuery != "a=1" || form != "q=za%C5%BC%C3%B3%C5%82%C4%87" {
		t.Fatalf("request %s %s form %q", got.Method, got.URL, form)
	}
	if c, _ := got.Cookie("arc"); c == nil || c.Value != "pl" {
		t.Fatalf("cookie %v", got.Cookies())
	}
	for k, want := range map[string]string{
		"Accept-Language": "pl-PL,pl;q=0.9,en;q=0.8",
		"Accept-Encoding": "gzip, deflate, br, zstd",
		"User-Agent":      chromeUA,
		"Sec-Ch-Ua":       chromeBrands,
		"Accept":          chromeAccept,
		"Priority":        "u=0, i",
		"Referer":         srv.URL + "/",
		// A form posted from a page of the same site.
		"Sec-Fetch-Site": "same-origin",
		"Origin":         srv.URL,
		"Cache-Control":  "max-age=0",
	} {
		if got.Header.Get(k) != want {
			t.Fatalf("%s: %q want %q", k, got.Header.Get(k), want)
		}
	}
	if got.Header.Get("Pragma") != "" {
		t.Fatal("a navigation does not send Pragma: that is a hard reload")
	}

	// A second request carries no cookie from the first. An address typed
	// into the bar has no referer, no origin and no cache-control.
	if _, err := fetch(bg, request{url: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if len(got.Cookies()) != 0 || got.Header.Get("Sec-Fetch-Site") != "none" || got.Header.Get("Origin") != "" || got.Header.Get("Cache-Control") != "" {
		t.Fatalf("cookies %v headers %v", got.Cookies(), got.Header)
	}
	if got.Header.Get("Accept-Language") != "en-US,en;q=0.9" {
		t.Fatalf("accept-language %q", got.Header.Get("Accept-Language"))
	}

	// An API is asked under our own name, without a browser's headers.
	if _, err := fetch(bg, request{url: srv.URL, api: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Header.Get("User-Agent"), "SiloAgent/") || got.Header.Get("Sec-Ch-Ua") != "" || got.Header.Get("Sec-Fetch-Site") != "" {
		t.Fatalf("api headers %v", got.Header)
	}
}

func TestNavigation(t *testing.T) {
	for _, c := range []struct{ method, ref, to, site, origin string }{
		{"GET", "", "https://search.brave.com/search", "none", ""},
		{"POST", "https://html.duckduckgo.com/", "https://html.duckduckgo.com/html/", "same-origin", "https://html.duckduckgo.com"},
		{"GET", "https://www.startpage.com/", "https://www.startpage.com/sp/search", "same-origin", ""},
		{"GET", "https://duckduckgo.com/", "https://html.duckduckgo.com/html/", "same-site", ""},
		{"POST", "https://example.org/", "https://www.mojeek.com/search", "cross-site", "https://example.org"},
	} {
		rq := request{method: c.method, url: c.to}
		if c.ref != "" {
			rq.headers = map[string]string{"referer": c.ref}
		}
		got := navigation(rq)
		if got["sec-fetch-site"] != c.site || got["origin"] != c.origin || (got["cache-control"] != "") != (c.origin != "") {
			t.Fatalf("%s %s from %q: %v", c.method, c.to, c.ref, got)
		}
	}
	if retryAfter("120") != 2*time.Minute || retryAfter("") != 0 || retryAfter("Wed, 21 Oct 2026 07:28:00 GMT") != 0 || retryAfter("-5") != 0 {
		t.Fatal("retryAfter")
	}
}
