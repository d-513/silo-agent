// Package dummy is a deterministic, scripted LLM provider for tests. It speaks
// the neutral llm.Client interface but never touches the network, so feature
// tests can drive the whole agent loop — streaming, tool calls, sections,
// approvals — without spending tokens or depending on an upstream model.
//
// Routing is token-based. A conversation whose input contains "Test_NN_Input"
// gets "Test_NN_Output" back. Richer multi-turn flows are registered with
// Script, keyed by the same token. The provider is stateless across calls: it
// infers the current turn from how many assistant messages already sit in the
// request, so a tool-call turn followed by a tool-result turn replays exactly.
//
// The package is imported only by test binaries; it registers itself on init.
// Production code never imports it, so "dummy" never appears in the shipped
// provider registry or admin UI.
package dummy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"silo.agent/internal/llm"
)

// Provider is the registered provider id. Model ids look like "dummy/echo".
const Provider = "dummy"

// DefaultModel is the conventional model part of a dummy model id.
const DefaultModel = "echo"

// TokenPrefix marks an input token the dummy provider understands.
const TokenPrefix = "Test_"

// Turn is one scripted assistant turn. Reasoning and Text are streamed in
// small chunks so callers exercise the section splitter; ToolCalls are emitted
// as start/delta events just like a real provider.
type Turn struct {
	Reasoning string
	Text      string
	ToolCalls []llm.ToolCall
}

var (
	mu        sync.Mutex
	scenarios = map[string][]Turn{}
)

// Script registers an ordered set of turns for a Test_* token. Registering the
// same token again replaces it.
func Script(token string, turns ...Turn) {
	mu.Lock()
	defer mu.Unlock()
	scenarios[token] = turns
}

// Reset clears every registered scenario. Tests call it from TestMain.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	scenarios = map[string][]Turn{}
}

func scenarioFor(token string) ([]Turn, bool) {
	mu.Lock()
	defer mu.Unlock()
	turns, ok := scenarios[token]
	return turns, ok
}

func init() {
	llm.Register(llm.Descriptor{
		ID:            Provider,
		Name:          "Dummy",
		Description:   "Deterministic scripted provider for tests. Not for production.",
		SupportsCache: false,
		Settings: []llm.SettingDef{
			{Key: "api_key", Label: "API key", Description: "Ignored; kept for shape compatibility."},
		},
	}, func(llm.Settings) (llm.Client, error) { return &client{}, nil })
}

type client struct{}

func (c *client) Stream(ctx context.Context, req llm.Request) (llm.Stream, error) {
	turn := resolveTurn(req)
	events := buildEvents(turn)
	return &sliceStream{events: events}, nil
}

func (c *client) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	return llm.Response{Text: "Dummy Title", Usage: llm.Usage{InputTokens: 8, OutputTokens: 2}}, nil
}

// resolveTurn picks the scripted turn for this request. The scenario key is the
// first Test_* token in any user message, which keeps it stable across the
// turns of a single run. The turn index is the number of assistant messages
// already present.
func resolveTurn(req llm.Request) Turn {
	token := ""
	lastUser := ""
	for _, m := range req.Messages {
		if m.Role != llm.RoleUser {
			continue
		}
		lastUser = m.Text
		if token == "" {
			token = firstToken(m.Text)
		}
	}
	index := 0
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			index++
		}
	}
	if turns, ok := scenarioFor(token); ok {
		if index < len(turns) {
			return turns[index]
		}
		if len(turns) > 0 {
			return Turn{Text: token + "_End"}
		}
	}
	if index == 0 {
		return defaultTurn(token, lastUser)
	}
	return Turn{Text: token + "_End"}
}

func defaultTurn(token, lastUser string) Turn {
	if token != "" {
		out := strings.Replace(token, "_Input", "_Output", 1)
		if out == token {
			out += "_Output"
		}
		return Turn{Text: out}
	}
	lastUser = strings.TrimSpace(lastUser)
	if lastUser == "" {
		return Turn{Text: "Echo"}
	}
	return Turn{Text: "Echo: " + lastUser}
}

// firstToken extracts the first Test_*-shaped token from s and normalizes the
// conventional "_Input" suffix away, so "Test_20_Input" and a scenario
// registered as "Test_20" match.
func firstToken(s string) string {
	i := strings.Index(s, TokenPrefix)
	if i < 0 {
		return ""
	}
	j := i + len(TokenPrefix)
	for j < len(s) && isWordByte(s[j]) {
		j++
	}
	return strings.TrimSuffix(s[i:j], "_Input")
}

func isWordByte(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}

// buildEvents turns a Turn into the neutral event stream.
func buildEvents(t Turn) []llm.Event {
	var out []llm.Event
	for _, chunk := range chunkRunes(t.Reasoning, 7) {
		out = append(out, llm.Event{Kind: llm.EventReasoning, Text: chunk})
	}
	for _, chunk := range chunkRunes(t.Text, 7) {
		out = append(out, llm.Event{Kind: llm.EventText, Text: chunk})
	}
	for i, tc := range t.ToolCalls {
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("dummy_call_%d", i)
		}
		name := tc.Name
		args := strings.TrimSpace(tc.Arguments)
		if args == "" {
			args = "{}"
		}
		if !json.Valid([]byte(args)) {
			raw, _ := json.Marshal(args)
			args = string(raw)
		}
		out = append(out, llm.Event{Kind: llm.EventToolCallStart, Index: i, ToolCallID: id, ToolName: name})
		for _, chunk := range chunkString(args, 8) {
			out = append(out, llm.Event{Kind: llm.EventToolCallDelta, Index: i, Text: chunk})
		}
	}
	out = append(out, llm.Event{Kind: llm.EventUsage, Usage: llm.Usage{
		InputTokens:  10 + utf8.RuneCountInString(t.Text),
		OutputTokens: 5 + utf8.RuneCountInString(t.Text),
	}})
	out = append(out, llm.Event{Kind: llm.EventDone})
	return out
}

func chunkRunes(s string, n int) []string {
	if s == "" {
		return nil
	}
	var out []string
	runes := []rune(s)
	for i := 0; i < len(runes); i += n {
		end := i + n
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

func chunkString(s string, n int) []string {
	if s == "" {
		return nil
	}
	var out []string
	for i := 0; i < len(s); i += n {
		end := i + n
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[i:end])
	}
	return out
}

// sliceStream is an in-memory Stream.
type sliceStream struct {
	events []llm.Event
	pos    int
	cur    llm.Event
}

func (s *sliceStream) Next() bool {
	if s.pos >= len(s.events) {
		return false
	}
	s.cur = s.events[s.pos]
	s.pos++
	return true
}

func (s *sliceStream) Event() llm.Event { return s.cur }
func (s *sliceStream) Err() error       { return nil }
func (s *sliceStream) Close() error     { return nil }
