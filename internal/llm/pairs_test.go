package llm

import "testing"

func TestRepairToolPairs(t *testing.T) {
	calls := Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "a", Name: "x"}, {ID: "b", Name: "y"}}}
	in := []Message{
		{Role: RoleTool, ToolCallID: "stray", Text: "orphan"},
		{Role: RoleUser, Text: "hi"},
		calls,
		{Role: RoleTool, ToolCallID: "b", Text: "B"},
		{Role: RoleUser, Text: "look", Images: []Image{{}}},
		{Role: RoleTool, ToolCallID: "b", Text: "dup"},
		{Role: RoleTool, ToolCallID: "zzz", Text: "extra"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "a", Name: "x"}, {Name: "z"}}},
	}
	out := RepairToolPairs(in)
	want := []struct {
		role Role
		id   string
		text string
	}{
		{RoleUser, "", "hi"},
		{RoleAssistant, "", ""},
		{RoleTool, "a", "dup"},
		{RoleTool, "b", "B"},
		{RoleUser, "", "look"},
		{RoleAssistant, "", ""},
		{RoleTool, "a_2", missingToolResult},
		{RoleTool, "call_7_1", missingToolResult},
	}
	if len(out) != len(want) {
		t.Fatalf("got %d messages: %+v", len(out), out)
	}
	for i, w := range want {
		if out[i].Role != w.role || out[i].ToolCallID != w.id || (w.text != "" && out[i].Text != w.text) {
			t.Fatalf("msg %d = %+v, want %+v", i, out[i], w)
		}
	}
	if in[7].ToolCalls[0].ID != "a" || in[3].ToolCallID != "b" {
		t.Fatal("input was modified")
	}
}

func TestRepairToolPairsKeepsAValidHistory(t *testing.T) {
	in := []Message{
		{Role: RoleUser, Text: "hi"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "a"}, {ID: "b"}}},
		{Role: RoleTool, ToolCallID: "a", Text: "A"},
		{Role: RoleTool, ToolCallID: "b", Text: "B"},
		{Role: RoleUser, Text: "shot", Images: []Image{{}}},
		{Role: RoleAssistant, Text: "done"},
	}
	out := RepairToolPairs(in)
	if len(out) != len(in) {
		t.Fatalf("%+v", out)
	}
	for i := range in {
		if out[i].Role != in[i].Role || out[i].ToolCallID != in[i].ToolCallID || out[i].Text != in[i].Text {
			t.Fatalf("msg %d changed: %+v", i, out[i])
		}
	}
}
