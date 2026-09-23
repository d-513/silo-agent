package app

import (
	"encoding/json"
	"testing"

	"silo.agent/internal/config"
	"silo.agent/internal/db"
)

func TestResolveConnectorExpandsTechnicalFields(t *testing.T) {
	store, err := config.FromYAML([]byte("connector_vars:\n  TENANT: acme\n  TOKEN: s3cret\n  PORT: \"8443\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Store: store}
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
	got := a.resolveConnector(c)

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
	a := &App{Store: store}
	c := &db.Connector{HTTPURL: "https://${MISSING}.example.com"}
	if got := a.resolveConnector(c); got != c {
		t.Fatalf("expected identity when no vars configured")
	}
	// With vars configured but no match, the reference is left untouched.
	store2, err := config.FromYAML([]byte("connector_vars:\n  OTHER: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	a2 := &App{Store: store2}
	if got := a2.resolveConnector(c); got.HTTPURL != "https://${MISSING}.example.com" {
		t.Fatalf("unknown ref should stay, got %q", got.HTTPURL)
	}
}
