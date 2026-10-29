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

	"silo.agent/internal/registry"
	"silo.agent/internal/settingdef"
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
	// Thinking holds the provider-signed reasoning of an assistant turn, and
	// ThinkingModel the bare model that wrote it. A provider replays the
	// blocks only to that same model (Anthropic needs them in a tool loop).
	Thinking      []ThinkingBlock
	ThinkingModel string
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

// CachePolicy describes how a request should be cached. The operator can turn
// it off per provider.
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
	// Thinking is a level from ThinkingOrder; empty sends nothing and leaves
	// the model on its default. Callers fit it to the model first
	// (NearestThinking over the model's levels).
	Thinking string
	// Latency asks a gateway that routes between upstreams (OpenRouter) for
	// the one that answers first, not the cheapest: a voice call's talker
	// waits on its first token.
	Latency bool
}

// EventKind identifies a streamed event.
type EventKind string

const (
	EventText          EventKind = "text"
	EventReasoning     EventKind = "reasoning"
	EventToolCallStart EventKind = "tool_call_start"
	EventToolCallDelta EventKind = "tool_call_delta"
	// EventThinkingBlock carries one finished signed reasoning block
	// (Event.Block) to hand back on the next request.
	EventThinkingBlock EventKind = "thinking_block"
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
	Block      *ThinkingBlock
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

// ContextWindower is the optional interface a provider implements when it can
// report a model's context window in tokens (OpenRouter's /models does).
// Callers fall back to operator config when it is missing or errors.
type ContextWindower interface {
	ContextWindow(ctx context.Context, model string) (int, error)
}

// MaxAudioBytes is the per-recording upload cap OpenAI and OpenRouter enforce
// on /audio/transcriptions.
const MaxAudioBytes = 25 << 20

// Audio is one recording to transcribe. MIME names the container (the upstream
// sniffs it from the filename extension); Language is an optional ISO-639-1
// hint.
type Audio struct {
	Data     []byte
	MIME     string
	Filename string
	Language string
}

// Transcript is a transcription result. Language and Seconds are filled when
// the upstream reports them.
type Transcript struct {
	Text     string
	Language string
	Seconds  float64
	Cost     float64
}

// Transcriber is the optional interface a provider implements when it can turn
// speech into text (the OpenAI /audio/transcriptions shape that OpenRouter,
// Groq, LocalAI, Speaches, vLLM and whisper.cpp all speak). Anthropic does not.
type Transcriber interface {
	Transcribe(ctx context.Context, model string, audio Audio) (Transcript, error)
}

// AudioExt maps an audio MIME type (parameters ignored) to the file extension
// transcription upstreams use to detect the container. Unknown types give "".
func AudioExt(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	switch mime {
	case "audio/webm", "video/webm":
		return ".webm"
	case "audio/ogg", "audio/opus", "application/ogg":
		return ".ogg"
	case "audio/mp4", "audio/m4a", "audio/x-m4a", "audio/aac", "video/mp4":
		return ".m4a"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/wave", "audio/x-wav", "audio/vnd.wave":
		return ".wav"
	case "audio/flac", "audio/x-flac":
		return ".flac"
	}
	return ""
}

// SettingDef describes one configurable provider setting for the admin UI.
type SettingDef = settingdef.Def

// Descriptor is provider metadata for config, the admin UI, and the model
// picker.
type Descriptor struct {
	ID            string
	Name          string
	Description   string
	Settings      []SettingDef
	SupportsCache bool
	CacheTTLs     []string
	// KeyOptional marks a provider that works without an API key (a
	// self-hosted server). BaseURLRequired marks one with no default URL.
	KeyOptional     bool
	BaseURLRequired bool
	// Kinds are the kinds of model the provider serves (see Kinds); empty
	// means text only. The client implements each kind's interface.
	Kinds []string
	// Live are the kinds some of its models serve live (SpeechCaps):
	// transcription as you speak, a voice that stays connected. Which models
	// do is SpeechCaps; this is what the provider map shows.
	Live []string
}

// Serves reports whether the provider has models of a kind.
func (d Descriptor) Serves(kind string) bool {
	if len(d.Kinds) == 0 {
		return kind == KindText
	}
	for _, k := range d.Kinds {
		if k == kind {
			return true
		}
	}
	return false
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
	Local      = "local"
	TypeSafe   = "typesafe"
	Groq       = "groq"
	Cerebras   = "cerebras"
	Deepgram   = "deepgram"
	ElevenLabs = "elevenlabs"
	Cartesia   = "cartesia"
	Inworld    = "inworld"
	Soniox     = "soniox"
	AssemblyAI = "assemblyai"
)

// keyAndURL are the settings of a provider that needs a key and may be
// pointed elsewhere.
func keyAndURL(name string) []SettingDef {
	return []SettingDef{
		{Key: "api_key", Label: "API key", Description: name + " API key.", Secret: true},
		{Key: "base_url", Label: "Base URL", Description: "Override the API base URL."},
	}
}

// builtinProviders are the providers every control plane starts with.
var builtinProviders = []registered{
	{
		desc: Descriptor{
			ID:            OpenRouter,
			Name:          "OpenRouter",
			Description:   "OpenAI-compatible gateway to many models, decision models included (Jev, Clef and others through its Decisions API). Most upstreams cache a stable prefix on their own; Claude is cached only when the request marks it, which prompt caching does.",
			Kinds:         []string{KindText, KindDecision, KindTranscription, KindSpeech, KindEmbedding},
			SupportsCache: true,
			CacheTTLs:     []string{"5m", "1h"},
			Settings: []SettingDef{
				{Key: "api_key", Label: "API key", Description: "OpenRouter API key.", Secret: true},
				{Key: "base_url", Label: "Base URL", Description: "Override the API base URL."},
				{Key: "ignore", Label: "Skip upstreams", Description: "Comma list of upstream hosts OpenRouter must not route to. Default DeepInfra (it buffers tool arguments, so code does not stream); none routes anywhere."},
				{Key: "cache", Label: "Prompt caching", Type: "bool", Description: "Mark Claude prompts with cache_control and send prompt_cache_key so a conversation stays on the upstream that holds its cache."},
				{Key: "cache_ttl", Label: "Cache TTL", Type: "select", Description: "Claude cache lifetime. Default 5m; 1h costs more to write and survives a pause between messages."},
			},
		},
		new: newOpenAICompat(openRouterBase, map[string]string{
			"HTTP-Referer": "https://silo.agent",
			"X-Title":      "Silo Agent",
		}, true, true),
	},
	{
		desc: Descriptor{
			ID:            OpenAI,
			Live:          []string{KindTranscription},
			Name:          "OpenAI",
			Description:   "OpenAI's API. Automatic prefix caching; prompt_cache_key improves routing.",
			Kinds:         []string{KindText, KindTranscription, KindSpeech, KindEmbedding},
			SupportsCache: true,
			Settings: []SettingDef{
				{Key: "api_key", Label: "API key", Description: "OpenAI API key.", Secret: true},
				{Key: "base_url", Label: "Base URL", Description: "Override the API base URL (proxies/self-hosted)."},
				{Key: "cache", Label: "Prompt caching", Type: "bool", Description: "Send prompt_cache_key so cached prefixes route consistently."},
			},
		},
		new: newOpenAICompat(openAIBase, nil, true, false),
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
	{
		desc: Descriptor{
			ID:              Local,
			Live:            []string{KindTranscription},
			Name:            "Local",
			Description:     "Any self-hosted OpenAI-compatible server (LocalAI, Ollama, vLLM, Speaches, whisper.cpp). Silo only connects; it does not run models.",
			Kinds:           []string{KindText, KindTranscription, KindSpeech, KindEmbedding},
			KeyOptional:     true,
			BaseURLRequired: true,
			Settings: []SettingDef{
				{Key: "base_url", Label: "Base URL", Description: "The server's OpenAI-compatible root, e.g. http://localai:8080/v1."},
				{Key: "api_key", Label: "API key", Description: "Optional; only if the server checks one.", Secret: true},
				{Key: "realtime", Label: "Live transcription", Description: "Whether the server transcribes live over the Realtime API, and in which form: openai (LocalAI, and other servers that follow OpenAI's current API) or speaches (Speaches). Empty: one request per turn."},
			},
		},
		new: newOpenAICompat("", nil, false, false),
	},
	{
		desc: Descriptor{
			ID:          TypeSafe,
			Name:        "TypeSafe",
			Description: "TypeSafe's System One API: Jev, a decision model. It answers typed questions (yes or no, a choice, a score) with probabilities in well under a second and writes no text, so it has no chat models.",
			Kinds:       []string{KindDecision},
			Settings: []SettingDef{
				{Key: "api_key", Label: "API key", Description: "TypeSafe API key.", Secret: true},
				{Key: "base_url", Label: "Base URL", Description: "Override the API base URL."},
			},
		},
		new: newTypeSafe,
	},
	{
		desc: Descriptor{
			ID:          Groq,
			Name:        "Groq",
			Description: "Open models on Groq's fast inference: a talker for calls that answers in a fraction of a second (gpt-oss, Llama, Qwen), and Whisper transcription.",
			Kinds:       []string{KindText, KindTranscription},
			Settings:    keyAndURL("Groq"),
		},
		new: narrowed("https://api.groq.com/openai/v1", true),
	},
	{
		desc: Descriptor{
			ID:          Cerebras,
			Name:        "Cerebras",
			Description: "Open models on Cerebras' fast inference (gpt-oss, Qwen): a talker for calls with a very short time to the first word.",
			Kinds:       []string{KindText},
			Settings:    keyAndURL("Cerebras"),
		},
		new: narrowed("https://api.cerebras.ai/v1", false),
	},
	{
		desc: Descriptor{
			ID:          Deepgram,
			Live:        []string{KindTranscription, KindSpeech},
			Name:        "Deepgram",
			Description: "Speech recognition that streams (Nova-3, and Flux, which also judges when you have finished speaking) and Aura voices. An Aura model is one voice: aura-2-thalia-en. No Polish voices.",
			Kinds:       []string{KindTranscription, KindSpeech},
			Settings:    keyAndURL("Deepgram"),
		},
		new: newDeepgram,
	},
	{
		desc: Descriptor{
			ID:          ElevenLabs,
			Live:        []string{KindTranscription, KindSpeech},
			Name:        "ElevenLabs",
			Description: "Voices that stay connected for a call with word timings (Flash v2.5, many languages including Polish), and Scribe transcription: files with scribe_v2, live with scribe_v2_realtime.",
			Kinds:       []string{KindTranscription, KindSpeech},
			Settings:    keyAndURL("ElevenLabs"),
		},
		new: newElevenLabs,
	},
	{
		desc: Descriptor{
			ID:          Cartesia,
			Live:        []string{KindTranscription, KindSpeech},
			Name:        "Cartesia",
			Description: "Sonic voices that stay connected for a call, with word timings, and Ink live transcription.",
			Kinds:       []string{KindTranscription, KindSpeech},
			Settings:    keyAndURL("Cartesia"),
		},
		new: newCartesia,
	},
	{
		desc: Descriptor{
			ID:          Inworld,
			Live:        []string{KindSpeech},
			Name:        "Inworld",
			Description: "TTS 2 voices that stay connected for a call, with word timings; Flash is among the fastest to the first sound.",
			Kinds:       []string{KindSpeech},
			Settings: []SettingDef{
				{Key: "api_key", Label: "API key", Description: "The Base64 key the Inworld Portal shows, as it is.", Secret: true},
				{Key: "base_url", Label: "Base URL", Description: "Override the API base URL."},
			},
		},
		new: newInworld,
	},
	{
		desc: Descriptor{
			ID:          Soniox,
			Live:        []string{KindTranscription},
			Name:        "Soniox",
			Description: "Live transcription in 60+ languages with code-switching (strong Polish), which also judges when you have finished speaking.",
			Kinds:       []string{KindTranscription},
			Settings:    keyAndURL("Soniox"),
		},
		new: newSoniox,
	},
	{
		desc: Descriptor{
			ID:          AssemblyAI,
			Live:        []string{KindTranscription},
			Name:        "AssemblyAI",
			Description: "Universal streaming transcription that judges when you have finished speaking, and files in one request.",
			Kinds:       []string{KindTranscription},
			Settings:    keyAndURL("AssemblyAI"),
		},
		new: newAssemblyAI,
	},
}

var providers = newProviders()

func newProviders() *registry.Registry[Descriptor, factory] {
	r := registry.New[Descriptor, factory](func(d Descriptor) string { return d.ID })
	for _, p := range builtinProviders {
		r.Put(p.desc, p.new)
	}
	return r
}

// Descriptors returns provider metadata in registration order.
func Descriptors() []Descriptor { return providers.All() }

// Lookup finds a provider descriptor by id.
func Lookup(id string) (Descriptor, bool) {
	d, _, ok := providers.Lookup(strings.TrimSpace(id))
	return d, ok
}

// Known reports whether id is a registered provider.
func Known(id string) bool {
	_, ok := Lookup(id)
	return ok
}

// New builds a client for a provider id.
func New(id string, settings Settings) (Client, error) {
	id = strings.TrimSpace(id)
	if _, f, ok := providers.Lookup(id); ok {
		return f(settings)
	}
	return nil, fmt.Errorf("unknown model provider %q", id)
}

// Register adds a provider to the registry. It is the seam test and embedder
// packages use to plug in a provider without editing the built-in list; a
// production control plane never calls it. Registering an id that already
// exists replaces the factory but keeps the original descriptor unless the new
// one is non-empty.
func Register(desc Descriptor, newFn func(Settings) (Client, error)) {
	if old, _, ok := providers.Lookup(desc.ID); ok && desc.Name == "" && len(desc.Settings) == 0 {
		desc = old
	}
	providers.Put(desc, newFn)
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
