package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

const (
	defaultAnthropicMaxTokens = 8192
	anthropicDefaultBase      = "https://api.anthropic.com"
)

func newAnthropic(s Settings) (Client, error) {
	key := s.Get("api_key")
	if key == "" {
		return nil, fmt.Errorf("api key missing")
	}
	opts := []option.RequestOption{option.WithAPIKey(key)}
	if base := s.Get("base_url"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	max := defaultAnthropicMaxTokens
	if v := s.Get("max_tokens"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			max = n
		}
	}
	base := s.Get("base_url")
	if base == "" {
		base = anthropicDefaultBase
	}
	return &anthropicClient{client: anthropic.NewClient(opts...), maxTokens: max, base: base}, nil
}

type anthropicClient struct {
	client    anthropic.Client
	maxTokens int
	base      string
}

// thinkMode is how a Claude model takes a thinking level.
type thinkMode int

const (
	thinkNone     thinkMode = iota
	thinkAdaptive           // thinking {type: adaptive} + output_config.effort
	thinkBudget             // thinking {type: enabled, budget_tokens}
)

type anthropicCaps struct {
	at     time.Time
	err    error
	mode   thinkMode
	levels []string
}

// anthropicCapsCache holds Models API answers per base URL + model, shared by
// every client the CP builds (a client is built per run).
var anthropicCapsCache = struct {
	sync.Mutex
	m map[string]*anthropicCaps
}{m: map[string]*anthropicCaps{}}

// budgetTokens maps a level to a thinking budget for models that only take
// {type: enabled}.
var budgetTokens = map[string]int{
	ThinkingMinimal: 1024,
	ThinkingLow:     2048,
	ThinkingMedium:  8192,
	ThinkingHigh:    16384,
	ThinkingXHigh:   32000,
	ThinkingMax:     64000,
}

// caps asks the Models API what thinking a model supports. Adaptive models
// list their effort levels; budget-only models get off/low/medium/high.
func (c *anthropicClient) caps(ctx context.Context, model string) *anthropicCaps {
	key := c.base + "|" + model
	anthropicCapsCache.Lock()
	defer anthropicCapsCache.Unlock()
	if e := anthropicCapsCache.m[key]; e != nil {
		if (e.err == nil && time.Since(e.at) < windowTTL) || (e.err != nil && time.Since(e.at) < windowRetry) {
			return e
		}
	}
	e := &anthropicCaps{at: time.Now()}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := c.client.Models.Get(cctx, model, anthropic.ModelGetParams{})
	if err != nil {
		e.err = err
	} else {
		th, ef := info.Capabilities.Thinking, info.Capabilities.Effort
		switch {
		case th.Types.Adaptive.Supported && ef.Supported:
			e.mode = thinkAdaptive
			for _, l := range []struct {
				name string
				ok   bool
			}{{ThinkingLow, ef.Low.Supported}, {ThinkingMedium, ef.Medium.Supported}, {ThinkingHigh, ef.High.Supported}, {ThinkingXHigh, ef.Xhigh.Supported}, {ThinkingMax, ef.Max.Supported}} {
				if l.ok {
					e.levels = append(e.levels, l.name)
				}
			}
		case th.Types.Enabled.Supported:
			e.mode = thinkBudget
			e.levels = []string{ThinkingOff, ThinkingLow, ThinkingMedium, ThinkingHigh}
		}
	}
	anthropicCapsCache.m[key] = e
	return e
}

// ThinkingLevels reports the model's levels from the Models API.
func (c *anthropicClient) ThinkingLevels(ctx context.Context, model string) ([]string, error) {
	e := c.caps(ctx, model)
	return e.levels, e.err
}

// thinkingMode is the cached mode for a model when a level is requested. A
// model the Models API could not describe is assumed adaptive (every current
// Claude model is).
func (c *anthropicClient) thinkingMode(ctx context.Context, req Request) thinkMode {
	if req.Thinking == "" || req.Thinking == ThinkingOff {
		return thinkNone
	}
	e := c.caps(ctx, req.Model)
	if e.err != nil {
		return thinkAdaptive
	}
	return e.mode
}

