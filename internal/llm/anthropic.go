package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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
	return &anthropicClient{client: anthropic.NewClient(opts...), maxTokens: max}, nil
}

type anthropicClient struct {
	client    anthropic.Client
	maxTokens int
}

func (c *anthropicClient) params(req Request) anthropic.MessageNewParams {
	max := req.MaxTokens
	if max <= 0 {
		max = c.maxTokens
	}
	p := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(max),
		Messages:  anthropicMessages(req.Messages, req.Cache),
		Tools:     anthropicTools(req.Tools),
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

func anthropicMessages(msgs []Message, cache CachePolicy) []anthropic.MessageParam {
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
	return &anthropicStream{stream: c.client.Messages.NewStreaming(ctx, c.params(req))}, nil
}

func (c *anthropicClient) Complete(ctx context.Context, req Request) (Response, error) {
	res, err := c.client.Messages.New(ctx, c.params(req))
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
		if ev.ContentBlock.Type == "tool_use" {
			s.queue = append(s.queue, Event{
				Kind: EventToolCallStart, Index: int(ev.Index),
				ToolCallID: ev.ContentBlock.ID, ToolName: ev.ContentBlock.Name,
			})
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
