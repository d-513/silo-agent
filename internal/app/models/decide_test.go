package models

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"silo.agent/internal/config"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

func decideService(t *testing.T, cfg config.Config) *Service {
	t.Helper()
	dummy.Reset()
	if cfg.Providers == nil {
		cfg.Providers = map[string]config.Provider{dummy.Provider: {APIKey: "test"}}
	}
	return New(nil, func() config.Config { return cfg })
}

// The Bot's decide tool asks the default decision model and answers the
// typed answers as JSON, whichever surface called it.
func TestDecideTool(t *testing.T) {
	s := decideService(t, config.Config{
		Models:         []string{"dummy/echo"},
		DecisionModels: []string{"dummy/judge", "dummy/other"},
		ModelDecision:  "dummy/judge",
	})
	out, err := s.DecideTool(context.Background(), "bot", map[string]any{
		"state": map[string]any{"mail": "You won a prize! Test_Yes_junk Test_Pick_spam"},
		"questions": map[string]any{
			"junk": map[string]any{"type": "noul", "instructions": "Is this unwanted mail?"},
			"kind": map[string]any{"type": "choice", "instructions": "What is it?", "criteria": map[string]any{"spam": "junk", "personal": "from a person"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Model   string                `json:"model"`
		Answers map[string]llm.Answer `json:"answers"`
		Usage   map[string]any        `json:"usage"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Model != "dummy/judge" {
		t.Fatalf("model %q", got.Model)
	}
	if a := got.Answers["junk"]; a.Noul == nil || *a.Noul < 0.9 {
		t.Fatalf("noul %+v", a)
	}
	if a := got.Answers["kind"]; a.Choice != "spam" || a.Probabilities["spam"] < 0.9 {
		t.Fatalf("choice %+v", a)
	}
	if _, ok := got.Usage["input_tokens"]; !ok {
		t.Fatalf("usage %v", got.Usage)
	}
	sent := dummy.Decided()
	if len(sent) != 1 || sent[0].Model != "judge" {
		t.Fatalf("sent %+v", sent)
	}

	// Another allowed decision model by name; a model writes questions as a
	// JSON string as often as an object.
	out, err = s.DecideTool(context.Background(), "bot", map[string]any{
		"model": "dummy/other", "state": "plain text",
		"questions": `{"q": {"type": "score", "instructions": "How long?", "criteria": ["short", "long"]}}`,
	})
	if err != nil || !strings.Contains(out, `"model":"dummy/other"`) {
		t.Fatalf("named model: %v %s", err, out)
	}

	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"a text model":     {map[string]any{"model": "dummy/echo", "state": "x", "questions": map[string]any{"q": map[string]any{"type": "noul", "instructions": "?"}}}, "dummy/judge"},
		"no questions":     {map[string]any{"state": "x"}, "questions"},
		"no state":         {map[string]any{"questions": map[string]any{"q": map[string]any{"type": "noul", "instructions": "?"}}}, "state"},
		"a bad question":   {map[string]any{"state": "x", "questions": map[string]any{"q": map[string]any{"type": "choice", "instructions": "?"}}}, `"q"`},
		"upstream failure": {map[string]any{"state": "Test_Decide_Error", "questions": map[string]any{"q": map[string]any{"type": "noul", "instructions": "?"}}}, "529"},
	} {
		if _, err := s.DecideTool(context.Background(), "bot", tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error with %q", name, err, tc.want)
		}
	}
}

// With no decision model allowed there is nothing to decide with: the tool
// and its prompt section are off.
func TestDecideNeedsADecisionModel(t *testing.T) {
	s := decideService(t, config.Config{Models: []string{"dummy/echo"}})
	if s.CanDecide() {
		t.Fatal("no decision models allowed, yet CanDecide")
	}
	if got := s.DecidePrompt("Default {default}; allowed {models}."); got != "" {
		t.Fatalf("prompt %q", got)
	}
	if _, err := s.DecideTool(context.Background(), "bot", map[string]any{"state": "x", "questions": map[string]any{}}); err == nil {
		t.Fatal("decide should fail with no decision model")
	}

	s = decideService(t, config.Config{DecisionModels: []string{"dummy/judge", "dummy/other"}})
	if !s.CanDecide() {
		t.Fatal("a decision model is allowed")
	}
	if got := s.DecidePrompt("Default {default}; allowed {models}."); got != "Default dummy/judge; allowed dummy/judge, dummy/other." {
		t.Fatalf("prompt %q", got)
	}
}

// list_models tells the Bot which decision models decide() may name.
func TestListToolNamesDecisionModels(t *testing.T) {
	s := decideService(t, config.Config{Model: "dummy/echo", Models: []string{"dummy/echo"}, DecisionModels: []string{"dummy/judge"}})
	out, err := s.ListTool("", "")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if got["default_decision"] != "dummy/judge" || len(got["decision_models"].([]any)) != 1 {
		t.Fatalf("list %s", out)
	}
	if len(got["models"].([]any)) != 1 {
		t.Fatalf("text models %s", out)
	}

	// The two allowlists are told apart for the UI.
	if o := s.Allowed(); len(o) != 1 || o[0].Kind != llm.KindText {
		t.Fatalf("text options %+v", o)
	}
	if o := s.Deciders(); len(o) != 1 || o[0].ID != "dummy/judge" || o[0].Kind != llm.KindDecision {
		t.Fatalf("decision options %+v", o)
	}
}
