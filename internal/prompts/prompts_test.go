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

// A secret is used inside code and never seen by the model: a value a cell
// prints, or one put in a tool's arguments, has gone through inference.
func TestSystemPromptKeepsSecretsOutOfTheConversation(t *testing.T) {
	for _, want := range []string{
		"A secret's value never passes through you",
		"never print, return, log",
		"only when your owner explicitly asks",
		"type_text(get_secret(",
	} {
		if !strings.Contains(System, want) {
			t.Fatalf("SYSTEM.md no longer says %q", want)
		}
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

// The box ships the everyday shell tools and uv (botimage/Containerfile, whose
// last check fails the build when one is missing); the model reaches for one
// only if the prompt says it is there.
func TestSystemPromptNamesTheInstalledTools(t *testing.T) {
	for _, want := range []string{"`curl`", "`jq`", "`sqlite3`", "`uv`", "`pip install`"} {
		if !strings.Contains(System, want) {
			t.Fatalf("SYSTEM.md does not say %s is installed", want)
		}
	}
}

// Behind the network filter only 80 and 443 connect directly; the box's
// start.sh points proxychains4, ssh and $SILO_SOCKS_PROXY at the SOCKS5 door.
// The model only finds that door if the prompt names it and the switch.
func TestNetworkPromptNamesTheSOCKSDoor(t *testing.T) {
	for _, want := range []string{"SILO_SOCKS_PROXY", "proxychains4", "Other protocols", "X-Silo-Filter"} {
		if !strings.Contains(Network, want) {
			t.Fatalf("NETWORK.md does not mention %s", want)
		}
	}
}

// A run that answers the human is taught sections, questions and subagents by
// the lead part; the core every run gets, a subagent's included, is not.
func TestLeadPromptHoldsWhatOnlyALeadDoes(t *testing.T) {
	for _, want := range []string{"<section_send />", "ask_owner", "spawn_agent", "put_widget", "create_automation"} {
		if !strings.Contains(Lead, want) {
			t.Errorf("LEAD.md does not mention %s", want)
		}
		if strings.Contains(System, want) {
			t.Errorf("SYSTEM.md mentions %s, which belongs in LEAD.md", want)
		}
	}
}

// The compaction prompt asks for what the next turn cannot do without.
func TestCompactPromptAsksForWhatMustSurvive(t *testing.T) {
	for _, want := range []string{"Standing instructions", "Did not work", "denied", "still alive", "verbatim"} {
		if !strings.Contains(Compact, want) {
			t.Errorf("COMPACT.md does not ask for %q", want)
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

// The dummy provider recognizes a memory keeper's run by this heading, the
// keeper's prompt names the tools its profile offers, and the part about
// working things out is filled in from its own file.
func TestMemoryPromptKeepsMarkerAndTools(t *testing.T) {
	if !strings.HasPrefix(Memory, "# MEMORY KEEPER") {
		t.Fatal("MEMORY.md must start with the MEMORY KEEPER heading")
	}
	for _, want := range []string{"`remember`", "`forget`", "`reinforce`", "`recall`", "`review_memories`", "{infer}", "[Silo output filter:"} {
		if !strings.Contains(Memory, want) {
			t.Fatalf("MEMORY.md no longer mentions %s", want)
		}
	}
	if !strings.Contains(MemoryInfer, "inferred: true") || !strings.Contains(MemoryStated, "Never pass `inferred`") {
		t.Fatal("the keeper's two inference sections must say how inferred is used")
	}
	if !strings.Contains(MemoryTidy, "`review_memories`") {
		t.Fatal("MEMORY_TIDY.md must send the keeper to review_memories")
	}
}
