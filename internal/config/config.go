package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

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

type OpenRouter struct {
	APIKey string `koanf:"api_key"`
}

type Search struct {
	Engine string `koanf:"engine"`
}

const DefaultModel = "openai/gpt-5.6-luna"
const DefaultMCPStdioImage = "localhost/silo-mcp-stdio:v1"

type Config struct {
	HTTPAddr      string     `koanf:"http_addr"`
	PublicURL     string     `koanf:"public_url"`
	DataDir       string     `koanf:"data_dir"`
	DockerHost    string     `koanf:"docker_host"`
	CPURL         string     `koanf:"cp_url"`
	BotImage      string     `koanf:"bot_image"`
	MCPStdioImage string     `koanf:"mcp_stdio_image"`
	Model         string     `koanf:"model"`
	Bootstrap     Bootstrap  `koanf:"bootstrap"`
	OpenRouter    OpenRouter `koanf:"openrouter"`
	Search        Search     `koanf:"search"`
}

type fieldMeta struct {
	Key     string
	Secret  bool
	Restart bool
}

var fieldDefs = []fieldMeta{
	{Key: "model"},
	{Key: "openrouter.api_key", Secret: true},
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
	s := &Store{path: path}
	if err := s.reload(); err != nil {
		return nil, err
	}
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
	out := make([]Field, 0, len(fieldDefs)+4)
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
				EnvName: EnvName(key), Secret: f.Secret,
			})
		}
	}
	return out
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
	return nil
}
