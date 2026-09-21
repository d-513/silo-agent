package app_test

import (
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
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
  engine: duckduckgo_scraper
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
	for _, l := range res.Msg.GetLogs() {
		if l.GetLabel() == "chat" && strings.Contains(l.GetRequest(), "Test_Debug_Input") {
			sawChat = true
			if !strings.Contains(l.GetResponse(), "Test_Debug_Output") {
				t.Fatalf("response not captured: %q", l.GetResponse())
			}
		}
	}
	if !sawChat {
		t.Fatalf("no chat log captured: %+v", res.Msg.GetLogs())
	}
}
