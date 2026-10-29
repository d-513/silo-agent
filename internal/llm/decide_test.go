package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// decisionAnswer is the reply both hosted decision APIs give to the request in
// decisionAsk: one typed answer per question, under the caller's own keys.
const decisionAnswer = `{
	"id": "gen-dec-1",
	"model": "typesafe/jev-1.13-20260917",
	"answers": {
		"is_bug": {"type": "noul", "noul": 0.96},
		"team": {"type": "choice", "choice": "payments", "confidence": 0.67,
			"probabilities": {"payments": 0.78, "frontend": 0.22, "account": 0}},
		"urgency": {"type": "score", "score": 1.99, "confidence": 0.99,
			"probabilities": {"0": 0, "1": 0, "2": 1},
			"legend": {"0": "Can wait", "1": "This week", "2": "Blocking"}}
	},
	"usage": {"input_tokens": 476, "output_tokens": 70, "cost": 0.000019992}
}`

func decisionAsk(model string) DecisionRequest {
	return DecisionRequest{
		Model: model,
		State: json.RawMessage(`{"ticket":"Checkout is blank after I click Pay."}`),
		Questions: map[string]Question{
			"is_bug": {Type: QuestionNoul, Instructions: "Is this a defect?", True: "Broken behaviour.", False: "A question."},
			"team": {Type: QuestionChoice, Instructions: "Which team?", Options: map[string]string{
				"payments": "Checkout or billing.", "frontend": "Rendering.", "account": "Login.",
			}},
			"urgency": {Type: QuestionScore, Instructions: "How urgent?", Levels: []string{"Can wait", "This week", "Blocking"}},
		},
	}
}

// decisionServer answers one path with decisionAnswer and keeps what it was sent.
func decisionServer(t *testing.T, path string, got *map[string]any, auth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		*auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, got); err != nil {
			t.Errorf("request body: %v", err)
		}
		_, _ = w.Write([]byte(decisionAnswer))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func checkDecision(t *testing.T, res DecisionResponse, sent map[string]any, wantModel string) {
	t.Helper()
	if sent["model"] != wantModel {
		t.Fatalf("model sent %v, want %s", sent["model"], wantModel)
	}
	if !reflect.DeepEqual(sent["state"], map[string]any{"ticket": "Checkout is blank after I click Pay."}) {
		t.Fatalf("state sent %v", sent["state"])
	}
	wantQs := map[string]any{
		"is_bug": map[string]any{"type": "noul", "instructions": "Is this a defect?",
			"criteria": map[string]any{"true": "Broken behaviour.", "false": "A question."}},
		"team": map[string]any{"type": "choice", "instructions": "Which team?",
			"criteria": map[string]any{"payments": "Checkout or billing.", "frontend": "Rendering.", "account": "Login."}},
		"urgency": map[string]any{"type": "score", "instructions": "How urgent?",
			"criteria": []any{"Can wait", "This week", "Blocking"}},
	}
	if !reflect.DeepEqual(sent["questions"], wantQs) {
		t.Fatalf("questions sent\n%v\nwant\n%v", sent["questions"], wantQs)
	}
	if res.Usage.InputTokens != 476 || res.Usage.OutputTokens != 70 || res.Cost != 0.000019992 {
		t.Fatalf("usage %+v cost %v", res.Usage, res.Cost)
	}
	bug, team, urgency := res.Answers["is_bug"], res.Answers["team"], res.Answers["urgency"]
	if bug.Noul == nil || *bug.Noul != 0.96 || bug.Confidence != nil {
		t.Fatalf("noul answer %+v", bug)
	}
	if team.Choice != "payments" || team.Confidence == nil || *team.Confidence != 0.67 || team.Probabilities["payments"] != 0.78 {
		t.Fatalf("choice answer %+v", team)
	}
	if urgency.Score == nil || *urgency.Score != 1.99 || urgency.Legend["2"] != "Blocking" {
		t.Fatalf("score answer %+v", urgency)
	}
}

