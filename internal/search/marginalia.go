package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// marginaliaURL is Marginalia's API. The key goes in a header; the shared
// "public" key is over its quota most of the time, so the source is asked
// only with a key of the operator's own (free for non-commercial use).
var marginaliaURL = "https://api2.marginalia-search.com/search"

// marginalia is an independent index of the small, text-heavy web. It is
// English only.
func marginalia(key string) func(ctx context.Context, q Query) ([]Hit, error) {
	return func(ctx context.Context, q Query) ([]Hit, error) {
		body, err := get(ctx, q, request{api: true, url: marginaliaURL,
			query: url.Values{"query": {q.Text}, "count": {"10"}}, headers: map[string]string{"API-Key": key}})
		if err != nil {
			return nil, err
		}
		var res struct {
			Results *[]struct {
				URL         string `json:"url"`
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"results"`
		}
		if err := json.Unmarshal(body, &res); err != nil || res.Results == nil {
			return nil, fmt.Errorf("%w: not a search answer", errBlocked)
		}
		var out []Hit
		for _, r := range *res.Results {
			if r.Title != "" && r.URL != "" {
				out = append(out, Hit{Title: r.Title, URL: r.URL, Snippet: r.Description})
			}
		}
		return out, nil
	}
}
