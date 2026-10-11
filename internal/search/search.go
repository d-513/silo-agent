// Package search is the Control Plane's web search: a registry of engines and
// the one that ships, the Built-in Scraper (a keyless metasearch, meta.go).
package search

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"silo.agent/internal/registry"
	"silo.agent/internal/settingdef"
)

const (
	Builtin           = "builtin"
	DefaultEngine     = Builtin
	DefaultMaxResults = 8
	MaxResultsCap     = 20
	// MaxQueries is how many queries one batch may carry.
	MaxQueries = 10

	// legacyDuckDuckGo is the engine before the Built-in Scraper; a silo.yaml
	// that still names it means the built-in.
	legacyDuckDuckGo = "duckduckgo_scraper"
	// manyParallel is how many queries of a batch run at once.
	manyParallel = 4
)

// Query is one search. Language (ISO 639-1, "pl") and Country (ISO 3166
// alpha-2, "pl") are optional and say where the results should come from.
type Query struct {
	Text     string
	Language string
	Country  string
	Max      int
}

// Hit is one result. Engines is how many independent sources returned it:
// agreement between them is a cheap sign of relevance.
type Hit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Engines int    `json:"engines,omitempty"`
}

// Result is what a search answers. Blocked names the sources that were
// blocked, failed or timed out; Note is set when there are no results because
// of them, so "nothing came back" is never read as "nothing exists".
type Result struct {
	Results []Hit    `json:"results"`
	Blocked []string `json:"blocked,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// SettingDef describes one configurable engine setting for the admin UI.
type SettingDef = settingdef.Def

type Descriptor struct {
	ID          string
	Name        string
	Description string
	Settings    []SettingDef
}

type Settings map[string]string

type Engine interface {
	Search(ctx context.Context, q Query) (Result, error)
}

type factory func(Settings) Engine

var engines = newEngines()

func newEngines() *registry.Registry[Descriptor, factory] {
	r := registry.New[Descriptor, factory](func(d Descriptor) string { return d.ID })
	r.Put(Descriptor{
		ID:          Builtin,
		Name:        "Built-in Scraper",
		Description: "Asks several search engines at once and merges what they return. No API key.",
		Settings: []SettingDef{{
			Key:   "sources",
			Label: "Sources",
			Description: "Comma-separated sources to ask; empty asks all of them: " + strings.Join(sourceNames(), ", ") +
				". Marginalia's public key is a quota shared with everyone and its data is CC-BY-NC-SA.",
		}},
	}, func(s Settings) Engine { return newMeta(s) })
	return r
}

func Descriptors() []Descriptor { return engines.All() }

// ID is the engine an id names: trimmed, and an engine from before the
// Built-in Scraper reads as the built-in.
func ID(id string) string {
	id = strings.TrimSpace(id)
	if id == legacyDuckDuckGo {
		return Builtin
	}
	return id
}

func Lookup(id string) (Descriptor, bool) {
	d, _, ok := engines.Lookup(ID(id))
	return d, ok
}

func Known(id string) bool {
	_, ok := Lookup(id)
	return ok
}

// Register adds a search engine to the registry. It is the seam test and
// embedder packages use to plug in an engine without editing the built-in
// list; production code never calls it.
func Register(desc Descriptor, newFn func(Settings) Engine) { engines.Put(desc, newFn) }

// Unregister removes a search engine from the registry.
func Unregister(id string) { engines.Remove(strings.TrimSpace(id)) }

func New(id string, settings Settings) (Engine, error) {
	id = ID(id)
	if id == "" {
		id = DefaultEngine
	}
	if _, f, ok := engines.Lookup(id); ok {
		return f(settings), nil
	}
	return nil, fmt.Errorf("unknown search engine %q", id)
}

// Many runs several queries on one engine, a few at a time, and answers in
// the order asked. A query that fails outright is a Result with the reason as
// its Note, so one bad query does not cost the others.
func Many(ctx context.Context, eng Engine, queries []Query) []Result {
	out := make([]Result, len(queries))
	slots := make(chan struct{}, manyParallel)
	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			res, err := eng.Search(ctx, q)
			if err != nil {
				res = Result{Note: "search failed: " + err.Error()}
			}
			if res.Results == nil {
				res.Results = []Hit{}
			}
			out[i] = res
		}()
	}
	wg.Wait()
	return out
}

func ClampMax(n int) int {
	if n <= 0 {
		return DefaultMaxResults
	}
	if n > MaxResultsCap {
		return MaxResultsCap
	}
	return n
}
