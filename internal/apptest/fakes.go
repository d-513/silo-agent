package apptest

import (
	"context"
	"fmt"
	"sync"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/search"
)

// FakeAdapter is an in-process channels.Adapter for feature tests. It records
// every outbound message so a test can assert what a channel run delivered.
type FakeAdapter struct {
	Slug     string
	Name     string
	Requires bool
	Fields   []channels.Field

	mu           sync.Mutex
	Sent         []channels.Outbound
	ValidateFunc func(ctx context.Context, ch *db.Channel, cfg channels.Config) (channels.State, error)
}

func NewFakeAdapter(slug string) *FakeAdapter {
	return &FakeAdapter{Slug: slug, Name: slug,
		Fields: []channels.Field{{Key: "token", Label: "Token", Type: channels.FieldSecret, Required: true}}}
}

func (f *FakeAdapter) Descriptor() channels.Descriptor {
	return channels.Descriptor{
		Slug: f.Slug, Name: f.Name, Description: "fake adapter",
		RequiresTarget: f.Requires, Fields: f.Fields,
	}
}

func (f *FakeAdapter) Validate(ctx context.Context, ch *db.Channel, cfg channels.Config) (channels.State, error) {
	if f.ValidateFunc != nil {
		return f.ValidateFunc(ctx, ch, cfg)
	}
	return channels.State{Kind: channels.StateInfo, Message: "ready"}, nil
}

func (f *FakeAdapter) Start(ctx context.Context, _ *db.Channel, _ channels.Config, _ channels.Host) error {
	<-ctx.Done()
	return nil
}

func (f *FakeAdapter) Send(_ context.Context, _ *db.Channel, _ channels.Config, msg channels.Outbound) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Sent = append(f.Sent, msg)
	return nil
}

func (f *FakeAdapter) History(_ context.Context, _ *db.Channel, _ channels.Config, _ string, _ int) ([]channels.Message, error) {
	return nil, nil
}

func (f *FakeAdapter) Action(_ context.Context, _ *db.Channel, _ channels.Config, action string, _ map[string]string) (channels.State, error) {
	return channels.State{Kind: channels.StateInfo, Message: action}, nil
}

// Messages returns a copy of everything the adapter delivered.
func (f *FakeAdapter) Messages() []channels.Outbound {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]channels.Outbound(nil), f.Sent...)
}

// FakeSearch is a deterministic search.Engine.
type FakeSearch struct {
	mu    sync.Mutex
	Hits  []search.Hit
	Calls []string
}

func (f *FakeSearch) Search(_ context.Context, query string, maxResults int) (search.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, query)
	hits := f.Hits
	if len(hits) == 0 {
		hits = []search.Hit{{Title: "Result for " + query, URL: "https://example.test/" + query, Snippet: "snippet"}}
	}
	if maxResults > 0 && len(hits) > maxResults {
		hits = hits[:maxResults]
	}
	return search.Result{Results: hits}, nil
}

// RegisterFakeSearch installs a fake search engine under id.
func RegisterFakeSearch(id string, eng *FakeSearch) {
	search.Register(search.Descriptor{ID: id, Name: "Fake", Description: "fake engine"},
		func(search.Settings) search.Engine { return eng })
}

// UniqueSlug keeps adapter slugs from colliding across tests in one binary.
var slugSeq int
var slugMu sync.Mutex

func UniqueSlug(prefix string) string {
	slugMu.Lock()
	defer slugMu.Unlock()
	slugSeq++
	return fmt.Sprintf("%s-%d", prefix, slugSeq)
}
