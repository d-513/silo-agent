package app_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

// subagentHarness logs every model request so a test can read what a subagent
// was sent.
func subagentHarness(t *testing.T) *apptest.H {
	dir := t.TempDir()
	path := dir + "/silo.yaml"
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)+"debug: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return apptest.New(t, apptest.WithConfigPath(path))
}

func listSubagents(t *testing.T, h *apptest.H, botID, chatID string) []*v1.Subagent {
	t.Helper()
	res, err := h.Client.ListSubagents(h.Ctx(), connect.NewRequest(&v1.ListSubagentsRequest{BotId: botID, ChatId: chatID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetSubagents()
}

// waitSubagent polls until the named subagent satisfies ok.
func waitSubagent(t *testing.T, h *apptest.H, botID, chatID, name string, ok func(*v1.Subagent) bool) *v1.Subagent {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		for _, sa := range listSubagents(t, h, botID, chatID) {
			if sa.GetName() == name && ok(sa) {
				return sa
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("subagent %s never reached the wanted state: %v", name, listSubagents(t, h, botID, chatID))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func chatRuns(h *apptest.H, chatID string) []db.Run {
	var runs []db.Run
	h.DB.Where("chat_id = ?", chatID).Order("created_at").Find(&runs)
	return runs
}

// reportRuns are the lead's wake runs: runs that open with a subagent report.
func reportRuns(h *apptest.H, chatID string) []db.Run {
	var out []db.Run
	for _, r := range chatRuns(h, chatID) {
		if _, ok := find(h.Events(r.ID), "subagent_report"); ok {
			out = append(out, r)
		}
	}
	return out
}

func waitReportRuns(t *testing.T, h *apptest.H, chatID string, n int) []db.Run {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if rs := reportRuns(h, chatID); len(rs) >= n {
			return rs
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d wake runs, got %d", n, len(reportRuns(h, chatID)))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// settle waits out the wake debounce so a missing wake can be asserted.
func settle() { time.Sleep(1500 * time.Millisecond) }

func toolResult(evs []db.RunEvent, tool string) string {
	for _, ev := range evs {
		if ev.Kind == "tool_result" && ev.Tool == tool {
			return ev.Body
		}
	}
	return ""
}

func subagentRequest(t *testing.T, h *apptest.H, botID, marker string) string {
	t.Helper()
	var rows []db.LLMLog
	h.DB.Where("bot_id = ? AND label = ?", botID, "chat").Order("at").Find(&rows)
	for _, r := range rows {
		if strings.Contains(r.Request, marker) {
			return r.Request
		}
	}
	t.Fatalf("no request containing %q", marker)
	return ""
}

func TestSubagentSpawnReportAndWake(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_SA1",
		memCall("task_add", `{"tasks":["[scout] look around the workspace","write the summary"]}`),
		memCall("spawn_agent", `{"name":"scout","goal":"Test_SA1Sub explore","context":"only read"}`),
		dummy.Turn{Text: "spawned scout"},
	)
	dummy.Script("Test_SA1Sub",
		memCall("task_done", `{"ids":[1],"note":"all read"}`),
		memCall("task_reset", `{}`),
		dummy.Turn{Text: "scout report: found three things"},
	)
	h := subagentHarness(t)
	bot := h.CreateBot("Lead")
	id := bot.GetId()
	chat := h.FirstChat(id)
	run, _ := h.Send(id, chat, "Test_SA1_Input")
	evs := h.WaitRun(run)
	if res := toolResult(evs, "spawn_agent"); !strings.Contains(res, "started subagent scout") {
		t.Fatalf("spawn result %q\n%s", res, h.RunBody(run))
	}

	sa := waitSubagent(t, h, id, chat, "scout", func(s *v1.Subagent) bool { return !s.GetRunning() && s.GetStatus() == "done" })
	if !strings.Contains(sa.GetResult(), "scout report") || sa.GetModel() != "dummy/echo" {
		t.Fatalf("subagent %v", sa)
	}

	// Its log is hidden and read-only.
	chats, _ := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: id}))
	for _, c := range chats.Msg.GetChats() {
		if c.GetId() == sa.GetChatId() {
			t.Fatal("subagent log leaked into the chat list")
		}
	}
	if _, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: id, ChatId: sa.GetChatId(), Text: "hi"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("send into a subagent log: %v", err)
	}
	// The brief is from the lead.
	subRuns := chatRuns(h, sa.GetChatId())
	subEvs := h.Events(subRuns[0].ID)
	if u, _ := find(subEvs, "user"); u.Tool != "lead" || !strings.Contains(u.Body, "## Goal") || !strings.Contains(u.Body, "only read") {
		t.Fatalf("brief %+v", u)
	}
	if res := toolResult(subEvs, "task_reset"); !strings.Contains(res, "only the lead") {
		t.Fatalf("subagent reset should be refused, got %q", res)
	}

	// The subagent saw a fresh context, its own tool set, and the board.
	req := subagentRequest(t, h, id, "You are the subagent “scout”")
	if strings.Contains(req, "Test_SA1_Input") {
		t.Fatal("subagent saw the lead's history")
	}
	for _, lead := range []string{"- spawn_agent:", "- message_agent:", "- task_reset:", "- switch_model:"} {
		if strings.Contains(req, lead) {
			t.Fatalf("subagent got lead tool %s", lead)
		}
	}
	if !strings.Contains(req, "You are the subagent “scout”") || !strings.Contains(req, "<taskboard>") || !strings.Contains(req, "[scout] look around the workspace") {
		t.Fatalf("subagent prompt missing role or board:\n%s", req)
	}

	// Board: item 1 done by scout, reset refused so both remain.
	board, err := h.Client.GetTaskboard(h.Ctx(), connect.NewRequest(&v1.GetTaskboardRequest{BotId: id, ChatId: sa.GetChatId()}))
	if err != nil {
		t.Fatal(err)
	}
	items := board.Msg.GetItems()
	if board.Msg.GetChatId() != chat || len(items) != 2 || !items[0].GetDone() || items[0].GetDoneBy() != "scout" || items[0].GetAssignee() != "scout" || items[1].GetDone() {
		t.Fatalf("board %v", board.Msg)
	}

	// Exactly one wake, with the report replayed as the lead's user turn.
	wakes := waitReportRuns(t, h, chat, 1)
	wevs := h.WaitRun(wakes[0].ID)
	rep, _ := find(wevs, "subagent_report")
	if !strings.Contains(rep.Body, "scout report") || rep.Tool != "scout:done" {
		t.Fatalf("report %+v", rep)
	}
	if wakes[0].Origin != "subagents" {
		t.Fatalf("wake origin %q", wakes[0].Origin)
	}
	if !strings.Contains(lastChatRequest(t, h, id), "your subagents finished") {
		t.Fatal("wake request did not carry the report")
	}
	settle()
	if n := len(reportRuns(h, chat)); n != 1 {
		t.Fatalf("wake runs %d, want 1", n)
	}
	var row db.Subagent
	h.DB.First(&row, "id = ?", sa.GetId())
	if !row.Reported {
		t.Fatal("report should mark the subagent seen")
	}
}

func TestSubagentSleepWakesAndSafetyWake(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_SA2",
		memCall("spawn_agent", `{"name":"quick","goal":"Test_SA2Sub go"}`),
		memCall("sleep", `{"seconds":30}`),
		dummy.Turn{Text: "done waiting"},
	)
	dummy.Script("Test_SA2Sub", dummy.Turn{Text: "quick result"})
	h := apptest.New(t)
	bot := h.CreateBot("Sleeper")
	id := bot.GetId()
	chat := h.FirstChat(id)
	start := time.Now()
	run, _ := h.Send(id, chat, "Test_SA2_Input")
	evs := h.WaitRun(run)
	if time.Since(start) > 10*time.Second {
		t.Fatal("sleep did not return early")
	}
	if res := toolResult(evs, "sleep"); !strings.HasPrefix(res, "woke") {
		t.Fatalf("sleep result %q", res)
	}
	// The lead never read the result, so its finished run wakes it once.
	waitReportRuns(t, h, chat, 1)
	settle()
	if n := len(reportRuns(h, chat)); n != 1 {
		t.Fatalf("wake runs %d, want 1", n)
	}
}

func TestSubagentInspectedNeedsNoWake(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_SA3",
		memCall("spawn_agent", `{"name":"s3","goal":"Test_SA3Sub go"}`),
		memCall("sleep", `{"seconds":30}`),
		memCall("agent_status", `{"name":"s3"}`),
		memCall("agent_status", `{}`),
		dummy.Turn{Text: "read it"},
	)
	dummy.Script("Test_SA3Sub", dummy.Turn{Text: "s3 findings"})
	h := apptest.New(t)
	bot := h.CreateBot("Inspector")
	id := bot.GetId()
	chat := h.FirstChat(id)
	run, _ := h.Send(id, chat, "Test_SA3_Input")
	evs := h.WaitRun(run)
	var status []string
	for _, ev := range evs {
		if ev.Kind == "tool_result" && ev.Tool == "agent_status" {
			status = append(status, ev.Body)
		}
	}
	if len(status) != 2 || !strings.Contains(status[0], "Result:") || !strings.Contains(status[0], "s3 findings") || !strings.Contains(status[1], "s3: done") {
		t.Fatalf("agent_status %q", status)
	}
	settle()
	if n := len(reportRuns(h, chat)); n != 0 {
		t.Fatalf("an inspected result should not wake the lead (%d wakes)", n)
	}
}

func TestSubagentMessageResumes(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_SA5",
		memCall("spawn_agent", `{"name":"w","goal":"Test_SA5Sub start"}`),
		memCall("sleep", `{"seconds":30}`),
		memCall("agent_status", `{"name":"w"}`),
		memCall("message_agent", `{"name":"w","text":"also do more"}`),
		memCall("sleep", `{"seconds":30}`),
		memCall("agent_status", `{"name":"w"}`),
		dummy.Turn{Text: "all done"},
	)
	dummy.Script("Test_SA5Sub", dummy.Turn{Text: "first pass"})
	h := apptest.New(t)
	bot := h.CreateBot("Steer")
	id := bot.GetId()
	chat := h.FirstChat(id)
	run, _ := h.Send(id, chat, "Test_SA5_Input")
	evs := h.WaitRun(run)
	if res := toolResult(evs, "message_agent"); !strings.Contains(res, "resumed w") {
		t.Fatalf("message_agent %q\n%s", res, h.RunBody(run))
	}
	var last string
	for _, ev := range evs {
		if ev.Kind == "tool_result" && ev.Tool == "agent_status" {
			last = ev.Body
		}
	}
	if !strings.Contains(last, "Test_SA5Sub_End") {
		t.Fatalf("resumed result %q", last)
	}
	sa := waitSubagent(t, h, id, chat, "w", func(s *v1.Subagent) bool { return !s.GetRunning() })
	runs := chatRuns(h, sa.GetChatId())
	if len(runs) != 2 {
		t.Fatalf("subagent runs %d, want 2", len(runs))
	}
	if u, _ := find(h.Events(runs[1].ID), "user"); u.Tool != "lead" || u.Body != "also do more" {
		t.Fatalf("lead message %+v", u)
	}
}

