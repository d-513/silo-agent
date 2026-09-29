package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"silo.agent/internal/drives"
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
	HTTPAddr      string              `koanf:"http_addr"`
	PublicURL     string              `koanf:"public_url"`
	DataDir       string              `koanf:"data_dir"`
	DatabaseURL   string              `koanf:"database_url"`
	DockerHost    string              `koanf:"docker_host"`
	CPURL         string              `koanf:"cp_url"`
	BotImage      string              `koanf:"bot_image"`
	MCPStdioImage string              `koanf:"mcp_stdio_image"`
	Model         string              `koanf:"model"`
	ModelTitle    string              `koanf:"model_title"`
	ModelApproval string              `koanf:"model_approval"`
	// ModelSubagent is the default model of subagents a lead starts; empty
	// means the lead's own model. spawn_agent may still name another.
	ModelSubagent string `koanf:"model_subagent"`
	// ModelMemory is the memory collector's model; empty means the title model.
	ModelMemory string `koanf:"model_memory"`
	EmbedModel    string              `koanf:"embedding_model"`
	Transcribe    string              `koanf:"transcribe_model"`
	Memory        Memory              `koanf:"memory"`
	Context       Context             `koanf:"context"`
	Thinking      Thinking            `koanf:"thinking"`
	Runs          Runs                `koanf:"runs"`
	Models        []string            `koanf:"models"`
	Debug         bool                `koanf:"debug"`
	Bootstrap     Bootstrap           `koanf:"bootstrap"`
	Providers     map[string]Provider `koanf:"providers"`
	Search        Search              `koanf:"search"`
	Drives        Drives              `koanf:"drives"`
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

// DriveSystemKey names a drive template's system value in silo.yaml.
func DriveSystemKey(template, name string) string {
	return "drives.providers." + template + "." + name
}

// driveSystemKey is true for drives.providers.<template>.<system var>.
func driveSystemKey(key string) bool {
	rest, ok := strings.CutPrefix(key, "drives.providers.")
	if !ok {
		return false
	}
	tk, v, ok := strings.Cut(rest, ".")
	if !ok {
		return false
	}
	t, ok := drives.Builtin().Get(tk)
	if !ok {
		return false
	}
	_, ok = t.Var(drives.KindSystem, v)
	return ok
}

// DriveSystem is one template's system values, env winning over yaml.
func (s *Store) DriveSystem(template string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]string{}
	t, ok := drives.Builtin().Get(template)
	if !ok || s.merged == nil {
		return out
	}
	for _, v := range t.VarsOf(drives.KindSystem) {
		if val := strings.TrimSpace(s.merged.String(DriveSystemKey(template, v.Key))); val != "" {
			out[v.Key] = val
		}
	}
	return out
}

func setDefaults(k *koanf.Koanf) {
	_ = k.Set("http_addr", ":8080")
	_ = k.Set("data_dir", "./data")
	_ = k.Set("database_url", DefaultDatabaseURL)
	_ = k.Set("cp_url", "http://host.containers.internal:8080")
	_ = k.Set("bot_image", "localhost/silo-bot:v1")
	_ = k.Set("mcp_stdio_image", DefaultMCPStdioImage)
	_ = k.Set("drives.image", DefaultDriveImage)
	_ = k.Set("drives.cache_max_size", "10G")
	_ = k.Set("search.engine", search.DefaultEngine)
	_ = k.Set("model", DefaultModel)
	_ = k.Set("embedding_model", DefaultEmbeddingModel)
	_ = k.Set("transcribe_model", DefaultTranscribeModel)
	_ = k.Set("memory.auto_recall", true)
	_ = k.Set("memory.collect", true)
	_ = k.Set("context.window", DefaultContextWindow)
	_ = k.Set("context.compact_at", DefaultCompactAt)
	_ = k.Set("runs.max_duration", "120m")
}

type yamlBytes []byte

func (y yamlBytes) ReadBytes() ([]byte, error) { return []byte(y), nil }
func (y yamlBytes) Read() (map[string]any, error) {
	return nil, fmt.Errorf("bytes only")
}

func Load() (*Store, error) {
	path, err := filepath.Abs(YAMLName)
	if err != nil {
		return nil, err
	}
	return LoadPath(path)
}

// LoadPath loads a Store from an explicit YAML path. Environment overrides
// still apply (SILO_*), matching Load.
func LoadPath(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// FromYAML builds a Store from in-memory YAML with no file access and no
// environment overrides. Tests use it to get a deterministic config that cannot
// be perturbed by a stray SILO_* variable in the shell. Patch and WriteYAML are
// unavailable on a store with no path and return an error.
func FromYAML(raw []byte) (*Store, error) {
	s := &Store{}
	y := koanf.New(".")
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := y.Load(yamlBytes(raw), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("invalid yaml: %w", err)
		}
	}
	m := koanf.New(".")
	setDefaults(m)
	if err := m.Merge(y); err != nil {
		return nil, err
	}
	var c Config
	if err := m.Unmarshal("", &c); err != nil {
		return nil, err
	}
	if c.DockerHost == "" {
		c.DockerHost = os.Getenv("DOCKER_HOST")
	}
	s.yaml = y
	s.env = koanf.New(".")
	s.merged = m
	s.live = c
	return s, nil
}

