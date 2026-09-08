package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvKeyNesting(t *testing.T) {
	if got := envKey("SILO_OPENROUTER__API_KEY"); got != "openrouter.api_key" {
		t.Fatalf("got %q", got)
	}
	if got := envKey("SILO_HTTP_ADDR"); got != "http_addr" {
		t.Fatalf("got %q", got)
	}
	if got := envKey("SILO_SEARCH__ENGINE"); got != "search.engine" {
		t.Fatalf("got %q", got)
	}
	if got := EnvName("openrouter.api_key"); got != "SILO_OPENROUTER__API_KEY" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("openrouter:\n  api_key: from-yaml\nhttp_addr: \":9\"\nsearch:\n  engine: from-yaml\n")
	if err := os.WriteFile(filepath.Join(dir, "silo.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("SILO_OPENROUTER__API_KEY", "from-env")
	t.Setenv("SILO_SEARCH__ENGINE", "from-env")
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c := s.Config()
	if c.OpenRouter.APIKey != "from-env" {
		t.Fatalf("key %q", c.OpenRouter.APIKey)
	}
	if c.HTTPAddr != ":9" {
		t.Fatalf("http_addr %q", c.HTTPAddr)
	}
	if c.Search.Engine != "from-env" {
		t.Fatalf("search.engine %q", c.Search.Engine)
	}
	if s.Source("openrouter.api_key") != SourceEnv {
		t.Fatalf("source %s", s.Source("openrouter.api_key"))
	}
	if s.Source("http_addr") != SourceYAML {
		t.Fatalf("source %s", s.Source("http_addr"))
	}
	if s.Source("model") != SourceDefault {
		t.Fatalf("source %s", s.Source("model"))
	}
}

func TestLoadSearchEngineDefault(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c := s.Config()
	if c.Search.Engine != "duckduckgo_scraper" {
		t.Fatalf("search.engine %q", c.Search.Engine)
	}
	if c.Model != DefaultModel {
		t.Fatalf("model %q", c.Model)
	}
}

func TestPatchPreservesCommentsAndDoesNotBakeEnv(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("# keep me\nopenrouter:\n  api_key: from-yaml # secret\nhttp_addr: \":9\"\n")
	if err := os.WriteFile(filepath.Join(dir, "silo.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("SILO_OPENROUTER__API_KEY", "from-env")
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Patch(map[string]string{"model": "openai/gpt-test"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "silo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	if !strings.Contains(body, "keep me") || !strings.Contains(body, "from-yaml") {
		t.Fatalf("lost yaml:\n%s", body)
	}
	if strings.Contains(body, "from-env") {
		t.Fatalf("baked env:\n%s", body)
	}
	if !strings.Contains(body, "openai/gpt-test") {
		t.Fatalf("missing model:\n%s", body)
	}
	if s.Config().OpenRouter.APIKey != "from-env" {
		t.Fatalf("env should still win %q", s.Config().OpenRouter.APIKey)
	}
	if s.Config().Model != "openai/gpt-test" {
		t.Fatalf("model %q", s.Config().Model)
	}
}

func TestWriteYAMLRejectsUnknownEngine(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteYAML([]byte("search:\n  engine: nope\n")); err == nil {
		t.Fatal("expected unknown engine")
	}
	if err := s.WriteYAML([]byte("search:\n  engine: duckduckgo_scraper\nmodel: openai/gpt-test\n")); err != nil {
		t.Fatal(err)
	}
	if s.Config().Model != "openai/gpt-test" {
		t.Fatalf("model %q", s.Config().Model)
	}
	if s.Source("model") != SourceYAML {
		t.Fatalf("source %s", s.Source("model"))
	}
}

func TestKnownKey(t *testing.T) {
	if !KnownKey("model") || !KnownKey("search.engine") || KnownKey("nope") {
		t.Fatal("catalog")
	}
}
