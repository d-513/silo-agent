package app_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/builtin/email/emailtest"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// offeredTools runs one scripted turn and returns the tool list the model was
// sent, in order. The scripted model answers the first token in a conversation,
// so every turn here gets a chat of its own.
func offeredTools(t *testing.T, h *apptest.H, botID, token string) []llm.Tool {
	t.Helper()
	before := len(dummy.Streamed())
	runID, _ := h.Send(botID, newChat(t, h, botID), token+"_Input")
	h.WaitRun(runID)
	reqs := dummy.Streamed()
	if len(reqs) <= before {
		t.Fatal("the model was never called")
	}
	return reqs[before].Tools
}

func toolNamed(tools []llm.Tool, name string) (int, *llm.Tool) {
	for i := range tools {
		if tools[i].Name == name {
			return i, &tools[i]
		}
	}
	return -1, nil
}

// TestConnectorExposedToCompletions turns on "Expose completions directly" for
// an MCP connector: its tools join the model's tool list as <slug>__<action>,
// run through the same rule as the Python path, and leave again when the
// switch goes off.
func TestConnectorExposedToCompletions(t *testing.T) {
	mcpSrv := echoHTTPServer(t)
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Direct")
	id := bot.GetId()
	created, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: id, Name: "Echo", Transport: "http", HttpUrl: mcpSrv.URL, Auth: "none", DefaultMode: "allow",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if created.Msg.GetConnector().GetExposeCompletion() {
		t.Fatal("a connector is Python-only until the switch is turned on")
	}
	h.WaitConnector(id, "Echo")
	connID := created.Msg.GetConnector().GetId()

	if i, _ := toolNamed(offeredTools(t, h, id, "Test_95"), "echo__echo"); i >= 0 {
		t.Fatal("echo__echo offered before the switch was on")
	}

	on := true
	upd, err := h.Client.UpdateConnector(h.Ctx(), connect.NewRequest(&v1.UpdateConnectorRequest{Id: connID, ExposeCompletion: &on}))
	if err != nil || !upd.Msg.GetExposeCompletion() {
		t.Fatalf("turn on: %v %+v", err, upd)
	}
	// An update that does not mention the switch leaves it alone.
	if upd, err = h.Client.UpdateConnector(h.Ctx(), connect.NewRequest(&v1.UpdateConnectorRequest{Id: connID, Description: "echoes"})); err != nil || !upd.Msg.GetExposeCompletion() {
		t.Fatalf("unrelated update dropped the switch: %v %+v", err, upd)
	}
	h.WaitConnector(id, "Echo")

	dummy.Script("Test_96",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "echo__echo", Arguments: `{"q":"direct-hello"}`}}},
		dummy.Turn{Text: "done"},
	)
	before := len(dummy.Streamed())
	runID, _ := h.Send(id, newChat(t, h, id), "Test_96_Input")
	h.WaitRun(runID)
	tools := dummy.Streamed()[before].Tools
	i, tl := toolNamed(tools, "echo__echo")
	if tl == nil {
		t.Fatalf("echo__echo not offered: %d tools", len(tools))
	}
	if !strings.Contains(tl.Description, "Echo connector") || !strings.Contains(tl.Description, "echo the q argument") {
		t.Fatalf("description %q", tl.Description)
	}
	if !strings.Contains(string(tl.Parameters), `"q"`) || !strings.Contains(string(tl.Parameters), `"type":"object"`) {
		t.Fatalf("parameters %s", tl.Parameters)
	}
	// Connector tools follow the built-ins, so the stable list stays a prefix.
	if j, _ := toolNamed(tools, "terminal"); j != 0 || i != len(tools)-1 {
		t.Fatalf("order: terminal at %d, echo__echo at %d of %d", j, i, len(tools))
	}
	if got := toolResults(h, runID)["echo__echo"]; got != "direct-hello" {
		t.Fatalf("direct call result %q", got)
	}

	// One rule covers both ways in.
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: id, Connector: "echo", Action: "echo", Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	dummy.Script("Test_97",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "echo__echo", Arguments: `{"q":"nope"}`}}},
		dummy.Turn{Text: "done"},
	)
	runID, _ = h.Send(id, newChat(t, h, id), "Test_97_Input")
	h.WaitRun(runID)
	if got := toolResults(h, runID)["echo__echo"]; !strings.Contains(got, "denied") {
		t.Fatalf("a deny rule must stop the direct call: %q", got)
	}
	if res := pyCall(t, h, id, "", "echo", "echo", `{"q":"nope"}`); !strings.Contains(res.GetError(), "denied") {
		t.Fatalf("and the Python call: %+v", res)
	}

	// The prompt says which connectors are also direct tools.
	sys := ""
	for _, b := range dummy.Streamed()[before].System {
		sys += b.Text
	}
	if !strings.Contains(sys, "echo__") {
		t.Fatalf("the Connectors section does not mention the direct tools")
	}

	off := false
	if _, err := h.Client.UpdateConnector(h.Ctx(), connect.NewRequest(&v1.UpdateConnectorRequest{Id: connID, ExposeCompletion: &off})); err != nil {
		t.Fatal(err)
	}
	h.WaitConnector(id, "Echo")
	if i, _ := toolNamed(offeredTools(t, h, id, "Test_98"), "echo__echo"); i >= 0 {
		t.Fatal("echo__echo still offered after the switch went off")
	}
}

