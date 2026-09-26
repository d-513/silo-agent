// Package llm is the modular model engine. It exposes a provider-neutral
// request/response/stream shape and a registry of providers (OpenRouter,
// OpenAI, Anthropic). The agent loop speaks only in these types; each provider
// adapter translates to and from its own SDK.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Role is the author of a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Image is an inline image carried on a user turn. Detail maps to the
// OpenAI-compatible "detail" hint ("high" for presented files); Anthropic
// ignores it.
type Image struct {
	Mime   string
	Data   []byte
	Detail string
}

// ToolCall is a model-requested tool invocation.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Message is one provider-neutral conversation turn. Assistant turns carry
// ToolCalls; tool turns carry ToolCallID and Text (the result).
type Message struct {
	Role       Role
	Text       string
	Images     []Image
	ToolCalls  []ToolCall
	ToolCallID string
}

// Tool is a callable function exposed to the model.
type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// SystemBlock is one ordered slice of the system prompt. Blocks are written
// stable-first so prompt caches keep the longest possible prefix. A CacheAfter
// block ends a cache breakpoint.
type SystemBlock struct {
	Text       string
	CacheAfter bool
}

// CachePolicy describes how a request should be cached. It is off by default;
// the operator enables it per provider.
type CachePolicy struct {
	// Enabled turns on provider cache markers (Anthropic cache_control) and the
	// routing key (OpenAI/OpenRouter prompt_cache_key).
	Enabled bool
	// TTL is a provider-specific lifetime ("5m" or "1h"); empty means provider
	// default.
	TTL string
	// Key is a stable cache identity. For OpenAI/OpenRouter it maps to
	// prompt_cache_key so cached prefixes route to the same machine.
	Key string
	// Messages caches through the last message of the conversation.
	Messages bool
}

// Request is a provider-neutral completion request. Model is the bare model
// name (the provider prefix has already been stripped).
type Request struct {
	Model     string
	System    []SystemBlock
	Messages  []Message
	Tools     []Tool
	Cache     CachePolicy
	MaxTokens int
}

// EventKind identifies a streamed event.
type EventKind string

const (
	EventText          EventKind = "text"
	EventReasoning     EventKind = "reasoning"
	EventToolCallStart EventKind = "tool_call_start"
	EventToolCallDelta EventKind = "tool_call_delta"
	EventUsage         EventKind = "usage"
	EventDone          EventKind = "done"
	EventError         EventKind = "error"
)

// Event is one streamed delta. Text/Reasoning carry Text; tool events carry
// Index plus ToolCallID/ToolName on start and Text (argument fragment) on
// delta; usage carries Usage.
type Event struct {
	Kind       EventKind
	Text       string
	Index      int
	ToolCallID string
	ToolName   string
	Usage      Usage
	Err        error
}

// Usage is token accounting for one completion. CacheRead/CacheWrite are only
// meaningful on providers with prompt caching.
type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

// Response is a non-streamed completion result.
type Response struct {
	Text      string
	Reasoning string
	ToolCalls []ToolCall
	Usage     Usage
}

// Stream is a provider-neutral stream of Events. Callers drive it with Next and
// must Close it.
type Stream interface {
	Next() bool
	Event() Event
	Err() error
	Close() error
}

// Client is the interface every provider implements.
type Client interface {
	Stream(ctx context.Context, req Request) (Stream, error)
	Complete(ctx context.Context, req Request) (Response, error)
}

// EmbedDims is the vector width stored in Postgres (memories.embedding).
// Changing it means dropping the database.
const EmbedDims = 1536

// Embedder is the optional interface a provider implements when it can turn
// text into vectors. Anthropic does not.
type Embedder interface {
	Embed(ctx context.Context, model string, texts []string) ([][]float32, error)
}

// SettingDef describes one configurable provider setting for the admin UI.
type SettingDef struct {
	Key         string
	Label       string
	Type        string
	Description string
	Secret      bool
}

// Descriptor is provider metadata for config, the admin UI, and the model
// picker.
type Descriptor struct {
	ID            string
	Name          string
	Description   string
	Settings      []SettingDef
	SupportsCache bool
	CacheTTLs     []string
}

// Settings is provider config keyed by SettingDef.Key.
type Settings map[string]string

func (s Settings) Get(key string) string { return strings.TrimSpace(s[key]) }

// Enabled reports whether a boolean setting is on.
func (s Settings) Enabled(key string) bool {
	v := strings.ToLower(s.Get(key))
	return v == "true" || v == "1" || v == "on" || v == "yes"
}

type factory func(Settings) (Client, error)

type registered struct {
	desc Descriptor
	new  factory
}

const (
	OpenRouter = "openrouter"
	OpenAI     = "openai"
	Anthropic  = "anthropic"
)

