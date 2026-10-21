package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// braveAPIURL is Brave's own Search API (an API key, a plan, a quota): the
// same index as the "brave" source of the Built-in Scraper, asked properly.
var braveAPIURL = "https://api.search.brave.com/res/v1/web/search"

// braveLang is how Brave spells a few languages that are not their ISO code.
// Brave answers 422 to a value it does not list, which braveAPI handles by
// asking again without the region.
var braveLang = map[string]string{"ja": "jp", "zh": "zh-hans", "pt": "pt-pt", "no": "nb"}

func newBraveAPI(s Settings) Engine {
	key := strings.TrimSpace(s["api_key"])
	return newKeyed("brave", func(ctx context.Context, q Query) ([]Hit, error) {
		if key == "" {
			return nil, errNoKey("Brave Search", "brave")
		}
		return braveAPI(ctx, key, q)
	})
}

func errNoKey(name, id string) error {
	return fmt.Errorf("%s has no API key: set search.%s.api_key (Admin → Settings → Search) or SILO_SEARCH__%s__API_KEY", name, id, strings.ToUpper(id))
}

func braveAPI(ctx context.Context, key string, q Query) ([]Hit, error) {
	query := url.Values{"q": {q.Text}, "count": {strconv.Itoa(q.Max)}}
	regional := false
	if country, lang := region(q); country != "" {
		query.Set("country", strings.ToUpper(country))
		if l := braveLang[lang]; l != "" {
			lang = l
		}
		query.Set("search_lang", lang)
		regional = true
	}
	call := func(query url.Values) (page, error) {
		return keyedCall{name: "Brave Search", rq: request{
			url: braveAPIURL, query: query,
			headers: map[string]string{"X-Subscription-Token": key, "Accept": "application/json"},
		}}.call(ctx)
	}
	p, err := call(query)
	var he *httpError
	if errors.As(err, &he) && regional && he.status == http.StatusUnprocessableEntity {
		// A country or language Brave does not list: search worldwide.
		query = url.Values{"q": {q.Text}, "count": {strconv.Itoa(q.Max)}}
		p, err = call(query)
	}
	if err != nil {
		return nil, err
	}
	var res struct {
		Web *struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(p.body, &res); err != nil {
		return nil, fmt.Errorf("Brave Search: not a search answer")
	}
	// No "web" section is a query Brave found no pages for.
	var out []Hit
	if res.Web != nil {
		for _, r := range res.Web.Results {
			title := plainText(r.Title)
			if title != "" && r.URL != "" {
				out = append(out, Hit{Title: title, URL: r.URL, Snippet: plainText(r.Description)})
			}
		}
	}
	return out, nil
}
