package app_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

// allowBotMessages sets bots.message to Allow on the Bot: its default is
// Auto, which with no policy written asks.
func allowBotMessages(t *testing.T, h *apptest.H, botID string) {
	t.Helper()
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: botID, Connector: "bots", Action: "message", Decision: "allow"})); err != nil {
		t.Fatal(err)
	}
}

// peerChatOf is the receiving Bot's conversation for the sender.
func peerChatOf(t *testing.T, h *apptest.H, toBot, fromBot string) db.Chat {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var c db.Chat
		h.DB.Where("bot_id = ? AND peer_bot_id = ?", toBot, fromBot).Limit(1).Find(&c)
		if c.ID != "" {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("no conversation on %s for %s", toBot, fromBot)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitMessage polls until the message from chatID to toBot satisfies ok.
func waitMessage(t *testing.T, h *apptest.H, fromChat, toBot string, ok func(db.BotMessage) bool) db.BotMessage {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var m db.BotMessage
		h.DB.Where("from_chat_id = ? AND to_bot_id = ?", fromChat, toBot).Order("created_at desc").Limit(1).Find(&m)
		if m.ID != "" && ok(m) {
			return m
		}
		if time.Now().After(deadline) {
			t.Fatalf("message to %s never reached the wanted state: %+v", toBot, m)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBotMessageRoundTrip(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_BM1",
		memCall("message_bot", `{"bot":"Helper","text":"Test_BM1Peer count the apples"}`),
		dummy.Turn{Text: "asked Helper"},
		dummy.Turn{Text: "Helper counted 42"},
	)
	dummy.Script("Test_BM1Peer", dummy.Turn{Text: "there are 42 apples"})
	h := subagentHarness(t)
	lead, helper := h.CreateBot("Lead"), h.CreateBot("Helper")
	allowBotMessages(t, h, lead.GetId())
	chat := h.FirstChat(lead.GetId())

	run, _ := h.Send(lead.GetId(), chat, "Test_BM1 ask Helper")
	evs := h.WaitRun(run)
	if res := toolResult(evs, "message_bot"); !strings.Contains(res, "sent to Helper") {
		t.Fatalf("message_bot result %q\n%s", res, h.RunBody(run))
	}

	// The request is answered in Helper's conversation for Lead: closed,
	// read-only, the person's, and labelled with who sent it.
	peer := peerChatOf(t, h, helper.GetId(), lead.GetId())
	if !peer.Hidden || peer.UserID != h.AdminUser().ID || peer.Title != "From Lead" {
		t.Fatalf("peer chat %+v", peer)
	}
	m := waitMessage(t, h, chat, helper.GetId(), func(m db.BotMessage) bool { return !m.Reported })
	if m.Status != "done" || !strings.Contains(m.Result, "42 apples") || m.Hops != 1 || m.ToChatID != peer.ID || m.RootChatID != chat {
		t.Fatalf("message %+v", m)
	}
	pruns := waitChatRuns(t, h, peer.ID)
	if len(pruns) == 0 || pruns[0].Origin != "bot" || pruns[0].UserID != h.AdminUser().ID || pruns[0].Trigger != "bot" {
		t.Fatalf("peer runs %+v", pruns)
	}
	u, _ := find(h.Events(pruns[0].ID), "user")
	if u.Tool != "bot" || !strings.Contains(u.Meta, `"name":"Lead"`) {
		t.Fatalf("peer opening %+v", u)
	}
	req := subagentRequest(t, h, helper.GetId(), "count the apples")
	if !strings.Contains(req, "[Message from the Bot “Lead”, asking for") || !strings.Contains(req, "You are answering a message from another Bot of your Workspace, “Lead”") {
		t.Fatalf("Helper was not told who asked:\n%s", req)
	}
	if strings.Contains(req, "Test_BM1 ask Helper") {
		t.Fatal("Helper saw Lead's chat")
	}

	// People read it but cannot write in it, and its name is on it.
	res, err := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: helper.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, c := range res.Msg.GetChats() {
		if c.GetId() == peer.ID {
			listed = c.GetFromBot() == "Lead" && c.GetHidden()
		}
	}
	if !listed {
		t.Fatalf("peer chat not listed as closed and from Lead: %v", res.Msg.GetChats())
	}
	if _, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: helper.GetId(), ChatId: peer.ID, Text: "hi"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("send into a Bot's conversation: %v", err)
	}

	// Lead is woken once, with the reply as a report it reads as handed-off
	// work, not as the human.
	wakes := waitReportRuns(t, h, chat, 1)
	wevs := h.WaitRun(wakes[0].ID)
	rep, _ := find(wevs, "subagent_report")
	if rep.Tool != "@Helper:done" || !strings.Contains(rep.Body, "### Bot Helper — done") || !strings.Contains(rep.Body, "42 apples") {
		t.Fatalf("report %+v", rep)
	}
	if wakes[0].Title != "Handed-off work came back" {
		t.Fatalf("wake title %q", wakes[0].Title)
	}
	if !strings.Contains(lastChatRequest(t, h, lead.GetId()), "work you handed off came back") {
		t.Fatal("wake request did not frame the reply")
	}
	settle()
	if n := len(reportRuns(h, chat)); n != 1 {
		t.Fatalf("wake runs %d, want 1", n)
	}
	var row db.BotMessage
	h.DB.First(&row, "id = ?", m.ID)
	if !row.Reported {
		t.Fatal("the wake should mark the reply read")
	}
}

func TestBotMessageIsAutoByDefault(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_BM2",
		memCall("message_bot", `{"bot":"Helper","text":"Test_BM2Peer hello"}`),
		dummy.Turn{Text: "done"},
	)
	h := subagentHarness(t)
	lead, helper := h.CreateBot("Lead"), h.CreateBot("Helper")
	rules, err := h.Client.ListRules(h.Ctx(), connect.NewRequest(&v1.ListRulesRequest{BotId: lead.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rules.Msg.GetRules() {
		if r.GetConnector() == "bots" && r.GetAction() == "message" {
			found = r.GetDecision() == "auto" && r.GetOutput() == "check"
		}
	}
	if !found {
		t.Fatal("bots.message should default to Auto with its reply checked")
	}

	// Auto with no policy written asks; a denial sends nothing.
	run, _ := h.Send(lead.GetId(), h.FirstChat(lead.GetId()), "Test_BM2 go")
	ap := h.WaitApproval(lead.GetId())
	if ap.GetConnector() != "bots" || ap.GetAction() != "message" {
		t.Fatalf("approval %+v", ap)
	}
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	evs := h.WaitRun(run)
	if res := toolResult(evs, "message_bot"); !strings.Contains(res, "denied") {
		t.Fatalf("denied send answered %q", res)
	}
	var n int64
	h.DB.Model(&db.BotMessage{}).Count(&n)
	var chats int64
	h.DB.Model(&db.Chat{}).Where("bot_id = ? AND peer_bot_id <> ''", helper.GetId()).Count(&chats)
	if n != 0 || chats != 0 {
		t.Fatalf("a denied message left %d rows and %d conversations", n, chats)
	}
}

func TestBotMessageReach(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_BM3",
		memCall("message_bot", `{"bot":"Helper","text":"Test_BM3Peer hello"}`),
		dummy.Turn{Text: "done"},
	)
	h := subagentHarness(t)
	lead, helper := h.CreateBot("Lead"), h.CreateBot("Helper")
	allowBotMessages(t, h, lead.GetId())

	// Bob uses Lead but not Helper: Lead cannot reach Helper for him.
	bob, bu := joinSpace(t, h, homeSpace(t, h), "bob@test.local", access.RoleMember)
	if _, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: lead.GetId(), UserId: bu.ID, Role: access.RoleUse})); err != nil {
		t.Fatal(err)
	}
	sendAs := func() string {
		t.Helper()
		res, err := bob.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: lead.GetId(), Text: "Test_BM3 go", NewChat: true}))
		if err != nil {
			t.Fatal(err)
		}
		return toolResult(h.WaitRun(res.Msg.GetRunId()), "message_bot")
	}
	if res := sendAs(); !strings.Contains(res, "not shared with the person") {
		t.Fatalf("Bob reached a Bot he is not on: %q", res)
	}

	// On Helper too, it works, and the answering run is Bob's.
	if _, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: helper.GetId(), UserId: bu.ID, Role: access.RoleUse})); err != nil {
		t.Fatal(err)
	}
	if res := sendAs(); !strings.Contains(res, "sent to Helper") {
		t.Fatalf("Bob's send: %q", res)
	}
	var peer db.Chat
	h.DB.Where("bot_id = ? AND peer_bot_id = ?", helper.GetId(), lead.GetId()).Limit(1).Find(&peer)
	if peer.UserID != bu.ID {
		t.Fatalf("the conversation is %q's, want Bob's", peer.UserID)
	}

	// A Bot whose managers turned messages off takes none.
	off := false
	if _, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{Id: helper.GetId(), Name: "Helper", AcceptBotMessages: &off})); err != nil {
		t.Fatal(err)
	}
	got, _ := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: helper.GetId()}))
	if got.Msg.GetAcceptBotMessages() {
		t.Fatal("the switch did not stick")
	}
	if res := sendAs(); !strings.Contains(res, "does not take messages from other Bots") {
		t.Fatalf("a closed Bot took a message: %q", res)
	}
}