func TestSubagentStopCascadesAndDeleteDrops(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_SA4",
		memCall("task_add", `{"tasks":["[slow] take a while"]}`),
		memCall("spawn_agent", `{"name":"slow","goal":"Test_SA4Sub wait"}`),
		dummy.Turn{Text: "spawned"},
	)
	dummy.Script("Test_SA4Sub",
		memCall("sleep", `{"seconds":60}`),
		dummy.Turn{Text: "never"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Stopper")
	id := bot.GetId()
	chat := h.FirstChat(id)
	run, _ := h.Send(id, chat, "Test_SA4_Input")
	h.WaitRun(run)
	sa := waitSubagent(t, h, id, chat, "slow", func(s *v1.Subagent) bool { return s.GetRunning() })

	if _, err := h.Client.StopRun(h.Ctx(), connect.NewRequest(&v1.StopRunRequest{BotId: id, ChatId: chat})); err != nil {
		t.Fatal(err)
	}
	sa = waitSubagent(t, h, id, chat, "slow", func(s *v1.Subagent) bool { return !s.GetRunning() })
	if sa.GetStatus() != "stopped" {
		t.Fatalf("status %q", sa.GetStatus())
	}
	settle()
	if n := len(reportRuns(h, chat)); n != 0 {
		t.Fatalf("the lead's Stop must not wake it (%d wakes)", n)
	}

	if _, err := h.Client.DeleteChat(h.Ctx(), connect.NewRequest(&v1.DeleteChatRequest{BotId: id, Id: chat})); err != nil {
		t.Fatal(err)
	}
	var n int64
	h.DB.Model(&db.Chat{}).Where("id = ?", sa.GetChatId()).Count(&n)
	if n != 0 {
		t.Fatal("subagent log survived its lead chat")
	}
	h.DB.Model(&db.Subagent{}).Where("parent_chat_id = ?", chat).Count(&n)
	if n != 0 {
		t.Fatal("subagent row survived its lead chat")
	}
	h.DB.Model(&db.TaskItem{}).Where("chat_id = ?", chat).Count(&n)
	if n != 0 {
		t.Fatal("taskboard survived its chat")
	}
}

func TestSubagentSpawnValidation(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_SA6",
		memCall("spawn_agent", `{"name":"bad name!","goal":"x"}`),
		memCall("spawn_agent", `{"name":"ok","goal":"x","model":"dummy/nope"}`),
		memCall("spawn_agent", `{"name":"dup","goal":"Test_SA6Sub a"}`),
		memCall("spawn_agent", `{"name":"DUP","goal":"Test_SA6Sub b"}`),
		dummy.Turn{Text: "end"},
	)
	dummy.Script("Test_SA6Sub", dummy.Turn{Text: "fine"})
	h := apptest.New(t)
	bot := h.CreateBot("Picky")
	id := bot.GetId()
	chat := h.FirstChat(id)
	run, _ := h.Send(id, chat, "Test_SA6_Input")
	var res []string
	for _, ev := range h.WaitRun(run) {
		if ev.Kind == "tool_result" {
			res = append(res, ev.Body)
		}
	}
	if len(res) != 4 || !strings.Contains(res[0], "name must") || !strings.Contains(res[1], "not allowed") ||
		!strings.Contains(res[2], "started") || !strings.Contains(res[3], "already exists") {
		t.Fatalf("results %q", res)
	}
}

