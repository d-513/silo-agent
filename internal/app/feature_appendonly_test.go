package app_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"silo.agent/internal/app/tool"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// assertAppendOnly checks the property prompt caches depend on: every model
// request of a conversation starts with exactly the previous one's system
// prompt and messages, and only adds to the end.
func assertAppendOnly(t *testing.T, reqs []llm.Request) {
	t.Helper()
	for i := 1; i < len(reqs); i++ {
		prev, next := reqs[i-1], reqs[i]
		if !reflect.DeepEqual(prev.System, next.System) {
			t.Fatalf("request %d changed the system prompt:\n%s", i, firstSystemDiff(prev.System, next.System))
		}
		if !reflect.DeepEqual(prev.Tools, next.Tools) {
			t.Fatalf("request %d changed the tool list", i)
		}
		if len(next.Messages) < len(prev.Messages) {
			t.Fatalf("request %d has %d messages, fewer than the %d before it", i, len(next.Messages), len(prev.Messages))
		}
		for j, m := range prev.Messages {
			if !reflect.DeepEqual(m, next.Messages[j]) {
				t.Fatalf("request %d rewrote message %d:\n was %+v\n now %+v", i, j, m, next.Messages[j])
			}
		}
	}
}

func firstSystemDiff(a, b []llm.SystemBlock) string {
	for i := range a {
		if i >= len(b) {
			return "a block was removed"
		}
		if a[i] != b[i] {
			return "block " + string(rune('0'+i)) + ":\n was " + a[i].Text + "\n now " + b[i].Text
		}
	}
	return "a block was added"
}

func addTask(t *testing.T, h *apptest.H, chat string, n int, text string) {
	t.Helper()
	if err := h.DB.Create(&db.TaskItem{ID: ids.New(), ChatID: chat, N: n, Text: text, CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
}

// countIn is how many messages of the request hold s.
func countIn(req llm.Request, s string) int {
	n := 0
	for _, m := range req.Messages {
		n += strings.Count(m.Text, s)
	}
	return n
}

// A tool loop followed by a second message: what the next run replays from
// the database is byte for byte what the live run sent, so the provider's
// cache of the conversation keeps hitting.
func TestHistoryIsAppendOnly(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_AO1",
		dummy.Turn{Text: "Checking the agents.", ToolCalls: []llm.ToolCall{{ID: "toolu_first", Name: "agent_status", Arguments: `{}`}}},
		dummy.Turn{Text: "A progress note.\n<section_send />\nOnce more.", ToolCalls: []llm.ToolCall{
			{ID: "toolu_a", Name: "agent_status", Arguments: `{"events":3}`},
			{ID: "toolu_b", Name: "agent_status", Arguments: `{"name":"nobody"}`},
		}},
		dummy.Turn{Text: "Test_AO1 finished"},
	)
	h := apptest.New(t)
	id := h.CreateBot("Appender").GetId()
	chat := h.FirstChat(id)
	addTask(t, h, chat, 1, "write the report")

	r1, _ := h.Send(id, chat, "Test_AO1_Input start")
	h.WaitRun(r1)
	addTask(t, h, chat, 2, "send the report")
	r2, _ := h.Send(id, chat, "and a follow-up")
	h.WaitRun(r2)
	r3, _ := h.Send(id, chat, "one more")
	h.WaitRun(r3)

	reqs := dummy.Streamed()
	if len(reqs) != 5 {
		t.Fatalf("%d model requests, want 5", len(reqs))
	}
	assertAppendOnly(t, reqs)

	last := reqs[len(reqs)-1]
	// The model's own words on a tool turn and the provider's call ids are
	// part of what it is shown again.
	var said, ids []string
	for _, m := range last.Messages {
		if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 {
			said = append(said, m.Text)
			for _, tc := range m.ToolCalls {
				ids = append(ids, tc.ID)
			}
		}
	}
	if len(said) != 2 || said[0] != "Checking the agents." || !strings.Contains(said[1], "<section_send />") {
		t.Fatalf("tool turns replayed as %q", said)
	}
	if strings.Join(ids, ",") != "toolu_first,toolu_a,toolu_b" {
		t.Fatalf("call ids replayed as %v", ids)
	}
	// The taskboard is told once per version, not re-sent with every turn.
	if n := countIn(last, "write the report"); n != 2 {
		t.Fatalf("the first task is in the context %d times, want 2 (once per board version)", n)
	}
	if n := countIn(last, "send the report"); n != 1 {
		t.Fatalf("the second task is in the context %d times, want 1", n)
	}
}

// A message sent while a tool runs is seen after that tool's result, and a
// later run replays it in the same place.
func TestInjectedMessageKeepsItsPlace(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_AO2",
		memCall("sleep", `{"seconds":30}`),
		memCall("agent_status", `{}`),
		dummy.Turn{Text: "Test_AO2 finished"},
	)
	h := apptest.New(t)
	id := h.CreateBot("Injector").GetId()
	chat := h.FirstChat(id)
	r1, _ := h.Send(id, chat, "Test_AO2_Input start")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, ok := find(h.Events(r1), "tool"); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sleep never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got, _ := h.Send(id, chat, "interjection while you sleep"); got != r1 {
		t.Fatalf("the message started run %s instead of joining %s", got, r1)
	}
	h.WaitRun(r1)
	r2, _ := h.Send(id, chat, "afterwards")
	h.WaitRun(r2)

	reqs := dummy.Streamed()
	assertAppendOnly(t, reqs)
	last := reqs[len(reqs)-1].Messages
	var order []string
	for _, m := range last {
		switch {
		case m.Role == llm.RoleTool && strings.Contains(m.Text, "message"):
			order = append(order, "sleep-result")
		case m.Role == llm.RoleUser && strings.Contains(m.Text, "interjection while you sleep"):
			order = append(order, "interjection")
		}
	}
	if strings.Join(order, ",") != "sleep-result,interjection" {
		t.Fatalf("replayed order %v\n%+v", order, last)
	}
}