func TestBotMessageChain(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_BM4",
		memCall("message_bot", `{"bot":"B","text":"Test_BM4B start"}`),
		dummy.Turn{Text: "asked B"},
		dummy.Turn{Text: "A got the answer"},
	)
	dummy.Script("Test_BM4B",
		// Never back round to a Bot already in the request.
		memCall("message_bot", `{"bot":"A","text":"loop"}`),
		memCall("message_bot", `{"bot":"C","text":"Test_BM4C go on"}`),
		dummy.Turn{Text: "waiting on C"},
		dummy.Turn{Text: "B final: C said deep"},
	)
	dummy.Script("Test_BM4C",
		memCall("message_bot", `{"bot":"D","text":"Test_BM4D last"}`),
		dummy.Turn{Text: "waiting on D"},
		dummy.Turn{Text: "deep"},
	)
	dummy.Script("Test_BM4D",
		// Three Bots deep already: no fourth.
		memCall("message_bot", `{"bot":"E","text":"too far"}`),
		dummy.Turn{Text: "D answers itself"},
	)
	h := subagentHarness(t)
	var bots = map[string]*v1.Bot{}
	for _, n := range []string{"A", "B", "C", "D", "E"} {
		bots[n] = h.CreateBot(n)
		allowBotMessages(t, h, bots[n].GetId())
	}
	chat := h.FirstChat(bots["A"].GetId())
	run, _ := h.Send(bots["A"].GetId(), chat, "Test_BM4 go")
	h.WaitRun(run)

	bChat := peerChatOf(t, h, bots["B"].GetId(), bots["A"].GetId())
	dChat := peerChatOf(t, h, bots["D"].GetId(), bots["C"].GetId())
	dRuns := waitChatRuns(t, h, dChat.ID)
	h.WaitRun(dRuns[0].ID)
	if res := toolResult(h.Events(dRuns[0].ID), "message_bot"); !strings.Contains(res, "3 Bots deep") {
		t.Fatalf("a fourth hop was let through: %q", res)
	}
	var dm db.BotMessage
	h.DB.Where("to_chat_id = ?", dChat.ID).Limit(1).Find(&dm)
	if dm.Hops != 3 || dm.RootChatID != chat || len(strings.Split(dm.Chain, ",")) != 3 {
		t.Fatalf("D's message %+v", dm)
	}

	// The reply climbs back: D → C → B → A, each woken in turn.
	m := waitMessage(t, h, chat, bots["B"].GetId(), func(m db.BotMessage) bool { return m.Status == "done" && !m.Reported })
	if !strings.Contains(m.Result, "B final: C said deep") {
		t.Fatalf("A got %q", m.Result)
	}
	bRuns := waitChatRuns(t, h, bChat.ID)
	if res := toolResult(h.Events(bRuns[0].ID), "message_bot"); !strings.Contains(res, "already part of this request") {
		t.Fatalf("B messaged A back: %q", res)
	}
	wakes := waitReportRuns(t, h, chat, 1)
	h.WaitRun(wakes[0].ID)
}

