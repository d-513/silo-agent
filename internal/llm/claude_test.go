package llm

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"silo.agent/internal/llm/claudetest"
)

// The test binary doubles as the fake Claude Code binary.
func init() { claudetest.Main() }

func fakeClaude(t *testing.T) *claudeClient {
	t.Helper()
	c, err := New(Claude, Settings{"binary": claudetest.Install(t), "dir": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(claudeSessions.stopAll)
	return c.(*claudeClient)
}

var echoTool = []Tool{{Name: "echo", Description: "Echo text back.", Parameters: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)}}

// claudeTurn runs one request to its end.
func claudeTurn(t *testing.T, c Client, req Request) (Message, Usage) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := c.Stream(ctx, req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer st.Close()
	m := Message{Role: RoleAssistant}
	var u Usage
	calls := map[int]*ToolCall{}
	var order []int
	for st.Next() {
		ev := st.Event()
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
	if err := st.Err(); err != nil {
		t.Fatalf("turn: %v", err)
	}
	for _, i := range order {
		m.ToolCalls = append(m.ToolCalls, *calls[i])
	}
	return m, u
}

func claudeDump(t *testing.T, c Client, req Request) claudetest.Dump {
	t.Helper()
	m, _ := claudeTurn(t, c, req)
	var d claudetest.Dump
	if err := json.Unmarshal([]byte(m.Text), &d); err != nil {
		t.Fatalf("not a dump: %q", m.Text)
	}
	return d
}

func user(text string) Message { return Message{Role: RoleUser, Text: text} }

func TestClaudeToolLoop(t *testing.T) {
	c := fakeClaude(t)
	req := Request{Model: "opus", Tools: echoTool, Conversation: "chat1", Messages: []Message{user(`Test_Call_echo {"text":"hi"}`)}}
	a1, u := claudeTurn(t, c, req)
	// The model's name for the tool is the binary's; Silo gets its own back.
	if len(a1.ToolCalls) != 1 || a1.ToolCalls[0].Name != "echo" || a1.ToolCalls[0].Arguments != `{"text":"hi"}` || a1.ToolCalls[0].ID == "" {
		t.Fatalf("calls: %+v", a1.ToolCalls)
	}
	// The whole prompt is the input, cached or not.
	if u.InputTokens != 123 || u.CacheReadTokens != 100 || u.CacheWriteTokens != 20 || u.OutputTokens != 7 {
		t.Fatalf("usage: %+v", u)
	}
	s := claudeSessions.get(c.conf, "chat1")
	if len(s.pending) != 1 {
		t.Fatalf("the call should be parked: %+v", s.pending)
	}
	// The result is what Silo's own engine made of the call.
	req.Messages = append(req.Messages, a1, Message{Role: RoleTool, ToolCallID: a1.ToolCalls[0].ID, Text: "pong"})
	a2, _ := claudeTurn(t, c, req)
	if a2.Text != "result: pong" || len(a2.ToolCalls) != 0 {
		t.Fatalf("reply: %+v", a2)
	}
	if w, err := c.ContextWindow(context.Background(), "opus"); err != nil || w != 400000 {
		t.Fatalf("window: %d %v", w, err)
	}
}

func TestClaudeRunsLockedDown(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-host-key")
	t.Setenv("SILO_DATABASE_URL", "postgres://secret")
	c := fakeClaude(t)
	d := claudeDump(t, c, Request{
		Model: "opus", Thinking: ThinkingHigh, Tools: echoTool, Conversation: "chat1",
		System: []SystemBlock{
			{Text: "BASE", CacheAfter: true}, {Text: "SESSION", CacheAfter: true}, {Text: "MEMORIES"},
		},
		Messages: []Message{user("Test_Dump")},
	})
	// Only Silo's prompt and Silo's tools.
	if !strings.HasPrefix(d.System, "BASE\n\nSESSION\n\n") || strings.Contains(d.System, "MEMORIES") {
		t.Fatalf("system: %q", d.System)
	}
	// What the binary adds around the prompt is explained in it: its names
	// for the tools, its note on its own process, and where memories arrive.
	for _, want := range []string{"mcp__silo__read", "headed Environment", "<silo-context>"} {
		if !strings.Contains(d.System, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
	if !slices.Equal(d.Tools, []string{"echo"}) {
		t.Fatalf("tools: %v", d.Tools)
	}
	// What changes between runs goes in the conversation, so the cached
	// system prompt stays the same bytes.
	if len(d.History) != 1 || !strings.HasPrefix(d.History[0], "<silo-context>\nMEMORIES\n</silo-context>\n\n") {
		t.Fatalf("history: %q", d.History)
	}
	args := strings.Join(d.Args, " ")
	for _, want := range []string{"--tools  --strict-mcp-config", "--setting-sources  --disable-slash-commands", "--permission-prompt-tool=stdio", "--model opus", "--effort high", "--session-id "} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q: %s", want, args)
		}
	}
	// Nothing of the host's environment but what the binary needs.
	for _, e := range d.Env {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") || strings.HasPrefix(e, "SILO_DATABASE_URL=") {
			t.Errorf("leaked %s", e)
		}
	}
	if !slices.Contains(d.Env, "CLAUDE_CONFIG_DIR="+c.conf.configDir()) || d.Cwd != c.conf.workDir() {
		t.Fatalf("not in its own directory: cwd %s", d.Cwd)
	}
}

func TestClaudeNeverRunsAForeignTool(t *testing.T) {
	c := fakeClaude(t)
	ctx := context.Background()
	st, err := c.Stream(ctx, Request{Model: "opus", Tools: echoTool, Conversation: "chat1", Messages: []Message{user("Test_Alien")}})
	if err != nil {
		t.Fatal(err)
	}
	for st.Next() {
		if st.Event().Kind == EventToolCallStart {
			t.Fatalf("a tool that is not Silo's reached the loop: %+v", st.Event())
		}
	}
	st.Close()
	if st.Err() == nil || !strings.Contains(st.Err().Error(), "not Silo's") {
		t.Fatalf("err: %v", st.Err())
	}
	s := claudeSessions.get(c.conf, "chat1")
	if s.proc != nil || !s.dirty {
		t.Fatalf("the binary should be stopped and the session dropped: proc=%v dirty=%v", s.proc != nil, s.dirty)
	}
}

func TestClaudeResumesAfterRestart(t *testing.T) {
	c := fakeClaude(t)
	req := Request{Model: "opus", Conversation: "chat1", Messages: []Message{user("hello")}}
	a1, _ := claudeTurn(t, c, req)
	if a1.Text != "echo: hello" {
		t.Fatalf("reply: %q", a1.Text)
	}
	id := claudeSessions.get(c.conf, "chat1").id
	// The control plane restarts: no process, no memory of the session.
	claudeSessions.stopAll()
	req.Messages = append(req.Messages, a1, user("Test_Dump"))
	d := claudeDump(t, c, req)
	if !d.Resumed || d.Session != id {
		t.Fatalf("want session %s resumed, got %s resumed=%v", id, d.Session, d.Resumed)
	}
	// Only the new message was sent: the binary had the rest.
	if !slices.Equal(d.History, []string{"hello", "Test_Dump"}) {
		t.Fatalf("history: %q", d.History)
	}
}

func TestClaudeStartsAgainFromSilosHistory(t *testing.T) {
	c := fakeClaude(t)
	req := Request{Model: "opus", Conversation: "chat1", Messages: []Message{user("hello")}}
	a1, _ := claudeTurn(t, c, req)
	first := claudeSessions.get(c.conf, "chat1").id
	// The history was rewritten (a compaction, an edit): the session's copy
	// is of no use, and the new one is told the history as Silo has it.
	req.Messages = []Message{
		user("summary of before"),
		{Role: RoleAssistant, Text: "ok", ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"x"}`}}},
		{Role: RoleTool, ToolCallID: "c1", Text: "x-result"},
		a1,
		user("and now Test_Dump"),
	}
	d := claudeDump(t, c, req)
	if d.Resumed || d.Session == first {
		t.Fatalf("want a new session, got %s resumed=%v", d.Session, d.Resumed)
	}
	if len(d.History) != 1 {
		t.Fatalf("history: %q", d.History)
	}
	seed := d.History[0]
	for _, want := range []string{"<record>", "## user\nsummary of before", "## you called echo\n{\"text\":\"x\"}", "## result of echo\nx-result", "## you\necho: hello", "</record>\n\nand now Test_Dump"} {
		if !strings.Contains(seed, want) {
			t.Errorf("seed lacks %q:\n%s", want, seed)
		}
	}
}

func TestClaudeAnswersCallsInOrderWithWhatCameMeanwhile(t *testing.T) {
	c := fakeClaude(t)
	req := Request{Model: "opus", Tools: echoTool, Conversation: "chat1", Messages: []Message{user("Test_Two")}}
	a1, _ := claudeTurn(t, c, req)
	if len(a1.ToolCalls) != 2 {
		t.Fatalf("calls: %+v", a1.ToolCalls)
	}
	// Both results, a message the human sent while the tools ran, and an
	// image the loop showed: one request, read together.
	req.Messages = append(req.Messages, a1,
		Message{Role: RoleTool, ToolCallID: a1.ToolCalls[0].ID, Text: "one"},
		Message{Role: RoleTool, ToolCallID: a1.ToolCalls[1].ID, Text: "two"},
		Message{Role: RoleUser, Text: "screenshot", Images: []Image{{Mime: "image/png", Data: []byte("png")}}},
		user("wait, also this"),
	)
	a2, _ := claudeTurn(t, c, req)
	if a2.Text != "result: one | two queued: screenshot\n\nwait, also this[image]" {
		t.Fatalf("reply: %q", a2.Text)
	}
	// The image's turn later reads as a line saying it was seen: the same
	// turn, so the session goes on.
	req.Messages[len(req.Messages)-2] = user("You saw this screenshot; the image is not kept.")
	req.Messages = append(req.Messages, a2, user("Test_Dump"))
	if d := claudeDump(t, c, req); d.Resumed || len(d.History) != 3 {
		t.Fatalf("the session should have gone on: resumed=%v history=%q", d.Resumed, d.History)
	}
}

func TestClaudeEndTurnLeavesNothingWaiting(t *testing.T) {
	c := fakeClaude(t)
	req := Request{Model: "opus", Tools: echoTool, Conversation: "chat1", Messages: []Message{user(`Test_Call_echo {"text":"q"}`)}}
	a1, _ := claudeTurn(t, c, req)
	req.Messages = append(req.Messages, a1, Message{Role: RoleTool, ToolCallID: a1.ToolCalls[0].ID, Text: "asked the owner"})
	if err := c.EndTurn(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	s := claudeSessions.get(c.conf, "chat1")
	if s.dirty || len(s.pending) != 0 || s.proc == nil {
		t.Fatalf("after EndTurn: dirty=%v pending=%v", s.dirty, s.pending)
	}
	id := s.id
	// The human's answer starts the next run in the same session.
	req.Messages = append(req.Messages, user("my answer Test_Dump"))
	d := claudeDump(t, c, req)
	if d.Session != id || len(d.History) != 2 {
		t.Fatalf("session %s (want %s), history %q", d.Session, id, d.History)
	}
}

func TestClaudeCutTurnIsNotReused(t *testing.T) {
	c := fakeClaude(t)
	ctx, cancel := context.WithCancel(context.Background())
	req := Request{Model: "opus", Conversation: "chat1", Messages: []Message{user("Test_Slow")}}
	st, err := c.Stream(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Next() || st.Event().Text != "half a " {
		t.Fatalf("first piece: %+v %v", st.Event(), st.Err())
	}
	cancel()
	if st.Next() {
		t.Fatal("a cancelled turn went on")
	}
	st.Close()
	// Silo keeps "half a " as the reply; the binary's record of the turn is
	// unknown, so the next request starts from Silo's.
	req.Messages = append(req.Messages, Message{Role: RoleAssistant, Text: "half a "}, user("Test_Dump"))
	d := claudeDump(t, c, req)
	if d.Resumed || len(d.History) != 1 || !strings.Contains(d.History[0], "## you\nhalf a ") {
		t.Fatalf("resumed=%v history=%q", d.Resumed, d.History)
	}
}

func TestClaudeFailedTurnSaysWhy(t *testing.T) {
	c := fakeClaude(t)
	st, err := c.Stream(context.Background(), Request{Model: "opus", Conversation: "chat1", Messages: []Message{user("Test_Fail")}})
	if err != nil {
		t.Fatal(err)
	}
	for st.Next() {
	}
	st.Close()
	if st.Err() == nil || !strings.Contains(st.Err().Error(), "500 boom") {
		t.Fatalf("err: %v", st.Err())
	}
}

func TestClaudeCompleteIsACallOnItsOwn(t *testing.T) {
	c := fakeClaude(t)
	res, err := c.Complete(context.Background(), Request{Model: "haiku", System: []SystemBlock{{Text: "Name the chat."}}, Messages: []Message{user("Test_Dump")}})
	if err != nil {
		t.Fatal(err)
	}
	var d claudetest.Dump
	if err := json.Unmarshal([]byte(res.Text), &d); err != nil {
		t.Fatal(err)
	}
	if d.System != "Name the chat." || d.Session != "" || !slices.Contains(d.Args, "--no-session-persistence") || len(d.Tools) != 0 {
		t.Fatalf("dump: %+v", d)
	}
	if res.Usage.InputTokens != 123 {
		t.Fatalf("usage: %+v", res.Usage)
	}
}

func TestClaudeListsModels(t *testing.T) {
	c := fakeClaude(t)
	ctx := context.Background()
	list, err := c.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range list {
		ids = append(ids, m.ID)
	}
	// "default" is one of the others under another name.
	if !slices.Equal(ids, []string{"opus", "haiku", "claude-sonnet-5"}) || list[0].Name != "Opus · For complex work" {
		t.Fatalf("models: %+v", list)
	}
	if levels, err := c.ThinkingLevels(ctx, "haiku"); err != nil || !slices.Equal(levels, []string{"off", "low", "medium", "high"}) {
		t.Fatalf("haiku levels: %v %v", levels, err)
	}
	// A pinned id the alias resolves to reads the alias's levels.
	if levels, _ := c.ThinkingLevels(ctx, "claude-opus-5-5"); len(levels) != 5 {
		t.Fatalf("opus levels: %v", levels)
	}
	if levels, err := c.ThinkingLevels(ctx, "claude-sonnet-5"); err != nil || len(levels) != 0 {
		t.Fatalf("sonnet levels: %v %v", levels, err)
	}
}

func TestClaudeSignIn(t *testing.T) {
	c := fakeClaude(t)
	ctx := context.Background()
	st, err := c.AccountStatus(ctx)
	if err != nil || st.SignedIn || st.Problem != "" {
		t.Fatalf("fresh: %+v %v", st, err)
	}
	step, err := c.BeginSignIn(ctx, "")
	if err != nil || !strings.HasPrefix(step.URL, "https://claude.com/cai/oauth/authorize?") {
		t.Fatalf("begin: %+v %v", step, err)
	}
	// A wrong code fails in the binary's words, without the page's address.
	if _, err := c.FinishSignIn(ctx, "nope"); err == nil || !strings.Contains(err.Error(), "not valid") || strings.Contains(err.Error(), "authorize") {
		t.Fatalf("wrong code: %v", err)
	}
	// The failed sign-in is spent.
	if _, err := c.FinishSignIn(ctx, claudetest.GoodCode); err == nil || !strings.Contains(err.Error(), "start it again") {
		t.Fatalf("spent: %v", err)
	}
	if _, err := c.FinishSignIn(ctx, "a\nb"); err == nil {
		t.Fatal("a code is one line")
	}
	if _, err := c.BeginSignIn(ctx, claudeMethodLogin); err != nil {
		t.Fatal(err)
	}
	save, err := c.FinishSignIn(ctx, " "+claudetest.GoodCode+" ")
	if err != nil || len(save) != 0 {
		t.Fatalf("finish: %v %v", save, err)
	}
	st, _ = c.AccountStatus(ctx)
	if !st.SignedIn || st.Email != claudetest.Email || st.Plan != "max" || st.Expires.Year() != 2030 {
		t.Fatalf("signed in: %+v", st)
	}
	clear, err := c.SignOut(ctx)
	if err != nil || !slices.Equal(clear, []string{"oauth_token"}) {
		t.Fatalf("sign out: %v %v", clear, err)
	}
	if st, _ = c.AccountStatus(ctx); st.SignedIn {
		t.Fatalf("still signed in: %+v", st)
	}
}

func TestClaudeLongLivedToken(t *testing.T) {
	c := fakeClaude(t)
	ctx := context.Background()
	// setup-token only draws on a terminal, with control sequences around
	// the address and the token.
	step, err := c.BeginSignIn(ctx, claudeMethodToken)
	if err != nil || !strings.HasSuffix(step.URL, "state=fake-state") {
		t.Fatalf("begin: %+v %v", step, err)
	}
	save, err := c.FinishSignIn(ctx, claudetest.GoodCode)
	if err != nil || save["oauth_token"] != claudetest.Token {
		t.Fatalf("finish: %v %v", save, err)
	}
	// The token, once saved, is the sign-in.
	c.conf.token = save["oauth_token"]
	if st, _ := c.AccountStatus(ctx); !st.SignedIn || st.Method != "oauth_token" {
		t.Fatalf("status: %+v", st)
	}
	d := claudeDump(t, c, Request{Model: "opus", Messages: []Message{user("Test_Dump")}})
	if !slices.Contains(d.Env, "CLAUDE_CODE_OAUTH_TOKEN="+claudetest.Token) {
		t.Fatal("the token did not reach the binary")
	}
}

func TestClaudeMissingBinary(t *testing.T) {
	c, _ := New(Claude, Settings{"binary": "/nonexistent/claude", "dir": t.TempDir()})
	st, err := c.(Account).AccountStatus(context.Background())
	if err != nil || !strings.Contains(st.Problem, "/nonexistent/claude") {
		t.Fatalf("status: %+v %v", st, err)
	}
	if _, err := c.Stream(context.Background(), Request{Model: "opus", Messages: []Message{user("hi")}}); err == nil || !strings.Contains(err.Error(), "providers.claude.binary") {
		t.Fatalf("stream: %v", err)
	}
}
