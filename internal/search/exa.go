package search

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// exaURL is Exa's search API: a neural index that returns the passages of
// each page that answer the query, which is what a snippet is here.
var exaURL = "https://api.exa.ai/search"

// exaSnippet is how many characters of highlights a hit carries.
const exaSnippet = 700

func newExa(s Settings) Engine {
	key := strings.TrimSpace(s["api_key"])
	return newKeyed("exa", func(ctx context.Context, q Query) ([]Hit, error) {
		if key == "" {
			return nil, errNoKey("Exa", "exa")
		}
		return exa(ctx, key, q)
	})
}

func exa(ctx context.Context, key string, q Query) ([]Hit, error) {
	body := map[string]any{
		"query":      q.Text,
		"numResults": q.Max,
		"type":       "auto",
		"contents":   map[string]any{"highlights": map[string]any{"maxCharacters": exaSnippet, "query": q.Text}},
	}
	// Exa has no language, only a place the searcher is in.
	if country, _ := region(q); country != "" {
		body["userLocation"] = strings.ToUpper(country)
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	p, err := keyedCall{name: "Exa", rq: request{
		method: "POST", url: exaURL, body: b,
		headers: map[string]string{"x-api-key": key, "Content-Type": "application/json", "Accept": "application/json"},
	}}.call(ctx)
	if err != nil {
		return nil, err
	}
	var res struct {
		Results *[]struct {
			Title      string   `json:"title"`
			URL        string   `json:"url"`
			Text       string   `json:"text"`
			Highlights []string `json:"highlights"`
		} `json:"results"`
	}
	if err := json.Unmarshal(p.body, &res); err != nil || res.Results == nil {
		return nil, fmt.Errorf("Exa: not a search answer")
	}
	var out []Hit
	for _, r := range *res.Results {
		if r.URL == "" {
			continue
		}
		snippet := strings.Join(r.Highlights, " … ")
		if snippet == "" {
			snippet = r.Text
		}
		snippet = strings.Join(strings.Fields(snippet), " ")
		if rs := []rune(snippet); len(rs) > exaSnippet {
			snippet = string(rs[:exaSnippet]) + "…"
		}
		title := strings.Join(strings.Fields(r.Title), " ")
		if title == "" {
			title = r.URL
		}
		out = append(out, Hit{Title: title, URL: r.URL, Snippet: snippet})
	}
	return out, nil
}
