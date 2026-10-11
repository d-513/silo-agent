//go:build live

package llm

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// TestLiveDecide asks a real decision model the three kinds of question,
// through whichever provider has a key, and checks the answers come back in
// the shape this engine reads. It spends a few hundred Jev input tokens (a
// few thousandths of a cent). Run it after changing decide.go, or when a
// provider changes its decisions API:
//
//	SILO_LIVE_OPENROUTER_KEY=sk-or-... go test -tags live ./internal/llm -run LiveDecide -v
//	SILO_LIVE_TYPESAFE_KEY=...        go test -tags live ./internal/llm -run LiveDecide -v
func TestLiveDecide(t *testing.T) {
	ran := false
	for _, p := range []struct{ provider, env, model string }{
		{OpenRouter, "SILO_LIVE_OPENROUTER_KEY", "typesafe/jev-1.13"},
		{TypeSafe, "SILO_LIVE_TYPESAFE_KEY", "jev-latest"},
	} {
		key := os.Getenv(p.env)
		if key == "" {
			continue
		}
		ran = true
		if m := os.Getenv("SILO_LIVE_DECISION_MODEL"); m != "" {
			p.model = m
		}
		c, err := New(p.provider, Settings{"api_key": key})
		if err != nil {
			t.Fatal(err)
		}
		res, err := c.(Decider).Decide(context.Background(), DecisionRequest{
			Model: p.model,
			State: json.RawMessage(`{"ticket":"My checkout page shows a blank screen after I click Pay. I have tried two browsers."}`),
			Questions: map[string]Question{
				"is_bug": {Type: QuestionNoul, Instructions: "Is the customer reporting a software defect?"},
				"team": {Type: QuestionChoice, Instructions: "Which team should own this ticket?", Options: map[string]string{
					"payments": "Checkout, billing, or payment processing issues.",
					"account":  "Login, permissions, or profile issues.",
					"sales":    "Questions about plans and pricing.",
				}},
				"urgency": {Type: QuestionScore, Instructions: "How urgent is this ticket?", Levels: []string{
					"Can wait for the next release", "Should be fixed this week", "Blocking revenue right now",
				}},
			},
		})
		if err != nil {
			t.Fatalf("%s: %v", p.provider, err)
		}
		out, _ := json.MarshalIndent(res, "", "  ")
		t.Logf("%s/%s answered:\n%s", p.provider, p.model, out)
		if bug := res.Answers["is_bug"]; *bug.Noul < 0.5 {
			t.Errorf("%s: a blank checkout page should read as a defect, got %v", p.provider, *bug.Noul)
		}
		if team := res.Answers["team"]; team.Choice != "payments" || team.Certainty() <= 0 {
			t.Errorf("%s: team %+v", p.provider, team)
		}
		if res.Usage.InputTokens == 0 {
			t.Errorf("%s: no usage reported", p.provider)
		}

		// The listing names the model as a decision model.
		list, err := c.(ModelLister).ListModels(context.Background())
		if err != nil {
			t.Fatalf("%s list: %v", p.provider, err)
		}
		found := false
		for _, m := range list {
			if m.ID == p.model {
				found = m.Kind == KindDecision
			}
		}
		if !found {
			t.Errorf("%s does not list %s as a decision model", p.provider, p.model)
		}
	}
	if !ran {
		t.Skip("set SILO_LIVE_OPENROUTER_KEY or SILO_LIVE_TYPESAFE_KEY")
	}
}
