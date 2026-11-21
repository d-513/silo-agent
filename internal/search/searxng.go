package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// searxng is the operator's own SearXNG instance: a metasearch whose scrapers
// its community keeps working. It is asked through its JSON format, which an
// instance serves only when its settings.yml lists it (search.formats).
type searxng struct {
	base string
	keep *cache
}

func newSearXNG(s Settings) Engine {
	return &searxng{base: strings.TrimRight(strings.TrimSpace(s["url"]), "/"), keep: sharedCache}
}

func (x *searxng) Search(ctx context.Context, q Query) (Result, error) {
	q = cleanQuery(q)
	if q.Text == "" {
		return Result{}, fmt.Errorf("query required")
	}
	if x.base == "" {
		return Result{}, fmt.Errorf("SearXNG is the search engine but its address is not set: an admin sets search.searxng.url")
	}
	var failure error
	f, err := x.keep.do(ctx, "searxng\x00"+x.base+"\x00"+cacheKey(q), func() found {
		f, err := x.ask(context.WithoutCancel(ctx), q)
		if err != nil {
			failure = err
			return found{blocked: []string{"searxng"}}
		}
		return f
	})
	if err != nil {
		return Result{}, err
	}
	if failure != nil {
		return Result{}, failure
	}
	if len(f.hits) == 0 && len(f.blocked) == 1 && f.blocked[0] == "searxng" {
		// Another caller's request failed while this one waited on it.
		return Result{}, fmt.Errorf("search failed: try again")
	}
	return f.result(q.Max), nil
}

func (x *searxng) ask(ctx context.Context, q Query) (found, error) {
	query := url.Values{"q": {q.Text}, "format": {"json"}, "pageno": {"1"}}
	if country, lang := region(q); lang != "" {
		query.Set("language", lang+"-"+strings.ToUpper(country))
	}
	p, err := fetch(ctx, request{keyed: true, url: x.base + "/search", query: query, headers: map[string]string{"Accept": "application/json"}})
	if err != nil {
		return found{}, fmt.Errorf("SearXNG at %s did not answer: %s", x.base, clip(err.Error(), 200))
	}
	switch p.status {
	case http.StatusOK:
	case http.StatusForbidden:
		return found{}, fmt.Errorf("SearXNG refused the JSON format (HTTP 403): add `json` to search.formats in its settings.yml")
	case http.StatusTooManyRequests:
		return found{}, fmt.Errorf("SearXNG's limiter refused this server (HTTP 429): let the control plane's address pass in its limiter.toml, or turn the limiter off")
	default:
		return found{}, fmt.Errorf("SearXNG answered HTTP %d", p.status)
	}
	var res struct {
		Results *[]struct {
			URL     string   `json:"url"`
			Title   string   `json:"title"`
			Content string   `json:"content"`
			Engines []string `json:"engines"`
		} `json:"results"`
		// Each is [engine, reason].
		Unresponsive [][]string `json:"unresponsive_engines"`
	}
	if err := json.Unmarshal(p.body, &res); err != nil || res.Results == nil {
		return found{}, fmt.Errorf("SearXNG at %s did not answer a search (is the address its root?)", x.base)
	}
	var out found
	seen := map[string]bool{}
	for _, r := range *res.Results {
		key := canonical(r.URL)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		title := strings.Join(strings.Fields(r.Title), " ")
		if title == "" {
			title = r.URL
		}
		out.hits = append(out.hits, Hit{Title: title, URL: r.URL, Snippet: clip(plainText(r.Content), snippetMax), Engines: max(len(r.Engines), 1)})
	}
	// With results in hand an engine of its that did not answer is its own
	// business; with none it is why.
	if len(out.hits) == 0 {
		for _, u := range res.Unresponsive {
			if len(u) > 0 && u[0] != "" {
				out.blocked = append(out.blocked, u[0])
			}
		}
	}
	return out, nil
}
