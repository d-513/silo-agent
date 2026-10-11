//go:build live

package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"silo.agent/internal/app/models"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/config"
	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
)

// TestLiveDecideToolCall offers the decide tool to real chat models, the way
// a run does, and runs the call each one writes against a real decision
// model. It is the check that the schema is one providers accept and models
// fill in correctly, which no fake can make. A few cents at most; run it
// after changing decide's schema, description or prompt section:
//
//	SILO_LIVE_OPENROUTER_KEY=sk-or-... go test -tags live ./internal/app -run LiveDecideToolCall -v
//
// SILO_LIVE_MODELS is a comma list of OpenRouter chat models to try.
func TestLiveDecideToolCall(t *testing.T) {
	key := os.Getenv("SILO_LIVE_OPENROUTER_KEY")
	if key == "" {
		t.Skip("SILO_LIVE_OPENROUTER_KEY is not set")
	}
	chat := "anthropic/claude-haiku-5.5,openai/gpt-5.6-luna,deepseek/deepseek-v4.1-flash"
	if m := os.Getenv("SILO_LIVE_MODELS"); m != "" {
		chat = m
	}
	const decider = "openrouter/typesafe/jev-1.13"
	cfg := config.Config{
		DecisionModels: []string{decider},
		Providers:      map[string]config.Provider{llm.OpenRouter: {APIKey: key}},
	}
	svc := models.New(nil, func() config.Config { return cfg })

	var def tool.Def
	for _, d := range (&App{}).toolDefs() {
		if d.Name == "decide" {
			def = d
		}
	}
	if !def.ExposeCompletion {
		t.Fatal("decide is not a chat tool")
	}
	client, err := llm.New(llm.OpenRouter, llm.Settings{"api_key": key})
	if err != nil {
		t.Fatal(err)
	}
	system := "You are a Bot with tools.\n\n## Decisions\n" + svc.DecidePrompt(prompts.Decide)
	const ask = "A support ticket came in: \"I was charged twice for my subscription this month and I want one charge refunded.\" " +
		"Use the decide tool, once, to judge three things about it: whether it is a billing problem, which team should own it " +
		"(billing, technical or sales), and how urgent it is on a three-level scale."

	for _, model := range strings.Split(chat, ",") {
		model = strings.TrimSpace(model)
		res, err := client.Complete(context.Background(), llm.Request{
			Model:     model,
			System:    []llm.SystemBlock{{Text: system}},
			Messages:  []llm.Message{{Role: llm.RoleUser, Text: ask}},
			Tools:     tool.LLM([]tool.Def{def}),
			MaxTokens: 1500,
		})
		if err != nil {
			t.Errorf("%s refused the request (the schema?): %v", model, err)
			continue
		}
		if len(res.ToolCalls) == 0 || res.ToolCalls[0].Name != "decide" {
			t.Errorf("%s did not call decide: %q %+v", model, res.Text, res.ToolCalls)
			continue
		}
		raw := res.ToolCalls[0].Arguments
		args := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			t.Errorf("%s wrote unreadable arguments: %v\n%s", model, err, raw)
			continue
		}
		out, err := svc.DecideTool(context.Background(), "", args)
		if err != nil {
			t.Errorf("%s wrote a call the decision model could not answer: %v\n%s", model, err, raw)
			continue
		}
		var got struct {
			Answers map[string]llm.Answer `json:"answers"`
		}
		_ = json.Unmarshal([]byte(out), &got)
		kinds := map[string]int{}
		for _, a := range got.Answers {
			kinds[a.Type]++
		}
		t.Logf("%s called decide:\n  %s\n  → %s", model, raw, out)
		if kinds[llm.QuestionNoul] == 0 || kinds[llm.QuestionChoice] == 0 || kinds[llm.QuestionScore] == 0 {
			t.Errorf("%s did not use all three question types: %v", model, kinds)
		}
	}
}
