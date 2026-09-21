package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
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
	out["cache"] = "false"
	if p.Cache {
		out["cache"] = "true"
	}
	return out
}

type Search struct {
	Engine string `koanf:"engine"`
}

const DefaultModel = "openrouter/openai/gpt-5.6-luna"
const DefaultMCPStdioImage = "localhost/silo-mcp-stdio:v1"

type Config struct {
	HTTPAddr      string              `koanf:"http_addr"`
	PublicURL     string              `koanf:"public_url"`
	DataDir       string              `koanf:"data_dir"`
	DockerHost    string              `koanf:"docker_host"`
	CPURL         string              `koanf:"cp_url"`
	BotImage      string              `koanf:"bot_image"`
	MCPStdioImage string              `koanf:"mcp_stdio_image"`
	Model         string              `koanf:"model"`
	ModelTitle    string              `koanf:"model_title"`
	Models        []string            `koanf:"models"`
	Bootstrap     Bootstrap           `koanf:"bootstrap"`
	Providers     map[string]Provider `koanf:"providers"`
	Search        Search              `koanf:"search"`
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

type fieldMeta struct {
	Key     string
	Secret  bool
	Restart bool
}

var fieldDefs = []fieldMeta{
	{Key: "model"},
	{Key: "model_title"},
	{Key: "search.engine"},
	{Key: "http_addr", Restart: true},
	{Key: "public_url"},
	{Key: "cp_url"},
	{Key: "data_dir", Restart: true},
	{Key: "docker_host", Restart: true},
	{Key: "bot_image"},
	{Key: "mcp_stdio_image"},
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
	return false
}

func setDefaults(k *koanf.Koanf) {
	_ = k.Set("http_addr", ":8080")
	_ = k.Set("data_dir", "./data")
	_ = k.Set("cp_url", "http://host.containers.internal:8080")
	_ = k.Set("bot_image", "localhost/silo-bot:v1")
	_ = k.Set("mcp_stdio_image", DefaultMCPStdioImage)
	_ = k.Set("search.engine", search.DefaultEngine)
	_ = k.Set("model", DefaultModel)
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
			EnvName: EnvName(m.Key), Secret: m.Secret, Restart: m.Restart,
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
	for _, m := range k.Strings("models") {
		if _, _, err := llm.Parse(strings.TrimSpace(m)); err != nil {
			return fmt.Errorf("invalid model %q: %w", m, err)
		}
	}
	for _, key := range []string{"model", "model_title"} {
		if v := strings.TrimSpace(k.String(key)); v != "" {
			if _, _, err := llm.Parse(v); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
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
