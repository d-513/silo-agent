package app

import (
	"errors"
	"strings"
	"testing"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
)

// These are pure helpers behind OAuth and connector editing. Their behavior is
// expensive to reproduce end-to-end (it needs a live authorization server), so
// they are tested directly here.

func TestOriginOfStripsPathQuery(t *testing.T) {
	if got := originOf("https://mcp.cloudflare.com/authorize?x=1"); got != "https://mcp.cloudflare.com" {
		t.Fatalf("originOf %q", got)
	}
	if got := originOf("not a url"); got != "" {
		t.Fatalf("originOf bad input %q", got)
	}
	if got := originOf(""); got != "" {
		t.Fatalf("originOf empty %q", got)
	}
}

func TestWrapOAuthHint(t *testing.T) {
	raw := errors.New("oauth: no configured client registration methods are supported by the authorization server")
	got := wrapOAuth(raw)
	if got == nil || !strings.Contains(got.Error(), "OAuth Client ID") {
		t.Fatalf("hint missing: %v", got)
	}
	plain := errors.New("network down")
	if wrapOAuth(plain) != plain {
		t.Fatal("unrelated error should pass through")
	}
}

func TestPreregisteredClient(t *testing.T) {
	if preregisteredClient(&db.Connector{}) != nil {
		t.Fatal("empty connector should have no client")
	}
	full := preregisteredClient(&db.Connector{OAuthClientID: " id ", OAuthClientSecret: "s"})
	if full == nil || full.ClientID != "id" || full.ClientSecretAuth == nil || full.ClientSecretAuth.ClientSecret != "s" {
		t.Fatalf("full client %+v", full)
	}
	pub := preregisteredClient(&db.Connector{OAuthClientID: "id"})
	if pub == nil || pub.ClientSecretAuth != nil {
		t.Fatalf("public client %+v", pub)
	}
}

func TestCallTitle(t *testing.T) {
	if got := callTitle("Twilio Docs", "twilio__retrieve"); got != "Twilio Docs · retrieve" {
		t.Fatalf("callTitle %q", got)
	}
}

func TestMergeHeadersKeepsSecret(t *testing.T) {
	got, err := mergeHeaders(`{"A":"one"}`, []*v1.HeaderInput{
		{Name: "A", Value: ""},
		{Name: "B", Value: "two"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"A":"one"`) || !strings.Contains(got, `"B":"two"`) {
		t.Fatalf("mergeHeaders %s", got)
	}
}
