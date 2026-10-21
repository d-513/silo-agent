package app

import (
	"testing"

	"silo.agent/internal/db"
	"silo.agent/internal/llm"
)

// Two calls in one turn, the first with pixels: the note the pixels leave
// follows the turn's last result, so no user turn sits between two results
// (pairing repair would drop the second one).
func TestReplayPutsSeenNotesAfterTheResults(t *testing.T) {
	turn := ev("t", "r1", turnKind, "")
	turn.Meta = `{"calls":[{"id":"a","name":"look","args":"{}"},{"id":"b","name":"read","args":"{}"}]}`
	msgs := historyFromEvents([]db.RunEvent{
		ev("1", "r1", "user", "go"),
		turn,
		ev("2", "r1", "tool_result", "looked"),
		ev("3", "r1", seenKind, lookOmitted),
		ev("4", "r1", "tool_result", "read it"),
	})
	var roles []llm.Role
	for _, m := range msgs {
		roles = append(roles, m.Role)
	}
	want := []llm.Role{llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleTool, llm.RoleUser}
	if len(roles) != len(want) {
		t.Fatalf("roles %v", roles)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("roles %v, want %v", roles, want)
		}
	}
	if msgs[3].ToolCallID != "b" || msgs[3].Text != "read it" {
		t.Fatalf("second result %+v", msgs[3])
	}
	if fixed := llm.RepairToolPairs(msgs); len(fixed) != len(msgs) || fixed[3].Text != "read it" {
		t.Fatalf("pairing repair changed the replay: %+v", fixed)
	}
}

// Only the message right after a framed turn folds into it: a later user turn
// that follows another user-role message (a pixel note) stays its own, as the
// run that sent it appended it.
func TestReplayFoldsOnlyTheNextMessage(t *testing.T) {
	turn := ev("t", "r2", turnKind, "")
	turn.Meta = `{"calls":[{"id":"a","name":"present","args":"{}"}]}`
	msgs := historyFromEvents([]db.RunEvent{
		compactionEv("c", "r1", "summary", ""),
		ev("1", "r2", "user", "first"),
		turn,
		ev("2", "r2", "tool_result", "shown"),
		ev("3", "r2", seenKind, "an image"),
		ev("4", "r2", "user", "second"),
	})
	if len(msgs) != 5 {
		t.Fatalf("got %d messages: %+v", len(msgs), msgs)
	}
	if last := msgs[4]; last.Role != llm.RoleUser || last.Text == msgs[3].Text {
		t.Fatalf("the second message folded into the pixel note: %+v", msgs)
	}
}

// Two messages read together after a framed turn: the first folds, the second
// is a turn of its own (drainInbox does the same).
func TestReplayFoldsOneOfTwoMessages(t *testing.T) {
	msgs := historyFromEvents([]db.RunEvent{
		compactionEv("c", "r1", "summary", ""),
		ev("1", "r2", "user", "first"),
		ev("2", "r2", "user", "second"),
	})
	if len(msgs) != 2 {
		t.Fatalf("got %d messages: %+v", len(msgs), msgs)
	}
}
