package app

import (
	"strings"
	"testing"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/llm"
)

func TestRunContextUnlimited(t *testing.T) {
	ctx, cancel := runContext(0)
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("0 should mean no deadline")
	}
	ctx2, cancel2 := runContext(time.Minute)
	defer cancel2()
	if _, ok := ctx2.Deadline(); !ok {
		t.Fatal("a cap should set a deadline")
	}
}

func TestFormatBoard(t *testing.T) {
	out := formatBoard([]db.TaskItem{
		{N: 1, Text: "look", Assignee: "scout", Done: true, DoneBy: "scout", Note: "saved a.md"},
		{N: 2, Text: "write"},
	})
	for _, want := range []string{"(1/2 done)", "#1 [x] [scout] look — done by scout: saved a.md", "#2 [ ] write"} {
		if !strings.Contains(out, want) {
			t.Fatalf("board missing %q:\n%s", want, out)
		}
	}
}

func TestSubagentReportReplaysAsUserTurn(t *testing.T) {
	evs := []db.RunEvent{
		{RunID: "r1", Kind: "user", Body: "go"},
		{RunID: "r1", Kind: "assistant", Body: "spawned"},
		{RunID: "r2", Kind: subagentReportKind, Body: "### a — done\nok", Tool: "a:done"},
		{RunID: "r2", Kind: "assistant", Body: "thanks"},
	}
	msgs := historyFromEvents(evs)
	if len(msgs) != 4 || msgs[2].Role != llm.RoleUser || !strings.Contains(msgs[2].Text, "subagents finished") || !strings.Contains(msgs[2].Text, "### a — done") {
		t.Fatalf("history %+v", msgs)
	}
}