func (c *anthropicClient) params(req Request, mode thinkMode) anthropic.MessageNewParams {
	max := req.MaxTokens
	if max <= 0 {
		max = c.maxTokens
	}
	p := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(max),
		Messages:  anthropicMessages(req.Messages, req.Cache, req.Model),
		Tools:     anthropicTools(req.Tools),
	}
	switch mode {
	case thinkAdaptive:
		// Summarized so the thread shows the reasoning; the default on current
		// models is omitted (empty thinking text).
		p.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{
			Display: anthropic.ThinkingConfigAdaptiveDisplaySummarized,
		}}
		effort := req.Thinking
		if effort == ThinkingMinimal {
			effort = ThinkingLow
		}
		p.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffort(effort)}
	case thinkBudget:
		budget := budgetTokens[req.Thinking]
		if budget == 0 {
			budget = budgetTokens[ThinkingMedium]
		}
		// max_tokens counts the thinking too, so the answer keeps its room.
		p.Thinking = anthropic.ThinkingConfigParamOfEnabled(int64(budget))
		p.MaxTokens = int64(budget + max)
	}
	for _, blk := range req.System {
		if blk.Text == "" {
			continue
		}
		tb := anthropic.TextBlockParam{Text: blk.Text}
		if req.Cache.Enabled && blk.CacheAfter {
			tb.CacheControl = anthropic.CacheControlEphemeralParam{TTL: cacheTTL(req.Cache.TTL)}
		}
		p.System = append(p.System, tb)
	}
	return p
}

func cacheTTL(ttl string) anthropic.CacheControlEphemeralTTL {
	if strings.TrimSpace(ttl) == "1h" {
		return anthropic.CacheControlEphemeralTTLTTL1h
	}
	return anthropic.CacheControlEphemeralTTLTTL5m
}

func anthropicTools(tools []Tool) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		tp := &anthropic.ToolParam{
			Name:        t.Name,
			Description: param.NewOpt(t.Description),
		}
		schema := t.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		tp.InputSchema = param.Override[anthropic.ToolInputSchemaParam](schema)
		out = append(out, anthropic.ToolUnionParam{OfTool: tp})
	}
	return out
}

func anthropicMessages(msgs []Message, cache CachePolicy, model string) []anthropic.MessageParam {
	var out []anthropic.MessageParam
	for _, m := range msgs {
		switch m.Role {
		case RoleUser:
			blocks := []anthropic.ContentBlockParamUnion{}
			if strings.TrimSpace(m.Text) != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Text))
			}
			for _, img := range m.Images {
				mime := img.Mime
				if mime == "" {
					mime = "image/png"
				}
				blocks = append(blocks, anthropic.NewImageBlockBase64(mime, base64.StdEncoding.EncodeToString(img.Data)))
			}
			out = appendUser(out, blocks)
		case RoleAssistant:
			blocks := []anthropic.ContentBlockParamUnion{}
			// Signed reasoning goes back unchanged, first, and only to the
			// model that wrote it (a tool loop with thinking needs it).
			if m.ThinkingModel == model {
				for _, t := range m.Thinking {
					if t.Redacted != "" {
						blocks = append(blocks, anthropic.NewRedactedThinkingBlock(t.Redacted))
					} else if t.Signature != "" {
						blocks = append(blocks, anthropic.NewThinkingBlock(t.Signature, t.Text))
					}
				}
			}
			if strings.TrimSpace(m.Text) != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Text))
			}
			for _, tc := range m.ToolCalls {
				input := any(json.RawMessage(tc.Arguments))
				if strings.TrimSpace(tc.Arguments) == "" {
					input = json.RawMessage(`{}`)
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Name))
			}
			if len(blocks) > 0 {
				out = append(out, anthropic.NewAssistantMessage(blocks...))
			}
		case RoleTool:
			out = appendUser(out, []anthropic.ContentBlockParamUnion{
				anthropic.NewToolResultBlock(m.ToolCallID, m.Text, false),
			})
		}
	}
	if cache.Enabled && cache.Messages && len(out) > 0 {
		markLastBlock(&out[len(out)-1], cache.TTL)
	}
	return out
}

// appendUser appends content blocks to the trailing user message when possible
// so consecutive tool results share one turn, as the Messages API expects.
func appendUser(out []anthropic.MessageParam, blocks []anthropic.ContentBlockParamUnion) []anthropic.MessageParam {
	if len(blocks) == 0 {
		return out
	}
	if n := len(out); n > 0 && out[n-1].Role == anthropic.MessageParamRoleUser {
		out[n-1].Content = append(out[n-1].Content, blocks...)
		return out
	}
	return append(out, anthropic.NewUserMessage(blocks...))
}

// markLastBlock sets a cache breakpoint on the final content block.
func markLastBlock(m *anthropic.MessageParam, ttl string) {
	if len(m.Content) == 0 {
		return
	}
	ctrl := anthropic.CacheControlEphemeralParam{TTL: cacheTTL(ttl)}
	if b := m.Content[len(m.Content)-1].OfText; b != nil {
		b.CacheControl = ctrl
	}
	if b := m.Content[len(m.Content)-1].OfToolResult; b != nil {
		b.CacheControl = ctrl
	}
}

func (c *anthropicClient) Stream(ctx context.Context, req Request) (Stream, error) {
	return &anthropicStream{stream: c.client.Messages.NewStreaming(ctx, c.params(req, c.thinkingMode(ctx, req)))}, nil
}