var registry = []registered{
	{
		desc: Descriptor{
			ID:            OpenRouter,
			Name:          "OpenRouter",
			Description:   "OpenAI-compatible gateway to many models. Upstream providers cache the stable prefix automatically; no setting needed.",
			SupportsCache: false,
			Settings: []SettingDef{
				{Key: "api_key", Label: "API key", Description: "OpenRouter API key.", Secret: true},
				{Key: "base_url", Label: "Base URL", Description: "Override the API base URL."},
			},
		},
		new: newOpenAICompat(openRouterBase, map[string]string{
			"HTTP-Referer": "https://silo.agent",
			"X-Title":      "Silo Agent",
		}, false),
	},
	{
		desc: Descriptor{
			ID:            OpenAI,
			Name:          "OpenAI",
			Description:   "OpenAI's API. Automatic prefix caching; prompt_cache_key improves routing.",
			SupportsCache: true,
			Settings: []SettingDef{
				{Key: "api_key", Label: "API key", Description: "OpenAI API key.", Secret: true},
				{Key: "base_url", Label: "Base URL", Description: "Override the API base URL (proxies/self-hosted)."},
				{Key: "cache", Label: "Prompt caching", Type: "bool", Description: "Send prompt_cache_key so cached prefixes route consistently."},
			},
		},
		new: newOpenAICompat(openAIBase, nil, true),
	},
	{
		desc: Descriptor{
			ID:            Anthropic,
			Name:          "Anthropic",
			Description:   "Anthropic's native Messages API. Explicit cache_control prompt-cache breakpoints.",
			SupportsCache: true,
			CacheTTLs:     []string{"5m", "1h"},
			Settings: []SettingDef{
				{Key: "api_key", Label: "API key", Description: "Anthropic API key.", Secret: true},
				{Key: "base_url", Label: "Base URL", Description: "Override the API base URL."},
				{Key: "max_tokens", Label: "Max output tokens", Description: "Per-response output cap. Default 8192."},
				{Key: "cache", Label: "Prompt caching", Type: "bool", Description: "Add cache_control breakpoints to the system prompt and tools."},
				{Key: "cache_ttl", Label: "Cache TTL", Type: "select", Description: "Cache lifetime. Default 5m."},
			},
		},
		new: newAnthropic,
	},
}

// Descriptors returns provider metadata in registration order.
func Descriptors() []Descriptor {
	out := make([]Descriptor, len(registry))
	for i, r := range registry {
		out[i] = r.desc
	}
	return out
}

// Lookup finds a provider descriptor by id.
func Lookup(id string) (Descriptor, bool) {
	id = strings.TrimSpace(id)
	for _, r := range registry {
		if r.desc.ID == id {
			return r.desc, true
		}
	}
	return Descriptor{}, false
}

// Known reports whether id is a registered provider.
func Known(id string) bool {
	_, ok := Lookup(id)
	return ok
}

// New builds a client for a provider id.
func New(id string, settings Settings) (Client, error) {
	id = strings.TrimSpace(id)
	for _, r := range registry {
		if r.desc.ID == id {
			return r.new(settings)
		}
	}
	return nil, fmt.Errorf("unknown model provider %q", id)
}

// Register adds a provider to the registry. It is the seam test and embedder
// packages use to plug in a provider without editing the built-in list; a
// production control plane never calls it. Registering an id that already
// exists replaces the factory but keeps the original descriptor unless the new
// one is non-empty.
func Register(desc Descriptor, newFn func(Settings) (Client, error)) {
	for i, r := range registry {
		if r.desc.ID == desc.ID {
			if desc.Name != "" || len(desc.Settings) > 0 {
				registry[i].desc = desc
			}
			registry[i].new = newFn
			return
		}
	}
	registry = append(registry, registered{desc: desc, new: newFn})
}

// Unregister removes a provider from the registry. Tests use it to keep a
// process-scoped registration from leaking into another test binary's run.
func Unregister(id string) {
	id = strings.TrimSpace(id)
	out := registry[:0]
	for _, r := range registry {
		if r.desc.ID != id {
			out = append(out, r)
		}
	}
	registry = out
}

// Split parses a "provider/model" id on the first slash. The model part may
// itself contain slashes (e.g. "openrouter/openai/gpt-5.6-luna").
func Split(modelID string) (provider, model string, ok bool) {
	modelID = strings.TrimSpace(modelID)
	i := strings.Index(modelID, "/")
	if i <= 0 || i == len(modelID)-1 {
		return "", "", false
	}
	return modelID[:i], modelID[i+1:], true
}

// Parse validates a model id against the registry and returns its parts.
func Parse(modelID string) (provider, model string, err error) {
	p, m, ok := Split(modelID)
	if !ok {
		return "", "", fmt.Errorf("model id %q must be provider/model", modelID)
	}
	if !Known(p) {
		return "", "", fmt.Errorf("unknown model provider %q in %q", p, modelID)
	}
	return p, m, nil
}

// Allowed reports whether modelID is in the operator allowlist.
func Allowed(modelID string, allowed []string) bool {
	modelID = strings.TrimSpace(modelID)
	for _, a := range allowed {
		if strings.TrimSpace(a) == modelID {
			return true
		}
	}
	return false
}

// CacheSettings reads the cache-related settings for a provider.
func CacheSettings(s Settings) (enabled bool, ttl string) {
	return s.Enabled("cache"), s.Get("cache_ttl")
}
