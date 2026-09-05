package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvKeyNesting(t *testing.T) {
	if got := envKey("SILO_OPENROUTER__API_KEY"); got != "openrouter.api_key" {
		t.Fatalf("got %q", got)
	}
	if got := envKey("SILO_HTTP_ADDR"); got != "http_addr" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte("openrouter:\n  api_key: from-yaml\nhttp_addr: \":9\"\n")
	if err := os.WriteFile(filepath.Join(dir, "silo.yaml"), yaml, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("SILO_OPENROUTER__API_KEY", "from-env")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.OpenRouter.APIKey != "from-env" {
		t.Fatalf("key %q", c.OpenRouter.APIKey)
	}
	if c.HTTPAddr != ":9" {
		t.Fatalf("http_addr %q", c.HTTPAddr)
	}
}