// OpenRouter serves every decision model through its Decisions API, which
// sits beside the OpenAI-compatible root, not under it.
func TestOpenRouterDecide(t *testing.T) {
	var sent map[string]any
	var auth string
	srv := decisionServer(t, "/api/alpha/decisions", &sent, &auth)
	c, err := New(OpenRouter, Settings{"api_key": "k", "base_url": srv.URL + "/api/v1"})
	if err != nil {
		t.Fatal(err)
	}
	d, ok := c.(Decider)
	if !ok {
		t.Fatal("openrouter client is not a Decider")
	}
	res, err := d.Decide(context.Background(), decisionAsk("typesafe/jev-1.13"))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer k" {
		t.Fatalf("auth %q", auth)
	}
	checkDecision(t, res, sent, "typesafe/jev-1.13")
}

// TypeSafe's own API takes the same request at /v1/systemone.
func TestTypeSafeDecide(t *testing.T) {
	var sent map[string]any
	var auth string
	srv := decisionServer(t, "/v1/systemone", &sent, &auth)
	c, err := New(TypeSafe, Settings{"api_key": "ts", "base_url": srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.(Decider).Decide(context.Background(), decisionAsk("jev-latest"))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer ts" {
		t.Fatalf("auth %q", auth)
	}
	checkDecision(t, res, sent, "jev-latest")

	// It has no text models: a completion says so instead of calling out.
	if _, err := c.Complete(context.Background(), Request{Model: "jev-latest"}); err == nil || !strings.Contains(err.Error(), "decision") {
		t.Fatalf("complete on a decision-only provider: %v", err)
	}
	if _, err := c.Stream(context.Background(), Request{Model: "jev-latest"}); err == nil {
		t.Fatal("stream on a decision-only provider should fail")
	}
}

// A provider serves a kind exactly when its client has the kind's
// interface, for every kind in the registry.
func TestProviderKinds(t *testing.T) {
	for _, d := range Descriptors() {
		c, err := New(d.ID, Settings{"api_key": "k", "base_url": "http://probe.invalid"})
		if err != nil {
			t.Fatalf("%s: %v", d.ID, err)
		}
		for _, k := range Kinds {
			if k.ID == KindText {
				continue // every client streams; a decision-only one refuses to
			}
			if Implements(c, k.ID) != d.Serves(k.ID) {
				t.Errorf("%s: implements %s=%v but descriptor serves it=%v", d.ID, k.ID, Implements(c, k.ID), d.Serves(k.ID))
			}
		}
	}
	for id, want := range map[string][]bool{
		OpenRouter: {true, true}, OpenAI: {true, false}, Anthropic: {true, false}, Local: {true, false}, TypeSafe: {false, true},
	} {
		d, _ := Lookup(id)
		if d.Serves(KindText) != want[0] || d.Serves(KindDecision) != want[1] {
			t.Errorf("%s serves text=%v decision=%v", id, d.Serves(KindText), d.Serves(KindDecision))
		}
	}
	// A kind a provider says is live is one it serves, through the live
	// interface.
	for _, d := range Descriptors() {
		c, _ := New(d.ID, Settings{"api_key": "k", "base_url": "http://probe.invalid"})
		for _, k := range d.Live {
			if !d.Serves(k) {
				t.Errorf("%s: live %s but does not serve it", d.ID, k)
			}
			_, lt := c.(LiveTranscriber)
			_, ls := c.(LiveSpeaker)
			if k == KindTranscription && !lt || k == KindSpeech && !ls || k != KindTranscription && k != KindSpeech {
				t.Errorf("%s: live %s without its live interface", d.ID, k)
			}
		}
	}
	for _, id := range []string{OpenRouter, OpenAI, Local} {
		d, _ := Lookup(id)
		if !d.Serves(KindSpeech) || !d.Serves(KindTranscription) || !d.Serves(KindEmbedding) {
			t.Errorf("%s, an OpenAI-compatible provider, should serve speech, transcription and embeddings", id)
		}
	}
}

// A refusal names the status and the upstream's reason, never the key, in
// either provider's error shape.
func TestDecideErrors(t *testing.T) {
	body, status := "", 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	or, _ := New(OpenRouter, Settings{"api_key": "sk-secret", "base_url": srv.URL})
	ts, _ := New(TypeSafe, Settings{"api_key": "sk-secret", "base_url": srv.URL})

	status, body = http.StatusPaymentRequired, `{"error":{"code":402,"message":"Insufficient credits for sk-secret"}}`
	_, err := or.(Decider).Decide(context.Background(), decisionAsk("typesafe/jev-1.13"))
	if err == nil || !strings.Contains(err.Error(), "402") || !strings.Contains(err.Error(), "Insufficient credits") {
		t.Fatalf("openrouter error: %v", err)
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("error leaks the key: %v", err)
	}

	status, body = http.StatusUnprocessableEntity, `{"detail":{"error_type":"validation_error","message":"criteria needs 2 levels"}}`
	_, err = ts.(Decider).Decide(context.Background(), decisionAsk("jev-latest"))
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "criteria needs 2 levels") {
		t.Fatalf("typesafe error: %v", err)
	}

	// An answer that is missing, or of another type than asked, is an error:
	// a caller must never act on a question the model did not answer.
	status, body = http.StatusOK, `{"answers":{"is_bug":{"type":"noul","noul":0.5},"team":{"type":"noul","noul":0.5}}}`
	if _, err = or.(Decider).Decide(context.Background(), decisionAsk("typesafe/jev-1.13")); err == nil {
		t.Fatal("a wrong-typed answer should fail")
	}
}

