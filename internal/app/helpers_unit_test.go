package app

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

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

// TestStampUserTextUsesLocalTime guards the date/time marker that rides on user
// messages: it must render in the machine's local zone and leave the original
// text intact.
func TestStampUserTextUsesLocalTime(t *testing.T) {
	when := time.Date(2026, 9, 21, 14, 30, 5, 0, time.UTC)
	got := stampUserText("hello", when)
	if !strings.HasSuffix(got, " hello") || !strings.HasPrefix(got, "[") {
		t.Fatalf("stampUserText %q", got)
	}
	want := "[" + when.Local().Format("Mon, 2006-01-02 15:04:05 MST") + "] hello"
	if got != want {
		t.Fatalf("stampUserText = %q, want %q", got, want)
	}
	if !regexp.MustCompile(`^\[\w{3}, \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \S+\] hello$`).MatchString(got) {
		t.Fatalf("marker shape %q", got)
	}
}

func TestAutomationSectionIsTrailingAndNamesTheRun(t *testing.T) {
	a := &App{}
	if got := a.automationSections(promptContext{}); len(got) != 0 {
		t.Fatalf("no automation, no section: %v", got)
	}
	secs := a.automationSections(promptContext{automation: &db.Automation{Name: "Digest", Schedule: "0 9 * * *"}})
	if len(secs) != 1 || !secs[0].trailing {
		t.Fatalf("want one trailing section, got %v", secs)
	}
	if !strings.Contains(secs[0].body, "“Digest”") || !strings.Contains(secs[0].body, "0 9 * * *") || !strings.Contains(secs[0].body, "fresh context") {
		t.Fatalf("section body %q", secs[0].body)
	}
}

func TestParseScheduleEnforcesGap(t *testing.T) {
	for _, ok := range []string{"", "*/5 * * * *", "0 9 * * 1-5", "@daily", "@every 1h"} {
		if _, err := parseSchedule(ok); err != nil {
			t.Fatalf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"* * * * *", "@every 1m", "61 * * * *", "0 9 * *"} {
		if _, err := parseSchedule(bad); err == nil {
			t.Fatalf("%q should fail", bad)
		}
	}
}
