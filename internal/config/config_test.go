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
	raw := []byte("providers:\n  openrouter:\n    api_key: from-yaml\nhttp_addr: \":9\"\nsearch:\n  engine: from-yaml\n")
	if err := os.WriteFile(filepath.Join(dir, "silo.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("SILO_PROVIDERS__OPENROUTER__API_KEY", "from-env")
	t.Setenv("SILO_SEARCH__ENGINE", "from-env")
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c := s.Config()
	if c.Providers["openrouter"].APIKey != "from-env" {
		t.Fatalf("key %q", c.Providers["openrouter"].APIKey)
	}
	if c.HTTPAddr != ":9" {
		t.Fatalf("http_addr %q", c.HTTPAddr)
	}
	if c.Search.Engine != "from-env" {
		t.Fatalf("search.engine %q", c.Search.Engine)
	}
	if s.Source("providers.openrouter.api_key") != SourceEnv {
		t.Fatalf("source %s", s.Source("providers.openrouter.api_key"))
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
	if c.MCPStdioImage != DefaultMCPStdioImage {
		t.Fatalf("mcp_stdio_image %q", c.MCPStdioImage)
	}
	if c.Model != DefaultModel {
		t.Fatalf("model %q", c.Model)
	}
}

func TestPatchPreservesCommentsAndDoesNotBakeEnv(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("# keep me\nproviders:\n  openrouter:\n    api_key: from-yaml # secret\nhttp_addr: \":9\"\n")
	if err := os.WriteFile(filepath.Join(dir, "silo.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("SILO_PROVIDERS__OPENROUTER__API_KEY", "from-env")
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Patch(map[string]string{"model": "openrouter/openai/gpt-test"}); err != nil {
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
	if !strings.Contains(body, "openrouter/openai/gpt-test") {
		t.Fatalf("missing model:\n%s", body)
	}
	if s.Config().Providers["openrouter"].APIKey != "from-env" {
		t.Fatalf("env should still win %q", s.Config().Providers["openrouter"].APIKey)
	}
	if s.Config().Model != "openrouter/openai/gpt-test" {
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
	if !KnownKey("model") || !KnownKey("search.engine") || !KnownKey("mcp_stdio_image") || KnownKey("nope") {
		t.Fatal("catalog")
	}
	if !KnownKey("providers.openai.api_key") || !KnownKey("providers.anthropic.cache") {
		t.Fatal("provider settings should be known")
	}
	if KnownKey("providers.nope.api_key") {
		t.Fatal("unknown provider should not be known")
	}
}

func TestSetAndReadModels(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	models := []string{"openrouter/openai/gpt-5.6-luna", "anthropic/claude-opus-5"}
	if err := s.SetModels(models); err != nil {
		t.Fatal(err)
	}
	got := s.AllowedModels()
	if len(got) != 2 || got[0] != models[0] || got[1] != models[1] {
		t.Fatalf("allowed models %v", got)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "silo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "models:") {
		t.Fatalf("yaml missing models:\n%s", raw)
	}
}

func TestProviderSettingsParse(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("providers:\n  openai:\n    api_key: k\n    cache: true\n  anthropic:\n    api_key: a\n    cache: true\n    cache_ttl: 1h\n    max_tokens: 1234\n")
	if err := os.WriteFile(filepath.Join(dir, "silo.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c := s.Config()
	if !c.Providers["openai"].Cache {
		t.Fatal("openai cache should be true")
	}
	an := c.Providers["anthropic"]
	if an.MaxTokens != 1234 || an.CacheTTL != "1h" {
		t.Fatalf("anthropic provider %+v", an)
	}
	settings := c.ProviderSettings("anthropic")
	if settings.Get("cache_ttl") != "1h" || settings.Get("max_tokens") != "1234" {
		t.Fatalf("settings %+v", settings)
	}
}

func TestValidateRejectsBadProviderAndModel(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteYAML([]byte("providers:\n  nope:\n    api_key: x\n")); err == nil {
		t.Fatal("expected unknown provider rejection")
	}
	if err := s.WriteYAML([]byte("models:\n  - notamodel\n")); err == nil {
		t.Fatal("expected invalid model rejection")
	}
	if err := s.WriteYAML([]byte("providers:\n  openai:\n    api_key: x\nmodels:\n  - openai/gpt-x\n")); err != nil {
		t.Fatal(err)
	}
}