func (c *anthropicClient) Complete(ctx context.Context, req Request) (Response, error) {
	res, err := c.client.Messages.New(ctx, c.params(req, c.thinkingMode(ctx, req)))
	if err != nil {
		return Response{}, err
	}
	out := Response{Usage: Usage{
		InputTokens:      int(res.Usage.InputTokens),
		OutputTokens:     int(res.Usage.OutputTokens),
		CacheReadTokens:  int(res.Usage.CacheReadInputTokens),
		CacheWriteTokens: int(res.Usage.CacheCreationInputTokens),
	}}
	for _, blk := range res.Content {
		switch blk.Type {
		case "text":
			out.Text += blk.Text
		case "thinking":
			out.Reasoning += blk.Thinking
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID: blk.ID, Name: blk.Name, Arguments: string(blk.Input),
			})
		}
	}
	return out, nil
}

type anthropicStream struct {
	stream    *ssestream.Stream[anthropic.MessageStreamEventUnion]
	queue     []Event
	done      bool
	err       error
	usage     Usage
	usageSeen bool
	// block is the thinking block being streamed, handed on at its stop.
	block *ThinkingBlock
}

func (s *anthropicStream) Next() bool {
	for len(s.queue) == 0 {
		if s.done {
			return false
		}
		if !s.stream.Next() {
			s.err = s.stream.Err()
			s.done = true
			if s.err == nil {
				if s.usageSeen {
					s.queue = append(s.queue, Event{Kind: EventUsage, Usage: s.usage})
				}
				s.queue = append(s.queue, Event{Kind: EventDone})
			}
			break
		}
		s.enqueue(s.stream.Current())
	}
	return true
}

func (s *anthropicStream) Event() Event {
	if len(s.queue) == 0 {
		return Event{}
	}
	e := s.queue[0]
	s.queue = s.queue[1:]
	return e
}

func (s *anthropicStream) Err() error   { return s.err }
func (s *anthropicStream) Close() error { return s.stream.Close() }

func (s *anthropicStream) enqueue(ev anthropic.MessageStreamEventUnion) {
	switch ev.Type {
	case "message_start":
		u := ev.Message.Usage
		s.usage.InputTokens = int(u.InputTokens)
		s.usage.CacheReadTokens = int(u.CacheReadInputTokens)
		s.usage.CacheWriteTokens = int(u.CacheCreationInputTokens)
		s.usageSeen = true
	case "content_block_start":
		switch ev.ContentBlock.Type {
		case "tool_use":
			s.queue = append(s.queue, Event{
				Kind: EventToolCallStart, Index: int(ev.Index),
				ToolCallID: ev.ContentBlock.ID, ToolName: ev.ContentBlock.Name,
			})
		case "thinking":
			s.block = &ThinkingBlock{Text: ev.ContentBlock.Thinking, Signature: ev.ContentBlock.Signature}
		case "redacted_thinking":
			s.block = &ThinkingBlock{Redacted: ev.ContentBlock.Data}
		}
	case "content_block_stop":
		if s.block != nil {
			s.queue = append(s.queue, Event{Kind: EventThinkingBlock, Index: int(ev.Index), Block: s.block})
			s.block = nil
		}
	case "content_block_delta":
		switch ev.Delta.Type {
		case "text_delta":
			if ev.Delta.Text != "" {
				s.queue = append(s.queue, Event{Kind: EventText, Text: ev.Delta.Text})
			}
		case "thinking_delta":
			if ev.Delta.Thinking != "" {
				s.queue = append(s.queue, Event{Kind: EventReasoning, Text: ev.Delta.Thinking})
				if s.block != nil {
					s.block.Text += ev.Delta.Thinking
				}
			}
		case "signature_delta":
			if s.block != nil {
				s.block.Signature += ev.Delta.Signature
			}
		case "input_json_delta":
			if ev.Delta.PartialJSON != "" {
				s.queue = append(s.queue, Event{Kind: EventToolCallDelta, Index: int(ev.Index), Text: ev.Delta.PartialJSON})
			}
		}
	case "message_delta":
		if ev.Usage.JSON.OutputTokens.Valid() {
			s.usage.OutputTokens = int(ev.Usage.OutputTokens)
		}
		if ev.Usage.JSON.InputTokens.Valid() {
			s.usage.InputTokens = int(ev.Usage.InputTokens)
		}
		if ev.Usage.JSON.CacheReadInputTokens.Valid() {
			s.usage.CacheReadTokens = int(ev.Usage.CacheReadInputTokens)
		}
		if ev.Usage.JSON.CacheCreationInputTokens.Valid() {
			s.usage.CacheWriteTokens = int(ev.Usage.CacheCreationInputTokens)
		}
		s.usageSeen = true
	}
}
