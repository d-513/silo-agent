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
	}
	for in, want := range cases {
		if got := relWorkspace(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestPresentAck(t *testing.T) {
	got := presentAck(`{"name":"notes.md","size":2048}`)
	if !strings.Contains(got, "notes.md") || !strings.Contains(got, "2.0 KB") || !strings.Contains(got, "do not retype") {
		t.Fatal(got)
	}
	trunc := presentAck(`{"name":"big.bin","size":3000000,"truncated":true}`)
	if !strings.Contains(trunc, "2 MB") {
		t.Fatal(trunc)
	}
}

func TestPresentNeedsPath(t *testing.T) {
	a := &App{}
	_, err := a.execTool(context.Background(), "b", "r", "present", `{}`)
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("%v", err)
	}
}
