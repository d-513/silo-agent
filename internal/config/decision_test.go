package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// Decision models are an allowlist of their own, with their own default; a
// model is of the kind whose list holds it.
func TestDecisionModels(t *testing.T) {
	s, err := FromYAML([]byte(`model: openrouter/openai/gpt-5.6-luna
models: [openrouter/openai/gpt-5.6-luna]
decision_models: [openrouter/typesafe/jev-1.13, typesafe/jev-latest]
model_decision: typesafe/jev-latest
`))
	if err != nil {
		t.Fatal(err)
	}
	c := s.Config()
	if got := c.DecisionModel(); got != "typesafe/jev-latest" {
		t.Fatalf("default decision model %q", got)
	}
	for id, want := range map[string]string{
		"openrouter/openai/gpt-5.6-luna": "text", "openrouter/typesafe/jev-1.13": "decision", "typesafe/jev-latest": "decision",
		// Not listed anywhere: a provider with decision models only says so.
		"typesafe/jev-preview": "decision", "openrouter/unlisted": "text", "": "text",
	} {
		if got := c.ModelKind(id); got != want {
			t.Errorf("kind of %q = %q, want %q", id, got, want)
		}
	}

	// With no default chosen (or one no longer allowed) the first allowed
	// decision model stands in; with none allowed there is none.
	c.ModelDecision = ""
	if got := c.DecisionModel(); got != "openrouter/typesafe/jev-1.13" {
		t.Fatalf("fallback decision model %q", got)
	}
	c.ModelDecision = "openrouter/gone"
	if got := c.DecisionModel(); got != "openrouter/typesafe/jev-1.13" {
		t.Fatalf("stale default should fall back, got %q", got)
	}
	c.DecisionModels = nil
	if got := c.DecisionModel(); got != "" {
		t.Fatalf("no decision models, got %q", got)
	}
}

// The auto-approval model may be of either kind.
func TestApprovalModelKind(t *testing.T) {
	s, err := FromYAML([]byte("model: openai/gpt-x\nmodels: [openai/gpt-x]\ndecision_models: [typesafe/jev-latest]\nmodel_approval: typesafe/jev-latest\n"))
	if err != nil {
		t.Fatal(err)
	}
	c := s.Config()
	if c.ApprovalModel() != "typesafe/jev-latest" || c.ModelKind(c.ApprovalModel()) != "decision" {
		t.Fatalf("approval model %q kind %q", c.ApprovalModel(), c.ModelKind(c.ApprovalModel()))
	}
}

func TestValidateDecisionModels(t *testing.T) {
	const or, jev = "openrouter/openai/gpt-5.6-luna", "openrouter/typesafe/jev-1.13"
	for name, tc := range map[string]struct{ yaml, want string }{
		"both lists":            {"models: [" + jev + "]\ndecision_models: [" + jev + "]\n", "both"},
		"text-only provider":    {"decision_models: [anthropic/claude-opus-5-5]\n", "no decision models"},
		"decision-only in text": {"models: [typesafe/jev-latest]\n", "decision models only"},
		"bad id":                {"decision_models: [nope]\n", "provider/model"},
		"bad default":           {"model_decision: nope\n", "model_decision"},
		"decision as a role":    {"decision_models: [" + jev + "]\nmodel_title: " + jev + "\n", "model_title"},
		"decision-only as role": {"model: typesafe/jev-latest\n", "model:"},
		"text as the decider":   {"models: [" + or + "]\nmodel_decision: " + or + "\n", "model_decision"},
	} {
		if err := validateYAML([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error with %q", name, err, tc.want)
		}
	}
	// Either kind may judge auto-approvals.
	for _, approval := range []string{or, jev} {
		yaml := "models: [" + or + "]\ndecision_models: [" + jev + "]\nmodel_decision: " + jev + "\nmodel_approval: " + approval + "\n"
		if err := validateYAML([]byte(yaml)); err != nil {
			t.Errorf("approval model %s: %v", approval, err)
		}
	}
}

func TestSetDecisionModels(t *testing.T) {
	s, err := LoadPath(filepath.Join(t.TempDir(), "silo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetModels([]string{"openai/gpt-x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDecisionModels([]string{"typesafe/jev-latest"}); err != nil {
		t.Fatal(err)
	}
	c := s.Config()
	if len(c.Models) != 1 || c.Models[0] != "openai/gpt-x" || len(c.DecisionModels) != 1 || c.DecisionModels[0] != "typesafe/jev-latest" {
		t.Fatalf("lists %v %v", c.Models, c.DecisionModels)
	}
	if err := s.SetDecisionModels([]string{"openai/gpt-x"}); err == nil {
		t.Fatal("a text-only provider's model must not become a decision model")
	}
	if !KnownKey("model_decision") {
		t.Fatal("model_decision is not a known setting")
	}
}
