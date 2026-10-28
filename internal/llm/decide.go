package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"silo.agent/internal/textx"
)

// A model is one of two kinds. A text model writes: it streams prose and tool
// calls (Client). A decision model answers typed questions about a state with
// probabilities and writes nothing (Decider).
const (
	KindText     = "text"
	KindDecision = "decision"
)

// The three questions a decision model answers. The names are the ones the
// hosted APIs share (TypeSafe's System One API, OpenRouter's Decisions API).
const (
	// QuestionNoul is yes or no: the answer is the probability of yes.
	QuestionNoul = "noul"
	// QuestionChoice picks one of the options the caller defines.
	QuestionChoice = "choice"
	// QuestionScore places the state on an ordered scale of levels.
	QuestionScore = "score"
)

// What the hosted APIs accept in one request.
const (
	maxQuestions   = 200
	maxOptions     = 255
	minScoreLevels = 2
	maxScoreLevels = 10
)

// Question is one typed question. Which criteria it carries depends on Type:
// True/False describe the two answers of a noul (both optional), Options maps
// each choice to what it means, Levels are a score's rungs from lowest up.
type Question struct {
	Type         string
	Instructions string
	True, False  string
	Options      map[string]string
	Levels       []string
}

type questionWire struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// MarshalJSON writes the question as the decision APIs take it.
func (q Question) MarshalJSON() ([]byte, error) {
	w := questionWire{Type: q.Type, Instructions: q.Instructions}
	switch q.Type {
	case QuestionNoul:
		if q.True != "" || q.False != "" {
			w.Criteria = map[string]string{"true": q.True, "false": q.False}
		}
	case QuestionChoice:
		options := make(map[string]string, len(q.Options))
		for name, means := range q.Options {
			if means == "" {
				means = name
			}
			options[name] = means
		}
		w.Criteria = options
	case QuestionScore:
		w.Criteria = q.Levels
	}
	return json.Marshal(w)
}

// UnmarshalJSON reads the same shape. A choice may also list bare option
// names where it has nothing to say about them.
func (q *Question) UnmarshalJSON(raw []byte) error {
	var w struct {
		Type         string          `json:"type"`
		Instructions string          `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return errors.New(`a question is {"type", "instructions", "criteria"} with a text instruction`)
	}
	*q = Question{Type: strings.TrimSpace(w.Type), Instructions: strings.TrimSpace(w.Instructions)}
	if len(w.Criteria) == 0 || string(w.Criteria) == "null" {
		return nil
	}
	switch q.Type {
	case QuestionNoul:
		var c map[string]string
		if err := json.Unmarshal(w.Criteria, &c); err != nil {
			return errors.New(`noul criteria are {"true": "...", "false": "..."}`)
		}
		q.True, q.False = c["true"], c["false"]
	case QuestionChoice:
		if err := json.Unmarshal(w.Criteria, &q.Options); err != nil {
			var names []string
			if err := json.Unmarshal(w.Criteria, &names); err != nil {
				return errors.New(`choice criteria are {"option": "what it means", ...}`)
			}
			q.Options = make(map[string]string, len(names))
			for _, n := range names {
				q.Options[n] = ""
			}
		}
	case QuestionScore:
		if err := json.Unmarshal(w.Criteria, &q.Levels); err != nil {
			return errors.New(`score criteria are a list of levels, lowest first`)
		}
	}
	return nil
}

func (q Question) validate() error {
	if strings.TrimSpace(q.Instructions) == "" {
		return errors.New("instructions are missing")
	}
	switch q.Type {
	case QuestionNoul:
	case QuestionChoice:
		if n := len(q.Options); n < 2 || n > maxOptions {
			return fmt.Errorf("a choice needs 2 to %d options in criteria, got %d", maxOptions, n)
		}
		for name := range q.Options {
			if strings.TrimSpace(name) == "" {
				return errors.New("a choice option has no name")
			}
		}
	case QuestionScore:
		if n := len(q.Levels); n < minScoreLevels || n > maxScoreLevels {
			return fmt.Errorf("a score needs %d to %d levels in criteria, got %d", minScoreLevels, maxScoreLevels, n)
		}
	default:
		return fmt.Errorf("type %q is not one of %s, %s, %s", q.Type, QuestionNoul, QuestionChoice, QuestionScore)
	}
	return nil
}

// ParseQuestions reads a map of named questions from JSON.
func ParseQuestions(raw json.RawMessage) (map[string]Question, error) {
	var qs map[string]Question
	if err := json.Unmarshal(raw, &qs); err != nil {
		var typed *json.UnmarshalTypeError
		if errors.As(err, &typed) || json.Valid(raw) && !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
			return nil, errors.New(`questions are an object of {"name": {"type", "instructions", "criteria"}}`)
		}
		return nil, err
	}
	return qs, nil
}

// DecisionRequest asks a decision model its questions about one state. Model
// is the bare model name. State is JSON: a string, an object or an array.
type DecisionRequest struct {
	Model     string
	State     json.RawMessage
	Questions map[string]Question
}

// Validate refuses what no decision API would take, in words a caller can
// act on, before anything is sent.
func (r DecisionRequest) Validate() error {
	state := bytes.TrimSpace(r.State)
	if len(state) == 0 || !json.Valid(state) || !strings.ContainsRune(`"{[`, rune(state[0])) {
		return errors.New("state must be text, an object or a list")
	}
	if len(r.Questions) == 0 {
		return errors.New("ask at least one question")
	}
	if len(r.Questions) > maxQuestions {
		return fmt.Errorf("at most %d questions a call, got %d", maxQuestions, len(r.Questions))
	}
	names := make([]string, 0, len(r.Questions))
	for name := range r.Questions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return errors.New("a question has no name")
		}
		if err := r.Questions[name].validate(); err != nil {
			return fmt.Errorf("question %q: %w", name, err)
		}
	}
	return nil
}

