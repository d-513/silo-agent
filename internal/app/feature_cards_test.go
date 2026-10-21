package app_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

// A question asked in a chat is a card there and ends the turn; the owner's
// answer follows the card into the same chat with no quote, and putting it
// away tells the Bot too.
func TestAskOwnerInChat(t *testing.T) {
	dummy.Reset()
	blocks := `[{"type":"segmented","name":"when","options":["morning","evening"]}]`
	dummy.Script("Test_ask1",
		memCall("ask_owner", `{"text":"When should I send your digest?","submit_label":"Save","blocks":`+blocks+`}`),
		memCall("ask_owner", `{"text":"And on weekends?"}`),
		dummy.Turn{Text: "fine, weekdays only"},
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

	var second db.DashboardCard
	h.DB.Where("bot_id = ? AND status = ?", id, "open").First(&second)
	if second.ID == "" || second.ID == c.ID {
		t.Fatalf("no second question: %+v", second)
	}
	if _, err := h.Client.DismissDashboardCard(h.Ctx(), connect.NewRequest(&v1.DismissDashboardCardRequest{BotId: id, Id: second.ID})); err != nil {
		t.Fatal(err)
	}
	waitUserTurn(t, h, main, "Not now for your question")
}

func waitUserTurn(t *testing.T, h *apptest.H, chat, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if ev, ok := h.FindLastUserEvent(chat); ok && strings.Contains(ev.Body, want) {
			return
		}
		if time.Now().After(deadline) {
			ev, _ := h.FindLastUserEvent(chat)
			t.Fatalf("the Bot was told %q, want %q", ev.Body, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// An offer is a card of its own: a catalog entry, with the form's values the
// Bot filled in. Nothing else can be filled in. The Bot hears a "not now"
// once the turn's other offers are answered, and a setup at once.
func TestOfferSetup(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_offer1",
		memCall("offer_setup", `{"kind":"connector","target":"https://evil.example/mcp"}`),
		memCall("offer_setup", `{"kind":"connector","target":"email","options":{"imap_host":"evil.example","password":"hunter2"}}`),
		memCall("offer_setup", `{"kind":"connector","target":"email","why":"to read your mail","options":{"address":"me@example.com","default_mode":"ask","expose_completion":true,"name":"My mail"}}`),
		memCall("offer_setup", `{"kind":"connector","target":"calendar"}`),
		memCall("offer_setup", `{"kind":"connector","target":"github"}`),
		dummy.Turn{Text: "These would let me read your mail and calendar."},
		dummy.Turn{Text: "noted"},
		dummy.Turn{Text: "noted again"},
	)
	h := apptest.New(t)
	id := h.CreateBot("Offerer").GetId()
	main := h.FirstChat(id)
	run, _ := h.Send(id, main, "Test_offer1_Input")
	var results []string
	said := false
	for _, ev := range h.WaitRun(run) {
		switch ev.Kind {
		case "tool_result":
			results = append(results, ev.Body)
		case "assistant", "section":
			said = true
		}
	}
	if len(results) != 5 || !strings.Contains(results[0], "no connector") || !strings.Contains(results[1], "cannot be filled in") {
		t.Fatalf("results %q", results)
	}
	if !said {
		t.Fatal("the Bot could not say what the offers are for")
	}
	var offers []db.SetupOffer
	h.DB.Where("bot_id = ?", id).Order("created_at").Find(&offers)
	if len(offers) != 3 {
		t.Fatalf("%d offers", len(offers))
	}
	mail, cal, gh := offers[0], offers[1], offers[2]
	if mail.ChatID != main || mail.Status != "open" || mail.Label == "" || !strings.Contains(mail.ValuesJSON, `"expose_completion":"true"`) || !strings.Contains(mail.ValuesJSON, `"address":"me@example.com"`) {
		t.Fatalf("offer %+v", mail)
	}
	if ev, ok := find(h.ChatEvents(main), "setup_offer"); !ok || ev.Body != mail.ID {
		t.Fatalf("no offer in the chat: %+v", ev)
	}
	resolve := func(o db.SetupOffer, outcome string, rest bool) {
		t.Helper()
		if _, err := h.Client.ResolveSetupOffer(h.Ctx(), connect.NewRequest(&v1.ResolveSetupOfferRequest{BotId: id, Id: o.ID, Outcome: outcome, Rest: rest})); err != nil {
			t.Fatal(err)
		}
	}
	users := func() int { return len(userBodies(h.ChatEvents(main))) }
	before := users()

	resolve(cal, "skipped", false)
	time.Sleep(100 * time.Millisecond)
	if users() != before {
		t.Fatal("a single not-now told the Bot while other offers were open")
	}
	resolve(mail, "done", false)
	waitUserTurn(t, h, main, "I set up **"+mail.Label+"**. Not now for **"+cal.Label+"**. Still open: **"+gh.Label+"**.")
	h.WaitRun(lastRun(h, main))

	resolve(gh, "skipped", true)
	waitUserTurn(t, h, main, "Not now for **"+gh.Label+"**.")
}

func lastRun(h *apptest.H, chat string) string {
	ev, _ := h.FindLastUserEvent(chat)
	return ev.RunID
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
