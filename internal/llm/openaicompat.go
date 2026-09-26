package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/packages/ssestream"
)

const (
	openRouterBase = "https://openrouter.ai/api/v1"
	openAIBase     = "https://api.openai.com/v1"
)

// defaultIgnoredUpstreams are OpenRouter hosts that buffer a tool call's
// arguments into one chunk at the end, so the thread cannot stream the code or
// command as the model writes it.
var defaultIgnoredUpstreams = []string{"DeepInfra"}

// ignoredUpstreams reads the OpenRouter `ignore` setting: unset means the
// defaults, "" or "none" means route anywhere, else a comma list.
func ignoredUpstreams(s Settings) []string {
	raw, ok := s["ignore"]
	if !ok {
		return defaultIgnoredUpstreams
	}
	if strings.EqualFold(strings.TrimSpace(raw), "none") {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// newOpenAICompat builds a provider factory for any OpenAI-compatible API
// (OpenRouter and OpenAI today). defaultHeaders are only set when the
// organizer has not provided their own base URL. sendCacheKey controls the
// OpenAI-only prompt_cache_key parameter; routing sends OpenRouter's
// provider.ignore.
func newOpenAICompat(defaultBase string, defaultHeaders map[string]string, sendCacheKey, routing bool) factory {
	return func(s Settings) (Client, error) {
		key := s.Get("api_key")
		if key == "" {
			return nil, fmt.Errorf("api key missing")
		}
		base := s.Get("base_url")
		if base == "" {
			base = defaultBase
		}
		opts := []option.RequestOption{
			option.WithAPIKey(key),
			option.WithBaseURL(base),
		}
		if s.Get("base_url") == "" {
			for k, v := range defaultHeaders {
				opts = append(opts, option.WithHeader(k, v))
			}
		}
		c := &openAICompatClient{client: openai.NewClient(opts...), sendCacheKey: sendCacheKey}
		if routing {
			if ig := ignoredUpstreams(s); len(ig) > 0 {
				c.reqOpts = append(c.reqOpts, option.WithJSONSet("provider.ignore", ig))
			}
		}
		return c, nil
	}
}

type openAICompatClient struct {
	client       openai.Client
	sendCacheKey bool
	// reqOpts ride on every chat request (after the body is serialized).
	reqOpts []option.RequestOption
}

func (c *openAICompatClient) params(req Request) openai.ChatCompletionNewParams {
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if sys := joinSystem(req.System); sys != "" {
		msgs = append(msgs, openai.SystemMessage(sys))
	}
	for _, m := range req.Messages {
		msgs = append(msgs, openAIMessages(m)...)
	}
	tools := make([]openai.ChatCompletionToolUnionParam, 0, len(req.Tools))
	for _, t := range req.Tools {
		var params openai.FunctionParameters
		if len(t.Parameters) > 0 {
			_ = json.Unmarshal(t.Parameters, &params)
		}
		tools = append(tools, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        t.Name,
			Description: openai.String(t.Description),
			Parameters:  params,
		}))
	}
	p := openai.ChatCompletionNewParams{
		Model:         req.Model,
		Messages:      msgs,
		Tools:         tools,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)},
	}
	if req.Cache.Enabled && req.Cache.Key != "" && c.sendCacheKey {
		p.PromptCacheKey = openai.String(req.Cache.Key)
	}
	return p
}

func openAIMessages(m Message) []openai.ChatCompletionMessageParamUnion {
	switch m.Role {
	case RoleUser:
		if len(m.Images) == 0 {
			return []openai.ChatCompletionMessageParamUnion{openai.UserMessage(m.Text)}
		}
		parts := []openai.ChatCompletionContentPartUnionParam{openai.TextContentPart(m.Text)}
		for _, img := range m.Images {
			parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
				URL:    dataURL(img),
				Detail: img.Detail,
			}))
		}
		return []openai.ChatCompletionMessageParamUnion{openai.UserMessage(parts)}
	case RoleAssistant:
		if len(m.ToolCalls) == 0 {
			return []openai.ChatCompletionMessageParamUnion{openai.AssistantMessage(m.Text)}
		}
		asst := &openai.ChatCompletionAssistantMessageParam{}
		if strings.TrimSpace(m.Text) != "" {
			asst.Content = openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String(m.Text)}
		}
		for _, tc := range m.ToolCalls {
			asst.ToolCalls = append(asst.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: tc.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				},
			})
		}
		return []openai.ChatCompletionMessageParamUnion{{OfAssistant: asst}}
	case RoleTool:
		return []openai.ChatCompletionMessageParamUnion{openai.ToolMessage(m.Text, m.ToolCallID)}
	default:
		return nil
	}
}