// Answer is one typed answer. Noul is P(yes); Choice is the picked option and
// Score the probability-weighted position on the scale, both with the
// distribution in Probabilities (a score's keys are level indexes, named by
// Legend). Confidence is the model's calibrated certainty in a choice or a
// score; a noul has none, its probability is the certainty.
type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

// Certainty is how sure the model is of the answer it gave, from 0 to 1: the
// reported confidence, else the probability of the chosen option, else how
// far a noul sits from a coin toss toward its nearer end.
func (a Answer) Certainty() float64 {
	switch {
	case a.Confidence != nil:
		return *a.Confidence
	case a.Type == QuestionChoice:
		return a.Probabilities[a.Choice]
	case a.Noul != nil:
		return max(*a.Noul, 1-*a.Noul)
	}
	return 0
}

// DecisionResponse is one answer per question, under the question's name.
type DecisionResponse struct {
	// Model is the model as the provider names the one that answered.
	Model   string
	Answers map[string]Answer
	Usage   Usage
	// Cost is in USD when the provider reports it.
	Cost float64
}

// Decider is the optional interface a provider implements when it serves
// decision models.
type Decider interface {
	Decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error)
}

// decisionTimeout bounds one decision with its retries; a decision model
// answers in well under a second.
const (
	decisionTimeout = 30 * time.Second
	decisionTries   = 3
)

// decisionBackoff is the wait before the first retry; it doubles.
var decisionBackoff = 400 * time.Millisecond

// decisionEndpoint is one hosted decision API. TypeSafe's and OpenRouter's
// take the same body and answer in the same shape, so they share this.
type decisionEndpoint struct {
	url     string
	key     string
	headers map[string]string
}

func (e decisionEndpoint) decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error) {
	if err := req.Validate(); err != nil {
		return DecisionResponse{}, err
	}
	body, err := json.Marshal(struct {
		Model     string              `json:"model"`
		State     json.RawMessage     `json:"state"`
		Questions map[string]Question `json:"questions"`
	}{req.Model, req.State, req.Questions})
	if err != nil {
		return DecisionResponse{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, decisionTimeout)
	defer cancel()
	for try := 0; ; try++ {
		res, again, err := e.post(ctx, body)
		if err == nil {
			return res, res.check(req)
		}
		if !again || try == decisionTries-1 {
			return DecisionResponse{}, err
		}
		select {
		case <-ctx.Done():
			return DecisionResponse{}, ctx.Err()
		case <-time.After(decisionBackoff << try):
		}
	}
}

// post sends the request once; again says the failure is worth another try
// (rate-limited or overloaded, not refused).
func (e decisionEndpoint) post(ctx context.Context, body []byte) (res DecisionResponse, again bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(body))
	if err != nil {
		return res, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.key != "" {
		req.Header.Set("Authorization", "Bearer "+e.key)
	}
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}
	hres, err := http.DefaultClient.Do(req)
	if err != nil {
		return res, ctx.Err() == nil, fmt.Errorf("decide: %w", err)
	}
	defer hres.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(hres.Body, 4<<20))
	if hres.StatusCode != http.StatusOK {
		again = hres.StatusCode == http.StatusTooManyRequests || hres.StatusCode == http.StatusServiceUnavailable || hres.StatusCode == 529
		return res, again, fmt.Errorf("decide: http %d%s", hres.StatusCode, e.reason(raw))
	}
	var w struct {
		Model   string            `json:"model"`
		Answers map[string]Answer `json:"answers"`
		Usage   struct {
			InputTokens  int     `json:"input_tokens"`
			OutputTokens int     `json:"output_tokens"`
			Cost         float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return res, false, fmt.Errorf("decide: unreadable answer: %w", err)
	}
	return DecisionResponse{
		Model: w.Model, Answers: w.Answers, Cost: w.Usage.Cost,
		Usage: Usage{InputTokens: w.Usage.InputTokens, OutputTokens: w.Usage.OutputTokens},
	}, false, nil
}

