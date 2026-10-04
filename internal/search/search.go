package search

import (
	"context"
	"fmt"
	"strings"

	"silo.agent/internal/registry"
	"silo.agent/internal/settingdef"
)

const (
	DuckDuckGoScraper = "duckduckgo_scraper"
	DefaultEngine     = DuckDuckGoScraper
	DefaultMaxResults = 8
	MaxResultsCap     = 20
)

type Hit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type Result struct {
	Results []Hit `json:"results"`
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
	Search(ctx context.Context, query string, maxResults int) (Result, error)
}

type factory func(Settings) Engine

var engines = newEngines()

func newEngines() *registry.Registry[Descriptor, factory] {
	r := registry.New[Descriptor, factory](func(d Descriptor) string { return d.ID })
	r.Put(Descriptor{
		ID:          DuckDuckGoScraper,
		Name:        "DuckDuckGo Scraper",
		Description: "Scrapes DuckDuckGo HTML results. No API key.",
	}, func(Settings) Engine { return NewDuckDuckGo() })
	return r
}

func Descriptors() []Descriptor { return engines.All() }

func Lookup(id string) (Descriptor, bool) {
	d, _, ok := engines.Lookup(strings.TrimSpace(id))
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
	id = strings.TrimSpace(id)
	if id == "" {
		id = DefaultEngine
	}
	if _, f, ok := engines.Lookup(id); ok {
		return f(settings), nil
	}
	return nil, fmt.Errorf("unknown search engine %q", id)
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
