package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFromYAMLDefaultsAndOverrides(t *testing.T) {
	t.Setenv("SILO_MODEL", "openrouter/from-env")
	s, err := FromYAML([]byte("model: dummy/echo\nmodels:\n  - dummy/echo\n"))
	if err != nil {
		t.Fatal(err)
	}
	c := s.Config()
	if c.Model != "dummy/echo" {
		t.Fatalf("model %q", c.Model)
	}
	// FromYAML must ignore the environment; that is the whole point.
	if c.Model == "openrouter/from-env" {
		t.Fatal("FromYAML read SILO_* environment")
	}
	if c.Search.Engine != "duckduckgo_scraper" {
		t.Fatalf("default search engine %q", c.Search.Engine)
	}
	if c.DataDir != "./data" {
		t.Fatalf("default data dir %q", c.DataDir)
	}
	if c.EmbedModel != DefaultEmbeddingModel || !c.Memory.AutoRecall {
		t.Fatalf("memory defaults %q %v", c.EmbedModel, c.Memory.AutoRecall)
	}
	if c.DatabaseURL != DefaultDatabaseURL {
		t.Fatalf("default database url %q", c.DatabaseURL)
	}
	if got := s.StringList("models"); len(got) != 1 || got[0] != "dummy/echo" {
		t.Fatalf("models %v", got)
	}
	if s.Source("model") != SourceYAML {
		t.Fatalf("source %v", s.Source("model"))
	}
}

func TestFromYAMLRejectsBadYAML(t *testing.T) {
	if _, err := FromYAML([]byte("model: [unterminated")); err == nil {
		t.Fatal("bad yaml should fail")
	}
}

func TestFromYAMLNoPathCannotPatch(t *testing.T) {
	s, err := FromYAML([]byte("model: dummy/echo\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Patch(map[string]string{"model": "dummy/echo"}); err == nil {
		t.Fatal("Patch on a path-less store should fail")
	}
}

func TestLoadPathWritesAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "silo.yaml")
	if err := os.WriteFile(path, []byte("model: openrouter/demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Path() != path {
		t.Fatalf("path %q", s.Path())
	}
	if err := s.Patch(map[string]string{"model_title": "openrouter/title"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Config().ModelTitle; got != "openrouter/title" {
		t.Fatalf("title after patch %q", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		t.Fatalf("file not written: %v", err)
	}
	reloaded, err := LoadPath(path)
	if err != nil || reloaded.Config().ModelTitle != "openrouter/title" {
		t.Fatalf("reload: %v %q", err, reloaded.Config().ModelTitle)
	}
}

func TestKnownKeyAndEnvName(t *testing.T) {
	if !KnownKey("model") || !KnownKey("providers.openrouter.api_key") || !KnownKey("search.engine") {
		t.Fatal("known keys misclassified")
	}
	if KnownKey("nope.nope") {
		t.Fatal("unknown key accepted")
	}
	if EnvName("providers.openrouter.api_key") != "SILO_PROVIDERS__OPENROUTER__API_KEY" {
		t.Fatalf("EnvName %q", EnvName("providers.openrouter.api_key"))
	}
}
