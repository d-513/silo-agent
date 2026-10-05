package config

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/knadh/koanf/v2"

	"silo.agent/internal/llm"
	"silo.agent/internal/search"
)

const YAMLName = "silo.yaml"

type Source string

const (
	SourceDefault Source = "default"
	SourceYAML    Source = "yaml"
	SourceEnv     Source = "env"
)

type Bootstrap struct {
	Email    string `koanf:"email"`
	Password string `koanf:"password"`
}

// Provider is the operator configuration for one model provider. Keys mirror
// llm.SettingDef.Key under providers.<id>.
type Provider struct {
	APIKey    string `koanf:"api_key"`
	BaseURL   string `koanf:"base_url"`
	Cache     bool   `koanf:"cache"`
	CacheTTL  string `koanf:"cache_ttl"`
	MaxTokens int    `koanf:"max_tokens"`
	// Ignore is OpenRouter's upstream skip list; nil means the engine default.
	Ignore *string `koanf:"ignore"`
}

// Settings renders a provider's config as the generic map the engine reads.
func (p Provider) Settings() llm.Settings {
	out := llm.Settings{}
	if p.APIKey != "" {
		out["api_key"] = p.APIKey
	}
	if p.BaseURL != "" {
		out["base_url"] = p.BaseURL
	}
	if p.CacheTTL != "" {
		out["cache_ttl"] = p.CacheTTL
	}
	if p.MaxTokens > 0 {
		out["max_tokens"] = strconv.Itoa(p.MaxTokens)
	}
	if p.Ignore != nil {
		out["ignore"] = *p.Ignore
	}
	out["cache"] = "false"
	if p.Cache {
		out["cache"] = "true"
	}
	return out
}

// Memory configures long-term (pgvector) memories.
type Memory struct {
	// AutoRecall injects the closest memories to the user's message into
	// each run's volatile prompt tail.
	AutoRecall bool `koanf:"auto_recall"`
	// Collect runs the memory collector over chats that went idle with new
	// messages. The manual composer button works either way.
	Collect bool `koanf:"collect"`
}

// Knowledge configures the folder index behind `search_docs` (RAG).
type Knowledge struct {
	// Enabled turns on the background sweep, adding folders, and the
	// search_docs tool. Already-indexed chunks stay in Postgres either way.
	Enabled bool `koanf:"enabled"`
	// SyncInterval is how often a folder is re-checked for changes (a Go
	// duration such as 15m or 1h). Drive folders are checked four times less
	// often: every look is a network round trip.
	SyncInterval string `koanf:"sync_interval"`
	// OCR reads scanned PDF pages and image files with tesseract. Off, they are
	// listed as not indexed. A folder of photos makes this slow: turn it off.
	OCR bool `koanf:"ocr"`
}

// DefaultKnowledgeInterval is the folder re-check cadence when unset or
// unparsable; KnowledgeMinInterval is the floor.
const (
	DefaultKnowledgeInterval = 15 * time.Minute
	KnowledgeMinInterval     = time.Minute
)

// Interval returns the re-check cadence for a local folder.
func (k Knowledge) Interval() time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(k.SyncInterval))
	if err != nil || d <= 0 {
		return DefaultKnowledgeInterval
	}
	return max(d, KnowledgeMinInterval)
}

// Context configures the model context window and automatic compaction.
type Context struct {
	// Window is the fallback context size in tokens when the provider cannot
	// report one (OpenRouter does, via /models).
	Window int `koanf:"window"`
	// Windows are per-model overrides; they win over the provider's own
	// report. A list, not a map: model ids contain dots, koanf's delimiter.
	Windows []ModelWindow `koanf:"windows"`
	// CompactAt is the fraction of the window that triggers compaction.
	CompactAt float64 `koanf:"compact_at"`
}

// DefaultContextWindow and DefaultCompactAt are the context fallbacks.
const (
	DefaultContextWindow = 128000
	DefaultCompactAt     = 0.8
)

// ModelWindow is one per-model context window override.
type ModelWindow struct {
	Model  string `koanf:"model"`
	Window int    `koanf:"window"`
}

// WindowFor returns the operator override for modelID, or 0.
func (c Context) WindowFor(modelID string) int {
	modelID = strings.TrimSpace(modelID)
	for _, w := range c.Windows {
		if strings.TrimSpace(w.Model) == modelID && w.Window > 0 {
			return w.Window
		}
	}
	return 0
}

// Thinking configures the composer's thinking-level picker.
type Thinking struct {
	// Levels are per-model overrides of the levels a model accepts; they win
	// over the provider's own report (OpenRouter /models, the Anthropic Models
	// API, OpenAI model families). An empty list hides the picker for that
	// model. A list, not a map: model ids contain dots, koanf's delimiter.
	Levels []ModelThinking `koanf:"levels"`
}

// ModelThinking is one per-model thinking-level override.
type ModelThinking struct {
	Model  string   `koanf:"model"`
	Levels []string `koanf:"levels"`
}

