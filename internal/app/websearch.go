package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"silo.agent/internal/search"
)

// searchEngine is the engine the operator picked, with its settings.
func (a *App) searchEngine() (search.Engine, error) {
	get := func(string) string { return "" }
	if a.Store != nil {
		get = a.Store.Get
	}
	id, settings := search.Configured(a.cfg().Search.Engine, get)
	return search.New(id, settings)
}

// searchAnswer is one query's result inside a batch.
type searchAnswer struct {
	Query string `json:"query"`
	search.Result
}

// runWebSearch answers one `query` with its result, or a list of `queries`
// with {"searches": [...]} in the order asked.
func (a *App) runWebSearch(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Query      string   `json:"query"`
		Queries    []string `json:"queries"`
		MaxResults int      `json:"max_results"`
		Language   string   `json:"language"`
		Country    string   `json:"country"`
	}
	if raw := strings.TrimSpace(argsJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "", fmt.Errorf("invalid args")
		}
	}
	one := strings.TrimSpace(args.Query) != ""
	var texts []string
	for _, q := range args.Queries {
		if q = strings.TrimSpace(q); q != "" {
			texts = append(texts, q)
		}
	}
	switch {
	case one && len(texts) > 0:
		return "", fmt.Errorf("give query or queries, not both")
	case one:
		texts = []string{strings.TrimSpace(args.Query)}
	case len(texts) == 0:
		return "", fmt.Errorf("query required")
	case len(texts) > search.MaxQueries:
		return "", fmt.Errorf("at most %d queries in one call", search.MaxQueries)
	}
	eng, err := a.searchEngine()
	if err != nil {
		return "", err
	}
	queries := make([]search.Query, len(texts))
	for i, t := range texts {
		queries[i] = search.Query{Text: t, Language: args.Language, Country: args.Country, Max: args.MaxResults}
	}
	var out any
	if one {
		res, err := eng.Search(ctx, queries[0])
		if err != nil {
			return "", err
		}
		if res.Results == nil {
			res.Results = []search.Hit{}
		}
		out = res
	} else {
		answers := make([]searchAnswer, len(texts))
		for i, res := range search.Many(ctx, eng, queries) {
			answers[i] = searchAnswer{Query: texts[i], Result: res}
		}
		out = map[string]any{"searches": answers}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
