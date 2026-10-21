package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// engine builds a keyed engine with a cache of its own, so one test's answers
// never serve another.
func engine(t *testing.T, id string, s Settings) Engine {
	t.Helper()
	e, err := New(id, s)
	if err != nil {
		t.Fatal(err)
	}
	e.(*keyed).keep = newCache(time.Now)
	return e
}

const braveAnswer = `{"web":{"results":[
 {"title":"Context · <strong>Go</strong>","url":"https://go.dev/blog/context","description":"Package <strong>context</strong> &amp; cancellation"},
 {"title":"","url":"https://empty.example","description":"no title"},
 {"title":"Second","url":"https://example.com/2","description":""}]}}`

func TestBraveAPISearch(t *testing.T) {
	p := serve(t, map[string]page{"api.search.brave.com": body(braveAnswer)})
	res, err := engine(t, Brave, Settings{"api_key": " k-secret-1234 "}).Search(bg, Query{Text: "go context", Language: "pl", Max: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 2 || res.Results[0].Title != "Context · Go" || res.Results[0].Snippet != "Package context & cancellation" {
		t.Fatalf("results %+v", res.Results)
	}
	rq := p.requests("api.search.brave.com")[0]
	if rq.headers["X-Subscription-Token"] != "k-secret-1234" || !rq.keyed {
		t.Fatalf("auth %+v", rq)
	}
	if q := rq.query; q.Get("q") != "go context" || q.Get("count") != "5" || q.Get("country") != "PL" || q.Get("search_lang") != "pl" {
		t.Fatalf("query %v", q)
	}
}

func TestBraveAPIWorldwideSendsNoRegion(t *testing.T) {
	p := serve(t, map[string]page{"api.search.brave.com": body(braveAnswer)})
	if _, err := engine(t, Brave, Settings{"api_key": "k"}).Search(bg, Query{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	q := p.requests("brave.com")[0].query
	if q.Has("country") || q.Has("search_lang") || q.Get("count") != "8" {
		t.Fatalf("query %v", q)
	}
}

func TestBraveAPIRetriesWithoutAnUnlistedRegion(t *testing.T) {
	var n atomic.Int32
	real := fetch
	t.Cleanup(func() { fetch = real })
	var seen []request
	fetch = func(_ context.Context, rq request) (page, error) {
		rq.query = cloneValues(rq.query)
		seen = append(seen, rq)
		if n.Add(1) == 1 {
			return page{status: http.StatusUnprocessableEntity, body: []byte(`{"error":{"message":"bad country"}}`)}, nil
		}
		return body(braveAnswer), nil
	}
	res, err := engine(t, Brave, Settings{"api_key": "k"}).Search(bg, Query{Text: "x", Language: "sw"})
	if err != nil || len(res.Results) != 2 || len(seen) != 2 {
		t.Fatalf("%v %+v %d", err, res, len(seen))
	}
	if seen[0].query.Get("country") == "" || seen[1].query.Has("country") || seen[1].query.Has("search_lang") {
		t.Fatalf("queries %v / %v", seen[0].query, seen[1].query)
	}
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func TestBraveAPINoWebSectionIsNoResults(t *testing.T) {
	serve(t, map[string]page{"brave.com": body(`{"type":"search","query":{}}`)})
	res, err := engine(t, Brave, Settings{"api_key": "k"}).Search(bg, Query{Text: "asdkjhqwe"})
	if err != nil || res.Results == nil || len(res.Results) != 0 {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestExaSearch(t *testing.T) {
	p := serve(t, map[string]page{"api.exa.ai": body(`{"requestId":"r","results":[
	  {"title":"Cancellation","url":"https://go.dev/a","highlights":["first  passage","second\npassage"]},
	  {"title":"","url":"https://go.dev/b","text":"only text"},
	  {"title":"no url","url":""}]}`)})
	res, err := engine(t, Exa, Settings{"api_key": "exa-secret-1234"}).Search(bg, Query{Text: "go context", Country: "de", Max: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 2 || res.Results[0].Snippet != "first passage … second passage" ||
		res.Results[1].Title != "https://go.dev/b" || res.Results[1].Snippet != "only text" {
		t.Fatalf("results %+v", res.Results)
	}
	rq := p.requests("api.exa.ai")[0]
	if rq.method != http.MethodPost || rq.headers["x-api-key"] != "exa-secret-1234" || !rq.keyed {
		t.Fatalf("request %+v", rq)
	}
	var sent struct {
		Query        string `json:"query"`
		NumResults   int    `json:"numResults"`
		UserLocation string `json:"userLocation"`
		Contents     struct {
			Highlights map[string]any `json:"highlights"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(rq.body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Query != "go context" || sent.NumResults != 3 || sent.UserLocation != "DE" || sent.Contents.Highlights["maxCharacters"] == nil {
		t.Fatalf("body %s", rq.body)
	}
}

func TestExaNotASearchAnswer(t *testing.T) {
	serve(t, map[string]page{"exa.ai": body(`<html>hi</html>`)})
	if _, err := engine(t, Exa, Settings{"api_key": "k"}).Search(bg, Query{Text: "x"}); err == nil {
		t.Fatal("a page is not a search answer")
	}
}

func TestKeyedEnginesSayWhatWentWrong(t *testing.T) {
	for _, tc := range []struct {
		id     string
		host   string
		status int
		want   string
	}{
		{Brave, "brave.com", http.StatusUnauthorized, "rejected the API key"},
		{Exa, "exa.ai", http.StatusForbidden, "search.exa.api_key"},
		{Exa, "exa.ai", http.StatusPaymentRequired, "no credit"},
		{Brave, "brave.com", http.StatusInternalServerError, "HTTP 500: upstream broke"},
	} {
		serve(t, map[string]page{tc.host: {status: tc.status, body: []byte(`{"error":"upstream broke"}`)}})
		_, err := engine(t, tc.id, Settings{"api_key": "the-secret-key"}).Search(bg, Query{Text: "x"})
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "the-secret-key") {
			t.Fatalf("%s %d: %v", tc.id, tc.status, err)
		}
	}
}

func TestKeyedEngineWithoutKey(t *testing.T) {
	p := serve(t, nil)
	for _, id := range []string{Brave, Exa} {
		_, err := engine(t, id, nil).Search(bg, Query{Text: "x"})
		if err == nil || !strings.Contains(err.Error(), "search."+id+".api_key") {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if len(p.seen) != 0 {
		t.Fatal("nothing should be asked without a key")
	}
}

func TestKeyedRateLimitIsRetriedOnce(t *testing.T) {
	var n atomic.Int32
	real := fetch
	t.Cleanup(func() { fetch = real })
	fetch = func(context.Context, request) (page, error) {
		if n.Add(1) == 1 {
			return page{status: http.StatusTooManyRequests, retryAfter: 5 * time.Millisecond}, nil
		}
		return body(braveAnswer), nil
	}
	if res, err := engine(t, Brave, Settings{"api_key": "k"}).Search(bg, Query{Text: "x"}); err != nil || len(res.Results) != 2 {
		t.Fatalf("%v %+v", err, res)
	}
	n.Store(0)
	fetch = func(context.Context, request) (page, error) {
		n.Add(1)
		return page{status: http.StatusTooManyRequests, retryAfter: time.Millisecond}, nil
	}
	_, err := engine(t, Exa, Settings{"api_key": "k"}).Search(bg, Query{Text: "y"})
	if err == nil || !strings.Contains(err.Error(), "rate limited") || n.Load() != 2 {
		t.Fatalf("%v after %d requests", err, n.Load())
	}
}

func TestKeyedAnswersAreCachedFailuresAreNot(t *testing.T) {
	p := serve(t, map[string]page{"brave.com": body(braveAnswer)})
	e := engine(t, Brave, Settings{"api_key": "k"})
	for i := 0; i < 3; i++ {
		if _, err := e.Search(bg, Query{Text: "  Same   Query ", Max: 4}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(p.requests("brave.com")); n != 1 {
		t.Fatalf("%d requests for one query", n)
	}
	// A different count is a different answer.
	if _, err := e.Search(bg, Query{Text: "same query", Max: 6}); err != nil || len(p.requests("brave.com")) != 2 {
		t.Fatalf("count must be part of the key: %v", err)
	}
	serve(t, map[string]page{"brave.com": {status: http.StatusInternalServerError}})
	f := engine(t, Brave, Settings{"api_key": "k"})
	for i := 0; i < 2; i++ {
		if _, err := f.Search(bg, Query{Text: "boom"}); err == nil {
			t.Fatal("expected an error")
		}
	}
}

func TestKeyedEnginesAreRegistered(t *testing.T) {
	for _, id := range []string{Brave, Exa} {
		d, ok := Lookup(id)
		if !ok || len(d.Settings) != 1 || d.Settings[0].Key != "api_key" || !d.Settings[0].Secret {
			t.Fatalf("%s: %+v", id, d)
		}
	}
}