func (s *Store) reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reloadLocked()
}

func (s *Store) reloadLocked() error {
	y := koanf.New(".")
	_ = y.Load(file.Provider(s.path), yaml.Parser())
	e := koanf.New(".")
	if err := e.Load(env.Provider("SILO_", ".", envKey), nil); err != nil {
		return err
	}
	m := koanf.New(".")
	setDefaults(m)
	if err := m.Merge(y); err != nil {
		return err
	}
	if err := m.Merge(e); err != nil {
		return err
	}
	var c Config
	if err := m.Unmarshal("", &c); err != nil {
		return err
	}
	if c.DockerHost == "" {
		c.DockerHost = os.Getenv("DOCKER_HOST")
	}
	s.yaml = y
	s.env = e
	s.merged = m
	s.live = c
	return nil
}

func (s *Store) Path() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.path
}

func (s *Store) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live
}

func (s *Store) Get(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if key == "docker_host" {
		return s.live.DockerHost
	}
	return s.merged.String(key)
}

func (s *Store) Source(key string) Source {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sourceLocked(key)
}

func (s *Store) sourceLocked(key string) Source {
	if s.env != nil && s.env.Exists(key) {
		return SourceEnv
	}
	if s.yaml != nil && s.yaml.Exists(key) {
		return SourceYAML
	}
	return SourceDefault
}

func (s *Store) YAML() ([]byte, error) {
	s.mu.RLock()
	path := s.path
	s.mu.RUnlock()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []byte{}, nil
	}
	return b, err
}

func (s *Store) Fields() []Field {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Field, 0, len(fieldDefs)+8)
	for _, m := range fieldDefs {
		val := s.merged.String(m.Key)
		if m.Key == "docker_host" {
			val = s.live.DockerHost
		}
		out = append(out, Field{
			Key: m.Key, Value: val, Source: s.sourceLocked(m.Key),
			EnvName: EnvName(m.Key), Secret: m.Secret, Restart: m.Restart, Type: m.Type,
		})
	}
	for _, d := range search.Descriptors() {
		for _, f := range d.Settings {
			key := "search." + d.ID + "." + f.Key
			out = append(out, Field{
				Key: key, Value: s.merged.String(key), Source: s.sourceLocked(key),
				EnvName: EnvName(key), Secret: f.Secret, Type: f.Type,
			})
		}
	}
	for _, d := range llm.Descriptors() {
		for _, f := range d.Settings {
			key := "providers." + d.ID + "." + f.Key
			out = append(out, Field{
				Key: key, Value: s.merged.String(key), Source: s.sourceLocked(key),
				EnvName: EnvName(key), Secret: f.Secret, Type: f.Type,
			})
		}
	}
	return out
}

// StringList reads a YAML sequence setting (currently just models).
func (s *Store) StringList(key string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.merged == nil {
		return nil
	}
	return s.merged.Strings(key)
}

// SetModels writes the allowed-model allowlist and reloads.
func (s *Store) SetModels(models []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	node, err := parseOrEmpty(raw)
	if err != nil {
		return err
	}
	if err := setNodeList(node, "models", models); err != nil {
		return err
	}
	out, err := encodeNode(node)
	if err != nil {
		return err
	}
	if err := validateYAML(out); err != nil {
		return err
	}
	if err := os.WriteFile(s.path, out, 0o600); err != nil {
		return err
	}
	return s.reloadLocked()
}

func (s *Store) WriteYAML(raw []byte) error {
	if err := validateYAML(raw); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.WriteFile(s.path, raw, 0o600); err != nil {
		return err
	}
	return s.reloadLocked()
}

func (s *Store) Patch(fields map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	node, err := parseOrEmpty(raw)
	if err != nil {
		return err
	}
	for k, v := range fields {
		if err := setNodeKey(node, k, v); err != nil {
			return err
		}
	}
	out, err := encodeNode(node)
	if err != nil {
		return err
	}
	if err := validateYAML(out); err != nil {
		return err
	}
	if err := os.WriteFile(s.path, out, 0o600); err != nil {
		return err
	}
	return s.reloadLocked()
}

func (s *Store) Reload() error {
	return s.reload()
}

