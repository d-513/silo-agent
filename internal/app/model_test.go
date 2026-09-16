package app

import (
	"encoding/json"
	"strings"
	"testing"

	"silo.agent/internal/db"
)

func TestSwitchModelTool(t *testing.T) {
	a := testAppStore(t, nil)
	a.DB.Create(&db.Chat{ID: "c1", BotID: "b1"})

	if _, err := a.switchModelTool("c1", "openai/gpt-5.6-luna"); err == nil {
		t.Fatal("expected rejection while allowlist is empty")
	}
	if err := a.Store.SetModels([]string{"openai/gpt-5.6-luna", "anthropic/claude-opus-5"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.switchModelTool("c1", "openrouter/some-model"); err == nil {
		t.Fatal("expected rejection for a model outside the allowlist")
	}
	out, err := a.switchModelTool("c1", "openai/gpt-5.6-luna")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "openai/gpt-5.6-luna") {
		t.Fatalf("unexpected ack: %s", out)
	}
	if got := a.resolveModel("c1"); got != "openai/gpt-5.6-luna" {
		t.Fatalf("resolveModel = %q", got)
	}
}

func TestResolveModelFallsBackWhenDisallowed(t *testing.T) {
	a := testAppStore(t, nil)
	a.DB.Create(&db.Chat{ID: "c1", BotID: "b1", Model: "openai/removed-model"})
	got := a.resolveModel("c1")
	if got == "openai/removed-model" || got == "" {
		t.Fatalf("should fall back to the operator default, got %q", got)
	}
}

func TestListModelsTool(t *testing.T) {
	a := testAppStore(t, nil)
	a.DB.Create(&db.Chat{ID: "c1", BotID: "b1"})
	if err := a.Store.SetModels([]string{"openai/gpt-5.6-luna"}); err != nil {
		t.Fatal(err)
	}
	out, err := a.listModelsTool("c1")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Current string   `json:"current"`
		Default string   `json:"default"`
		Models  []string `json:"models"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Models) != 1 || parsed.Models[0] != "openai/gpt-5.6-luna" {
		t.Fatalf("models = %v", parsed.Models)
	}
	if parsed.Current == "" || parsed.Default == "" {
		t.Fatalf("current/default missing: %+v", parsed)
	}
}

// TestSystemBlocksCacheStable verifies the cache tier order: SOUL precedes
// MEMORY, SOUL ends a breakpoint, and editing MEMORY leaves every earlier block
// byte-identical so the cached prefix survives.
func TestSystemBlocksCacheStable(t *testing.T) {
	a := testAppStore(t, nil)
	a.DB.Create(&db.Bot{ID: "b1", Name: "Scout", Soul: "Be brief.", Memory: "v1"})

	before := a.buildSystemBlocks("b1")
	soulIdx, memIdx := -1, -1
	for i, b := range before {
		if strings.Contains(b.Text, "## SOUL") {
			soulIdx = i
		}
		if strings.Contains(b.Text, "## MEMORY") {
			memIdx = i
		}
	}
	if soulIdx < 0 || memIdx < 0 || soulIdx >= memIdx {
		t.Fatalf("SOUL/MEMORY order wrong: soul=%d memory=%d", soulIdx, memIdx)
	}
	if !before[soulIdx].CacheAfter {
		t.Fatal("SOUL should end a cache breakpoint")
	}

	a.DB.Model(&db.Bot{}).Where("id = ?", "b1").Update("memory", "v2 changed")
	after := a.buildSystemBlocks("b1")
	if len(after) < memIdx+1 {
		t.Fatalf("memory block missing after edit: %+v", after)
	}
	for i := 0; i <= soulIdx; i++ {
		if before[i].Text != after[i].Text || before[i].CacheAfter != after[i].CacheAfter {
			t.Fatalf("cached prefix changed at block %d", i)
		}
	}
}
