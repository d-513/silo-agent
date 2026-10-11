package app

import (
	"encoding/json"
	"strings"
	"testing"

	"silo.agent/internal/db"
	"silo.agent/internal/llm"
)

func toolTurn(id string, size int) []llm.Message {
	return []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "read", Arguments: `{}`}}},
		{Role: llm.RoleTool, ToolCallID: id, Text: strings.Repeat("x", size)},
	}
}

func TestStaleResultsKeepsTheNewest(t *testing.T) {
	msgs := []llm.Message{{Role: llm.RoleUser, Text: "go"}}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		msgs = append(msgs, toolTurn(id, 3500)...)
	}
	// Each result is about 1000 tokens. A budget that fits none still keeps
	// the newest clearKeepResults.
	if n := staleResults(msgs, 0); n != 5-clearKeepResults {
		t.Fatalf("stale with no budget = %d", n)
	}
	// A budget of four results keeps four.
	if n := staleResults(msgs, 4100); n != 1 {
		t.Fatalf("stale with room for four = %d", n)
	}
	if n := staleResults(msgs, 1<<30); n != 0 {
		t.Fatalf("stale with room for all = %d", n)
	}
	if n := staleResults(msgs[:1], 0); n != 0 {
		t.Fatalf("stale without results = %d", n)
	}
}

func TestClearResultsBlanksTheOldestLongOnes(t *testing.T) {
	msgs := []llm.Message{{Role: llm.RoleUser, Text: "go"}}
	msgs = append(msgs, toolTurn("a", 3500)...)
	msgs = append(msgs, toolTurn("b", 50)...) // too short to be worth it
	msgs = append(msgs, toolTurn("c", 3500)...)
	msgs = append(msgs, toolTurn("d", 3500)...)

	freed := clearResults(msgs, 3)
	if msgs[2].Text != clearedText || msgs[6].Text != clearedText {
		t.Fatalf("old results not cleared: %q / %q", msgs[2].Text[:20], msgs[6].Text[:20])
	}
	if len(msgs[4].Text) != 50 {
		t.Fatal("a short result was cleared")
	}
	if len(msgs[8].Text) != 3500 {
		t.Fatal("the newest result was cleared")
	}
	if msgs[1].ToolCalls[0].ID != "a" || msgs[2].ToolCallID != "a" {
		t.Fatal("clearing must keep the call and its pairing")
	}
	want := 2 * (runeTokens(strings.Repeat("x", 3500)) - runeTokens(clearedText))
	if freed != want {
		t.Fatalf("freed %d tokens, want %d", freed, want)
	}
	// Clearing again frees nothing more.
	if again := clearResults(msgs, 3); again != 0 {
		t.Fatalf("second clear freed %d", again)
	}
}

// A run's context is rebuilt from the log with the same results cleared.
func TestReplayClearsWhatTheRunCleared(t *testing.T) {
	var evs []db.RunEvent
	add := func(kind, body, meta string) {
		evs = append(evs, db.RunEvent{ID: kind + string(rune('a'+len(evs))), RunID: "r1", Kind: kind, Body: body, Meta: meta})
	}
	big := strings.Repeat("y", 2000)
	add("user", "go", "")
	for _, id := range []string{"t1", "t2", "t3"} {
		meta, _ := json.Marshal(turnMeta{Calls: []turnCall{{ID: id, Name: "read", Args: `{}`}}})
		add("tool", "", "")
		add(turnKind, "", string(meta))
		add("tool_result", big, "")
	}
	meta, _ := json.Marshal(clearedMeta{Results: 2})
	add(clearedKind, "", string(meta))

	r := replayEvents(evs)
	if r.cleared != 2 {
		t.Fatalf("cleared = %d", r.cleared)
	}
	// replayEvents leaves the text alone (the memory collector reads it);
	// the run's context applies the clear.
	if r.msgs[2].Text != big {
		t.Fatal("replayEvents cleared a result itself")
	}
	clearResults(r.msgs, r.cleared)
	var got []string
	for _, m := range r.msgs {
		if m.Role == llm.RoleTool {
			got = append(got, m.ToolCallID+":"+m.Text[:5])
		}
	}
	if strings.Join(got, " ") != "t1:[Old  t2:[Old  t3:yyyyy" {
		t.Fatalf("replayed results %v", got)
	}
}