func validateYAML(raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	k := koanf.New(".")
	if err := k.Load(yamlBytes(raw), yaml.Parser()); err != nil {
		return fmt.Errorf("invalid yaml: %w", err)
	}
	if e := strings.TrimSpace(k.String("search.engine")); e != "" && !search.Known(e) {
		return fmt.Errorf("unknown search engine %q", e)
	}
	if cv := k.Get("connector_vars"); cv != nil {
		m, ok := cv.(map[string]any)
		if !ok {
			return fmt.Errorf("connector_vars must be a mapping")
		}
		for name, val := range m {
			if !ValidVarName(name) {
				return fmt.Errorf("invalid connector variable name %q", name)
			}
			if _, ok := val.(string); !ok {
				return fmt.Errorf("connector variable %q must be a string", name)
			}
		}
	}
	for _, key := range k.Keys() {
		rest, ok := strings.CutPrefix(key, "providers.")
		if !ok {
			continue
		}
		id, sub, _ := strings.Cut(rest, ".")
		d, ok := llm.Lookup(id)
		if !ok {
			return fmt.Errorf("unknown model provider %q", id)
		}
		if sub == "" {
			continue
		}
		if !providerSettingKnown(d, sub) {
			return fmt.Errorf("unknown setting %q for provider %q", sub, id)
		}
	}
	for _, key := range k.Keys() {
		if strings.HasPrefix(key, "drives.providers.") && !driveSystemKey(key) {
			return fmt.Errorf("unknown drive setting %q", key)
		}
	}
	for _, m := range k.Strings("models") {
		if _, _, err := llm.Parse(strings.TrimSpace(m)); err != nil {
			return fmt.Errorf("invalid model %q: %w", m, err)
		}
	}
	for _, key := range []string{"model", "model_title", "model_approval", "model_subagent", "model_memory"} {
		if v := strings.TrimSpace(k.String(key)); v != "" {
			if _, _, err := llm.Parse(v); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	if v := strings.TrimSpace(k.String("transcribe_model")); v != "" && !strings.EqualFold(v, TranscribeOff) {
		if _, _, err := llm.Parse(v); err != nil {
			return fmt.Errorf("transcribe_model: %w", err)
		}
	}
	return nil
}

func providerSettingKnown(d llm.Descriptor, key string) bool {
	for _, s := range d.Settings {
		if s.Key == key {
			return true
		}
	}
	return false
}

// AllowedModels returns the operator's model allowlist.
func (s *Store) AllowedModels() []string {
	return s.StringList("models")
}

// ConnectorVarMap returns the effective connector variables with the
// environment winning over silo.yaml. Environment keys are lowercased by the
// koanf env provider, so an env override is matched to its YAML key
// case-insensitively to avoid a duplicate entry.
func (s *Store) ConnectorVarMap() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	yamlVars := map[string]string{}
	if s.yaml != nil {
		yamlVars = s.yaml.StringMap("connector_vars")
	}
	envVars := map[string]string{}
	if s.env != nil {
		envVars = s.env.StringMap("connector_vars")
	}
	out := make(map[string]string, len(yamlVars)+len(envVars))
	lowerToKey := map[string]string{}
	for k, v := range yamlVars {
		out[k] = v
		lk := strings.ToLower(k)
		if _, dup := lowerToKey[lk]; !dup {
			lowerToKey[lk] = k
		}
	}
	for k, v := range envVars {
		lk := strings.ToLower(k)
		if yk, ok := lowerToKey[lk]; ok {
			out[yk] = v
			continue
		}
		out[k] = v
		lowerToKey[lk] = k
	}
	return out
}

// ConnectorVars returns the effective connector variables (env over yaml),
// sorted by name so the UI and YAML stay stable.
func (s *Store) ConnectorVars() []NamedVar {
	m := s.ConnectorVarMap()
	out := make([]NamedVar, 0, len(m))
	for k, v := range m {
		out = append(out, NamedVar{Name: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if li == lj {
			return out[i].Name < out[j].Name
		}
		return li < lj
	})
	return out
}

// ConnectorVarSource reports where a variable's effective value comes from.
func (s *Store) ConnectorVarSource(name string) Source {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.varSourceLocked(name)
}

func (s *Store) varSourceLocked(name string) Source {
	key := "connector_vars." + name
	if s.env != nil && (s.env.Exists(key) || s.env.Exists(strings.ToLower(key))) {
		return SourceEnv
	}
	if s.yaml != nil && s.yaml.Exists(key) {
		return SourceYAML
	}
	return SourceDefault
}

// SetConnectorVars replaces the connector_vars mapping and reloads. Names must
// be simple identifiers; values are stored verbatim (they are not secrets).
func (s *Store) SetConnectorVars(vars []NamedVar) error {
	seen := map[string]bool{}
	for _, v := range vars {
		if !ValidVarName(v.Name) {
			return fmt.Errorf("invalid variable name %q", v.Name)
		}
		if seen[v.Name] {
			return fmt.Errorf("duplicate variable %q", v.Name)
		}
		seen[v.Name] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	node, err := parseOrEmpty(raw)
	if err != nil {
		return err
	}
	if err := setNodeMap(node, "connector_vars", vars); err != nil {
		return err
	}
	out, err := encodeNode(node)
	if err != nil {
		return err
	}
	if err := validateYAML(out); err != nil {
		return err
	}
	if err := os.WriteFile(s.path, out, 0o600); err != nil {
		return err
	}
	return s.reloadLocked()
}
