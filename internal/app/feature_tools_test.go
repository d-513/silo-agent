package app_test

import (
	"encoding/json"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/auth"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// toolResults maps tool name to the last result body for a run.
func toolResults(h *apptest.H, runID string) map[string]string {
	out := map[string]string{}
	for _, ev := range h.Events(runID) {
		if ev.Kind == "tool_result" {
			out[ev.Tool] = ev.Body
		}
	}
	return out
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestWorkerFileTools(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_30",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			{Name: "write", Arguments: `{"path":"notes.txt","content":"alpha\nbeta\n"}`},
			{Name: "read", Arguments: `{"path":"notes.txt"}`},
			{Name: "patch", Arguments: `{"path":"notes.txt","old_text":"beta","new_text":"gamma"}`},
			{Name: "read", Arguments: `{"path":"notes.txt"}`},
			{Name: "grep", Arguments: `{"pattern":"gamma"}`},
		}},
		dummy.Turn{Text: "file work done"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Files")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_30_Input")
	h.WaitRun(runID)

	res := toolResults(h, runID)
	if !strings.Contains(res["read"], "alpha") {
		t.Fatalf("read result %q", res["read"])
	}
	if !strings.Contains(res["read"], "gamma") {
		t.Fatalf("read after patch %q", res["read"])
	}
	if !strings.Contains(res["grep"], "gamma") {
		t.Fatalf("grep result %q", res["grep"])
	}
}

func TestPresentFile(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_31",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			{Name: "write", Arguments: `{"path":"report.md","content":"# Report\n"}`},
			{Name: "present", Arguments: `{"path":"report.md"}`},
		}},
		dummy.Turn{Text: "presented"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Present")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_31_Input")
	h.WaitRun(runID)

	res := toolResults(h, runID)
	if !strings.Contains(res["present"], "presented report.md") {
		t.Fatalf("present result %q", res["present"])
	}
}

func TestReadRepeatGuard(t *testing.T) {
	dummy.Reset()
	read := `{"path":"loop.txt"}`
	dummy.Script("Test_40",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			{Name: "write", Arguments: `{"path":"loop.txt","content":"only line\n"}`},
			{Name: "read", Arguments: read},
			{Name: "read", Arguments: read},
			{Name: "read", Arguments: read},
			{Name: "read", Arguments: read},
		}},
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Repeat")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_40_Input")
	h.WaitRun(runID)

	stubbed := 0
	for _, ev := range h.Events(runID) {
		if ev.Kind == "tool_result" && ev.Tool == "read" && strings.Contains(ev.Body, "unchanged") {
			stubbed++
		}
	}
	if stubbed != 2 {
		t.Fatalf("expected 2 stubbed reads, got %d", stubbed)
	}
}

func TestDeleteTool(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_41",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			{Name: "write", Arguments: `{"path":"scratch/a.txt","content":"x"}`},
			{Name: "delete", Arguments: `{"path":"scratch"}`},
			{Name: "read", Arguments: `{"path":"scratch/a.txt"}`},
			{Name: "delete", Arguments: `{"path":""}`},
		}},
		dummy.Turn{Text: "deleted"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Delete")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_41_Input")
	h.WaitRun(runID)

	var dels []string
	for _, ev := range h.Events(runID) {
		if ev.Kind == "tool_result" && ev.Tool == "delete" {
			dels = append(dels, ev.Body)
		}
	}
	if len(dels) != 2 || dels[0] != "ok" || !strings.Contains(dels[1], "path required") {
		t.Fatalf("delete results %q", dels)
	}
	if res := toolResults(h, runID)["read"]; !strings.Contains(res, "error") {
		t.Fatalf("read after delete = %q", res)
	}
}

func TestArtifactSkillCard(t *testing.T) {
	skillMD := "---\nname: my-skill\ndescription: a demo skill\n---\n# My Skill\n"
	dummy.Reset()
	dummy.Script("Test_32",
		dummy.Turn{ToolCalls: []llm.ToolCall{
			{Name: "write", Arguments: `{"path":"my-skill/SKILL.md","content":` + mustJSON(skillMD) + `}`},
			{Name: "artifact", Arguments: `{"path":"my-skill"}`},
		}},
		dummy.Turn{Text: "artifact shown"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Artifact")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_32_Input")
	h.WaitRun(runID)

	var card string
	for _, ev := range h.Events(runID) {
		if ev.Kind == "artifact" {
			card = ev.Body
		}
	}
	if card == "" {
		t.Fatal("no artifact event emitted")
	}
	var info struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(card), &info); err != nil {
		t.Fatalf("artifact json: %v", err)
	}
	if info.Type != "skill" || info.Name != "my-skill" {
		t.Fatalf("artifact info %+v", info)
	}
}

func TestWebSearchTool(t *testing.T) {
	eng := &apptest.FakeSearch{}
	apptest.RegisterFakeSearch("fake", eng)
	yaml := strings.Replace(apptest.DefaultYAML(t.TempDir()),
		"engine: duckduckgo_scraper", "engine: fake", 1)
	dummy.Reset()
	dummy.Script("Test_33",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "web_search", Arguments: `{"query":"golang"}`}}},
		dummy.Turn{Text: "searched"},
	)
	h := apptest.New(t, apptest.WithYAML(yaml))
	bot := h.CreateBot("Search")
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_33_Input")
	h.WaitRun(runID)

	res := toolResults(h, runID)["web_search"]
	if !strings.Contains(res, "example.test") {
		t.Fatalf("web_search result %q", res)
	}
	if len(eng.Calls) != 1 || eng.Calls[0] != "golang" {
		t.Fatalf("engine calls %v", eng.Calls)
	}
}