func (c *openAICompatClient) Stream(ctx context.Context, req Request) (Stream, error) {
	return &openAIStream{stream: c.client.Chat.Completions.NewStreaming(ctx, c.params(req), c.reqOpts...)}, nil
}

func (c *openAICompatClient) Complete(ctx context.Context, req Request) (Response, error) {
	res, err := c.client.Chat.Completions.New(ctx, c.params(req), c.reqOpts...)
	if err != nil {
		return Response{}, err
	}
	out := Response{}
	if res.JSON.Usage.Valid() {
		out.Usage = Usage{
			InputTokens:     int(res.Usage.PromptTokens),
			OutputTokens:    int(res.Usage.CompletionTokens),
			CacheReadTokens: int(res.Usage.PromptTokensDetails.CachedTokens),
		}
	}
	if len(res.Choices) == 0 {
		return out, nil
	}
	msg := res.Choices[0].Message
	out.Text = msg.Content
	out.Reasoning = reasoningFromRaw(msg.RawJSON())
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments,
		})
	}
	return out, nil
}

// openAIStream adapts the OpenAI SDK's SSE stream to the neutral Stream.
type openAIStream struct {
	stream *ssestream.Stream[openai.ChatCompletionChunk]
	queue  []Event
	done   bool
	err    error
}

func (s *openAIStream) Next() bool {
	for len(s.queue) == 0 {
		if s.done {
			return false
		}
		if !s.stream.Next() {
			s.err = s.stream.Err()
			s.done = true
			if s.err == nil {
				s.queue = append(s.queue, Event{Kind: EventDone})
			}
			break
		}
		s.enqueue(s.stream.Current())
	}
	return true
}

func (s *openAIStream) Event() Event {
	if len(s.queue) == 0 {
		return Event{}
	}
	e := s.queue[0]
	s.queue = s.queue[1:]
	return e
}

func (s *openAIStream) Err() error   { return s.err }
func (s *openAIStream) Close() error { return s.stream.Close() }

func (s *openAIStream) enqueue(chunk openai.ChatCompletionChunk) {
	if chunk.JSON.Usage.Valid() {
		s.queue = append(s.queue, Event{Kind: EventUsage, Usage: Usage{
			InputTokens:     int(chunk.Usage.PromptTokens),
			OutputTokens:    int(chunk.Usage.CompletionTokens),
			CacheReadTokens: int(chunk.Usage.PromptTokensDetails.CachedTokens),
		}})
	}
	if len(chunk.Choices) == 0 {
		return
	}
	d := chunk.Choices[0].Delta
	if d.Content != "" {
		s.queue = append(s.queue, Event{Kind: EventText, Text: d.Content})
	}
	if r := reasoningFromRaw(d.RawJSON()); r != "" {
		s.queue = append(s.queue, Event{Kind: EventReasoning, Text: r})
	}
	for _, tc := range d.ToolCalls {
		idx := int(tc.Index)
		if tc.ID != "" || tc.Function.Name != "" {
			s.queue = append(s.queue, Event{
				Kind: EventToolCallStart, Index: idx,
				ToolCallID: tc.ID, ToolName: tc.Function.Name,
			})
		}
		if tc.Function.Arguments != "" {
			s.queue = append(s.queue, Event{Kind: EventToolCallDelta, Index: idx, Text: tc.Function.Arguments})
		}
	}
}

// reasoningFromRaw pulls reasoning text out of a raw delta. Providers disagree
// on the key ("reasoning" or "reasoning_content").
func reasoningFromRaw(raw string) string {
	if raw == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	for _, k := range []string{"reasoning", "reasoning_content"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// joinSystem renders ordered system blocks for providers with a single system
// string.
func joinSystem(blocks []SystemBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(blk.Text)
	}
	return b.String()
}

func dataURL(img Image) string {
	mime := img.Mime
	if mime == "" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
}

// Embed asks the /embeddings endpoint for EmbedDims-wide vectors, one per
// text, in input order.
func (c *openAICompatClient) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	resp, err := c.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model:      model,
		Input:      openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
		Dimensions: openai.Int(EmbedDims),
	})
	if err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for _, d := range resp.Data {
		if d.Index < 0 || int(d.Index) >= len(texts) {
			return nil, fmt.Errorf("embedding index %d out of range", d.Index)
		}
		if len(d.Embedding) != EmbedDims {
			return nil, fmt.Errorf("%s returned %d dimensions, need %d", model, len(d.Embedding), EmbedDims)
		}
		v := make([]float32, len(d.Embedding))
		for i, f := range d.Embedding {
			v[i] = float32(f)
		}
		out[d.Index] = v
	}
	for i, v := range out {
		if v == nil {
			return nil, fmt.Errorf("no embedding for input %d", i)
		}
	}
	return out, nil
}
