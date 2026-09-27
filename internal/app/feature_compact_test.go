package app_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// compactHarness runs on a real silo.yaml so a test can shrink the context
// window after measuring how big the dummy's requests are.
func compactHarness(t *testing.T) *apptest.H {
	dir := t.TempDir()
	path := dir + "/silo.yaml"
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)+"debug: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return apptest.New(t, apptest.WithConfigPath(path))
}

func setWindow(t *testing.T, h *apptest.H, window int) {
	t.Helper()
	raw := apptest.DefaultYAML(h.DataDir) + fmt.Sprintf(`debug: true
context:
  compact_at: 0.8
  windows:
    - model: dummy/echo
      window: %d
`, window)
	if err := h.Store.WriteYAML([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func newChat(t *testing.T, h *apptest.H, botID string) string {
	t.Helper()
	c, err := h.Client.CreateChat(h.Ctx(), connect.NewRequest(&v1.CreateChatRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return c.Msg.GetId()
}

func kinds(evs []db.RunEvent) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		if e.Kind == "tool_args_chunk" || e.Kind == "chunk" || e.Kind == "thinking_chunk" {
			continue
		}
		out = append(out, e.Kind)
	}
	return out
}

func find(evs []db.RunEvent, kind string) (db.RunEvent, bool) {
	for _, e := range evs {
		if e.Kind == kind {
			return e, true
		}
	}
	return db.RunEvent{}, false
}

// lastChatRequest is the formatted request of the newest chat-model call.
func lastChatRequest(t *testing.T, h *apptest.H, botID string) string {
	t.Helper()
	var rows []db.LLMLog
	h.DB.Where("bot_id = ? AND label = ?", botID, "chat").Order("at").Find(&rows)
	if len(rows) == 0 {
		t.Fatal("no chat model call logged")
	}
	return rows[len(rows)-1].Request
}

func TestManualCompaction(t *testing.T) {
	dummy.Reset()
	h := compactHarness(t)
	bot := h.CreateBot("Compactor")
	id := bot.GetId()
	chat := h.FirstChat(id)

	compact := func() (string, error) {
		res, err := h.Client.CompactChat(h.Ctx(), connect.NewRequest(&v1.CompactChatRequest{BotId: id, ChatId: chat}))
		if err != nil {
			return "", err
		}
		return res.Msg.GetRunId(), nil
	}
	if _, err := compact(); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("empty chat compaction: %v", err)
	}

	r1, _ := h.Send(id, chat, "Test_C1_Input alpha question")
	h.WaitRun(r1)
	r2, _ := h.Send(id, chat, "Test_C1_Input beta question")
	h.WaitRun(r2)

	runID, err := compact()
	if err != nil {
		t.Fatalf("CompactChat: %v", err)
	}
	evs := h.WaitRun(runID)
	got := strings.Join(kinds(evs), ",")
	if !strings.HasPrefix(got, "compacting,compaction,usage") {
		t.Fatalf("compaction run events %s", got)
	}
	c, _ := find(evs, "compaction")
	if c.Body != "Dummy Summary Test_C1_Compacted" || c.Tool != "manual" {
		t.Fatalf("compaction %q %q", c.Body, c.Tool)
	}
	var u db.RunEvent
	u, _ = find(evs, "usage")
	if !strings.Contains(u.Body, `"window":128000`) {
		t.Fatalf("usage lacks the window: %s", u.Body)
	}
	if _, ok := find(evs, "user"); ok {
		t.Fatal("a compaction run must not add a user message")
	}

	// A chat that is only a summary has nothing left to compact.
	if _, err := compact(); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("second compaction: %v", err)
	}

	r3, _ := h.Send(id, chat, "gamma question")
	h.WaitRun(r3)
	req := lastChatRequest(t, h, id)
	if !strings.Contains(req, "Dummy Summary Test_C1_Compacted") || !strings.Contains(req, "gamma question") {
		t.Fatalf("next request lacks the summary or the new message:\n%s", req)
	}
	if strings.Contains(req, "alpha question") || strings.Contains(req, "beta question") {
		t.Fatalf("compacted turns were replayed:\n%s", req)
	}

	// Diverging after the compaction keeps the cut in the new chat.
	nc := h.DivergeChat(id, chat, h.LastUserEvent(chat).ID)
	r4, _ := h.Send(id, nc.GetId(), "delta question")
	h.WaitRun(r4)
	req = lastChatRequest(t, h, id)
	if !strings.Contains(req, "Dummy Summary") || strings.Contains(req, "alpha question") {
		t.Fatalf("diverged chat lost the compaction cut:\n%s", req)
	}
}

func TestCompactChatRefusedWhileReplyRuns(t *testing.T) {
	dummy.Reset()
	h := compactHarness(t)
	bot := h.CreateBot("Busy")
	id := bot.GetId()
	chat := h.FirstChat(id)
	// The second message parks on an approval (create_automation is Ask by
	// default), so the chat has history and a live run.
	dummy.Script("Test_C5",
		dummy.Turn{Text: "warm"},
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "create_automation", Arguments: `{"name":"Sweep","prompt":"x","schedule":"*/30 * * * *"}`}}},
		dummy.Turn{Text: "done"},
	)
	r1, _ := h.Send(id, chat, "Test_C5_Input warmup")
	h.WaitRun(r1)
	r2, _ := h.Send(id, chat, "please")
	h.WaitApproval(id)
	_, err := h.Client.CompactChat(h.Ctx(), connect.NewRequest(&v1.CompactChatRequest{BotId: id, ChatId: chat}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "reply is running") {
		t.Fatalf("compaction during a live run: %v", err)
	}
	if _, err := h.Client.StopRun(h.Ctx(), connect.NewRequest(&v1.StopRunRequest{BotId: id, ChatId: chat})); err != nil {
		t.Fatal(err)
	}
	h.WaitRun(r2)
}

