package config

import (
	"strings"
	"testing"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/v2"
)

func modelsCheck(t *testing.T, raw string) error {
	t.Helper()
	k := koanf.New(".")
	if err := k.Load(yamlBytes([]byte(raw)), yaml.Parser()); err != nil {
		t.Fatal(err)
	}
	return validateModels(k)
}

func TestFastModelsAreTextOnly(t *testing.T) {
	if err := modelsCheck(t, "model: anthropic/claude-opus-5-5\nmodels: [anthropic/claude-opus-5-5, anthropic/claude-opus-5-5:fast]\n"); err != nil {
		t.Fatalf("a fast text model beside its plain one: %v", err)
	}
	err := modelsCheck(t, "embedding_models: [openai/text-embedding-3-small:fast]\n")
	if err == nil || !strings.Contains(err.Error(), "only text models") {
		t.Fatalf("a fast embedding model must be refused, got %v", err)
	}
	if err := modelsCheck(t, "models: [groq/llama:fast]\n"); err == nil || !strings.Contains(err.Error(), "no fast mode") {
		t.Fatalf("a provider with no fast mode must refuse :fast, got %v", err)
	}
}

func TestOverridesCoverTheFastModel(t *testing.T) {
	c := Context{Windows: []ModelWindow{{Model: "anthropic/claude-opus-5-5", Window: 500000}}}
	if c.WindowFor("anthropic/claude-opus-5-5:fast") != 500000 {
		t.Fatal("a fast model does not take its base model's window")
	}
	th := Thinking{Levels: []ModelThinking{{Model: "claude/opus", Levels: []string{"low"}}, {Model: "claude/opus:fast", Levels: []string{"high"}}}}
	if l, _ := th.LevelsFor("claude/opus:fast"); len(l) != 1 || l[0] != "high" {
		t.Fatalf("a fast model's own entry wins: %v", l)
	}
}