// LevelsFor returns the operator override for modelID and whether one is set.
func (t Thinking) LevelsFor(modelID string) ([]string, bool) {
	modelID = strings.TrimSpace(modelID)
	for _, m := range t.Levels {
		if strings.TrimSpace(m.Model) == modelID {
			return m.Levels, true
		}
	}
	return nil, false
}

// Threshold returns the valid compaction fraction.
func (c Context) Threshold() float64 {
	if c.CompactAt <= 0.1 || c.CompactAt > 0.98 {
		return DefaultCompactAt
	}
	return c.CompactAt
}

// FallbackWindow returns the valid fallback window.
func (c Context) FallbackWindow() int {
	if c.Window < 1000 {
		return DefaultContextWindow
	}
	return c.Window
}

type Search struct {
	Engine string `koanf:"engine"`
}

// Runs configures the agent loop's per-run limits.
type Runs struct {
	// MaxDuration caps one run (a Go duration such as 120m or 2h). -1 (or any
	// negative value) means no cap. A lead that sleeps while its subagents work
	// counts its sleep toward the cap.
	MaxDuration string `koanf:"max_duration"`
}

// DefaultRunMaxDuration is the run cap when unset or unparsable.
const DefaultRunMaxDuration = 120 * time.Minute

// Timeout returns the run cap; 0 means unlimited.
func (r Runs) Timeout() time.Duration {
	s := strings.TrimSpace(r.MaxDuration)
	if s == "" {
		return DefaultRunMaxDuration
	}
	if strings.HasPrefix(s, "-") {
		return 0
	}
	if n, err := strconv.Atoi(s); err == nil {
		// A bare number is minutes.
		if n <= 0 {
			return DefaultRunMaxDuration
		}
		return time.Duration(n) * time.Minute
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return DefaultRunMaxDuration
	}
	return d
}

const DefaultModel = "openrouter/openai/gpt-5.6-luna"

const DefaultMCPStdioImage = "localhost/silo-mcp-stdio:v1"

// DefaultEmbeddingModel embeds long-term memories. Its vectors must be
// llm.EmbedDims wide (text-embedding-3 honours the dimensions parameter).
const DefaultEmbeddingModel = "openrouter/openai/text-embedding-3-small"

// DefaultTranscribeModel turns speech into text for composer dictation and the
// Bot's transcribe tool. TranscribeOff disables voice.
const (
	DefaultTranscribeModel = "openrouter/openai/whisper-1"
	TranscribeOff          = "off"
)

// DefaultDatabaseURL is the dev Postgres from docker-compose.dev.yml.
const DefaultDatabaseURL = "postgres://silo:silo@localhost:5433/silo?sslmode=disable"

type Config struct {
	HTTPAddr      string `koanf:"http_addr"`
	PublicURL     string `koanf:"public_url"`
	DataDir       string `koanf:"data_dir"`
	DatabaseURL   string `koanf:"database_url"`
	DockerHost    string `koanf:"docker_host"`
	CPURL         string `koanf:"cp_url"`
	BotImage      string `koanf:"bot_image"`
	MCPStdioImage string `koanf:"mcp_stdio_image"`
	Model         string `koanf:"model"`
	ModelTitle    string `koanf:"model_title"`
	ModelApproval string `koanf:"model_approval"`
	// ModelSubagent is the default model of subagents a lead starts; empty
	// means the lead's own model. spawn_agent may still name another.
	ModelSubagent string `koanf:"model_subagent"`
	// ModelMemory is the memory collector's model; empty means the title model.
	ModelMemory string              `koanf:"model_memory"`
	EmbedModel  string              `koanf:"embedding_model"`
	Transcribe  string              `koanf:"transcribe_model"`
	Memory      Memory              `koanf:"memory"`
	Knowledge   Knowledge           `koanf:"knowledge"`
	Context     Context             `koanf:"context"`
	Thinking    Thinking            `koanf:"thinking"`
	Runs        Runs                `koanf:"runs"`
	Models      []string            `koanf:"models"`
	Debug       bool                `koanf:"debug"`
	Bootstrap   Bootstrap           `koanf:"bootstrap"`
	Providers   map[string]Provider `koanf:"providers"`
	Search      Search              `koanf:"search"`
	Drives      Drives              `koanf:"drives"`
	Tunnels     Tunnels             `koanf:"tunnels"`
}

// DefaultDriveImage is the rclone sidecar that mounts a Bot's drives.
const DefaultDriveImage = "localhost/silo-drive:v1"

// Drives configures rclone-backed drives.
type Drives struct {
	Image string `koanf:"image"`
	// MountRoot is where sidecar mounts live, as a path the container engine
	// sees (inside the VM on podman machine). It must be able to carry mount
	// propagation; the CP makes it a shared mount before use.
	MountRoot string `koanf:"mount_root"`
	// CacheMaxSize caps each drive's rclone VFS cache, e.g. "10G".
	CacheMaxSize string `koanf:"cache_max_size"`
	// Providers holds each template's system values (OAuth client ids and
	// secrets), keyed by template then var: drives.providers.gdrive.client_id.
	Providers map[string]map[string]string `koanf:"providers"`
}