// An overloaded or rate-limited upstream is asked again; a refusal is not.
func TestDecideRetries(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(529)
			return
		}
		_, _ = w.Write([]byte(decisionAnswer))
	}))
	defer srv.Close()
	old := decisionBackoff
	decisionBackoff = 0
	defer func() { decisionBackoff = old }()
	c, _ := New(TypeSafe, Settings{"api_key": "k", "base_url": srv.URL})
	if _, err := c.(Decider).Decide(context.Background(), decisionAsk("jev-latest")); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls %d, want 2", calls)
	}
}

func TestQuestionValidate(t *testing.T) {
	ok := decisionAsk("m")
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	bad := func(name string, mut func(*DecisionRequest), want string) {
		t.Helper()
		r := decisionAsk("m")
		mut(&r)
		err := r.Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", name, err, want)
		}
	}
	bad("no questions", func(r *DecisionRequest) { r.Questions = nil }, "at least one question")
	bad("no state", func(r *DecisionRequest) { r.State = nil }, "state")
	bad("null state", func(r *DecisionRequest) { r.State = json.RawMessage(`null`) }, "state")
	bad("number state", func(r *DecisionRequest) { r.State = json.RawMessage(`4`) }, "state")
	bad("type", func(r *DecisionRequest) { r.Questions["x"] = Question{Type: "essay", Instructions: "?"} }, `"x"`)
	bad("instructions", func(r *DecisionRequest) { r.Questions["x"] = Question{Type: QuestionNoul} }, "instructions")
	bad("one option", func(r *DecisionRequest) {
		r.Questions["x"] = Question{Type: QuestionChoice, Instructions: "?", Options: map[string]string{"a": ""}}
	}, "2")
	bad("one level", func(r *DecisionRequest) {
		r.Questions["x"] = Question{Type: QuestionScore, Instructions: "?", Levels: []string{"a"}}
	}, "2 to 10")
}

