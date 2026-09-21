package dummy

import (
	"context"
	"strings"
	"testing"

	"silo.agent/internal/llm"
)

func collect(t *testing.T, c llm.Client, req llm.Request) []llm.Event {
	t.Helper()
	stream, err := c.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	var out []llm.Event
	for stream.Next() {
		out = append(out, stream.Event())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream err: %v", err)
	}
	return out
}

func textOf(events []llm.Event) string {
	var b strings.Builder
	for _, ev := range events {
		if ev.Kind == llm.EventText {
			b.WriteString(ev.Text)
		}
	}
	return b.String()
}

func TestRegisteredProvider(t *testing.T) {
	if !llm.Known(Provider) {
		t.Fatal("dummy provider not registered")
	}
	if _, err := llm.New(Provider, llm.Settings{"api_key": "x"}); err != nil {
		t.Fatalf("new: %v", err)
	}
}

func TestDefaultTokenEcho(t *testing.T) {
	Reset()
	c, _ := llm.New(Provider, nil)
	events := collect(t, c, llm.Request{
		Model:    "echo",
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "Test_01_Input"}},
	})
	if got := textOf(events); got != "Test_01_Output" {
		t.Fatalf("text %q", got)
	}
	// The stream must terminate with done and carry usage.
	var done bool
	for _, ev := range events {
		if ev.Kind == llm.EventDone {
			done = true
		}
	}
	if !done {
		t.Fatal("no done event")
	}
}

func TestPlainEcho(t *testing.T) {
	Reset()
	c, _ := llm.New(Provider, nil)
	events := collect(t, c, llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Text: "hello"}}})
	if got := textOf(events); got != "Echo: hello" {
		t.Fatalf("text %q", got)
	}
}

func TestScenarioMultiTurn(t *testing.T) {
	Reset()
	Script("Test_02",
		Turn{Reasoning: "thinking", ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"echo hi"}`}}},
		Turn{Text: "finished"},
	)
	c, _ := llm.New(Provider, nil)
	base := llm.Request{
		Model:    "echo",
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "Test_02_Input"}},
	}

	first := collect(t, c, base)
	var haveStart, haveDelta, haveReasoning bool
	var args strings.Builder
	for _, ev := range first {
		switch ev.Kind {
		case llm.EventToolCallStart:
			haveStart = ev.ToolName == "terminal"
		case llm.EventToolCallDelta:
			haveDelta = true
			args.WriteString(ev.Text)
		case llm.EventReasoning:
			haveReasoning = true
		}
	}
	if !haveStart || !haveDelta || !haveReasoning {
		t.Fatalf("first turn start=%v delta=%v reasoning=%v", haveStart, haveDelta, haveReasoning)
	}
	if args.String() != `{"command":"echo hi"}` {
		t.Fatalf("tool args %q", args.String())
	}

	// Append the assistant turn (with tool calls) and the tool result, then the
	// next stream should return the scripted final text.
	base.Messages = append(base.Messages,
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "dummy_call_0", Name: "terminal", Arguments: `{"command":"echo hi"}`}}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "dummy_call_0", Text: "hi"},
	)
	second := collect(t, c, base)
	if got := textOf(second); got != "finished" {
		t.Fatalf("second text %q", got)
	}
}

func TestCompleteTitle(t *testing.T) {
	c, _ := llm.New(Provider, nil)
	res, err := c.Complete(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Text: "hi"}}})
	if err != nil || res.Text == "" {
		t.Fatalf("complete: %v %+v", err, res)
	}
}

func TestResetClearsScenarios(t *testing.T) {
	Script("Test_03", Turn{Text: "scripted"})
	Reset()
	c, _ := llm.New(Provider, nil)
	events := collect(t, c, llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Text: "Test_03_Input"}}})
	if got := textOf(events); got != "Test_03_Output" {
		t.Fatalf("after reset text %q", got)
	}
}