func TestBotMessageStopWithdraws(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_BM5",
		memCall("message_bot", `{"bot":"Helper","text":"Test_BM5Peer take your time"}`),
		dummy.Turn{Text: "asked"},
	)
	dummy.Script("Test_BM5Peer",
		memCall("sleep", `{"seconds":30}`),
		dummy.Turn{Text: "late"},
	)
	h := subagentHarness(t)
	lead, helper := h.CreateBot("Lead"), h.CreateBot("Helper")
	allowBotMessages(t, h, lead.GetId())
	chat := h.FirstChat(lead.GetId())
	run, _ := h.Send(lead.GetId(), chat, "Test_BM5 go")
	h.WaitRun(run)
	peer := peerChatOf(t, h, helper.GetId(), lead.GetId())
	waitLive(t, h, peer.ID)

	// Stop in the sending chat withdraws the request and stops the work,
	// without waking the sender.
	if _, err := h.Client.StopRun(h.Ctx(), connect.NewRequest(&v1.StopRunRequest{BotId: lead.GetId(), ChatId: chat})); err != nil {
		t.Fatal(err)
	}
	m := waitMessage(t, h, chat, helper.GetId(), func(m db.BotMessage) bool { return m.Status == "stopped" })
	if !m.Reported {
		t.Fatal("a withdrawn request should not wake the sender")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var live int64
		h.DB.Model(&db.Run{}).Where("chat_id = ? AND status = ?", peer.ID, "running").Count(&live)
		if live == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the receiving run went on after the sender stopped")
		}
		time.Sleep(20 * time.Millisecond)
	}
	settle()
	if n := len(reportRuns(h, chat)); n != 0 {
		t.Fatalf("a withdrawn request woke the sender %d times", n)
	}
}