// reason is the upstream's own words for a refusal, from either API's error
// body ({"error": {"message"}} or {"detail": {"message"}}), without the key:
// an error body can echo it back.
func (e decisionEndpoint) reason(raw []byte) string {
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	msg := ""
	for _, field := range []string{"error", "detail"} {
		switch v := body[field].(type) {
		case string:
			msg = v
		case map[string]any:
			msg, _ = v["message"].(string)
		}
		if msg != "" {
			break
		}
	}
	if msg == "" {
		return ""
	}
	if e.key != "" {
		msg = strings.ReplaceAll(msg, e.key, "••••")
	}
	return ": " + textx.Cap(msg, 300)
}

// check holds the answers to what was asked: every question answered, with
// the type it was asked as. A caller acts on these values without reading
// them, so a missing or mistyped one must not pass as a zero.
func (r DecisionResponse) check(req DecisionRequest) error {
	for name, q := range req.Questions {
		a, ok := r.Answers[name]
		if !ok {
			return fmt.Errorf("decide: no answer to %q", name)
		}
		if a.Type != q.Type {
			return fmt.Errorf("decide: %q was asked as %s and answered as %q", name, q.Type, a.Type)
		}
		switch q.Type {
		case QuestionNoul:
			ok = a.Noul != nil
		case QuestionChoice:
			_, ok = q.Options[a.Choice]
		case QuestionScore:
			ok = a.Score != nil
		}
		if !ok {
			return fmt.Errorf("decide: the answer to %q has no usable %s", name, q.Type)
		}
	}
	return nil
}

// openRouterClient is the OpenAI-compatible client plus what only OpenRouter
// has: decision models, served by its Decisions API and listed apart.
type openRouterClient struct {
	*openAICompatClient
	decisions decisionEndpoint
}

// openRouterDecisions is the Decisions API of an OpenRouter root: it sits
// beside the versioned OpenAI-compatible API (…/api/v1), not under it.
func openRouterDecisions(base string) string {
	base = strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1")
	return base + "/alpha/decisions"
}

func (c *openRouterClient) Decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error) {
	return c.decisions.decide(ctx, req)
}

// openRouterModalities are the output modalities OpenRouter's /models lists
// only when asked, and the kind each one is.
var openRouterModalities = []struct{ modality, kind string }{
	{"decisions", KindDecision},
	{"transcription", KindTranscription},
	{"speech", KindSpeech},
	{"embeddings", KindEmbedding},
}

// ListModels lists the text models and, from one more query per modality,
// the decision, transcription, speech and embedding models. A gateway that
// does not know those queries still lists its text models.
func (c *openRouterClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	list, err := fetchModels(ctx, c.base, c.key)
	if err != nil {
		return nil, err
	}
	// The default listing is the text models (some of which also make audio).
	out := modelInfos(list, KindText)
	type answer struct {
		kind string
		list []listedModel
	}
	answers := make(chan answer, len(openRouterModalities))
	for _, m := range openRouterModalities {
		go func() {
			got, err := fetchModels(ctx, c.base, c.key, "output_modalities="+m.modality)
			if err != nil {
				got = nil
			}
			// Only what says it is of the modality: a gateway that ignores the
			// query answers with its text models again.
			kept := got[:0]
			for _, e := range got {
				for _, o := range e.Architecture.OutputModalities {
					if o == m.modality {
						kept = append(kept, e)
						break
					}
				}
			}
			answers <- answer{m.kind, kept}
		}()
	}
	for range openRouterModalities {
		a := <-answers
		out = append(out, modelInfos(a.list, a.kind)...)
	}
	sortModels(out)
	return out, nil
}
