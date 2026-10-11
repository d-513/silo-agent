package app_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

func setLanguage(t *testing.T, h *apptest.H, code string) {
	t.Helper()
	if _, err := h.Client.SetLanguage(h.Ctx(), rq(&v1.SetLanguageRequest{Language: code})); err != nil {
		t.Fatalf("SetLanguage(%s): %v", code, err)
	}
}

func botNamed(t *testing.T, h *apptest.H, name string) *v1.Bot {
	t.Helper()
	res, err := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: name}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

// A user reads the console in English until they pick another language the
// console is translated into; the choice is theirs alone.
func TestSetLanguage(t *testing.T) {
	h := apptest.New(t)
	ann, _ := join(t, h, "ann@test.local", "first-password")
	if me, err := whoAmI(ann); err != nil || me.GetLanguage() != "en" {
		t.Fatalf("a new user reads %q: %v", me.GetLanguage(), err)
	}
	res, err := ann.SetLanguage(h.Ctx(), rq(&v1.SetLanguageRequest{Language: "pl"}))
	if err != nil || res.Msg.GetLanguage() != "pl" {
		t.Fatalf("SetLanguage(pl): %+v %v", res, err)
	}
	// A language a Bot can write in is not one the console speaks.
	for _, bad := range []string{"", "fr", "mixed", "PL"} {
		if _, err := ann.SetLanguage(h.Ctx(), rq(&v1.SetLanguageRequest{Language: bad})); code(err) != connect.CodeInvalidArgument {
			t.Errorf("language %q: %v", bad, err)
		}
	}
	if me, err := whoAmI(ann); err != nil || me.GetLanguage() != "pl" {
		t.Fatalf("Me after the change: %q %v", me.GetLanguage(), err)
	}
	if me, err := whoAmI(h.Client); err != nil || me.GetLanguage() != "en" {
		t.Fatalf("someone else's language moved: %q %v", me.GetLanguage(), err)
	}
	anon := h.NewClient()
	if _, err := anon.SetLanguage(h.Ctx(), rq(&v1.SetLanguageRequest{Language: "de"})); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("signed out: %v", err)
	}
}

// A new Bot writes in the language its maker reads the console in. A later
// change of the display language leaves the Bots that exist alone.
func TestBotStartsInItsMakersLanguage(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	if got := botNamed(t, h, "First").GetLanguage(); got != "en" {
		t.Fatalf("a Bot of an English reader writes %q", got)
	}
	setLanguage(t, h, "de")
	second := botNamed(t, h, "Zweiter")
	if second.GetLanguage() != "de" {
		t.Fatalf("a Bot of a German reader writes %q", second.GetLanguage())
	}
	setLanguage(t, h, "pl")
	got, err := h.Client.GetBot(h.Ctx(), rq(&v1.GetBotRequest{Id: second.GetId()}))
	if err != nil || got.Msg.GetLanguage() != "de" {
		t.Fatalf("the Bot followed the display language: %q %v", got.Msg.GetLanguage(), err)
	}
}

// The owner sets the language in Settings: any language Silo knows, not only
// the console's, or Mixed. Other saves of the Bot leave it alone.
func TestUpdateBotLanguage(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := botNamed(t, h, "Polyglot").GetId()
	update := func(m *v1.UpdateBotRequest) (*v1.Bot, error) {
		m.Id, m.Name = id, "Polyglot"
		res, err := h.Client.UpdateBot(h.Ctx(), rq(m))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	for _, want := range []string{"ja", "mixed", "pl"} {
		if b, err := update(&v1.UpdateBotRequest{Language: &want}); err != nil || b.GetLanguage() != want {
			t.Fatalf("language %q: %+v %v", want, b.GetLanguage(), err)
		}
	}
	if b, err := update(&v1.UpdateBotRequest{Description: "an unrelated save"}); err != nil || b.GetLanguage() != "pl" {
		t.Fatalf("an unrelated save moved the language: %q %v", b.GetLanguage(), err)
	}
	for _, bad := range []string{"", "xx", "Polish"} {
		if _, err := update(&v1.UpdateBotRequest{Language: &bad}); code(err) != connect.CodeInvalidArgument {
			t.Errorf("language %q: %v", bad, err)
		}
	}
	// A Bot from before languages has none and reads as Mixed.
	if err := h.DB.Model(&db.Bot{}).Where("id = ?", id).Update("language", "").Error; err != nil {
		t.Fatal(err)
	}
	got, err := h.Client.GetBot(h.Ctx(), rq(&v1.GetBotRequest{Id: id}))
	if err != nil || got.Msg.GetLanguage() != "mixed" {
		t.Fatalf("a Bot with no language reads %q: %v", got.Msg.GetLanguage(), err)
	}
}

// The model is told the Bot's language in the Bot's own tier of the prompt
// (never in the base, which every Bot shares), and so is the title model.
func TestLanguageReachesTheModel(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	setLanguage(t, h, "pl")
	id := botNamed(t, h, "Polka").GetId()
	h.WaitRun(sendFrom(t, h, id, h.FirstChat(id), "dzień dobry", ""))

	reqs := dummy.Streamed()
	sys := reqs[len(reqs)-1].System
	if strings.Contains(sys[0].Text, "Write to people in") {
		t.Fatal("the language is in the base prompt, which every Bot shares")
	}
	if !strings.Contains(systemText(reqs[len(reqs)-1]), "Write to people in Polish") {
		t.Fatalf("the prompt does not name the language:\n%s", sys[1].Text)
	}
	titled := false
	for _, req := range dummy.Completed() {
		if len(req.System) == 1 && strings.Contains(req.System[0].Text, "chat title") {
			titled = true
			if !strings.Contains(req.System[0].Text, "Write it in Polish.") {
				t.Fatalf("the title model is not told the language: %q", req.System[0].Text)
			}
		}
	}
	if !titled {
		t.Fatal("no title request")
	}

	mixed := "mixed"
	if _, err := h.Client.UpdateBot(h.Ctx(), rq(&v1.UpdateBotRequest{Id: id, Name: "Polka", Language: &mixed})); err != nil {
		t.Fatal(err)
	}
	chat, err := h.Client.CreateChat(h.Ctx(), rq(&v1.CreateChatRequest{BotId: id}))
	if err != nil {
		t.Fatal(err)
	}
	h.WaitRun(sendFrom(t, h, id, chat.Msg.GetId(), "hello", ""))
	reqs = dummy.Streamed()
	if got := systemText(reqs[len(reqs)-1]); strings.Contains(got, "Write to people in Polish") || !strings.Contains(got, "in the language they write to you in") {
		t.Fatalf("a Mixed Bot's prompt:\n%s", got)
	}
}