// Questions read from the shape the APIs (and the Bot's decide tool) use.
func TestParseQuestions(t *testing.T) {
	qs, err := ParseQuestions(json.RawMessage(`{
		"spam": {"type": "noul", "instructions": "Is it spam?"},
		"lang": {"type": "choice", "instructions": "Which language?", "criteria": {"en": "English", "pl": "Polish"}},
		"tone": {"type": "score", "instructions": "How polite?", "criteria": ["rude", "neutral", "polite"]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Question{
		"spam": {Type: QuestionNoul, Instructions: "Is it spam?"},
		"lang": {Type: QuestionChoice, Instructions: "Which language?", Options: map[string]string{"en": "English", "pl": "Polish"}},
		"tone": {Type: QuestionScore, Instructions: "How polite?", Levels: []string{"rude", "neutral", "polite"}},
	}
	if !reflect.DeepEqual(qs, want) {
		t.Fatalf("parsed %+v", qs)
	}
	// A choice may list bare option names.
	qs, err = ParseQuestions(json.RawMessage(`{"lang": {"type": "choice", "instructions": "?", "criteria": ["en", "pl"]}}`))
	if err != nil || !reflect.DeepEqual(qs["lang"].Options, map[string]string{"en": "", "pl": ""}) {
		t.Fatalf("list of options: %+v %v", qs, err)
	}
	for _, raw := range []string{`[]`, `{"q": 4}`, `{"q": {"type": "score", "instructions": "?", "criteria": {"a": "b"}}}`} {
		if _, err := ParseQuestions(json.RawMessage(raw)); err == nil {
			t.Errorf("%s should not parse", raw)
		}
	}
}

// OpenRouter's listing says which models are decision models: they are a
// second query, told apart by their output modality.
func TestOpenRouterListsKinds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("output_modalities") == "decisions" {
			_, _ = w.Write([]byte(`{"data":[
				{"id":"typesafe/jev-1.13","name":"TypeSafe: Jev 1.13","created":200,"context_length":64000,"architecture":{"output_modalities":["decisions"]}},
				{"id":"openai/gpt-5.6-luna","name":"stray text model","created":300,"architecture":{"output_modalities":["text"]}}
			]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-5.6-luna","name":"OpenAI: GPT-5.6 Luna","created":300,"context_length":400000,"architecture":{"output_modalities":["text"]}}]}`))
	}))
	defer srv.Close()
	c, _ := New(OpenRouter, Settings{"api_key": "k", "base_url": srv.URL})
	got, err := c.(ModelLister).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("listed %+v", got)
	}
	if got[0].ID != "openai/gpt-5.6-luna" || got[0].Kind != KindText {
		t.Fatalf("first %+v", got[0])
	}
	if got[1].ID != "typesafe/jev-1.13" || got[1].Kind != KindDecision || got[1].ContextWindow != 64000 {
		t.Fatalf("second %+v", got[1])
	}
}

// A gateway that does not know the decisions query still lists its text models.
func TestOpenRouterListSurvivesNoDecisions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"a/b"}]}`))
	}))
	defer srv.Close()
	c, _ := New(OpenRouter, Settings{"api_key": "k", "base_url": srv.URL})
	got, err := c.(ModelLister).ListModels(context.Background())
	if err != nil || len(got) != 1 || got[0].Kind != KindText {
		t.Fatalf("listed %+v, %v", got, err)
	}
}

func TestTypeSafeListModels(t *testing.T) {
	for _, body := range []string{
		`[{"name":"jev-latest","description":"Alias of jev-1.13.0","release_date":"2026-09-18"},{"name":"jev-1.13.0","description":"Jev 1.13","release_date":"2026-09-18"}]`,
		`{"data":[{"id":"jev-latest","description":"Alias of jev-1.13.0"},{"id":"jev-1.13.0","description":"Jev 1.13"}]}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer ts" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(body))
		}))
		c, _ := New(TypeSafe, Settings{"api_key": "ts", "base_url": srv.URL + "/v1"})
		got, err := c.(ModelLister).ListModels(context.Background())
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"jev-1.13.0", "jev-latest"}; !reflect.DeepEqual(ids(got), want) {
			t.Fatalf("listed %v, want %v", ids(got), want)
		}
		for _, m := range got {
			if m.Kind != KindDecision {
				t.Fatalf("%s kind %q", m.ID, m.Kind)
			}
		}
	}
}
