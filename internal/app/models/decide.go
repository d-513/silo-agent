package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"silo.agent/internal/llm"
)

// Decide asks a decision model typed questions about a state. modelID is a
// provider/model id (the request's own Model is filled from it); label names
// the feature in the debug log, where the call is recorded like any other.
func (s *Service) Decide(ctx context.Context, modelID, botID, label string, req llm.DecisionRequest) (llm.DecisionResponse, error) {
	client, provider, model, err := s.Client(modelID)
	if err != nil {
		return llm.DecisionResponse{}, err
	}
	decider, ok := client.(llm.Decider)
	if !ok {
		return llm.DecisionResponse{}, fmt.Errorf("%s has no decision models", provider)
	}
	req.Model = model
	start := time.Now()
	res, err := decider.Decide(ctx, req)
	s.Record(botID, label, decisionRecord(provider, req, res, err, time.Since(start)))
	return res, err
}

// decisionRecord is a decision as the debug log shows a model call: the state
// and questions as the request, the typed answers as the response.
func decisionRecord(provider string, req llm.DecisionRequest, res llm.DecisionResponse, err error, took time.Duration) llm.Record {
	questions, _ := json.MarshalIndent(req.Questions, "", "  ")
	rec := llm.Record{
		Provider: provider, Model: req.Model,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "state:\n" + string(req.State) + "\n\nquestions:\n" + string(questions)}},
		Usage:    res.Usage, Error: err, Duration: took,
	}
	if err == nil {
		answers, _ := json.MarshalIndent(res.Answers, "", "  ")
		rec.Text = string(answers)
	}
	return rec
}

// Deciders is the allowlist of decision models, for the UI.
func (s *Service) Deciders() []Option {
	return options(s.cfg().DecisionModels, llm.KindDecision)
}

// CanDecide reports whether the operator has allowed a decision model, which
// is what turns the Bot's decide tool on.
func (s *Service) CanDecide() bool { return s.cfg().DecisionModel() != "" }

// DecidePrompt fills the decide section of the system prompt with the default
// decision model and the allowed ones; "" while there is none. It reads only
// the operator's config, so it is the same text for every Bot.
func (s *Service) DecidePrompt(tmpl string) string {
	cfg := s.cfg()
	def := cfg.DecisionModel()
	if def == "" {
		return ""
	}
	ids := make([]string, 0, len(cfg.DecisionModels))
	for _, o := range s.Deciders() {
		ids = append(ids, o.ID)
	}
	return strings.NewReplacer("{default}", def, "{models}", strings.Join(ids, ", ")).Replace(tmpl)
}

// DecideTool is the bot-facing decide tool: typed questions about a state,
// answered by the default decision model or another allowed one. The answer
// is JSON on either surface, since its values are what the caller branches on.
func (s *Service) DecideTool(ctx context.Context, botID string, args map[string]any) (string, error) {
	cfg := s.cfg()
	modelID := cfg.DecisionModel()
	if modelID == "" {
		return "", errors.New("the operator has not allowed a decision model")
	}
	if m, _ := args["model"].(string); strings.TrimSpace(m) != "" {
		modelID = strings.TrimSpace(m)
		if !llm.Allowed(modelID, cfg.DecisionModels) {
			return "", fmt.Errorf("model %q is not an allowed decision model; allowed: %s", modelID, strings.Join(cfg.DecisionModels, ", "))
		}
	}
	req := llm.DecisionRequest{}
	if state, ok := args["state"]; ok && state != nil {
		raw, err := json.Marshal(state)
		if err != nil {
			return "", fmt.Errorf("state: %w", err)
		}
		req.State = raw
	}
	questions, err := rawQuestions(args["questions"])
	if err != nil {
		return "", err
	}
	if req.Questions, err = llm.ParseQuestions(questions); err != nil {
		return "", fmt.Errorf("questions: %w", err)
	}
	if err := req.Validate(); err != nil {
		return "", err
	}
	res, err := s.Decide(ctx, modelID, botID, "decide", req)
	if err != nil {
		return "", err
	}
	usage := map[string]any{"input_tokens": res.Usage.InputTokens, "output_tokens": res.Usage.OutputTokens}
	if res.Cost > 0 {
		usage["cost"] = res.Cost
	}
	out, err := json.Marshal(map[string]any{"model": modelID, "answers": res.Answers, "usage": usage})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// rawQuestions is the questions argument as JSON. A model on the chat surface
// sometimes writes the object as a JSON string; that is read as the object.
func rawQuestions(v any) (json.RawMessage, error) {
	switch q := v.(type) {
	case nil:
		return nil, errors.New(`questions are missing: pass {"name": {"type", "instructions", "criteria"}}`)
	case string:
		return json.RawMessage(q), nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("questions: %w", err)
	}
	return raw, nil
}