func TestTaskboardPythonPath(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("PyBoard")
	id := bot.GetId()
	chat := h.FirstChat(id)
	run, _ := h.Send(id, chat, "Test_SA7_Input")
	h.WaitRun(run)

	if res := pyCall(t, h, id, run, "tasks", "add", `{"tasks":["[bob] one","two"]}`); res.GetError() != "" {
		t.Fatalf("add: %+v", res)
	}
	if res := pyCall(t, h, id, run, "tasks", "done", `{"ids":[2],"note":"n"}`); res.GetError() != "" {
		t.Fatalf("done: %+v", res)
	}
	res := pyCall(t, h, id, run, "tasks", "read", `{}`)
	var got struct {
		Items []struct {
			N        int    `json:"n"`
			Assignee string `json:"assignee"`
			Done     bool   `json:"done"`
			DoneBy   string `json:"done_by"`
		} `json:"items"`
	}
	if res.GetError() != "" || json.Unmarshal([]byte(res.GetResultJson()), &got) != nil || len(got.Items) != 2 ||
		got.Items[0].Assignee != "bob" || got.Items[0].Done || !got.Items[1].Done || got.Items[1].DoneBy != "lead" {
		t.Fatalf("read: %+v", res)
	}
	if res := pyCall(t, h, id, run, "tasks", "reset", `{}`); res.GetError() != "" {
		t.Fatalf("reset: %+v", res)
	}
	board, _ := h.Client.GetTaskboard(h.Ctx(), connect.NewRequest(&v1.GetTaskboardRequest{BotId: id, ChatId: chat}))
	if len(board.Msg.GetItems()) != 0 {
		t.Fatal("reset left items")
	}
}

func TestSubagentDefaultModel(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_SA8",
		memCall("spawn_agent", `{"name":"cheap","goal":"Test_SA8Sub a"}`),
		memCall("spawn_agent", `{"name":"picked","goal":"Test_SA8Sub b","model":"dummy/echo"}`),
		dummy.Turn{Text: "end"},
	)
	dummy.Script("Test_SA8Sub", dummy.Turn{Text: "ok"})
	dir := t.TempDir()
	path := dir + "/silo.yaml"
	raw := strings.Replace(apptest.DefaultYAML(dir), "  - dummy/echo\n", "  - dummy/echo\n  - dummy/cheap\n", 1) + "model_subagent: dummy/cheap\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	h := apptest.New(t, apptest.WithConfigPath(path))
	bot := h.CreateBot("Frugal")
	id := bot.GetId()
	chat := h.FirstChat(id)
	run, _ := h.Send(id, chat, "Test_SA8_Input")
	h.WaitRun(run)
	got := map[string]string{}
	for _, sa := range listSubagents(t, h, id, chat) {
		got[sa.GetName()] = sa.GetModel()
	}
	if got["cheap"] != "dummy/cheap" || got["picked"] != "dummy/echo" {
		t.Fatalf("models %v", got)
	}
}
