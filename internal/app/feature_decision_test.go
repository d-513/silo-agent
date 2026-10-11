package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// decisionYAML is the test config with one decision model allowed, plus extra.
func decisionYAML(t *testing.T, extra string) string {
	t.Helper()
	return apptest.DefaultYAML(t.TempDir()) + "decision_models:\n  - dummy/judge\n" + extra
}

// setAuto stores the Bot's auto-approval policy and hands one action to the
// approval model.
func setAuto(t *testing.T, h *apptest.H, botID, policy, conn, action string) {
	t.Helper()
	if _, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{Id: botID, Name: "Auto", AutoApprove: policy})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: botID, Connector: conn, Action: action, Decision: "auto"})); err != nil {
		t.Fatal(err)
	}
}

// gateState is what the gate put in front of a decision model.
type gateState struct {
	Policy string  `json:"policy"`
	Origin string  `json:"origin"`
	Human  *string `json:"human_message"`
	Action struct {
		Name string `json:"name"`
	} `json:"action"`
}

func lastGateState(t *testing.T) gateState {
	t.Helper()
	sent := dummy.Decided()
	if len(sent) == 0 {
		t.Fatal("the decision model was not asked")
	}
	req := sent[len(sent)-1]
	if len(req.Questions) != 2 || req.Questions["allowed"].Type != llm.QuestionNoul || req.Questions["forbidden"].Type != llm.QuestionNoul {
		t.Fatalf("gate questions %+v", req.Questions)
	}
	var st gateState
	if err := json.Unmarshal(req.State, &st); err != nil {
		t.Fatalf("gate state: %v\n%s", err, req.State)
	}
	return st
}

// gateText is the request the gate put in front of a text model.
func gateText(t *testing.T) string {
	t.Helper()
	for _, req := range dummy.Completed() {
		if len(req.System) > 0 && strings.Contains(req.System[0].Text, "auto-approval gate") && len(req.Messages) == 1 {
			return req.Messages[0].Text
		}
	}
	t.Fatal("the text model was not asked")
	return ""
}

