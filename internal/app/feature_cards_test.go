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

// A question asked in a chat is a card in that chat; a setup offer in it
// keeps only what Silo's catalog allows, and the owner's answer follows the
// card into the same chat with no quote.
func TestAskOwnerInChat(t *testing.T) {
	dummy.Reset()
	blocks := `[{"type":"segmented","name":"when","options":["morning","evening"]},` +
		`{"type":"setup","kind":"connector","target":"email","why":"to read your mail",` +
		`"prefill":{"address":"me@example.com","provider":"gmail","imap_host":"evil.example","imap_port":"1","password":"hunter2","made_up":"x"}}]`
	dummy.Script("Test_ask1",
		memCall("ask_owner", `{"text":"When should I send your digest?","submit_label":"Save","blocks":`+blocks+`}`),
		dummy.Turn{Text: "asked"},
		dummy.Turn{Text: "got your answer"},
		dummy.Turn{Text: "now I can read your mail"},
	)
	h := apptest.New(t)
	id := h.CreateBot("Asker").GetId()
	main := h.FirstChat(id)
	run, _ := h.Send(id, main, "Test_ask1_Input")
	for _, ev := range h.WaitRun(run) {
		if ev.Kind == "assistant" || ev.Kind == "section" {
			t.Fatalf("the Bot kept talking after its question: %q", ev.Body)
		}
	}

	var c db.DashboardCard
	h.DB.Where("bot_id = ?", id).First(&c)
	if c.ChatID != main || c.Status != "open" || c.Summary != "When should I send your digest?" {
		t.Fatalf("card %+v", c)
	}
	var got []map[string]any
	_ = json.Unmarshal([]byte(c.BlocksJSON), &got)
	setup := got[1]
	prefill, _ := setup["prefill"].(map[string]any)
	if setup["label"] == "" || len(prefill) != 2 || prefill["address"] != "me@example.com" || prefill["provider"] != "gmail" {
		t.Fatalf("setup block kept %+v", setup)
	}
	if ev, ok := find(h.ChatEvents(main), "card"); !ok || ev.Body != c.ID {
		t.Fatalf("no card event in the chat: %+v", ev)
	}

	sub, err := h.Client.SubmitDashboardCard(h.Ctx(), connect.NewRequest(&v1.SubmitDashboardCardRequest{BotId: id, Id: c.ID, ValuesJson: `{"when":"morning"}`}))
	if err != nil {
		t.Fatal(err)
	}
	if sub.Msg.GetChat().GetId() != main {
		t.Fatalf("answered in %q, want the chat the card is in", sub.Msg.GetChat().GetId())
	}
	h.WaitRun(sub.Msg.GetRunId())
	for _, ev := range h.ChatEvents(main) {
		if ev.Kind == "feed_quote" {
			t.Fatal("an answer in the card's own chat quoted the card again")
		}
	}

	res, err := h.Client.ResolveCardSetup(h.Ctx(), connect.NewRequest(&v1.ResolveCardSetupRequest{BotId: id, Id: c.ID, Index: 1, Outcome: "done"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Msg.GetSetupJson(), `"1":"done"`) {
		t.Fatalf("setup state %q", res.Msg.GetSetupJson())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		user := h.LastUserEvent(main)
		if strings.Contains(user.Body, "I set up **") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the Bot was told %q", user.Body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := h.Client.ResolveCardSetup(h.Ctx(), connect.NewRequest(&v1.ResolveCardSetupRequest{BotId: id, Id: c.ID, Index: 0, Outcome: "done"})); err == nil {
		t.Fatal("a form control resolved as a setup")
	}
}

// A setup offer names a catalog entry or nothing.
func TestAskOwnerRefusesUnknownSetup(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_ask2",
		memCall("ask_owner", `{"text":"Connect this?","blocks":[{"type":"setup","kind":"connector","target":"https://evil.example/mcp"}]}`),
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t)
	id := h.CreateBot("Careful").GetId()
	run, _ := h.Send(id, h.FirstChat(id), "Test_ask2_Input")
	evs := h.WaitRun(run)
	res, _ := find(evs, "tool_result")
	if !strings.Contains(res.Body, "no connector") {
		t.Fatalf("result %q", res.Body)
	}
	var n int64
	h.DB.Model(&db.DashboardCard{}).Where("bot_id = ?", id).Count(&n)
	if n != 0 {
		t.Fatal("a card with an unknown target was saved")
	}
}

// From an automation the question goes to the main chat only when the owner
// lets the Bot write there; it waits on the Dashboard either way.
func TestAskOwnerFromAutomation(t *testing.T) {
	for _, level := range []string{"quiet", "active"} {
		t.Run(level, func(t *testing.T) {
			dummy.Reset()
			dummy.Script("Test_ask3",
				memCall("ask_owner", `{"text":"Renew the domain?"}`),
				dummy.Turn{Text: "asked"},
			)
			h := apptest.New(t)
			id := h.CreateBot("Background").GetId()
			h.DB.Model(&db.Bot{}).Where("id = ?", id).Update("proactivity", level)
			au, err := h.Client.CreateAutomation(h.Ctx(), connect.NewRequest(&v1.CreateAutomationRequest{BotId: id, Name: "Check", Prompt: "Test_ask3_Input", Schedule: "0 9 * * *"}))
			if err != nil {
				t.Fatal(err)
			}
			r, err := h.Client.RunAutomation(h.Ctx(), connect.NewRequest(&v1.RunAutomationRequest{BotId: id, Id: au.Msg.GetId()}))
			if err != nil {
				t.Fatal(err)
			}
			h.WaitRun(r.Msg.GetRunId())
			main := h.FirstChat(id)
			var c db.DashboardCard
			h.DB.Where("bot_id = ?", id).First(&c)
			ev, quoted := find(h.ChatEvents(main), "feed_quote")
			if level == "active" && (!quoted || c.ChatID != main || !strings.Contains(ev.Meta, c.ID)) {
				t.Fatalf("active: card %+v quoted=%v", c, quoted)
			}
			if level == "quiet" && (quoted || c.ChatID == main) {
				t.Fatalf("quiet: the Bot wrote into the main chat")
			}
			if c.Status != "open" {
				t.Fatalf("card %+v", c)
			}
		})
	}
}
