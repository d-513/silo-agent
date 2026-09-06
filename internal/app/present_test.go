package app

import (
	"context"
	"strings"
	"testing"
)

func TestRelWorkspace(t *testing.T) {
	cases := map[string]string{
		"twilio.md":                 "twilio.md",
		"/workspace/twilio.md":      "twilio.md",
		"workspace/twilio.md":       "twilio.md",
		"/workspace/workspace/x.md": "workspace/x.md",
		"/workspace":                "",
		"workspace":                 "",
		"notes/a.md":                "notes/a.md",
		"/workspace/bot/page.png":   "bot/page.png",
	}
	for in, want := range cases {
		if got := relWorkspace(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestBotScratch(t *testing.T) {
	if !botScratch("bot/page.png") || !botScratch("/workspace/bot/x") {
		t.Fatal("scratch")
	}
	if botScratch("page.png") || botScratch("notes.md") || botScratch("botany.md") {
		t.Fatal("user file")
	}
}

func TestPresentAck(t *testing.T) {
	got := presentAck("notes.md", `{"name":"notes.md","size":2048}`)
	if !strings.Contains(got, "notes.md") || !strings.Contains(got, "2.0 KB") || !strings.Contains(got, "do not retype") {
		t.Fatal(got)
	}
	trunc := presentAck("big.bin", `{"name":"big.bin","size":3000000,"truncated":true}`)
	if !strings.Contains(trunc, "2 MB") {
		t.Fatal(trunc)
	}
	seen := presentAck("bot/page.png", `{"name":"page.png","size":2048}`)
	if !strings.Contains(seen, "scratch") && !strings.Contains(seen, "Scratch") {
		t.Fatal(seen)
	}
	if strings.Contains(seen, "Shown in the thread") {
		t.Fatal(seen)
	}
}

func TestPresentNeedsPath(t *testing.T) {
	a := &App{}
	_, _, err := a.execTool(context.Background(), "b", "r", "present", `{}`)
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("%v", err)
	}
}

func TestPresentImageURL(t *testing.T) {
	got := presentImageURL("shots/page.png", `{"name":"page.png","data":"AAAA","size":3}`)
	if got != "data:image/png;base64,AAAA" {
		t.Fatal(got)
	}
	if presentImageURL("notes.md", `{"name":"notes.md","data":"eA=="}`) != "" {
		t.Fatal("markdown is not an image")
	}
	if presentImageURL("page.png", `{"name":"page.png"}`) != "" {
		t.Fatal("empty data")
	}
}
