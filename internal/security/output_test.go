package security

import "testing"

func TestOutputNormalises(t *testing.T) {
	for in, want := range map[string]string{
		OutPass: OutPass, OutFlag: OutFlag, OutCheck: OutCheck, OutWithhold: OutWithhold,
		"": OutPass, "allow": OutPass, "nonsense": OutPass,
	} {
		if got := Output(in); got != want {
			t.Errorf("Output(%q) = %q, want %q", in, got, want)
		}
	}
}

// What comes from outside the Bot is checked unless the owner says otherwise;
// what the Bot or its owner wrote is not.
func TestDefaultOutput(t *testing.T) {
	checked := [][2]string{
		{Web, "search"}, {Mailbox, "read"}, {Mailbox, "list"}, {Chats, "read"}, {Bot, "transcribe"},
	}
	for _, k := range checked {
		if got, ok := DefaultOutput(k[0], k[1]); !ok || got != OutCheck {
			t.Errorf("DefaultOutput(%s.%s) = %q, %v, want check", k[0], k[1], got, ok)
		}
	}
	passed := [][2]string{
		{Python, "run"}, {Terminal, "run"}, {Files, "read"}, {Desktop, "look"}, {Bot, "recall"},
		{Bot, "search_docs"}, {Tasks, "read"}, {Agents, "status"}, {Secrets, "token"}, {Channels, "abc"},
		{Model, "decide"}, {Skills, "load"},
	}
	for _, k := range passed {
		if got, ok := DefaultOutput(k[0], k[1]); !ok || got != OutPass {
			t.Errorf("DefaultOutput(%s.%s) = %q, %v, want pass", k[0], k[1], got, ok)
		}
	}
	// A connector's action is not in the catalog: its definition decides.
	if got, ok := DefaultOutput("github", "list_issues"); ok {
		t.Errorf("DefaultOutput(github.list_issues) = %q, want no catalog default", got)
	}
}

// Every built-in rule the owner can set has a row, or sweepRules drops it.
func TestCheckedActionsHaveRows(t *testing.T) {
	rows := map[string]bool{}
	for _, r := range BuiltinRows() {
		rows[Key(r.Connector, r.Action)] = true
	}
	for key, s := range catalog {
		if s.output == OutCheck && !rows[key] {
			t.Errorf("%s is checked by default but has no row on the Rules page", key)
		}
	}
}
