package llm

import (
	"context"
	"errors"
	"testing"
)

// stubStream mirrors the real provider streams: Next only checks whether an
// event is available, and Event pops it. A wrapper that peeks in Next would
// drop every other event, so this shape guards that contract.
type stubStream struct {
	events []Event
	pos    int
}

func (s *stubStream) Next() bool { return s.pos < len(s.events) }
func (s *stubStream) Event() Event {
	ev := s.events[s.pos]
	s.pos++
	return ev
}
func (s *stubStream) Err() error   { return nil }
func (s *stubStream) Close() error { return nil }

type stubClient struct {
	streamEvents []Event
	complete     Response
	err          error
}

func (c *stubClient) Stream(context.Context, Request) (Stream, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &stubStream{events: c.streamEvents}, nil
}

func (c *stubClient) Complete(context.Context, Request) (Response, error) {
	if c.err != nil {
		return Response{}, c.err
	}
	return c.complete, nil
}

func TestObserveAssemblesStream(t *testing.T) {
	var got []Record
	inner := &stubClient{streamEvents: []Event{
		{Kind: EventText, Text: "hello "},
		{Kind: EventText, Text: "world"},
		{Kind: EventReasoning, Text: "hmm"},
		{Kind: EventToolCallStart, Index: 0, ToolCallID: "t1", ToolName: "read"},
		{Kind: EventToolCallDelta, Index: 0, Text: `{"path":`},
		{Kind: EventToolCallDelta, Index: 0, Text: `"x"}`},
		{Kind: EventUsage, Usage: Usage{InputTokens: 3, OutputTokens: 4}},
		{Kind: EventDone},
	}}
	c := Observe(inner, "stub", func(r Record) { got = append(got, r) })
	s, err := c.Stream(context.Background(), Request{Model: "m", System: []SystemBlock{{Text: "S"}}})
	if err != nil {
		t.Fatal(err)
	}
	for s.Next() {
		_ = s.Event()
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("records %d", len(got))
	}
	r := got[0]
	if r.Provider != "stub" || r.Model != "m" || r.Text != "hello world" || r.Reasoning != "hmm" {
		t.Fatalf("record %+v", r)
	}
	if len(r.ToolCalls) != 1 || r.ToolCalls[0].Name != "read" || r.ToolCalls[0].Arguments != `{"path":"x"}` {
		t.Fatalf("tool calls %+v", r.ToolCalls)
	}
	if r.Usage.InputTokens != 3 || r.Usage.OutputTokens != 4 {
		t.Fatalf("usage %+v", r.Usage)
	}
}

func TestObserveCompleteAndError(t *testing.T) {
	var got []Record
	inner := &stubClient{complete: Response{Text: "title", Usage: Usage{OutputTokens: 2}}}
	c := Observe(inner, "stub", func(r Record) { got = append(got, r) })
	if _, err := c.Complete(context.Background(), Request{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "title" {
		t.Fatalf("complete record %+v", got)
	}

	got = nil
	boom := errors.New("boom")
	c = Observe(&stubClient{err: boom}, "stub", func(r Record) { got = append(got, r) })
	if _, err := c.Stream(context.Background(), Request{}); err == nil {
		t.Fatal("expected stream error")
	}
	if len(got) != 1 || !errors.Is(got[0].Error, boom) {
		t.Fatalf("error record %+v", got)
	}
}

func TestObserveNilReturnsSameClient(t *testing.T) {
	inner := &stubClient{}
	if Observe(inner, "stub", nil) != Client(inner) {
		t.Fatal("nil observer should not wrap")
	}
}
