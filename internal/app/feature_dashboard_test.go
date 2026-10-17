package app_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

func listDashboard(t *testing.T, h *apptest.H, botID string) *v1.ListDashboardResponse {
	t.Helper()
	res, err := h.Client.ListDashboard(h.Ctx(), connect.NewRequest(&v1.ListDashboardRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

func widgetNamed(t *testing.T, h *apptest.H, botID, key string) *v1.DashboardWidget {
	t.Helper()
	for _, w := range listDashboard(t, h, botID).GetWidgets() {
		if w.GetKey() == key {
			return w
		}
	}
	t.Fatalf("no widget %q", key)
	return nil
}

func TestDashboardWidgetsPushedData(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Board")
	id := bot.GetId()

	if res := pyCall(t, h, id, "", "bot", "widget_put", `{"key":"mail","kind":"stat","title":"Mail to answer","data":{"value":3}}`); res.GetError() != "" || !strings.Contains(res.GetResultJson(), `"created":true`) {
		t.Fatalf("put: %+v", res)
	}
	if res := pyCall(t, h, id, "", "bot", "widget_put", `{"key":"x","kind":"pie","title":"Nope"}`); !strings.Contains(res.GetError(), "kind") {
		t.Fatalf("unknown kind: %+v", res)
	}
	if res := pyCall(t, h, id, "", "bot", "widget_put", `{"key":"y"}`); !strings.Contains(res.GetError(), "kind required") {
		t.Fatalf("new widget without kind: %+v", res)
	}
	if res := pyCall(t, h, id, "", "bot", "widget_set", `{"key":"mail","data":{"caption":"Oldest from Thursday"},"merge":true}`); res.GetError() != "" {
		t.Fatalf("set: %+v", res)
	}
	w := widgetNamed(t, h, id, "mail")
	if w.GetSize() != "s" || !w.GetFresh() || !strings.Contains(w.GetDataJson(), `"value":3`) || !strings.Contains(w.GetDataJson(), "Thursday") {
		t.Fatalf("widget %+v", w)
	}
	if _, err := h.Client.MarkDashboardRead(h.Ctx(), connect.NewRequest(&v1.MarkDashboardReadRequest{BotId: id})); err != nil {
		t.Fatal(err)
	}
	if widgetNamed(t, h, id, "mail").GetFresh() {
		t.Fatal("looked-at widget is still fresh")
	}
	// Pushing the same data is not a change.
	pyCall(t, h, id, "", "bot", "widget_set", `{"key":"mail","data":{"value":3,"caption":"Oldest from Thursday"}}`)
	if widgetNamed(t, h, id, "mail").GetFresh() {
		t.Fatal("unchanged data marked fresh")
	}

	// The owner's checks are state the Bot reads back.
	pyCall(t, h, id, "", "bot", "widget_put", `{"key":"week","kind":"checklist","title":"This week","data":{"items":[{"id":"a","text":"Renew the domain"}]}}`)
	wk := widgetNamed(t, h, id, "week")
	if _, err := h.Client.SetWidgetState(h.Ctx(), connect.NewRequest(&v1.SetWidgetStateRequest{BotId: id, Id: wk.GetId(), StateJson: `{"checked":["a"]}`})); err != nil {
		t.Fatal(err)
	}
	if res := pyCall(t, h, id, "", "bot", "dashboard_list", `{}`); !strings.Contains(res.GetResultJson(), `"checked":["a"]`) {
		t.Fatalf("list does not show the owner's state: %s", res.GetResultJson())
	}

	// Once the owner places a widget, the Bot's size no longer moves it.
	if _, err := h.Client.ArrangeDashboard(h.Ctx(), connect.NewRequest(&v1.ArrangeDashboardRequest{BotId: id, Places: []*v1.WidgetPlace{
		{Id: wk.GetId(), Size: "l", Rows: 2, Position: 0},
		{Id: w.GetId(), Size: "m", Rows: 1, Position: 1},
	}})); err != nil {
		t.Fatal(err)
	}
	pyCall(t, h, id, "", "bot", "widget_put", `{"key":"week","size":"s"}`)
	got := listDashboard(t, h, id).GetWidgets()
	if got[0].GetKey() != "week" || got[0].GetSize() != "l" || got[0].GetRows() != 2 {
		t.Fatalf("arranged widgets %+v", got)
	}

	if _, err := h.Client.RemoveWidget(h.Ctx(), connect.NewRequest(&v1.RemoveWidgetRequest{BotId: id, Id: wk.GetId()})); err != nil {
		t.Fatal(err)
	}
	if res := pyCall(t, h, id, "", "bot", "widget_remove", `{"key":"mail"}`); res.GetError() != "" {
		t.Fatalf("remove: %+v", res)
	}
	if n := len(listDashboard(t, h, id).GetWidgets()); n != 0 {
		t.Fatalf("%d widgets left", n)
	}
}

func TestDashboardShowsUnreadFeedPosts(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("News")
	id := bot.GetId()
	pyCall(t, h, id, "", "bot", "feed", `{"title":"Digest","text":"spend is up"}`)
	if posts := listDashboard(t, h, id).GetPosts(); len(posts) != 1 || posts[0].GetTitle() != "Digest" {
		t.Fatalf("posts %+v", posts)
	}
	if feedUnread(t, h, id) != 1 {
		t.Fatal("post should be unread")
	}
	if _, err := h.Client.MarkDashboardRead(h.Ctx(), connect.NewRequest(&v1.MarkDashboardReadRequest{BotId: id})); err != nil {
		t.Fatal(err)
	}
	if len(listDashboard(t, h, id).GetPosts()) != 0 || feedUnread(t, h, id) != 0 {
		t.Fatal("looking at the Dashboard should read its posts")
	}
	if len(listFeed(t, h, id)) != 1 {
		t.Fatal("the post stays in the Feed")
	}
}

func TestDashboardProbesAndActions(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_handed", dummy.Turn{Text: "on it"})
	h := apptest.New(t)
	bot := h.CreateBot("Prober")
	id := bot.GetId()
	w := h.StartWorker(id)
	_ = w

	// A new probe runs at once and its data comes back to the Bot.
	res := pyCall(t, h, id, "", "bot", "widget_put", `{"key":"files","kind":"stat","title":"Files","every":"15m",
		"probe":"import os\nresult = {'value': len(os.listdir('.')), 'caption': ARGS.get('x', 'none')}",
		"spec":{"actions":[
			{"id":"touch","label":"Add a file","kind":"run","code":"open(ARGS['name'],'w').write('x')\nresult = {'receipt': 'Added ' + ARGS['name']}"},
			{"id":"ask","label":"Explain this","kind":"hand"}]}}`)
	if res.GetError() != "" || !strings.Contains(res.GetResultJson(), `"value":0`) || !strings.Contains(res.GetResultJson(), `"caption":"none"`) {
		t.Fatalf("put with probe: %+v", res)
	}
	wg := widgetNamed(t, h, id, "files")
	if !wg.GetHasProbe() || wg.GetEvery() != 900 || strings.Contains(wg.GetSpecJson(), "open(") {
		t.Fatalf("widget %+v (action code must not reach the page)", wg)
	}

	// A run action executes the stored code, leaves a receipt, and refreshes.
	ar, err := h.Client.RunWidgetAction(h.Ctx(), connect.NewRequest(&v1.RunWidgetActionRequest{BotId: id, Id: wg.GetId(), Action: "touch", ArgsJson: `{"name":"a.txt"}`}))
	if err != nil {
		t.Fatal(err)
	}
	var rc struct {
		Text string
		OK   bool
	}
	_ = json.Unmarshal([]byte(ar.Msg.GetWidget().GetReceiptJson()), &rc)
	if !rc.OK || rc.Text != "Added a.txt" || !strings.Contains(ar.Msg.GetWidget().GetDataJson(), `"value":1`) {
		t.Fatalf("after action: %+v", ar.Msg.GetWidget())
	}

	// A hand action opens a chat that quotes the widget.
	hr, err := h.Client.RunWidgetAction(h.Ctx(), connect.NewRequest(&v1.RunWidgetActionRequest{BotId: id, Id: wg.GetId(), Action: "ask", ArgsJson: `{"why":"Test_handed"}`}))
	if err != nil || hr.Msg.GetChat() == nil {
		t.Fatalf("hand: %v %+v", err, hr)
	}
	h.WaitRun(hr.Msg.GetRunId())
	if !strings.Contains(h.RunBody(hr.Msg.GetRunId()), "on it") {
		t.Fatal("handed chat did not run")
	}

	// A failing probe keeps the data and shows why.
	res = pyCall(t, h, id, "", "bot", "widget_put", `{"key":"files","probe":"raise ValueError('boom')"}`)
	if !strings.Contains(res.GetResultJson(), "ValueError: boom") {
		t.Fatalf("failing probe: %+v", res)
	}
	wg = widgetNamed(t, h, id, "files")
	if !strings.Contains(wg.GetError(), "boom") || !strings.Contains(wg.GetDataJson(), `"value":1`) {
		t.Fatalf("failed widget %+v", wg)
	}

	// The sweep runs a due probe with no model.
	pyCall(t, h, id, "", "bot", "widget_put", `{"key":"files","probe":"result = {'value': 42}"}`)
	h.DB.Model(&db.DashboardWidget{}).Where("bot_id = ? AND key = ?", id, "files").UpdateColumn("next_at", time.Now().Add(-time.Minute))
	pyCall(t, h, id, "", "bot", "widget_set", `{"key":"files","data":{"value":0}}`)
	h.App.Dashboard.Sweep(time.Now())
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(widgetNamed(t, h, id, "files").GetDataJson(), `"value":42`) {
		if time.Now().After(deadline) {
			t.Fatalf("sweep did not refresh: %+v", widgetNamed(t, h, id, "files"))
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Python set to Deny: new code is refused, and approved code stops running.
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: id, Connector: "python", Action: "run", Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
	if res := pyCall(t, h, id, "", "bot", "widget_put", `{"key":"files","probe":"result = {'value': 7}"}`); !strings.Contains(res.GetError(), "not allowed") {
		t.Fatalf("denied probe: %+v", res)
	}
	if _, err := h.Client.RefreshWidget(h.Ctx(), connect.NewRequest(&v1.RefreshWidgetRequest{BotId: id, Id: wg.GetId()})); err != nil {
		t.Fatal(err)
	}
	if wg = widgetNamed(t, h, id, "files"); !strings.Contains(wg.GetError(), "Deny") {
		t.Fatalf("refresh under Deny: %+v", wg)
	}
}

func TestAskDashboardRunsInTheDashboardLog(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_board_ask", dummy.Turn{Text: "changed it"})
	h := apptest.New(t)
	bot := h.CreateBot("Asker")
	id := bot.GetId()
	pyCall(t, h, id, "", "bot", "widget_put", `{"key":"spend","kind":"stat","title":"Spend","data":{"value":48}}`)
	wg := widgetNamed(t, h, id, "spend")
	res, err := h.Client.AskDashboard(h.Ctx(), connect.NewRequest(&v1.AskDashboardRequest{BotId: id, Text: "split it per server Test_board_ask", WidgetId: wg.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	h.WaitRun(res.Msg.GetRunId())
	board := listDashboard(t, h, id)
	if res.Msg.GetChatId() == "" || res.Msg.GetChatId() != board.GetChatId() || board.GetCuratedAt() == "" {
		t.Fatalf("ask went to chat %q, board %+v", res.Msg.GetChatId(), board)
	}
	if user := h.LastUserEvent(res.Msg.GetChatId()); !strings.Contains(user.Body, "`spend`") || !strings.Contains(user.Body, "split it per server") {
		t.Fatalf("user turn %q", user.Body)
	}
	if !strings.Contains(h.RunBody(res.Msg.GetRunId()), "changed it") {
		t.Fatal("no reply")
	}
	// The ask never shows up as a chat of its own.
	chats, _ := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: id}))
	for _, c := range chats.Msg.GetChats() {
		if c.GetId() == res.Msg.GetChatId() {
			t.Fatal("the Dashboard log is listed as a chat")
		}
	}
}

func TestDashboardCardsGradualUpsertSubmitDismiss(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_94", dummy.Turn{Text: "I received your dashboard form submission"})
	h := apptest.New(t)
	bot := h.CreateBot("Steward")
	chat := h.FirstChat(bot.GetId())
	run, _ := h.Send(bot.GetId(), chat, "hello")
	h.WaitRun(run)
	h.LiveRun(run)

	wc := h.WorkerClient(bot.GetId())
	callTool := func(action, args string) *v1.ToolRes {
		t.Helper()
		res, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{Connector: "bot", Action: action, ArgsJson: args, RunId: run}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg
	}

	// 1. Create a card with key "hygiene:tmp" and modular form blocks.
	r1 := callTool("dashboard_upsert", `{
		"key": "hygiene:tmp",
				"title": "Workspace cleanup",
		"summary": "3 stale files in bot/",
		"priority": 1,
		"submit_label": "Apply cleanup",
		"blocks": [
			{"type": "switch", "name": "delete_tmp", "label": "Delete temp files", "default": true},
			{"type": "select", "name": "mode", "label": "Archive mode", "options": ["zip", "delete"], "default": "zip"}
		]
	}`)
	if r1.GetError() != "" || !strings.Contains(r1.GetResultJson(), `"created":true`) {
		t.Fatalf("dashboard_upsert create: %+v", r1)
	}

	// 2. Gradual update in place by key without wiping existing blocks or submit_label.
	r2 := callTool("dashboard_upsert", `{
		"key": "hygiene:tmp",
		"summary": "4 stale files in bot/ (Test_94_Input)"
	}`)
	if r2.GetError() != "" || !strings.Contains(r2.GetResultJson(), `"updated":true`) {
		t.Fatalf("dashboard_upsert update: %+v", r2)
	}

	// 3. List from Python and via UI RPC.
	rList := callTool("dashboard_list", `{}`)
	if rList.GetError() != "" || !strings.Contains(rList.GetResultJson(), "4 stale files in bot/") || !strings.Contains(rList.GetResultJson(), "Apply cleanup") {
		t.Fatalf("dashboard_list: %+v", rList)
	}

	uiList, err := h.Client.ListDashboard(h.Ctx(), connect.NewRequest(&v1.ListDashboardRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if len(uiList.Msg.GetCards()) != 1 || uiList.Msg.GetCards()[0].GetRead() {
		t.Fatalf("ListDashboard: %+v", uiList.Msg)
	}
	card := uiList.Msg.GetCards()[0]
	if card.GetTitle() != "Workspace cleanup" || card.GetSubmitLabel() != "Apply cleanup" || !strings.Contains(card.GetBlocksJson(), "delete_tmp") {
		t.Fatalf("card: %+v", card)
	}

	// 4. Submit card with form values -> opens chat with quote and starts run with serialized values.
	subRes, err := h.Client.SubmitDashboardCard(h.Ctx(), connect.NewRequest(&v1.SubmitDashboardCardRequest{
		BotId:      bot.GetId(),
		Id:         card.GetId(),
		ValuesJson: `{"delete_tmp":true,"mode":"zip"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	h.WaitRun(subRes.Msg.GetRunId())
	if body := h.RunBody(subRes.Msg.GetRunId()); !strings.Contains(body, "I received your dashboard form submission") {
		t.Fatalf("submitted run body: %q", body)
	}

	// Submitted card is no longer open.
	uiAfter, _ := h.Client.ListDashboard(h.Ctx(), connect.NewRequest(&v1.ListDashboardRequest{BotId: bot.GetId()}))
	if len(uiAfter.Msg.GetCards()) != 0 {
		t.Fatalf("cards after submit: %+v", uiAfter.Msg.GetCards())
	}

	// 5. Create another card and dismiss with suppress_kind = true -> records a lesson in memory.
	callTool("dashboard_upsert", `{"key":"tasks:1","title":"Unfinished spec","summary":"spec.md"}`)
	uiList2, _ := h.Client.ListDashboard(h.Ctx(), connect.NewRequest(&v1.ListDashboardRequest{BotId: bot.GetId()}))
	if len(uiList2.Msg.GetCards()) != 1 {
		t.Fatalf("expected 1 card, got %d", len(uiList2.Msg.GetCards()))
	}
	_, err = h.Client.DismissDashboardCard(h.Ctx(), connect.NewRequest(&v1.DismissDashboardCardRequest{
		BotId:        bot.GetId(),
		Id:           uiList2.Msg.GetCards()[0].GetId(),
		SuppressKind: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var lessons []db.Memory
	h.DB.Where("bot_id = ? AND kind = ?", bot.GetId(), "lesson").Find(&lessons)
	if len(lessons) != 1 || !strings.Contains(lessons[0].Content, "tasks:1") {
		t.Fatalf("expected lesson from suppress_kind, got %+v", lessons)
	}
}
