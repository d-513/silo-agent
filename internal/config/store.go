package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"silo.agent/internal/llm"
	"silo.agent/internal/search"
	"silo.agent/internal/settingdef"
)

func setDefaults(k *koanf.Koanf) {
	_ = k.Set("http_addr", ":8080")
	_ = k.Set("data_dir", "./data")
	_ = k.Set("database_url", DefaultDatabaseURL)
	_ = k.Set("cp_url", "http://host.containers.internal:8080")
	_ = k.Set("bot_image", "localhost/silo-bot:v1")
	_ = k.Set("mcp_stdio_image", DefaultMCPStdioImage)
	_ = k.Set("drives.image", DefaultDriveImage)
	_ = k.Set("drives.cache_max_size", "10G")
	_ = k.Set("tunnels.enabled", true)
	_ = k.Set("mail.enabled", true)
	_ = k.Set("mail.addr", DefaultMailAddr)
	_ = k.Set("mail.max_size_mb", DefaultMailSizeMB)
	_ = k.Set("search.engine", search.DefaultEngine)
	_ = k.Set("model", DefaultModel)
	_ = k.Set("embedding_model", DefaultEmbeddingModel)
	_ = k.Set("transcribe_model", DefaultTranscribeModel)
	_ = k.Set("memory.auto_recall", true)
	_ = k.Set("memory.collect", true)
	_ = k.Set("knowledge.enabled", true)
	_ = k.Set("knowledge.sync_interval", "15m")
	_ = k.Set("knowledge.ocr", true)
	_ = k.Set("context.window", DefaultContextWindow)
	_ = k.Set("context.compact_at", DefaultCompactAt)
	_ = k.Set("runs.max_duration", "120m")
	_ = k.Set("auth.password", true)
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
	if err := e.Load(env.ProviderWithValue("SILO_", ".", envValue), nil); err != nil {
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
	addFamily := func(prefix, id string, defs []settingdef.Def) {
		for _, f := range defs {
			key := prefix + id + "." + f.Key
			out = append(out, Field{
				Key: key, Value: s.merged.String(key), Source: s.sourceLocked(key),
				EnvName: EnvName(key), Secret: f.Secret, Type: f.Type,
			})
		}
	}
	for _, d := range search.Descriptors() {
		addFamily("search.", d.ID, d.Settings)
	}
	for _, d := range llm.Descriptors() {
		addFamily("providers.", d.ID, d.Settings)
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
	return s.setList("models", models)
}

// SetAutoenableConnectors writes autoenable_connectors, the identifiers of the
// library connectors every new Bot gets, and reloads.
func (s *Store) SetAutoenableConnectors(identifiers []string) error {
	return s.setList("autoenable_connectors", identifiers)
}

// setList replaces a top-level YAML sequence and reloads.
func (s *Store) setList(key string, values []string) error {
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
	if err := setNodeList(node, key, values); err != nil {
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
	if err := validateTunnels(k.String("tunnels.host"), k.String("tunnels.scheme"), k.String("public_url")); err != nil {
		return err
	}
	if err := validateMail(k); err != nil {
		return err
	}
	if err := validateSignIn(k); err != nil {
		return err
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
	switch k.Get("autoenable_connectors").(type) {
	case nil, []any, []string:
	default:
		return fmt.Errorf("autoenable_connectors must be a list of connector identifiers")
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
