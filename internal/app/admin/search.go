package admin

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/search"
)

// testTimeout bounds the search an admin runs from the settings page.
const testTimeout = 30 * time.Second

// testHits is how many results of that search are shown.
const testHits = 5

// CheckSearch is the web search engine's health for an admin: the Built-in
// Scraper's sources as this process has seen them since it started and, with
// a query, one real search with the saved settings. A search that fails is an
// answer here (SearchTest.error), never an error of the RPC.
func (s *Service) CheckSearch(ctx context.Context, req *connect.Request[v1.CheckSearchRequest]) (*connect.Response[v1.CheckSearchResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	get := func(string) string { return "" }
	if s.store != nil {
		get = s.store.Get
	}
	id, settings := search.Configured(s.cfg().Search.Engine, get)
	out := &v1.CheckSearchResponse{Engine: id}
	if query := strings.TrimSpace(req.Msg.GetQuery()); query != "" {
		out.Test = runSearchTest(ctx, id, settings, query)
	}
	// After the test, so the page shows what it just did to the sources.
	if id == search.Builtin {
		for _, st := range search.Status(settings) {
			out.Sources = append(out.Sources, &v1.SearchSource{
				Name: st.Name, Api: st.API, On: st.On, OffReason: st.Off, State: st.State, Reason: st.Reason,
				RestingUntil: stamp(st.Until), LastOk: stamp(st.LastOK), LastFailed: stamp(st.LastFailed),
			})
		}
	}
	return connect.NewResponse(out), nil
}

func runSearchTest(ctx context.Context, id string, settings search.Settings, query string) *v1.SearchTest {
	out := &v1.SearchTest{}
	eng, err := search.New(id, settings)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	start := time.Now()
	res, err := eng.Search(ctx, search.Query{Text: query, Max: testHits})
	out.TookMs = int32(time.Since(start).Milliseconds())
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Blocked, out.Busy, out.Note = res.Blocked, res.Busy, res.Note
	for _, h := range res.Results {
		out.Hits = append(out.Hits, &v1.SearchTestHit{Title: h.Title, Url: h.URL, Engines: int32(h.Engines)})
	}
	return out
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
