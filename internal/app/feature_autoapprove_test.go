package app_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
)

// setAutoPolicy stores the Bot's auto-approval policy and routes desktop.click
// through the approval model.
func setAutoPolicy(t *testing.T, h *apptest.H, botID, policy string) {
	t.Helper()
	if _, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{
		Id: botID, Name: "Auto", AutoApprove: policy,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: botID, Connector: "desktop", Action: "click", Decision: "auto",
	})); err != nil {
		t.Fatal(err)
	}
}

func TestAutoApproveRuleAllows(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Auto")
	setAutoPolicy(t, h, bot.GetId(), "Auto-approve clicks. Test_Approve")

	wc := h.WorkerClient(bot.GetId())
	res, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: "desktop", Action: "click", ArgsJson: `{"x":1,"y":2}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg.GetError() != "" || !strings.Contains(res.Msg.GetResultJson(), "ok") {
		t.Fatalf("auto-approve should have allowed: %+v", res.Msg)
	}
}

func TestAutoApproveRuleDenies(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Auto")
	setAutoPolicy(t, h, bot.GetId(), "Never click. Test_Deny")

	wc := h.WorkerClient(bot.GetId())
	res, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: "desktop", Action: "click", ArgsJson: `{"x":1,"y":2}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg.GetError() != "denied" {
		t.Fatalf("auto-approve should have denied: %+v", res.Msg)
	}
}

func TestDebugLogsModelCalls(t *testing.T) {
	dir := t.TempDir()
	yaml := fmt.Sprintf(`http_addr: ":0"
data_dir: %q
cp_url: http://127.0.0.1:0
model: dummy/echo
model_title: dummy/echo
models:
  - dummy/echo
providers:
  dummy:
    api_key: test
search:
  engine: builtin
debug: true
`, dir)
	h := apptest.New(t, apptest.WithYAML(yaml))
	bot := h.CreateBot("Debug")
	chat := h.FirstChat(bot.GetId())
	run, _ := h.Send(bot.GetId(), chat, "Test_Debug_Input")
	h.WaitRun(run)

	res, err := h.Client.ListLLMLogs(h.Ctx(), connect.NewRequest(&v1.ListLLMLogsRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Msg.GetEnabled() {
		t.Fatal("debug flag not reported")
	}
	var sawChat bool
	marker := regexp.MustCompile(`\[\w{3}, \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [+-]\d{2}:\d{2} \S+\] Test_Debug_Input`)
	for _, l := range res.Msg.GetLogs() {
		if l.GetLabel() == "chat" && strings.Contains(l.GetRequest(), "Test_Debug_Input") {
			sawChat = true
			if !strings.Contains(l.GetResponse(), "Test_Debug_Output") {
				t.Fatalf("response not captured: %q", l.GetResponse())
			}
			if !marker.MatchString(l.GetRequest()) {
				t.Fatalf("chat request missing the date/time and zone marker:\n%s", l.GetRequest())
			}
		}
	}
	if !sawChat {
		t.Fatalf("no chat log captured: %+v", res.Msg.GetLogs())
	}
}

// ruleOf reads the decision stored for one action.
func ruleOf(t *testing.T, h *apptest.H, botID, conn, action string) string {
	t.Helper()
	var rule db.Rule
	h.DB.Where("bot_id = ? AND connector = ? AND action = ?", botID, conn, action).Limit(1).Find(&rule)
	return rule.Decision
}

// When the approval model will not decide, the human is asked about that one
// call. The approval says so, and voting on it never replaces the Auto rule:
// "always allow" from a client that still offers it allows this call only.
func TestAutoApproveUnsureAsksWithoutChangingTheRule(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Auto")
	setAutoPolicy(t, h, bot.GetId(), "Use your judgement.")
	click := func() chan *v1.ToolRes {
		done := make(chan *v1.ToolRes, 1)
		go func() {
			res, err := h.WorkerClient(bot.GetId()).CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
				Connector: "desktop", Action: "click", ArgsJson: `{"x":1,"y":2}`,
			}))
			if err != nil {
				t.Error(err)
				done <- &v1.ToolRes{}
				return
			}
			done <- res.Msg
		}()
		return done
	}

	done := click()
	ap := h.WaitApproval(bot.GetId())
	if !ap.GetAuto() {
		t.Fatalf("an approval the Auto rule fell back to must say so: %+v", ap)
	}
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "always"})); err != nil {
		t.Fatal(err)
	}
	if res := <-done; res.GetError() != "" {
		t.Fatalf("the human allowed: %+v", res)
	}
	if got := ruleOf(t, h, bot.GetId(), "desktop", "click"); got != "auto" {
		t.Fatalf("the rule must stay auto, reads %q", got)
	}

	// A plain Ask rule is the owner's to settle from the slip.
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "desktop", Action: "click", Decision: "ask",
	})); err != nil {
		t.Fatal(err)
	}
	done = click()
	ap = h.WaitApproval(bot.GetId())
	if ap.GetAuto() {
		t.Fatalf("an Ask rule's approval is not an auto one: %+v", ap)
	}
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "always"})); err != nil {
		t.Fatal(err)
	}
	if res := <-done; res.GetError() != "" {
		t.Fatalf("the human allowed: %+v", res)
	}
	if got := ruleOf(t, h, bot.GetId(), "desktop", "click"); got != "allow" {
		t.Fatalf("always on an Ask rule sets allow, reads %q", got)
	}
}