func TestSkillsToggle(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Skills")
	listed, err := h.Client.ListBotSkills(h.Ctx(), connect.NewRequest(&v1.ListBotSkillsRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Msg.GetSkills()) == 0 {
		t.Fatal("new bot should have default skills")
	}
	target := listed.Msg.GetSkills()[0]
	off, err := h.Client.SetBotSkill(h.Ctx(), connect.NewRequest(&v1.SetBotSkillRequest{
		BotId: bot.GetId(), Kind: target.GetKind(), Name: target.GetName(), Enabled: false,
	}))
	if err != nil || off.Msg.GetEnabled() {
		t.Fatalf("disable skill: %v %+v", err, off)
	}
	again, _ := h.Client.ListBotSkills(h.Ctx(), connect.NewRequest(&v1.ListBotSkillsRequest{BotId: bot.GetId()}))
	for _, s := range again.Msg.GetSkills() {
		if s.GetName() == target.GetName() && s.GetEnabled() {
			t.Fatalf("skill %s still enabled", target.GetName())
		}
	}
}

func TestChannelsCRUDAndSend(t *testing.T) {
	slug := apptest.UniqueSlug("fakechan")
	adapter := apptest.NewFakeAdapter(slug)
	channels.Register(adapter)

	h := apptest.New(t)
	bot := h.CreateBot("Channels")
	created, err := h.Client.CreateChannel(h.Ctx(), connect.NewRequest(&v1.CreateChannelRequest{
		BotId: bot.GetId(), Adapter: slug, Name: "Support", Enabled: true, Inbound: true,
		ExternalId: "chat-1", TargetTitle: "Chat One",
		Secrets: map[string]string{"token": "s3cret"},
	}))
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if created.Msg.GetName() != "Support" || created.Msg.GetExternalId() != "chat-1" {
		t.Fatalf("channel %+v", created.Msg)
	}

	// Channel secrets must stay hidden from the Secrets tab.
	secs, _ := h.Client.ListSecrets(h.Ctx(), connect.NewRequest(&v1.ListSecretsRequest{BotId: bot.GetId()}))
	for _, s := range secs.Msg.GetSecrets() {
		if strings.HasPrefix(s.GetName(), "channel.") {
			t.Fatalf("channel secret leaked: %s", s.GetName())
		}
	}

	// The channel tool sends through the registered adapter.
	dummy.Reset()
	dummy.Script("Test_34",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "channel", Arguments: `{"channel":"Support","text":"hello channel"}`}}},
		dummy.Turn{Text: "sent to channel"},
	)
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_34_Input")
	h.WaitRun(runID)
	sent := adapter.Messages()
	if len(sent) != 1 || sent[0].Text != "hello channel" || sent[0].ExternalID != "chat-1" {
		t.Fatalf("adapter messages %+v", sent)
	}

	// Delete removes the row.
	if _, err := h.Client.DeleteChannel(h.Ctx(), connect.NewRequest(&v1.DeleteChannelRequest{
		BotId: bot.GetId(), Id: created.Msg.GetId(),
	})); err != nil {
		t.Fatal(err)
	}
	after, _ := h.Client.ListBotChannels(h.Ctx(), connect.NewRequest(&v1.ListBotChannelsRequest{BotId: bot.GetId()}))
	if len(after.Msg.GetChannels()) != 0 {
		t.Fatalf("channel remains after delete: %+v", after.Msg.GetChannels())
	}
}

func TestExecPythonTool(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_35",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "exec_python", Arguments: `{"code":"print(6*7)"}`}}},
		dummy.Turn{Text: "python done"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Python")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_35_Input")
	h.WaitRun(runID)
	res := toolResults(h, runID)["exec_python"]
	if !strings.Contains(res, "42") {
		t.Fatalf("exec_python result %q", res)
	}
}

func TestSettingsAdminGateAndAudit(t *testing.T) {
	h := apptest.New(t)
	if _, err := h.Client.GetSettings(h.Ctx(), connect.NewRequest(&v1.GetSettingsRequest{})); err != nil {
		t.Fatalf("admin GetSettings: %v", err)
	}

	// A non-admin user is refused.
	hash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	h.DB.Create(&db.User{ID: ids.New(), Email: "user@test.local", PasswordHash: hash})
	anon := h.NewClient()
	if _, err := anon.SignIn(h.Ctx(), connect.NewRequest(&v1.SignInRequest{
		Email: "user@test.local", Password: "pw",
	})); err != nil {
		t.Fatalf("non-admin sign in: %v", err)
	}
	if _, err := anon.GetSettings(h.Ctx(), connect.NewRequest(&v1.GetSettingsRequest{})); err == nil {
		t.Fatal("non-admin GetSettings should fail")
	}

	// Deciding an approval writes an audit row.
	bot := h.CreateBot("Audited")
	h.DB.Create(&db.Approval{
		ID: "ap-audit", BotID: bot.GetId(), RunID: "r-audit",
		Connector: "terminal", Action: "run", ArgsJSON: "{}", Status: "pending",
	})
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{
		Id: "ap-audit", Decision: "deny",
	})); err != nil {
		t.Fatal(err)
	}
	audit, err := h.Client.ListAudit(h.Ctx(), connect.NewRequest(&v1.ListAuditRequest{}))
	if err != nil || len(audit.Msg.GetRows()) == 0 {
		t.Fatalf("ListAudit: %v %+v", err, audit)
	}
}
