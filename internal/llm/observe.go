package llm

import (
	"context"
	"sync"
	"time"
)

// Record is one model call captured by an Observer: the full request and the
// assembled response. Providers never see it; the engine builds it in Observe
// so every feature that goes through a Client is logged the same way.
type Record struct {
	Provider string
	Model    string
	System   []SystemBlock
	Messages []Message
	Tools    []Tool

	Text      string
	Reasoning string
	ToolCalls []ToolCall
	Usage     Usage
	Error     error
	Duration  time.Duration
}

// Observer receives one Record per completed model call. It must not block the
// caller for long; the engine invokes it synchronously when the stream ends.
type Observer func(Record)

// Observe wraps a Client so every Stream and Complete reports a Record to o.
// It is the single instrumentation seam in the model engine: features get
// debug logging by construction, not by remembering to log at each call site.
// A nil observer returns the client unchanged.
func Observe(c Client, provider string, o Observer) Client {
	if o == nil {
		return c
	}
	return &observedClient{inner: c, provider: provider, obs: o}
}

type observedClient struct {
	inner    Client
	provider string
	obs      Observer
}

func (c *observedClient) Stream(ctx context.Context, req Request) (Stream, error) {
	start := time.Now()
	s, err := c.inner.Stream(ctx, req)
	if err != nil {
		c.obs(c.record(req, Record{Error: err, Duration: time.Since(start)}))
		return nil, err
	}
	return &observedStream{inner: s, obs: c.obs, base: c.record(req, Record{}), start: start}, nil
}

func (c *observedClient) Complete(ctx context.Context, req Request) (Response, error) {
	start := time.Now()
	res, err := c.inner.Complete(ctx, req)
	rec := Record{
		Text: res.Text, Reasoning: res.Reasoning, ToolCalls: res.ToolCalls,
		Usage: res.Usage, Error: err, Duration: time.Since(start),
	}
	c.obs(c.record(req, rec))
	return res, err
}

func (c *observedClient) record(req Request, rec Record) Record {
	rec.Provider = c.provider
	rec.Model = req.Model
	rec.System = req.System
	rec.Messages = req.Messages
	rec.Tools = req.Tools
	return rec
}

// observedStream forwards the provider stream and assembles the response so
// the record carries what the model actually said, not just the request.
type observedStream struct {
	inner Stream
	obs   Observer
	base  Record
	start time.Time

	text      string
	reasoning string
	order     []int
	calls     map[int]*ToolCall
	usage     Usage

	once sync.Once
}

func (s *observedStream) Next() bool {
	if s.inner.Next() {
		return true
	}
	s.finish(s.inner.Err())
	return false
}

// Event must be the only place that reads the inner event: provider streams pop
// on Event, not on Next, so peeking in Next would swallow the stream.
func (s *observedStream) Event() Event {
	ev := s.inner.Event()
	s.capture(ev)
	return ev
}

func (s *observedStream) Err() error { return s.inner.Err() }

func (s *observedStream) Close() error {
	err := s.inner.Close()
	s.finish(err)
	return err
}

func (s *observedStream) capture(ev Event) {
	switch ev.Kind {
	case EventText:
		s.text += ev.Text
	case EventReasoning:
		s.reasoning += ev.Text
	case EventUsage:
		s.usage = ev.Usage
	case EventToolCallStart:
		c := s.touch(ev.Index)
		if ev.ToolCallID != "" {
			c.ID = ev.ToolCallID
		}
		if ev.ToolName != "" {
			c.Name = ev.ToolName
		}
	case EventToolCallDelta:
		s.touch(ev.Index).Arguments += ev.Text
	}
}

func (s *observedStream) touch(idx int) *ToolCall {
	if s.calls == nil {
		s.calls = map[int]*ToolCall{}
	}
	c := s.calls[idx]
	if c == nil {
		c = &ToolCall{}
		s.calls[idx] = c
		s.order = append(s.order, idx)
	}
	return c
}

func (s *observedStream) finish(err error) {
	s.once.Do(func() {
		rec := s.base
		rec.Text = s.text
		rec.Reasoning = s.reasoning
		rec.Usage = s.usage
		rec.Error = err
		rec.Duration = time.Since(s.start)
		for _, idx := range s.order {
			rec.ToolCalls = append(rec.ToolCalls, *s.calls[idx])
		}
		s.obs(rec)
	})
}
