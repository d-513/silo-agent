package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/search"
)

type checkedSearch struct{}

func (checkedSearch) Search(_ context.Context, q search.Query) (search.Result, error) {
	if q.Text == "broken" {
		return search.Result{}, errors.New("the engine rejected the API key")
	}
	return search.Result{
		Results: []search.Hit{{Title: "One", URL: "https://one.test/", Engines: 2}},
		Busy:    []string{"yahoo"},
		Note:    "a note",
	}, nil
}

// TestCheckSearch is the admin's view of web search: the Built-in Scraper's
// sources with why one is off, and a test search with the saved engine whose
// failure is an answer, not an RPC error.
func TestCheckSearch(t *testing.T) {
	h := apptest.New(t)
	// Without a query nothing is searched: only the sources are listed.
	st, err := h.Client.CheckSearch(h.Ctx(), connect.NewRequest(&v1.CheckSearchRequest{}))
	if err != nil {
		t.Fatalf("CheckSearch: %v", err)
	}
	if st.Msg.GetEngine() != search.Builtin || st.Msg.GetTest() != nil {
		t.Fatalf("status %+v", st.Msg)
	}
	by := map[string]*v1.SearchSource{}
	for _, s := range st.Msg.GetSources() {
		by[s.GetName()] = s
	}
	if !by["duckduckgo"].GetOn() || !by["yahoo"].GetOn() || !by["seznam"].GetOn() || !by["wikipedia"].GetApi() {
		t.Fatalf("the default sources are not on: %+v", st.Msg.GetSources())
	}
	if by["brave"].GetOn() || !strings.Contains(by["brave"].GetOffReason(), "challenge") || by["marginalia"].GetOn() || !strings.Contains(by["marginalia"].GetOffReason(), "key") {
		t.Fatalf("brave %+v marginalia %+v", by["brave"], by["marginalia"])
	}

	stranger, _ := h.SignedInUser("eve@test.local")
	if _, err := stranger.CheckSearch(h.Ctx(), connect.NewRequest(&v1.CheckSearchRequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a non-admin read the search status: %v", err)
	}

	// Another engine has no sources to list; its test is one real search.
	search.Register(search.Descriptor{ID: "fake-check", Name: "Fake"}, func(search.Settings) search.Engine { return checkedSearch{} })
	t.Cleanup(func() { search.Unregister("fake-check") })
	h = apptest.New(t, apptest.WithYAML(strings.Replace(apptest.DefaultYAML(t.TempDir()), "engine: builtin", "engine: fake-check", 1)))
	got, err := h.Client.CheckSearch(h.Ctx(), connect.NewRequest(&v1.CheckSearchRequest{Query: " anything "}))
	if err != nil {
		t.Fatalf("CheckSearch: %v", err)
	}
	test := got.Msg.GetTest()
	if got.Msg.GetEngine() != "fake-check" || len(got.Msg.GetSources()) != 0 || len(test.GetHits()) != 1 ||
		test.GetHits()[0].GetUrl() != "https://one.test/" || test.GetHits()[0].GetEngines() != 2 ||
		test.GetNote() != "a note" || len(test.GetBusy()) != 1 || test.GetError() != "" {
		t.Fatalf("test %+v", got.Msg)
	}
	got, err = h.Client.CheckSearch(h.Ctx(), connect.NewRequest(&v1.CheckSearchRequest{Query: "broken"}))
	if err != nil || !strings.Contains(got.Msg.GetTest().GetError(), "rejected the API key") {
		t.Fatalf("a failed search: %+v %v", got.Msg, err)
	}
}