func TestBotMessageCarriesFiles(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_BM6",
		memCall("message_bot", `{"bot":"Helper","text":"Test_BM6Peer summarise","files":["notes.txt"]}`),
		dummy.Turn{Text: "asked"},
	)
	h := subagentHarness(t)
	lead, helper := h.CreateBot("Lead"), h.CreateBot("Helper")
	allowBotMessages(t, h, lead.GetId())
	h.StartWorker(lead.GetId())
	h.StartWorker(helper.GetId())
	h.WaitBotStatus(lead.GetId(), "online")
	h.WaitBotStatus(helper.GetId(), "online")
	if _, err := h.Client.PutFile(h.Ctx(), connect.NewRequest(&v1.PutFileRequest{BotId: lead.GetId(), Path: "notes.txt", Data: []byte("apples and pears")})); err != nil {
		t.Fatal(err)
	}
	// Helper reads what came, and presents its answer as a file.
	dummy.Script("Test_BM6Peer",
		memCall("write", `{"path":"summary.md","content":"two fruits"}`),
		memCall("present", `{"path":"summary.md"}`),
		dummy.Turn{Text: "see summary.md"},
	)
	chat := h.FirstChat(lead.GetId())
	run, _ := h.Send(lead.GetId(), chat, "Test_BM6 go")
	if res := toolResult(h.WaitRun(run), "message_bot"); !strings.Contains(res, "sent to Helper") {
		t.Fatalf("send: %q", res)
	}

	peer := peerChatOf(t, h, helper.GetId(), lead.GetId())
	u := h.LastUserEvent(peer.ID)
	var meta struct {
		Files []struct{ Path string } `json:"files"`
	}
	_ = json.Unmarshal([]byte(u.Meta), &meta)
	if len(meta.Files) != 1 || !strings.HasPrefix(meta.Files[0].Path, "tmp/") || !strings.HasSuffix(meta.Files[0].Path, "-notes.txt") {
		t.Fatalf("Helper was handed %+v", meta)
	}
	got, err := h.Client.ReadFile(h.Ctx(), connect.NewRequest(&v1.ReadFileRequest{BotId: helper.GetId(), Path: meta.Files[0].Path}))
	if err != nil || got.Msg.GetContent() != "apples and pears" {
		t.Fatalf("Helper's copy: %v %v", got, err)
	}

	m := waitMessage(t, h, chat, helper.GetId(), func(m db.BotMessage) bool { return !m.Reported })
	var back []string
	_ = json.Unmarshal([]byte(m.ReplyFiles), &back)
	if len(back) != 1 || !strings.HasSuffix(back[0], "-summary.md") {
		t.Fatalf("reply files %q (result %q)", m.ReplyFiles, m.Result)
	}
	mine, err := h.Client.ReadFile(h.Ctx(), connect.NewRequest(&v1.ReadFileRequest{BotId: lead.GetId(), Path: back[0]}))
	if err != nil || mine.Msg.GetContent() != "two fruits" {
		t.Fatalf("Lead's copy: %v %v", mine, err)
	}
	wakes := waitReportRuns(t, h, chat, 1)
	rep, _ := find(h.WaitRun(wakes[0].ID), "subagent_report")
	if !strings.Contains(rep.Body, "Files it sent you: "+back[0]) {
		t.Fatalf("report %q", rep.Body)
	}
}

// waitLive waits until the chat has a live run.
func waitLive(t *testing.T, h *apptest.H, chatID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int64
		h.DB.Model(&db.Run{}).Where("chat_id = ? AND status = ?", chatID, "running").Count(&n)
		if n > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no live run")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitChatRuns waits until the chat has a run: its row is written just
// after the conversation is made.
func waitChatRuns(t *testing.T, h *apptest.H, chatID string) []db.Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if rs := chatRuns(h, chatID); len(rs) > 0 {
			return rs
		}
		if time.Now().After(deadline) {
			t.Fatal("no run in the conversation")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
