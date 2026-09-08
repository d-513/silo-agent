package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"silo.agent/internal/search"
)

func (a *App) searchEngineID() string {
	if v := strings.TrimSpace(a.cfg().Search.Engine); search.Known(v) {
		return v
	}
	return search.DefaultEngine
}

func (a *App) searchEngineSettings() search.Settings {
	id := a.searchEngineID()
	d, ok := search.Lookup(id)
	if !ok || a.Store == nil {
		return nil
	}
	out := search.Settings{}
	for _, f := range d.Settings {
		if v := a.Store.Get("search." + id + "." + f.Key); v != "" {
			out[f.Key] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (a *App) runWebSearch(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if raw := strings.TrimSpace(argsJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "", fmt.Errorf("invalid args")
		}
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", fmt.Errorf("query required")
	}
	eng, err := search.New(a.searchEngineID(), a.searchEngineSettings())
	if err != nil {
		return "", err
	}
	res, err := eng.Search(ctx, args.Query, args.MaxResults)
	if err != nil {
		return "", err
	}
	if res.Results == nil {
		res.Results = []search.Hit{}
	}
	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
