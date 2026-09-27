package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/llm"
)

func ev(id, run, kind, body string) db.RunEvent {
	return db.RunEvent{ID: id, RunID: run, Kind: kind, Body: body, CreatedAt: time.Unix(0, 0)}
}

func compactionEv(id, run, summary, after string) db.RunEvent {
	e := ev(id, run, compactionKind, summary)
	e.Tool = compactManual
	e.Meta = `{"after":"` + after + `"}`
	return e
}

func TestHistoryWithoutCompactionReplaysEverything(t *testing.T) {
	msgs := historyFromEvents([]db.RunEvent{
		ev("1", "r1", "user", "hi"),
		ev("2", "r1", "assistant", "hello"),
		ev("3", "r2", "user", "again"),
	})
	if len(msgs) != 3 || msgs[0].Role != llm.RoleUser || msgs[1].Text != "hello" {
		t.Fatalf("history %+v", msgs)
	}
}

func TestHistoryStartsFromTheLastCompaction(t *testing.T) {
	msgs := historyFromEvents([]db.RunEvent{
		ev("1", "r1", "user", "old question"),
		ev("2", "r1", "assistant", "old answer"),
		compactionEv("3", "r2", "first summary", "2"),
		ev("4", "r3", "user", "middle"),
		ev("5", "r3", "assistant", "middle answer"),
		ev("6", "r4", compactingKind, ""),
		compactionEv("7", "r4", "second summary", "5"),
		ev("8", "r5", "user", "newest"),
		ev("9", "r5", "assistant", "newest answer"),
	})
	if len(msgs) != 2 {
		t.Fatalf("want summary+reply, got %d: %+v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0].Text, "second summary") || strings.Contains(msgs[0].Text, "first summary") {
		t.Fatalf("summary turn %q", msgs[0].Text)
	}
	// The next user message folds into the summary turn so roles alternate.
	if !strings.Contains(msgs[0].Text, "newest") || msgs[1].Role != llm.RoleAssistant {
		t.Fatalf("fold %+v", msgs)
	}
	for _, m := range msgs {
		if strings.Contains(m.Text, "old question") || strings.Contains(m.Text, "middle") {
			t.Fatalf("pre-compaction text replayed: %q", m.Text)
		}
	}
}

// A message injected while the summary was being written is persisted before
// the compaction event but after its anchor; it must survive replay.
func TestHistoryKeepsMessageInjectedDuringCompaction(t *testing.T) {
	msgs := historyFromEvents([]db.RunEvent{
		ev("1", "r1", "user", "task"),
		ev("2", "r1", "assistant", "done"),
		ev("3", "r2", compactingKind, ""),
		ev("4", "r2", "user", "sent while compacting"),
		compactionEv("5", "r2", "the summary", "2"),
		ev("6", "r2", "assistant", "reply"),
	})
	if len(msgs) != 2 || !strings.Contains(msgs[0].Text, "sent while compacting") {
		t.Fatalf("injected message lost: %+v", msgs)
	}
}

func TestHistoryAfterCompactionKeepsToolPairs(t *testing.T) {
	msgs := historyFromEvents([]db.RunEvent{
		ev("1", "r1", "user", "task"),
		ev("2", "r1", "tool_result", "orphan before the cut"),
		compactionEv("3", "r1", "sum", "2"),
		{ID: "4", RunID: "r1", Kind: "tool", Tool: "read", Body: `{"path":"a"}`},
		{ID: "5", RunID: "r1", Kind: "tool_result", Tool: "read", Body: "contents"},
		ev("6", "r1", "assistant", "ok"),
	})
	if len(msgs) != 4 {
		t.Fatalf("history %+v", msgs)
	}
	if len(msgs[1].ToolCalls) != 1 || msgs[2].Role != llm.RoleTool || msgs[2].ToolCallID != msgs[1].ToolCalls[0].ID || msgs[2].Text != "contents" {
		t.Fatalf("tool pair broken: %+v", msgs)
	}
}

func TestNeedsCompaction(t *testing.T) {
	if !needsCompaction(90, 10, 100, 0.8) {
		t.Fatal("over threshold should compact")
	}
	if needsCompaction(70, 10, 100, 0.8) {
		t.Fatal("under threshold should not compact")
	}
	// A system prompt that alone crosses the threshold must not compact a
	// history that is already tiny, or every turn would compact.
	if needsCompaction(90, 85, 100, 0.8) {
		t.Fatal("tiny history compacted")
	}
}

func TestIsContextOverflow(t *testing.T) {
	for _, s := range []string{
		"This model's maximum context length is 128000 tokens",
		"prompt is too long: 210000 tokens > 200000 maximum",
		"context_length_exceeded",
	} {
		if !isContextOverflow(errors.New(s)) {
			t.Fatalf("%q not recognized", s)
		}
	}
	if isContextOverflow(errors.New("rate limited")) || isContextOverflow(nil) {
		t.Fatal("false positive")
	}
}

func TestTranscriptTrimsOldestButKeepsSummary(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Text: compactionText("keep me")},
		{Role: llm.RoleUser, Text: strings.Repeat("old ", 2000)},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{Name: "read", Arguments: strings.Repeat("x", 5000)}}},
		{Role: llm.RoleTool, Text: strings.Repeat("y", 9000)},
		{Role: llm.RoleUser, Text: "latest request"},
	}
	out := transcript(msgs, 1200)
	if !strings.Contains(out, "keep me") || !strings.Contains(out, "latest request") {
		t.Fatalf("transcript lost the summary or the latest request")
	}
	if strings.Contains(out, "old old") {
		t.Fatal("oldest entry not dropped")
	}
	if strings.Count(out, "y") > transcriptToolCap+10 {
		t.Fatal("tool result not capped")
	}
	full := transcript(msgs[4:], 100000)
	if !strings.HasPrefix(full, "USER:\nlatest request") {
		t.Fatalf("plain transcript %q", full)
	}
}