// A library preset carries the switch as what a new copy starts with, and the
// add form can override it either way.
func TestExposeCompletionCopiedFromLibrary(t *testing.T) {
	mcpSrv := echoHTTPServer(t)
	h := apptest.New(t)
	bot := h.CreateBot("DirectLib")
	lib, err := h.Client.CreateConnector(h.Ctx(), connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "Echo", Transport: "http", HttpUrl: mcpSrv.URL, Auth: "none", DefaultMode: "allow", ExposeCompletion: true,
	}))
	if err != nil || !lib.Msg.GetExposeCompletion() {
		t.Fatalf("library preset: %v %+v", err, lib)
	}
	att, err := h.Client.AttachConnector(h.Ctx(), connect.NewRequest(&v1.AttachConnectorRequest{BotId: bot.GetId(), ConnectorId: lib.Msg.GetId()}))
	if err != nil || !att.Msg.GetConnector().GetExposeCompletion() {
		t.Fatalf("attach should copy the switch: %v %+v", err, att)
	}
	inherit, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), SourceId: lib.Msg.GetId(), Name: "Echo",
	}))
	if err != nil || !inherit.Msg.GetConnector().GetExposeCompletion() {
		t.Fatalf("the add form without a choice inherits: %v %+v", err, inherit)
	}
	off := false
	plain, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), SourceId: lib.Msg.GetId(), Name: "Echo", ExposeCompletion: &off,
	}))
	if err != nil || plain.Msg.GetConnector().GetExposeCompletion() {
		t.Fatalf("the add form can turn it off: %v %+v", err, plain)
	}
}

// A built-in connector's tools are exposed the same way, with each action's
// own default mode.
func TestBuiltinConnectorExposedToCompletions(t *testing.T) {
	mail := emailtest.Start(t)
	mail.Deliver(t, "INBOX", "From: Alice <alice@example.org>\r\nTo: me@example.com\r\nSubject: Hello\r\n\r\nHi there\r\n")
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("DirectMail")
	on := true
	if _, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), SourceId: libraryEmail(t, h).GetId(), Config: mail.Config(), ExposeCompletion: &on,
	})); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, h, bot.GetId(), "authorized")
	dummy.Script("Test_99",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "email__search", Arguments: `{}`}}},
		dummy.Turn{Text: "done"},
	)
	before := len(dummy.Streamed())
	runID, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_99_Input")
	h.WaitRun(runID)
	tools := dummy.Streamed()[before].Tools
	if i, _ := toolNamed(tools, "email__send"); i < 0 {
		t.Fatalf("email__send not offered")
	}
	if got := toolResults(h, runID)["email__search"]; !strings.Contains(got, "Hello") {
		t.Fatalf("direct search result %q", got)
	}
}
