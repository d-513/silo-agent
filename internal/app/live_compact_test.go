//go:build live

package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
)

// TestLiveCompact gives a real model the compaction prompt over a transcript
// that ends on a request, and checks it summarizes instead of answering, and
// keeps what the next turn cannot do without: the standing instruction, the
// dead end, the denial and what is still running. Run it after touching
// COMPACT.md or how the request is built:
//
//	SILO_CLAUDE_DIR=data/claude SILO_CLAUDE_BIN=build/claude/package/claude \
//	  go test -tags live ./internal/app -run TestLiveCompact -v
//
// SILO_LIVE_MODEL names another model as provider/model, with the provider's
// key in SILO_LIVE_KEY.
func TestLiveCompact(t *testing.T) {
	provider, model, settings := llm.Claude, "haiku", llm.Settings{"dir": os.Getenv("SILO_CLAUDE_DIR"), "binary": os.Getenv("SILO_CLAUDE_BIN")}
	if id := os.Getenv("SILO_LIVE_MODEL"); id != "" {
		p, m, err := llm.Parse(id)
		if err != nil {
			t.Fatal(err)
		}
		provider, model, settings = p, m, llm.Settings{"api_key": os.Getenv("SILO_LIVE_KEY")}
	} else if settings["dir"] == "" {
		t.Skip("SILO_CLAUDE_DIR or SILO_LIVE_MODEL not set")
	}
	client, err := llm.New(provider, settings)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{Role: llm.RoleUser, Text: "[Sun, 2026-10-11 14:30:05 +02:00 Europe/Warsaw]\nBuild me a small site listing my three hiking routes from routes.csv. Always use pnpm, never npm."},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "read", Arguments: `{"path":"routes.csv"}`}}},
		{Role: llm.RoleTool, ToolCallID: "1", Text: "1|name,km\n2|Orla Perć,14\n3|Rysy,22\n4|Giewont,12"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "2", Name: "terminal", Arguments: `{"command":"cd site && pnpm create astro@latest . --yes"}`}}},
		{Role: llm.RoleTool, ToolCallID: "2", Text: "error: astro template needs Node 26, found 24\nfailed"},
		{Role: llm.RoleAssistant, Text: "Astro will not install here, so I am using Vite instead.", ToolCalls: []llm.ToolCall{{ID: "3", Name: "terminal", Arguments: `{"command":"cd site && pnpm create vite . --template vanilla && pnpm install"}`}}},
		{Role: llm.RoleTool, ToolCallID: "3", Text: "done in 4.1s"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "4", Name: "exec_python", Arguments: `{"code":"import subprocess; p = subprocess.Popen(['pnpm','dev','--port','5180'], cwd='/workspace/site', stdout=open('/workspace/bot/dev.log','w')); print(p.pid)"}`}}},
		{Role: llm.RoleTool, ToolCallID: "4", Text: "4411"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "5", Name: "open_tunnel", Arguments: `{"port":5180,"public":true}`}}},
		{Role: llm.RoleTool, ToolCallID: "5", Text: "error: denied by the owner"},
		{Role: llm.RoleAssistant, Text: "The site is in site/ and its dev server runs on port 5180. You declined a public link, so I have not shared one."},
		{Role: llm.RoleUser, Text: "[Sun, 2026-10-11 15:02:44 +02:00 Europe/Warsaw]\nGood. Now add the elevation gain for each route as a new column, and tell me which one is the hardest."},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	res, err := client.Complete(ctx, llm.Request{
		Model:    model,
		System:   []llm.SystemBlock{{Text: prompts.Compact}},
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "<transcript>\n" + transcript(msgs, 100000) + "\n</transcript>\n\n" + compactAsk}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := res.Text
	t.Logf("summary (%d words):\n%s", len(strings.Fields(sum)), sum)
	low := strings.ToLower(sum)
	for name, want := range map[string][]string{
		"the latest request, verbatim":  {"elevation gain for each route"},
		"the standing instruction":      {"pnpm"},
		"the approach that failed":      {"astro"},
		"the denial":                    {"denied", "declined", "refused"},
		"what is still running":         {"5180"},
		"the file by its relative path": {"site/", "`site`", "routes.csv"},
	} {
		found := false
		for _, w := range want {
			found = found || strings.Contains(low, w)
		}
		if !found {
			t.Errorf("the summary lost %s (looked for %q)", name, want)
		}
	}
	// It summarizes; it does not do the work it was asked about.
	if strings.Contains(low, "hardest is") || strings.Contains(low, "the hardest route is") {
		t.Errorf("the model answered the request instead of summarizing it")
	}
	if !strings.Contains(sum, "Request") || !strings.Contains(sum, "Did not work") {
		t.Errorf("the summary does not follow the sections")
	}
	if n := len(strings.Fields(sum)); n > 1200 {
		t.Errorf("the summary is %d words", n)
	}
}