// usageInputs are the input sizes the provider reported, one per model call.
func usageInputs(t *testing.T, evs []db.RunEvent) []int {
	t.Helper()
	var out []int
	for _, e := range evs {
		if e.Kind != "usage" {
			continue
		}
		var u struct {
			Input int `json:"input"`
		}
		if err := json.Unmarshal([]byte(e.Body), &u); err != nil {
			t.Fatal(err)
		}
		out = append(out, u.Input)
	}
	return out
}

// A long tool loop nears the context window: the old tool results are cleared
// instead of the conversation being summarized, and the next run replays the
// cleared context exactly.
func TestStaleToolResultsAreClearedBeforeCompaction(t *testing.T) {
	dummy.Reset()
	h := compactHarness(t)
	h.App.Tools.Register(tool.Def{
		Name: "test_blob", Description: "a large result", ExposeCompletion: true, SelfGated: true,
		Run: func(_ context.Context, c *tool.Call) (tool.Result, error) {
			return tool.Result{Text: c.Str("tag") + " " + strings.Repeat("blob ", 2300)}, nil
		},
	})
	var turns []dummy.Turn
	for _, tag := range []string{"one", "two", "three", "four", "five", "six", "seven", "eight"} {
		turns = append(turns, memCall("test_blob", `{"tag":"`+tag+`"}`))
	}
	turns = append(turns, dummy.Turn{Text: "Test_CL1 finished"})
	dummy.Script("Test_CL1", turns...)
	id := h.CreateBot("Clearer").GetId()

	// Measure the loop with room to spare, then give the model a window it
	// fills two results before the end.
	setWindow(t, h, 100_000_000)
	cal, _ := h.Send(id, h.FirstChat(id), "Test_CL1_Input measure")
	inputs := usageInputs(t, h.WaitRun(cal))
	if len(inputs) != 9 {
		t.Fatalf("%d model calls in the measuring run, want 9", len(inputs))
	}
	setWindow(t, h, inputs[6]*10/8)

	dummy.Reset()
	dummy.Script("Test_CL1", turns...)
	chat := newChat(t, h, id)
	r1, _ := h.Send(id, chat, "Test_CL1_Input for real")
	evs := h.WaitRun(r1)
	if _, ok := find(evs, "compaction"); ok {
		t.Fatalf("the conversation was summarized instead of cleared: %s", strings.Join(kinds(evs), ","))
	}
	if _, ok := find(evs, "cleared"); !ok {
		t.Fatalf("nothing was cleared: %s", strings.Join(kinds(evs), ","))
	}
	if !strings.Contains(h.RunBody(r1), "Test_CL1 finished") {
		t.Fatalf("the run did not finish:\n%s", h.RunBody(r1))
	}
	reqs := dummy.Streamed()
	last := reqs[len(reqs)-1]
	if n := countIn(last, clearedText); n < 3 {
		t.Fatalf("%d results cleared in the last request, want at least 3", n)
	}
	// Every call is still there, with its newest results intact.
	calls, intact := 0, 0
	for _, m := range last.Messages {
		calls += len(m.ToolCalls)
		if m.Role == llm.RoleTool && strings.Contains(m.Text, "blob blob") {
			intact++
		}
	}
	if calls != 8 || intact < 3 {
		t.Fatalf("%d calls and %d intact results in the last request", calls, intact)
	}

	// The next run rebuilds that same cleared context from the log.
	r2, _ := h.Send(id, chat, "and then?")
	h.WaitRun(r2)
	reqs = dummy.Streamed()
	assertAppendOnly(t, reqs[len(reqs)-2:])
}

const clearedText = "[Old tool output cleared to save context. Run the tool again if you need it.]"
