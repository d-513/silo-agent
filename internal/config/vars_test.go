package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandVars(t *testing.T) {
	vars := map[string]string{"TENANT": "acme", "PORT": "8080"}
	cases := []struct {
		in, want string
	}{
		{"https://${TENANT}.example.com:${PORT}/mcp", "https://acme.example.com:8080/mcp"},
		{"no refs here", "no refs here"},
		{"${MISSING}", "${MISSING}"},
		{"${tenant}", "acme"},
		{"prefix-${PORT}", "prefix-8080"},
	}
	for _, c := range cases {
		if got := ExpandVars(c.in, vars); got != c.want {
			t.Errorf("ExpandVars(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := ExpandVars("${TENANT}", nil); got != "${TENANT}" {
		t.Errorf("nil vars should leave refs untouched, got %q", got)
	}
}

func TestSetAndReadConnectorVars(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	vars := []NamedVar{{Name: "TENANT", Value: "acme"}, {Name: "REGION", Value: "eu-1"}}
	if err := s.SetConnectorVars(vars); err != nil {
		t.Fatal(err)
	}
	got := s.ConnectorVars()
	if len(got) != 2 || got[0].Name != "REGION" || got[1].Name != "TENANT" {
		t.Fatalf("vars %+v", got)
	}
	if got[0].Value != "eu-1" || got[1].Value != "acme" {
		t.Fatalf("values %+v", got)
	}
	if s.ConnectorVarSource("TENANT") != SourceYAML {
		t.Fatalf("source %s", s.ConnectorVarSource("TENANT"))
	}
	raw, err := os.ReadFile(filepath.Join(dir, "silo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "connector_vars:") || !strings.Contains(string(raw), "TENANT: acme") {
		t.Fatalf("yaml missing connector_vars:\n%s", raw)
	}
	if s.ConnectorVarMap()["TENANT"] != "acme" {
		t.Fatalf("connector var map %+v", s.ConnectorVarMap())
	}
}

func TestConnectorVarRejectsBadName(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetConnectorVars([]NamedVar{{Name: "1BAD", Value: "x"}}); err == nil {
		t.Fatal("expected invalid name rejection")
	}
	if err := s.SetConnectorVars([]NamedVar{{Name: "A", Value: "x"}, {Name: "A", Value: "y"}}); err == nil {
		t.Fatal("expected duplicate rejection")
	}
	if err := s.WriteYAML([]byte("connector_vars:\n  BAD-NAME: x\n")); err == nil {
		t.Fatal("expected invalid yaml var name rejection")
	}
}

func TestConnectorVarEnvOverride(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("connector_vars:\n  TENANT: from-yaml\n")
	if err := os.WriteFile(filepath.Join(dir, "silo.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("SILO_CONNECTOR_VARS__TENANT", "from-env")
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := s.ConnectorVars()
	if len(got) != 1 || got[0].Name != "TENANT" || got[0].Value != "from-env" {
		t.Fatalf("vars %+v", got)
	}
	if s.ConnectorVarSource("TENANT") != SourceEnv {
		t.Fatalf("source %s", s.ConnectorVarSource("TENANT"))
	}
}
