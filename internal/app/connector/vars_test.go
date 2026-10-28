package connector

import (
	"encoding/json"
	"strings"
	"testing"

	"silo.agent/internal/config"
	"silo.agent/internal/db"
)

func TestResolveConnectorExpandsTechnicalFields(t *testing.T) {
	store, err := config.FromYAML([]byte("connector_vars:\n  TENANT: acme\n  TOKEN: s3cret\n  PORT: \"8443\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: store, cfg: store.Config}
	c := &db.Connector{
		HTTPURL:           "https://${TENANT}.example.com:${PORT}/mcp",
		OAuthClientID:     "${TENANT}-client",
		OAuthClientSecret: "${TOKEN}",
		HeadersJSON:       `{"Authorization":"Bearer ${TOKEN}"}`,
		StdioCommand:      "npx",
		StdioArgsJSON:     `["-y","pkg","--tenant","${TENANT}"]`,
		StdioImage:        "localhost/${TENANT}:v1",
		EnvJSON:           `[{"name":"TENANT","value":"${TENANT}"},{"name":"CRED","secret":"my-secret"}]`,
		Prompt:            "Use ${TENANT} context.",
	}
	got := s.Resolve(c, "")

	if got.HTTPURL != "https://acme.example.com:8443/mcp" {
		t.Errorf("HTTPURL %q", got.HTTPURL)
	}
	if got.OAuthClientID != "acme-client" || got.OAuthClientSecret != "s3cret" {
		t.Errorf("oauth %q %q", got.OAuthClientID, got.OAuthClientSecret)
	}
	var hdr map[string]string
	if err := json.Unmarshal([]byte(got.HeadersJSON), &hdr); err != nil {
		t.Fatal(err)
	}
	if hdr["Authorization"] != "Bearer s3cret" {
		t.Errorf("header %q", hdr["Authorization"])
	}
	var args []string
	if err := json.Unmarshal([]byte(got.StdioArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	if len(args) != 4 || args[3] != "acme" {
		t.Errorf("args %v", args)
	}
	if got.StdioImage != "localhost/acme:v1" {
		t.Errorf("image %q", got.StdioImage)
	}
	env := parseEnv(got.EnvJSON)
	if len(env) != 2 || env[0].Value != "acme" || env[1].Secret != "my-secret" {
		t.Errorf("env %+v", env)
	}
	if got.Prompt != "Use acme context." {
		t.Errorf("prompt %q", got.Prompt)
	}
	// The source row keeps its placeholders.
	if c.HTTPURL != "https://${TENANT}.example.com:${PORT}/mcp" {
		t.Errorf("source mutated: %q", c.HTTPURL)
	}
}

func TestResolveConnectorNoVarsIsIdentity(t *testing.T) {
	store, err := config.FromYAML(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: store, cfg: store.Config}
	c := &db.Connector{HTTPURL: "https://${MISSING}.example.com"}
	if got := s.Resolve(c, ""); got != c {
		t.Fatalf("expected identity when no vars configured")
	}
	// With vars configured but no match, the reference is left untouched.
	store2, err := config.FromYAML([]byte("connector_vars:\n  OTHER: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	s2 := &Service{store: store2, cfg: store2.Config}
	if got := s2.Resolve(c, ""); got.HTTPURL != "https://${MISSING}.example.com" {
		t.Fatalf("unknown ref should stay, got %q", got.HTTPURL)
	}
}

// A URL whose variable the operator never defined is refused before it is
// dialed, in words that say where to define it, not as an "unsupported
// protocol scheme" from the HTTP client.
func TestUndefinedVariableNamesItself(t *testing.T) {
	store, err := config.FromYAML(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: store, cfg: store.Config}
	err = s.checkVars(s.Resolve(&db.Connector{Transport: transportHTTP, HTTPURL: "${LIGHTPANDA_URL}/mcp"}, ""))
	if err == nil || !strings.Contains(err.Error(), "LIGHTPANDA_URL") || !strings.Contains(err.Error(), "Connector options") {
		t.Fatalf("err = %v", err)
	}
	err = s.checkVars(s.Resolve(&db.Connector{Transport: transportHTTP, HTTPURL: "https://x/${secrets.TOKEN}"}, "bot"))
	if err == nil || !strings.Contains(err.Error(), "Secrets") {
		t.Fatalf("secret err = %v", err)
	}
	if err := s.checkVars(&db.Connector{Transport: transportHTTP, HTTPURL: "http://localhost:9223/mcp"}); err != nil {
		t.Fatalf("resolved: %v", err)
	}
}
