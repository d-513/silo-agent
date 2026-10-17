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

// The box ships Node.js and pnpm (botimage/Containerfile); the model only
// reaches for them if the prompt says they are there.
func TestSystemPromptNamesTheJavaScriptToolchain(t *testing.T) {
	for _, want := range []string{"Node.js", "npm", "pnpm"} {
		if !strings.Contains(System, want) {
			t.Fatalf("SYSTEM.md does not say %s is installed", want)
		}
	}
}

// Behind the network filter only 80 and 443 connect directly; the box's
// start.sh points proxychains4, ssh and $SILO_SOCKS_PROXY at the SOCKS5 door.
// The model only finds that door if the prompt names it and the switch.
func TestSystemPromptNamesTheSOCKSDoor(t *testing.T) {
	for _, want := range []string{"SILO_SOCKS_PROXY", "proxychains4", "Other protocols"} {
		if !strings.Contains(System, want) {
			t.Fatalf("SYSTEM.md does not mention %s", want)
		}
	}
}

func TestChannelPromptTeachesSections(t *testing.T) {
	if !strings.Contains(Channel, "<section_send />") {
		t.Fatal("CHANNEL.md no longer teaches the section sentinel")
	}
}

// The dummy provider recognizes a compaction request by this heading.
func TestCompactPromptKeepsMarker(t *testing.T) {
	if !strings.HasPrefix(Compact, "# CONTEXT COMPACTION") {
		t.Fatal("COMPACT.md must start with the CONTEXT COMPACTION heading")
	}
}

// The dummy provider recognizes a memory collection by this heading, and the
// collector parses the JSON shape the prompt teaches.
func TestMemoryPromptKeepsMarkerAndShape(t *testing.T) {
	if !strings.HasPrefix(Memory, "# MEMORY COLLECTOR") {
		t.Fatal("MEMORY.md must start with the MEMORY COLLECTOR heading")
	}
	for _, want := range []string{`"save"`, `"update"`, `"forget"`} {
		if !strings.Contains(Memory, want) {
			t.Fatalf("MEMORY.md no longer mentions %s", want)
		}
	}
}
