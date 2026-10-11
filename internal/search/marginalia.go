package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// marginaliaURL ends in the API key: "public" is the one everybody shares.
// Its quota is shared too and it answers 503 once that is spent, so
// Marginalia is an extra voice and nothing here depends on it.
var marginaliaURL = "https://api.marginalia.nu/public/search/"

// marginalia is an independent index of the small, text-heavy web. It is
// English only.
func marginalia(ctx context.Context, q Query) ([]Hit, error) {
	body, err := get(ctx, q, request{api: true, url: marginaliaURL + url.PathEscape(q.Text), query: url.Values{"count": {"10"}}})
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
