package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"connectrpc.com/connect"
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/search"
)

func TestSearchEngineIDPrecedence(t *testing.T) {
	a := testApp(t, nil)
	if got := a.searchEngineID(); got != search.DefaultEngine {
		t.Fatalf("default %q", got)
	}
	a.Store = testStore(t)
	if err := os.WriteFile(a.Store.Path(), []byte("search:\n  engine: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := a.searchEngineID(); got != search.DefaultEngine {
		t.Fatalf("unknown yaml %q", got)
	}
	if err := a.Store.Patch(map[string]string{"search.engine": search.DuckDuckGoScraper}); err != nil {
		t.Fatal(err)
	}
	if got := a.searchEngineID(); got != search.DuckDuckGoScraper {
		t.Fatalf("yaml %q", got)
	}
}

func TestPutSettingsSearchEngine(t *testing.T) {
	a := testApp(t, nil)
	a.Store = testStore(t)
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u", Admin: true})
	got, err := a.GetSettings(ctx, connect.NewRequest(&v1.GetSettingsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if settingsField(got.Msg, "search.engine") != search.DuckDuckGoScraper || len(got.Msg.GetSearchEngines()) != 1 {
		t.Fatalf("%+v", got.Msg)
	}
	if got.Msg.SearchEngines[0].Name != "DuckDuckGo Scraper" || len(got.Msg.SearchEngines[0].Fields) != 0 {
		t.Fatalf("%+v", got.Msg.SearchEngines[0])
	}
	if _, err := a.PutSettings(ctx, connect.NewRequest(&v1.PutSettingsRequest{Fields: map[string]string{"search.engine": "nope"}})); err == nil {
		t.Fatal("unknown")
	}
	saved, err := a.PutSettings(ctx, connect.NewRequest(&v1.PutSettingsRequest{Fields: map[string]string{"search.engine": search.DuckDuckGoScraper}}))
	if err != nil || settingsField(saved.Msg, "search.engine") != search.DuckDuckGoScraper {
		t.Fatalf("%v %+v", err, saved)
	}
	if a.Store.Get("search.engine") != search.DuckDuckGoScraper {
		t.Fatal("persist")
	}
	raw, err := a.Store.YAML()
	if err != nil || !strings.Contains(string(raw), search.DuckDuckGoScraper) {
		t.Fatalf("yaml %s %v", raw, err)
	}
}

func TestMigrateSettingsFromSQLite(t *testing.T) {
	a := testApp(t, nil)
	a.Store = testStore(t)
	if err := a.DB.Exec("CREATE TABLE settings (key text primary key, value text)").Error; err != nil {
		t.Fatal(err)
	}
	if err := a.DB.Exec("INSERT INTO settings (key, value) VALUES ('model', 'openai/migrated')").Error; err != nil {
		t.Fatal(err)
	}
	a.migrateSettings()
	if a.cfg().Model != "openai/migrated" {
		t.Fatalf("model %q", a.cfg().Model)
	}
	raw, err := a.Store.YAML()
	if err != nil || !strings.Contains(string(raw), "openai/migrated") {
		t.Fatalf("yaml %s %v", raw, err)
	}
	if err := a.DB.Exec("UPDATE settings SET value = 'openai/ignored' WHERE key = 'model'").Error; err != nil {
		t.Fatal(err)
	}
	a.migrateSettings()
	if a.cfg().Model != "openai/migrated" {
		t.Fatalf("did not skip existing yaml %q", a.cfg().Model)
	}
}

func TestPutSettingsYAML(t *testing.T) {
	a := testApp(t, nil)
	a.Store = testStore(t)
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u", Admin: true})
	if _, err := a.PutSettings(ctx, connect.NewRequest(&v1.PutSettingsRequest{
		Yaml: "model: openai/gpt-yaml\nsearch:\n  engine: duckduckgo_scraper\n",
	})); err != nil {
		t.Fatal(err)
	}
	if a.cfg().Model != "openai/gpt-yaml" {
		t.Fatalf("model %q", a.cfg().Model)
	}
	if _, err := a.PutSettings(ctx, connect.NewRequest(&v1.PutSettingsRequest{
		Yaml:   "model: x\n",
		Fields: map[string]string{"model": "y"},
	})); err == nil {
		t.Fatal("both")
	}
}

func TestExecToolWebSearch(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Bot{ID: "b1"})
	srv := ddgFixtureServer(t)
	t.Cleanup(func() { search.DuckDuckGoURL = "https://html.duckduckgo.com/html/" })
	search.DuckDuckGoURL = srv.URL + "/"

	out, img, err := a.execTool(context.Background(), "b1", "run1", "web_search", `{"query":"alpha","max_results":2}`)
	if err != nil || img != "" {
		t.Fatalf("%v %q", err, img)
	}
	var res search.Result
	if json.Unmarshal([]byte(out), &res) != nil || len(res.Results) != 2 || res.Results[0].URL != "https://example.com/alpha" {
		t.Fatalf("%s", out)
	}

	a.DB.Create(&db.Rule{ID: "r1", BotID: "b1", Connector: "web", Action: "search", Decision: "deny"})
	_, _, err = a.execTool(context.Background(), "b1", "run1", "web_search", `{"query":"alpha"}`)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("%v", err)
	}
}

func TestCallToolWebSearch(t *testing.T) {
	a := testApp(t, nil)
	bot := &db.Bot{ID: "b1"}
	a.DB.Create(bot)
	srv := ddgFixtureServer(t)
	t.Cleanup(func() { search.DuckDuckGoURL = "https://html.duckduckgo.com/html/" })
	search.DuckDuckGoURL = srv.URL + "/"

	res, err := a.CallTool(botCtx(bot), connect.NewRequest(&v1.ToolReq{
		Connector: "web", Action: "search", ArgsJson: `{"query":"alpha"}`,
	}))
	if err != nil || res.Msg.GetError() != "" {
		t.Fatalf("%v %+v", err, res)
	}
	if !strings.Contains(res.Msg.GetResultJson(), "example.com/alpha") {
		t.Fatalf("%s", res.Msg.GetResultJson())
	}
}

func ddgFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	raw, err := os.ReadFile("../search/testdata/ddg_results.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	return srv
}
