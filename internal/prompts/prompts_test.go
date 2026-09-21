package prompts

import (
	"strings"
	"testing"
)

// TestSystemPromptKeepsHardRules guards the directives that are easy to drop in
// a rewrite and expensive to rediscover: use the neutral tools, not a raw GUI
// driver, and deliver files with present/artifact.
func TestSystemPromptKeepsHardRules(t *testing.T) {
	if strings.TrimSpace(System) == "" {
		t.Fatal("SYSTEM.md is empty")
	}
	for _, want := range []string{"exec_python", "present", "artifact", "web_search", "look", "click"} {
		if !strings.Contains(System, want) {
			t.Fatalf("SYSTEM.md no longer mentions %q", want)
		}
	}
	if !strings.Contains(System, "playwright install") {
		t.Fatal("SYSTEM.md must forbid `playwright install`")
	}
}

func TestChannelPromptTeachesSections(t *testing.T) {
	if !strings.Contains(Channel, "<section_send />") {
		t.Fatal("CHANNEL.md no longer teaches the section sentinel")
	}
}
