package config

import (
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Bootstrap struct {
	Email    string `koanf:"email"`
	Password string `koanf:"password"`
}

type OpenRouter struct {
	APIKey string `koanf:"api_key"`
}

// DefaultModel is the Admin fallback when no settings row exists.
const DefaultModel = "openai/gpt-5.6-luna"

type Config struct {
	HTTPAddr   string     `koanf:"http_addr"`
	DataDir    string     `koanf:"data_dir"`
	DockerHost string     `koanf:"docker_host"`
	CPURL      string     `koanf:"cp_url"`
	BotImage   string     `koanf:"bot_image"`
	Bootstrap  Bootstrap  `koanf:"bootstrap"`
	OpenRouter OpenRouter `koanf:"openrouter"`
}

func envKey(s string) string {
	s = strings.ToLower(strings.TrimPrefix(s, "SILO_"))
	return strings.ReplaceAll(s, "__", ".")
}

func Load() (*Config, error) {
	k := koanf.New(".")
	_ = k.Set("http_addr", ":8080")
	_ = k.Set("data_dir", "./data")
	_ = k.Set("cp_url", "http://host.containers.internal:8080")
	_ = k.Set("bot_image", "localhost/silo-bot:v1")
	_ = k.Load(file.Provider("silo.yaml"), yaml.Parser())
	if err := k.Load(env.Provider("SILO_", ".", envKey), nil); err != nil {
		return nil, err
	}
	c := &Config{}
	if err := k.Unmarshal("", c); err != nil {
		return nil, err
	}
	if c.DockerHost == "" {
		c.DockerHost = os.Getenv("DOCKER_HOST")
	}
	return c, nil
}
