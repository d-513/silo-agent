package search

import (
	"context"
	"fmt"
	"strings"
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

type SettingDef struct {
	Key         string
	Label       string
	Type        string
	Description string
	Secret      bool
}

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

type registered struct {
	desc Descriptor
	new  factory
}

var registry = []registered{{
	desc: Descriptor{
		ID:          DuckDuckGoScraper,
		Name:        "DuckDuckGo Scraper",
		Description: "Scrapes DuckDuckGo HTML results. No API key.",
	},
	new: func(Settings) Engine { return NewDuckDuckGo() },
}}

func Descriptors() []Descriptor {
	out := make([]Descriptor, len(registry))
	for i, r := range registry {
		out[i] = r.desc
	}
	return out
}

func Lookup(id string) (Descriptor, bool) {
	id = strings.TrimSpace(id)
	for _, r := range registry {
		if r.desc.ID == id {
			return r.desc, true
		}
	}
	return Descriptor{}, false
}

func Known(id string) bool {
	_, ok := Lookup(id)
	return ok
}

// Register adds a search engine to the registry. It is the seam test and
// embedder packages use to plug in an engine without editing the built-in
// list; production code never calls it.
func Register(desc Descriptor, newFn func(Settings) Engine) {
	for i, r := range registry {
		if r.desc.ID == desc.ID {
			registry[i] = registered{desc: desc, new: newFn}
			return
		}
	}
	registry = append(registry, registered{desc: desc, new: newFn})
}

// Unregister removes a search engine from the registry.
func Unregister(id string) {
	id = strings.TrimSpace(id)
	out := registry[:0]
	for _, r := range registry {
		if r.desc.ID != id {
			out = append(out, r)
		}
	}
	registry = out
}

func New(id string, settings Settings) (Engine, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = DefaultEngine
	}
	for _, r := range registry {
		if r.desc.ID == id {
			return r.new(settings), nil
		}
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