// DriveMountRoot resolves drives.mount_root. On macOS the engine runs in a VM
// whose view of data_dir is a virtiofs share, which cannot propagate mounts,
// so the default is a VM-local directory there.
func (c Config) DriveMountRoot() string {
	if r := strings.TrimSpace(c.Drives.MountRoot); r != "" {
		return r
	}
	if runtime.GOOS == "darwin" {
		return "/var/tmp/silo-drives"
	}
	dir := c.DataDir
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Join(dir, "drives-mnt")
}

// DriveImage is drives.image or the default.
func (c Config) DriveImage() string {
	if i := strings.TrimSpace(c.Drives.Image); i != "" {
		return i
	}
	return DefaultDriveImage
}

// ProviderSettings returns the engine settings for a provider id.
func (c Config) ProviderSettings(id string) llm.Settings {
	if p, ok := c.Providers[id]; ok {
		return p.Settings()
	}
	return llm.Settings{}
}

// TitleModel returns the configured title model, falling back to the main
// default model.
func (c Config) TitleModel() string {
	if m := strings.TrimSpace(c.ModelTitle); m != "" {
		return m
	}
	if m := strings.TrimSpace(c.Model); m != "" {
		return m
	}
	return DefaultModel
}

// TranscribeModel returns the speech-to-text model id, or "" when voice is
// off.
func (c Config) TranscribeModel() string {
	m := strings.TrimSpace(c.Transcribe)
	if strings.EqualFold(m, TranscribeOff) {
		return ""
	}
	if m == "" {
		return DefaultTranscribeModel
	}
	return m
}

// ApprovalModel returns the model used to decide auto-approval rules, falling
// back to the title model and then the main default model.
func (c Config) ApprovalModel() string {
	if m := strings.TrimSpace(c.ModelApproval); m != "" {
		return m
	}
	return c.TitleModel()
}

// MemoryModel returns the model the memory collector reads chats with,
// falling back to the title model.
func (c Config) MemoryModel() string {
	if m := strings.TrimSpace(c.ModelMemory); m != "" {
		return m
	}
	return c.TitleModel()
}

type fieldMeta struct {
	Key     string
	Secret  bool
	Restart bool
	Type    string
}

var fieldDefs = []fieldMeta{
	{Key: "model"},
	{Key: "model_title"},
	{Key: "model_approval"},
	{Key: "model_subagent"},
	{Key: "model_memory"},
	{Key: "embedding_model"},
	{Key: "transcribe_model"},
	{Key: "memory.auto_recall", Type: "bool"},
	{Key: "memory.collect", Type: "bool"},
	{Key: "knowledge.enabled", Type: "bool"},
	{Key: "knowledge.sync_interval"},
	{Key: "knowledge.ocr", Type: "bool"},
	{Key: "context.window"},
	{Key: "context.compact_at"},
	{Key: "runs.max_duration"},
	{Key: "debug", Type: "bool"},
	{Key: "search.engine"},
	{Key: "http_addr", Restart: true},
	{Key: "public_url"},
	{Key: "cp_url"},
	{Key: "data_dir", Restart: true},
	{Key: "database_url", Secret: true, Restart: true},
	{Key: "docker_host", Restart: true},
	{Key: "bot_image"},
	{Key: "mcp_stdio_image"},
	{Key: "drives.image"},
	{Key: "drives.mount_root"},
	{Key: "drives.cache_max_size"},
	{Key: "tunnels.enabled", Type: "bool"},
	{Key: "tunnels.host"},
	{Key: "tunnels.scheme"},
	{Key: "bootstrap.email", Restart: true},
	{Key: "bootstrap.password", Secret: true, Restart: true},
}

type Field struct {
	Key     string
	Value   string
	Source  Source
	EnvName string
	Secret  bool
	Restart bool
	Type    string
}

type Store struct {
	mu     sync.RWMutex
	path   string
	yaml   *koanf.Koanf
	env    *koanf.Koanf
	merged *koanf.Koanf
	live   Config
}

func envKey(s string) string {
	s = strings.ToLower(strings.TrimPrefix(s, "SILO_"))
	return strings.ReplaceAll(s, "__", ".")
}

func EnvName(key string) string {
	return "SILO_" + strings.ToUpper(strings.ReplaceAll(key, ".", "__"))
}

func KnownKey(key string) bool {
	for _, f := range fieldDefs {
		if f.Key == key {
			return true
		}
	}
	for _, d := range search.Descriptors() {
		for _, f := range d.Settings {
			if key == "search."+d.ID+"."+f.Key {
				return true
			}
		}
	}
	for _, d := range llm.Descriptors() {
		for _, f := range d.Settings {
			if key == "providers."+d.ID+"."+f.Key {
				return true
			}
		}
	}
	return driveSystemKey(key)
}