// inputOf is the input token count of a run's first usage event.
func inputOf(t *testing.T, evs []db.RunEvent) int {
	t.Helper()
	u, ok := find(evs, "usage")
	if !ok {
		t.Fatal("no usage event")
	}
	i := strings.Index(u.Body, `"input":`)
	rest := u.Body[i+len(`"input":`):]
	n, err := strconv.Atoi(rest[:strings.IndexAny(rest, ",}")])
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAutoCompactionMidRun(t *testing.T) {
	dummy.Reset()
	h := compactHarness(t)
	bot := h.CreateBot("Grower")
	id := bot.GetId()

	// Measure the dummy's request size (it reports runes as tokens), then
	// give the model a window only twice that.
	cal, _ := h.Send(id, h.FirstChat(id), "Test_C6_Input")
	base := inputOf(t, h.WaitRun(cal))
	setWindow(t, h, 2*base)

	pad := strings.Repeat("x", 3*base)
	dummy.Script("Test_C7",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "no_such_tool", Arguments: `{"pad":"` + pad + `"}`}}},
		dummy.Turn{Text: "never reached"},
	)
	dummy.Script("Test_C7_Compacted", dummy.Turn{Text: "resumed after compaction"})

	chat := newChat(t, h, id)
	r, _ := h.Send(id, chat, "Test_C7_Input grow")
	evs := h.WaitRun(r)
	got := strings.Join(kinds(evs), ",")
	if !strings.Contains(got, "tool_result,compacting,compaction") {
		t.Fatalf("no mid-run compaction: %s", got)
	}
	if c, _ := find(evs, "compaction"); c.Tool != "auto" {
		t.Fatalf("reason %q", c.Tool)
	}
	if !strings.Contains(h.RunBody(r), "resumed after compaction") {
		t.Fatalf("run did not resume:\n%s", h.RunBody(r))
	}
	req := lastChatRequest(t, h, id)
	if strings.Contains(req, pad[:200]) || !strings.Contains(req, "Continue from where you left off") {
		t.Fatalf("post-compaction request still holds the padding or lacks the continue note")
	}
}

func TestOverflowErrorCompactsAndRetries(t *testing.T) {
	dummy.Reset()
	h := compactHarness(t)
	bot := h.CreateBot("Overflow")
	id := bot.GetId()
	chat := h.FirstChat(id)
	r1, _ := h.Send(id, chat, "Test_C8_Input "+strings.Repeat("filler ", 3000))
	base := inputOf(t, h.WaitRun(r1))

	// The provider refuses anything bigger than the history above, though
	// the window says it fits; the loop must compact and retry once.
	dummy.Script("Test_C8", dummy.Turn{Text: "unused"}, dummy.Turn{OverflowAbove: base, Text: "unused"})
	dummy.Script("Test_C8_Compacted", dummy.Turn{Text: "answered after retry"})
	r2, _ := h.Send(id, chat, "and another")
	evs := h.WaitRun(r2)
	if _, ok := find(evs, "compaction"); !ok {
		t.Fatalf("no compaction on overflow: %s", strings.Join(kinds(evs), ","))
	}
	if _, ok := find(evs, "error"); ok {
		t.Fatalf("overflow surfaced as an error: %s", h.RunBody(r2))
	}
	if !strings.Contains(h.RunBody(r2), "answered after retry") {
		t.Fatalf("no retry answer:\n%s", h.RunBody(r2))
	}
}
