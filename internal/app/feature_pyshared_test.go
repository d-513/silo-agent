package app_test

import (
	"encoding/json"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

// pyCall is what silo_runtime.call sends: CallTool(connector, action, args).
func pyCall(t *testing.T, h *apptest.H, botID, runID, conn, action, args string) *v1.ToolRes {
	t.Helper()
	res, err := h.WorkerClient(botID).CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: conn, Action: action, ArgsJson: args, RunId: runID,
	}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

func TestPythonMemoryPaths(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("PyMem")
	id := bot.GetId()

	if res := pyCall(t, h, id, "", "bot", "remember", `{"content":"The user's cat is named Miso"}`); res.GetError() != "" || !strings.Contains(res.GetResultJson(), "remembered") {
		t.Fatalf("remember: %+v", res)
	}
	res := pyCall(t, h, id, "", "bot", "recall", `{"query":"what is the cat called"}`)
	var got struct {
		Memories []struct {
			ID, Content string
		} `json:"memories"`
	}
	if res.GetError() != "" || json.Unmarshal([]byte(res.GetResultJson()), &got) != nil || len(got.Memories) != 1 || !strings.Contains(got.Memories[0].Content, "Miso") {
		t.Fatalf("recall should be structured for Python: %+v", res)
	}
	if res := pyCall(t, h, id, "", "bot", "forget", `{"id":"`+got.Memories[0].ID+`"}`); res.GetError() != "" {
		t.Fatalf("forget: %+v", res)
	}
	var n int64
	h.DB.Model(&db.Memory{}).Where("bot_id = ?", id).Count(&n)
	if n != 0 {
		t.Fatalf("memories %d after forget", n)
	}

	// Same gate as the chat tool.
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: id, Connector: "bot", Action: "remember", Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	if res := pyCall(t, h, id, "", "bot", "remember", `{"content":"x"}`); res.GetError() != "denied" {
		t.Fatalf("deny rule: %+v", res)
	}
}

func TestPythonAutomationAndModelPaths(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("PyAuto")
	id := bot.GetId()
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: id, Connector: "automations", Action: "create", Decision: "allow"})); err != nil {
		t.Fatal(err)
	}
	if res := pyCall(t, h, id, "", "automations", "create", `{"name":"Digest","prompt":"summarize","schedule":"0 9 * * *"}`); res.GetError() != "" || !strings.Contains(res.GetResultJson(), "created") {
		t.Fatalf("create: %+v", res)
	}
	if res := pyCall(t, h, id, "", "automations", "list", `{}`); !strings.Contains(res.GetResultJson(), "Digest") || !strings.Contains(res.GetResultJson(), "Heartbeat") {
		t.Fatalf("list: %+v", res)
	}
	// update asks by default: deny it so the call returns instead of waiting.
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: id, Connector: "automations", Action: "update", Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	if res := pyCall(t, h, id, "", "automations", "update", `{"automation":"Digest","schedule":""}`); res.GetError() != "denied" {
		t.Fatalf("update gate: %+v", res)
	}
	if res := pyCall(t, h, id, "", "automations", "delete", `{"automation":"Digest"}`); res.GetError() != "" {
		t.Fatalf("delete: %+v", res)
	}
	if len(listAutomations(t, h, id)) != 1 {
		t.Fatal("only the Heartbeat should remain")
	}

	res := pyCall(t, h, id, "", "model", "list", `{}`)
	var models struct {
		Current string   `json:"current"`
		Models  []string `json:"models"`
	}
	if res.GetError() != "" || json.Unmarshal([]byte(res.GetResultJson()), &models) != nil || models.Current == "" {
		t.Fatalf("list_models should pass its JSON through: %+v", res)
	}

	// Chat-only actions stay off the Python bus.
	for _, ca := range [][2]string{{"bot", "soul"}, {"bot", "core_memory"}, {"model", "switch"}} {
		if res := pyCall(t, h, id, "", ca[0], ca[1], `{}`); res.GetError() != "unknown connector" {
			t.Fatalf("%s.%s from Python: %+v", ca[0], ca[1], res)
		}
	}
}