func clickFromNowhere(t *testing.T, h *apptest.H, botID string) *v1.ToolRes {
	t.Helper()
	res, err := h.WorkerClient(botID).CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: "desktop", Action: "click", ArgsJson: `{"x":1,"y":2}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

// With a decision model as the auto-approval model the gate asks typed
// questions instead of prompting for a word, and acts only on a sure answer.
func TestAutoApproveWithDecisionModel(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t, apptest.WithYAML(decisionYAML(t, "model_approval: dummy/judge\n")))
	bot := h.CreateBot("Auto")

	setAuto(t, h, bot.GetId(), "Clicks are fine. Test_Approve", "desktop", "click")
	if res := clickFromNowhere(t, h, bot.GetId()); res.GetError() != "" {
		t.Fatalf("a sure yes should allow: %+v", res)
	}
	st := lastGateState(t)
	if st.Policy != "Clicks are fine. Test_Approve" || st.Action.Name != "desktop.click" {
		t.Fatalf("gate state %+v", st)
	}
	// A call from outside any run has no human behind it: the gate is told
	// so, and is not handed a message somebody made up.
	if !strings.HasPrefix(st.Origin, "Unknown") || st.Human != nil {
		t.Fatalf("orphan call origin %q human %v", st.Origin, st.Human)
	}
	for _, req := range dummy.Completed() {
		if len(req.System) > 0 && strings.Contains(req.System[0].Text, "auto-approval gate") {
			t.Fatal("a decision model was configured, yet a text model was prompted")
		}
	}

	setAuto(t, h, bot.GetId(), "Never click. Test_Deny", "desktop", "click")
	if res := clickFromNowhere(t, h, bot.GetId()); res.GetError() != "denied" {
		t.Fatalf("a sure no should deny: %+v", res)
	}

	// Leaning yes is not yes: the human gets the slip.
	setAuto(t, h, bot.GetId(), "Clicks are probably fine. Test_Approve Test_Unsure", "desktop", "click")
	done := make(chan *v1.ToolRes, 1)
	go func() { done <- clickFromNowhere(t, h, bot.GetId()) }()
	ap := h.WaitApproval(bot.GetId())
	if ap.GetConnector() != "desktop" || ap.GetAction() != "click" {
		t.Fatalf("approval %+v", ap)
	}
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	if res := <-done; res.GetError() != "denied" {
		t.Fatalf("the human denied: %+v", res)
	}

	// A decision model that fails asks a human; it never lets the action by.
	setAuto(t, h, bot.GetId(), "Test_Approve Test_Decide_Error", "desktop", "click")
	go func() { done <- clickFromNowhere(t, h, bot.GetId()) }()
	ap = h.WaitApproval(bot.GetId())
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "allow_once"})); err != nil {
		t.Fatal(err)
	}
	if res := <-done; res.GetError() != "" {
		t.Fatalf("the human allowed: %+v", res)
	}
}

// The gate is shown what the Bot asks for and the human's message before it,
// whichever kind of model judges.
func TestAutoApproveSeesTheHumansMessage(t *testing.T) {
	const said = "Test_Gate_Input please post the summary to my feed"
	for kind, extra := range map[string]string{llm.KindDecision: "model_approval: dummy/judge\n", llm.KindText: ""} {
		dummy.Reset()
		dummy.Script("Test_Gate", memCall("feed", `{"text":"The summary."}`), dummy.Turn{Text: "done"})
		h := apptest.New(t, apptest.WithYAML(decisionYAML(t, extra)+apptest.ChatTools))
		bot := h.CreateBot("Auto")
		setAuto(t, h, bot.GetId(), "Feed posts are fine. Test_Approve", "bot", "feed")
		run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), said)
		h.WaitRun(run)
		if posts := listFeed(t, h, bot.GetId()); len(posts) != 1 {
			t.Fatalf("%s: the post was not approved:\n%s", kind, h.RunBody(run))
		}
		if kind == llm.KindDecision {
			st := lastGateState(t)
			if st.Human == nil || *st.Human != said || !strings.HasPrefix(st.Origin, "A conversation: the chat") || st.Action.Name != "bot.feed" {
				t.Fatalf("gate state %+v", st)
			}
			continue
		}
		text := gateText(t)
		for _, want := range []string{"Origin: A conversation: the chat", "<human_message>\n" + said + "\n</human_message>", "Requested action: Post to Feed (bot.feed)"} {
			if !strings.Contains(text, want) {
				t.Fatalf("gate request missing %q:\n%s", want, text)
			}
		}
		if strings.Contains(text, "Feed posts are fine") {
			t.Fatalf("the policy is the system prompt's, not the request's:\n%s", text)
		}
	}
}

// An automation has no human message: the gate is told where the action comes
// from, and the automation's own prompt is not passed off as one.
func TestAutoApproveFromAutomationHasNoHumanMessage(t *testing.T) {
	for kind, extra := range map[string]string{llm.KindDecision: "model_approval: dummy/judge\n", llm.KindText: ""} {
		dummy.Reset()
		dummy.Script("Test_GateAuto", memCall("feed", `{"text":"Nothing found."}`), dummy.Turn{Text: "done"})
		h := apptest.New(t, apptest.WithYAML(decisionYAML(t, extra)+apptest.ChatTools))
		bot := h.CreateBot("Auto")
		setAuto(t, h, bot.GetId(), "Feed posts are fine. Test_Approve", "bot", "feed")
		au := createAutomation(t, h, bot.GetId(), "Nightly check", "Test_GateAuto_Input sweep the inbox", "")
		res, err := h.Client.RunAutomation(h.Ctx(), connect.NewRequest(&v1.RunAutomationRequest{BotId: bot.GetId(), Id: au.GetId()}))
		if err != nil {
			t.Fatal(err)
		}
		h.WaitRun(res.Msg.GetRunId())
		if posts := listFeed(t, h, bot.GetId()); len(posts) != 1 {
			t.Fatalf("%s: the post was not approved:\n%s", kind, h.RunBody(res.Msg.GetRunId()))
		}
		var seen string
		if kind == llm.KindDecision {
			st := lastGateState(t)
			if st.Human != nil {
				t.Fatalf("an automation was given a human message: %q", *st.Human)
			}
			seen = st.Origin
			sent := dummy.Decided()
			if strings.Contains(string(sent[len(sent)-1].State), "sweep the inbox") {
				t.Fatalf("the automation's prompt reached the gate: %s", sent[len(sent)-1].State)
			}
		} else {
			seen = gateText(t)
			if strings.Contains(seen, "human_message") || strings.Contains(seen, "sweep the inbox") {
				t.Fatalf("the automation's prompt reached the gate:\n%s", seen)
			}
		}
		if !strings.Contains(seen, `Automations: the automation "Nightly check"`) || !strings.Contains(seen, "No human message asked for this") {
			t.Fatalf("%s: origin not told: %s", kind, seen)
		}
	}
}

// The operator keeps two allowlists, and each role takes the kind it can use.
func TestDecisionModelSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "silo.yaml")
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)), 0o600); err != nil {
		t.Fatal(err)
	}
	h := apptest.New(t, apptest.WithConfigPath(path))
	set := func(kind string, ids ...string) (*v1.Settings, error) {
		res, err := h.Client.SetModels(h.Ctx(), connect.NewRequest(&v1.SetModelsRequest{Models: ids, Kind: kind}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	put := func(key, value string) error {
		_, err := h.Client.PutSettings(h.Ctx(), connect.NewRequest(&v1.PutSettingsRequest{Fields: map[string]string{key: value}}))
		return err
	}
	invalid := func(what string, err error) {
		t.Helper()
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("%s: want InvalidArgument, got %v", what, err)
		}
	}

	// No decision model yet: nothing can be the default.
	invalid("default with none allowed", put("model_decision", "dummy/judge"))

	st, err := set("decision", "dummy/judge")
	if err != nil {
		t.Fatal(err)
	}
	if got := st.GetDecisionModels(); len(got) != 1 || got[0].GetId() != "dummy/judge" || got[0].GetKind() != "decision" {
		t.Fatalf("decision models %+v", got)
	}
	if got := st.GetModels(); len(got) != 1 || got[0].GetId() != "dummy/echo" || got[0].GetKind() != "text" {
		t.Fatalf("the text allowlist changed: %+v", got)
	}
	kinds := map[string]string{}
	for _, p := range st.GetProviders() {
		kinds[p.GetId()] = strings.Join(p.GetKinds(), ",")
	}
	if kinds["openrouter"] != "text,decision" || kinds["typesafe"] != "decision" || kinds["anthropic"] != "text" {
		t.Fatalf("provider kinds %v", kinds)
	}

	// A model is of one kind, and its provider must have that kind.
	_, err = set("decision", "dummy/judge", "dummy/echo")
	invalid("a text model as a decision model", err)
	_, err = set("decision", "anthropic/claude-opus-5-5")
	invalid("a provider with no decision models", err)
	_, err = set("text", "dummy/echo", "typesafe/jev-latest")
	invalid("a decision-only provider's model as text", err)
	_, err = set("essay", "dummy/echo")
	invalid("an unknown kind", err)

	// Roles: the decider is a decision model, the writers are text models,
	// and the auto-approval model is either.
	if err := put("model_decision", "dummy/judge"); err != nil {
		t.Fatal(err)
	}
	invalid("a text model as the decider", put("model_decision", "dummy/echo"))
	invalid("a decision model as the chat model", put("model", "dummy/judge"))
	invalid("a decision model as the title model", put("model_title", "dummy/judge"))
	invalid("an approval model that is not allowed", put("model_approval", "dummy/unlisted"))
	for _, m := range []string{"dummy/judge", "dummy/echo", ""} {
		if err := put("model_approval", m); err != nil {
			t.Fatalf("approval model %q: %v", m, err)
		}
	}

	// A chat never offers a decision model.
	bot := h.CreateBot("Picker")
	list, err := h.Client.ListModels(h.Ctx(), connect.NewRequest(&v1.ListModelsRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list.Msg.GetModels() {
		if m.GetId() == "dummy/judge" {
			t.Fatal("a decision model is offered as a chat model")
		}
	}
}

// A provider's listing says which of its models are decision models.
func TestListProviderModelsKinds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/models" && r.URL.Query().Get("output_modalities") == "decisions":
			_, _ = w.Write([]byte(`{"data":[{"id":"typesafe/jev-1.13","name":"TypeSafe: Jev 1.13","created":2,"context_length":64000,"architecture":{"output_modalities":["decisions"]}}]}`))
		case r.URL.Path == "/api/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-5.6-luna","created":3,"architecture":{"output_modalities":["text"]}}]}`))
		case r.URL.Path == "/ts/models":
			_, _ = w.Write([]byte(`[{"name":"jev-latest","description":"Alias of jev-1.13.0","release_date":"2026-09-18"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	yaml := strings.Replace(apptest.DefaultYAML(t.TempDir()), "providers:\n",
		"providers:\n  openrouter:\n    api_key: k\n    base_url: "+srv.URL+"/api/v1\n  typesafe:\n    api_key: k\n    base_url: "+srv.URL+"/ts\n", 1)
	h := apptest.New(t, apptest.WithYAML(yaml))
	list := func(provider string) map[string]string {
		t.Helper()
		res, err := h.Client.ListProviderModels(h.Ctx(), connect.NewRequest(&v1.ListProviderModelsRequest{Provider: provider}))
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		out := map[string]string{}
		for _, m := range res.Msg.GetModels() {
			out[m.GetId()] = m.GetKind()
		}
		return out
	}
	if got := list("openrouter"); len(got) != 2 || got["openrouter/openai/gpt-5.6-luna"] != "text" || got["openrouter/typesafe/jev-1.13"] != "decision" {
		t.Fatalf("openrouter %v", got)
	}
	if got := list("typesafe"); len(got) != 1 || got["typesafe/jev-latest"] != "decision" {
		t.Fatalf("typesafe %v", got)
	}
}

// The Bot's decide tool answers typed questions from Python, is told about in
// the prompt, and is there only while the operator has a decision model.
func TestDecideTool(t *testing.T) {
	dummy.Reset()
	args := `{"state":{"mail":"You won! Test_Yes_junk"},"questions":{"junk":{"type":"noul","instructions":"Is this unwanted mail?"}}}`
	call := func(h *apptest.H, botID string) *v1.ToolRes {
		t.Helper()
		res, err := h.WorkerClient(botID).CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{Connector: "model", Action: "decide", ArgsJson: args}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg
	}
	prompt := func(h *apptest.H, botID string) string {
		t.Helper()
		run, _ := h.Send(botID, h.FirstChat(botID), "Test_Decide_Input")
		h.WaitRun(run)
		sent := dummy.Streamed()
		var b strings.Builder
		for _, blk := range sent[len(sent)-1].System {
			b.WriteString(blk.Text)
		}
		return b.String()
	}

	h := apptest.New(t, apptest.WithYAML(decisionYAML(t, "")))
	bot := h.CreateBot("Decider")
	res := call(h, bot.GetId())
	if res.GetError() != "" {
		t.Fatalf("decide: %s", res.GetError())
	}
	var got struct {
		Model   string                `json:"model"`
		Answers map[string]llm.Answer `json:"answers"`
	}
	if err := json.Unmarshal([]byte(res.GetResultJson()), &got); err != nil {
		t.Fatalf("result %q: %v", res.GetResultJson(), err)
	}
	if got.Model != "dummy/judge" || got.Answers["junk"].Noul == nil || *got.Answers["junk"].Noul < 0.9 {
		t.Fatalf("result %s", res.GetResultJson())
	}
	if p := prompt(h, bot.GetId()); !strings.Contains(p, "## Decisions") || !strings.Contains(p, "sr.decide(") || !strings.Contains(p, "`dummy/judge`") {
		t.Fatal("the prompt does not tell the Bot about decide")
	}

	// The owner can still take the tool away with a rule.
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: bot.GetId(), Connector: "model", Action: "decide", Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	if res := call(h, bot.GetId()); res.GetError() != "denied" {
		t.Fatalf("denied by rule: %+v", res)
	}

	// No decision model allowed: no tool, and no word of it in the prompt.
	dummy.Reset()
	plain := apptest.New(t)
	bot = plain.CreateBot("Plain")
	if res := call(plain, bot.GetId()); res.GetError() == "" {
		t.Fatalf("decide with no decision model: %s", res.GetResultJson())
	}
	if p := prompt(plain, bot.GetId()); strings.Contains(p, "## Decisions") || strings.Contains(p, "sr.decide(") {
		t.Fatal("the prompt offers decide with no decision model")
	}
}

// decide is also one of the model's own tools: a judgment or two mid-turn
// should not cost a Python cell. The schema says how questions are shaped,
// the arguments reach the decision model as the model wrote them, and the
// typed answers come back as the tool result.
func TestDecideIsAChatTool(t *testing.T) {
	offered := func() (llm.Tool, bool) {
		sent := dummy.Streamed()
		for _, tl := range sent[len(sent)-1].Tools {
			if tl.Name == "decide" {
				return tl, true
			}
		}
		return llm.Tool{}, false
	}

	dummy.Reset()
	dummy.Script("Test_ChatDecide",
		memCall("decide", `{"state":{"mail":"You won a prize! Test_Yes_junk Test_Pick_spam"},"questions":{`+
			`"junk":{"type":"noul","instructions":"Is `+"`mail`"+` unwanted?"},`+
			`"kind":{"type":"choice","instructions":"What is it?","criteria":{"spam":"junk mail","personal":"from a person"}},`+
			`"tone":{"type":"score","instructions":"How pushy?","criteria":["calm","pushy"]}}}`),
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t, apptest.WithYAML(decisionYAML(t, "")))
	bot := h.CreateBot("Decider")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_ChatDecide_Input is this spam?")
	var result string
	for _, ev := range h.WaitRun(run) {
		if ev.Kind == "tool_result" {
			result = ev.Body
		}
	}
	var got struct {
		Model   string                `json:"model"`
		Answers map[string]llm.Answer `json:"answers"`
	}
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatalf("tool result %q: %v\n%s", result, err, h.RunBody(run))
	}
	if got.Model != "dummy/judge" || got.Answers["junk"].Noul == nil || *got.Answers["junk"].Noul < 0.9 || got.Answers["kind"].Choice != "spam" || got.Answers["tone"].Score == nil {
		t.Fatalf("tool result %s", result)
	}
	// What the model wrote is what the decision model was asked.
	asked := dummy.Decided()
	if len(asked) != 1 || !strings.Contains(string(asked[0].State), "You won a prize!") || len(asked[0].Questions) != 3 ||
		asked[0].Questions["kind"].Options["personal"] != "from a person" || len(asked[0].Questions["tone"].Levels) != 2 {
		t.Fatalf("asked %+v", asked)
	}

	tl, ok := offered()
	if !ok {
		t.Fatal("decide is not in the model's tool list")
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties struct {
			State     map[string]any `json:"state"`
			Questions struct {
				Type  string `json:"type"`
				Named struct {
					Required   []string `json:"required"`
					Properties struct {
						Type struct {
							Enum []string `json:"enum"`
						} `json:"type"`
						Instructions map[string]any `json:"instructions"`
						Criteria     map[string]any `json:"criteria"`
					} `json:"properties"`
				} `json:"additionalProperties"`
			} `json:"questions"`
			Model map[string]any `json:"model"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tl.Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	if strings.Join(schema.Required, ",") != "state,questions" || schema.Properties.State == nil || schema.Properties.Model == nil {
		t.Fatalf("schema %s", tl.Parameters)
	}
	q := schema.Properties.Questions
	if q.Type != "object" || strings.Join(q.Named.Properties.Type.Enum, ",") != "noul,choice,score" ||
		strings.Join(q.Named.Required, ",") != "type,instructions" || q.Named.Properties.Criteria["description"] == nil {
		t.Fatalf("questions schema %s", tl.Parameters)
	}
	sent := dummy.Streamed()
	var prompt strings.Builder
	for _, blk := range sent[len(sent)-1].System {
		prompt.WriteString(blk.Text)
	}
	if !strings.Contains(prompt.String(), "`decide` tool") {
		t.Fatal("the prompt does not say decide is a tool the model can call")
	}

	// With no decision model allowed it is not offered at all.
	dummy.Reset()
	plain := apptest.New(t)
	bot = plain.CreateBot("Plain")
	run, _ = plain.Send(bot.GetId(), plain.FirstChat(bot.GetId()), "Test_NoDecide_Input")
	plain.WaitRun(run)
	if _, ok := offered(); ok {
		t.Fatal("decide is offered with no decision model allowed")
	}
}
