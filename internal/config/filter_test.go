package config

import (
	"strings"
	"testing"
)

func TestFilterDefaults(t *testing.T) {
	s, err := FromYAML([]byte("model: openai/gpt-x\nmodels: [openai/gpt-x]\n"))
	if err != nil {
		t.Fatal(err)
	}
	c := s.Config()
	if !c.Filter.Enabled || c.Filter.Chars() != DefaultFilterMaxChars {
		t.Fatalf("filter defaults = %+v", c.Filter)
	}
	if c.FilterModel() != c.ApprovalModel() {
		t.Fatalf("filter model %q, want the approval model %q", c.FilterModel(), c.ApprovalModel())
	}
	if (Filter{MaxChars: 5}).Chars() != DefaultFilterMaxChars || (Filter{MaxChars: 1 << 30}).Chars() != FilterMaxChars {
		t.Fatal("max_chars is not bounded")
	}
}

func TestFilterModelMayBeEitherKind(t *testing.T) {
	base := "model: openai/gpt-x\nmodels: [openai/gpt-x]\ndecision_models: [typesafe/jev-latest]\n"
	for model, kind := range map[string]string{"typesafe/jev-latest": "decision", "openai/gpt-x": "text"} {
		s, err := FromYAML([]byte(base + "model_filter: " + model + "\n"))
		if err != nil {
			t.Fatalf("%s: %v", model, err)
		}
		if c := s.Config(); c.FilterModel() != model || c.ModelKind(c.FilterModel()) != kind {
			t.Fatalf("filter model %q kind %q", c.FilterModel(), c.ModelKind(c.FilterModel()))
		}
	}
	if err := validateYAML([]byte(base + "model_filter: nonsense\n")); err == nil {
		t.Fatal("a model id with no provider was accepted")
	}
}

func TestFilterMaxCharsIsValidated(t *testing.T) {
	for yaml, want := range map[string]string{
		"filter:\n  max_chars: 20000\n": "",
		"filter:\n  max_chars: 10\n":    "filter.max_chars",
		"filter:\n  max_chars: lots\n":  "filter.max_chars",
	} {
		err := validateYAML([]byte(yaml))
		if (err == nil) != (want == "") || (err != nil && !strings.Contains(err.Error(), want)) {
			t.Errorf("%q: err = %v, want %q", yaml, err, want)
		}
	}
}
