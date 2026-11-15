//go:build live

package llm

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveClaude runs the real Claude Code binary: a tool loop over two
// requests, a run that ends on its tools, and a resumed session. It needs a
// signed-in provider directory:
//
//	SILO_CLAUDE_DIR=data/claude SILO_CLAUDE_BIN=tmp/claude-bin/package/claude \
//	  go test -tags live ./internal/llm -run TestLiveClaude -v
func TestLiveClaude(t *testing.T) {
	dir := os.Getenv("SILO_CLAUDE_DIR")
	if dir == "" {
		t.Skip("SILO_CLAUDE_DIR not set")
	}
	model := os.Getenv("SILO_CLAUDE_MODEL")
	if model == "" {
		model = "haiku"
	}
	client, err := New(Claude, Settings{"dir": dir, "binary": os.Getenv("SILO_CLAUDE_BIN")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	defer claudeSessions.stopAll()

	st, err := client.(Account).AccountStatus(ctx)
	if err != nil || !st.SignedIn {
		t.Fatalf("not signed in: %+v %v", st, err)
	}
	list, err := client.(ModelLister).ListModels(ctx)
	if err != nil || len(list) == 0 {
		t.Fatalf("models: %v %v", list, err)
	}
	t.Logf("%d models, first %s (%s)", len(list), list[0].ID, list[0].Name)
	levels, err := client.(Thinker).ThinkingLevels(ctx, model)
	t.Logf("thinking levels of %s: %v %v", model, levels, err)

	tools := []Tool{{Name: "echo", Description: "Echo text back.", Parameters: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)}}
	system := []SystemBlock{
		{Text: "You are a terse test bot. " + strings.Repeat("Stable filler. ", 400), CacheAfter: true},
		{Text: "You remember: the password is plum."},
	}
	req := Request{Model: model, System: system, Tools: tools, Conversation: "live-" + newUUID()}
	turn := func(req Request) (Message, Usage) {
		t.Helper()
		s, err := client.Stream(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		var m Message
		var u Usage
		calls := map[int]*ToolCall{}
		var order []int
		for s.Next() {
			ev := s.Event()
			switch ev.Kind {
			case EventText:
				m.Text += ev.Text
			case EventToolCallStart:
				calls[ev.Index] = &ToolCall{ID: ev.ToolCallID, Name: ev.ToolName}
				order = append(order, ev.Index)
			case EventToolCallDelta:
				calls[ev.Index].Arguments += ev.Text
			case EventUsage:
				u = ev.Usage
			}
		}
		if err := s.Err(); err != nil {
			t.Fatal(err)
		}
		m.Role = RoleAssistant
		for _, i := range order {
			m.ToolCalls = append(m.ToolCalls, *calls[i])
		}
		t.Logf("turn: text=%q calls=%+v usage=%+v", m.Text, m.ToolCalls, u)
		return m, u
	}

	// A tool loop: the call comes back as Silo's own name, its result goes
	// in with the next request.
	req.Messages = []Message{{Role: RoleUser, Text: "Call echo with text 'one', then reply with the echoed word and the password you remember."}}
	a1, _ := turn(req)
	if len(a1.ToolCalls) != 1 || a1.ToolCalls[0].Name != "echo" {
		t.Fatalf("want one echo call, got %+v", a1.ToolCalls)
	}
	req.Messages = append(req.Messages, a1, Message{Role: RoleTool, ToolCallID: a1.ToolCalls[0].ID, Text: "one"})
	a2, u2 := turn(req)
	if !strings.Contains(strings.ToLower(a2.Text), "one") || !strings.Contains(strings.ToLower(a2.Text), "plum") {
		t.Fatalf("reply %q lacks the tool result or the context", a2.Text)
	}
	if u2.CacheReadTokens == 0 {
		t.Errorf("second request read nothing from the cache: %+v", u2)
	}

	// A run that ends on its tools: the binary's turn is closed without a
	// model call, and the next message goes on in the same session.
	req.Messages = append(req.Messages, a2, Message{Role: RoleUser, Text: "Call echo with text 'two' and say nothing else."})
	a3, _ := turn(req)
	if len(a3.ToolCalls) != 1 {
		t.Fatalf("want one call, got %+v", a3)
	}
	req.Messages = append(req.Messages, a3, Message{Role: RoleTool, ToolCallID: a3.ToolCalls[0].ID, Text: "two"})
	if err := client.(TurnEnder).EndTurn(ctx, req); err != nil {
		t.Fatal(err)
	}
	s := claudeSessions.get(client.(*claudeClient).conf, req.Conversation)
	if s.dirty || len(s.pending) != 0 {
		t.Fatalf("session after EndTurn: dirty=%v pending=%v", s.dirty, s.pending)
	}
	id := s.id

	// A restarted control plane: the process is gone, the session resumes
	// from disk and still reads its prefix from the cache.
	claudeSessions.stopAll()
	req.Messages = append(req.Messages, Message{Role: RoleUser, Text: "Which two words did echo return so far? Answer with just the two words."})
	a4, u4 := turn(req)
	low := strings.ToLower(a4.Text)
	if !strings.Contains(low, "one") || !strings.Contains(low, "two") {
		t.Fatalf("resumed session forgot: %q", a4.Text)
	}
	if got := claudeSessions.get(client.(*claudeClient).conf, req.Conversation).id; got != id {
		t.Errorf("the session was started again (%s → %s) instead of resumed", id, got)
	}
	if u4.CacheReadTokens == 0 {
		t.Errorf("resumed request read nothing from the cache: %+v", u4)
	}

	// A history the session does not hold starts a new one from the record.
	req.Messages = []Message{
		{Role: RoleUser, Text: "Remember the number 41."},
		{Role: RoleAssistant, Text: "Noted: 41."},
		{Role: RoleUser, Text: "What is that number plus one? Just the number."},
	}
	a5, _ := turn(req)
	if !strings.Contains(a5.Text, "42") {
		t.Fatalf("seeded session answered %q", a5.Text)
	}

	// A call on its own.
	res, err := client.Complete(ctx, Request{Model: model, System: []SystemBlock{{Text: "Reply with one word."}}, Messages: []Message{{Role: RoleUser, Text: "Say: pong"}}})
	if err != nil || !strings.Contains(strings.ToLower(res.Text), "pong") {
		t.Fatalf("complete: %q %v", res.Text, err)
	}
}

// TestLiveClaudeSignInPages starts both sign-ins of the real binary in an
// empty directory and checks each shows its page; nobody signs in, so it
// needs no account and spends nothing.
func TestLiveClaudeSignInPages(t *testing.T) {
	bin := os.Getenv("SILO_CLAUDE_BIN")
	if bin == "" {
		t.Skip("SILO_CLAUDE_BIN not set")
	}
	client, err := New(Claude, Settings{"dir": t.TempDir(), "binary": bin})
	if err != nil {
		t.Fatal(err)
	}
	acc := client.(Account)
	ctx := context.Background()
	if st, err := acc.AccountStatus(ctx); err != nil || st.SignedIn || st.Problem != "" {
		t.Fatalf("an empty directory is signed out: %+v %v", st, err)
	}
	for _, m := range acc.SignInMethods() {
		step, err := acc.BeginSignIn(ctx, m.ID)
		if err != nil || !strings.Contains(step.URL, "oauth/authorize") || !strings.Contains(step.URL, "state=") || strings.ContainsAny(step.URL, " \r\n\x1b") {
			t.Fatalf("%s: %q %v", m.ID, step.URL, err)
		}
		t.Logf("%s: page of %d characters", m.ID, len(step.URL))
	}
	// A wrong code is refused in the binary's words.
	if _, err := acc.FinishSignIn(ctx, "not-a-code"); err == nil {
		t.Fatal("a made-up code signed in")
	} else {
		t.Logf("wrong code: %v", err)
	}
}

// TestLiveClaudeKnowsItsMachine checks the notes that explain what the binary
// adds around Silo's prompt: asked where it works and what its tools are
// called, the model answers from Silo's prompt, not from the binary's
// Environment note or its prefixed tool names.
func TestLiveClaudeKnowsItsMachine(t *testing.T) {
	dir := os.Getenv("SILO_CLAUDE_DIR")
	if dir == "" {
		t.Skip("SILO_CLAUDE_DIR not set")
	}
	client, err := New(Claude, Settings{"dir": dir, "binary": os.Getenv("SILO_CLAUDE_BIN")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	res, err := client.Complete(ctx, Request{
		Model:    "haiku",
		System:   []SystemBlock{{Text: "You are Silo, working as this Bot: one isolated Linux machine. Home of your work: `/workspace`. Read files with the `read` tool.", CacheAfter: true}},
		Tools:    []Tool{{Name: "read", Description: "Read a file.", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}},
		Messages: []Message{{Role: RoleUser, Text: "Without calling a tool, answer in two short lines: (1) the directory your work lives in, (2) the name of the tool you read files with, as your instructions call it."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("answer: %s", res.Text)
	if !strings.Contains(res.Text, "/workspace") || strings.Contains(res.Text, "claude/work") || strings.Contains(res.Text, "/config") {
		t.Errorf("the model placed itself in the binary's directory: %q", res.Text)
	}
}
