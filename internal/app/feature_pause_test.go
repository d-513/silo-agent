package app_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

// The owner pausing the desktop refuses the Bot's desktop actions and its
// Chromium on both surfaces, says the owner is in control, and asks nobody;
// look still works, and resuming hands the desktop back.
func TestPausedDesktopRefusesTheBot(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Paused")
	id := bot.GetId()

	setPaused := func(on bool) {
		t.Helper()
		res, err := h.Client.SetDesktopPaused(h.Ctx(), connect.NewRequest(&v1.SetDesktopPausedRequest{BotId: id, Paused: on}))
		if err != nil || res.Msg.GetPaused() != on {
			t.Fatalf("SetDesktopPaused(%v): %v %+v", on, err, res)
		}
		got, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id}))
		if err != nil || got.Msg.GetDesktopPaused() != on {
			t.Fatalf("GetBot desktop_paused = %v, want %v (%v)", got.Msg.GetDesktopPaused(), on, err)
		}
	}
	setPaused(true)
	setPaused(true) // idempotent

	for _, c := range []struct{ action, args string }{
		{"click", `{"x":1,"y":2}`}, {"type", `{}`}, {"key", `{"name":"Return"}`},
		{"scroll", `{"x":1,"y":2,"dy":1}`}, {"chrome", `{}`},
	} {
		res := pyCall(t, h, id, "", "desktop", c.action, c.args)
		if !strings.Contains(res.GetError(), "in control") {
			t.Fatalf("paused desktop.%s from Python: %+v", c.action, res)
		}
	}
	if res := pyCall(t, h, id, "", "desktop", "look", `{}`); res.GetError() != "" {
		t.Fatalf("look while paused: %+v", res)
	}

	// The chat surface, and a Playwright cell, are refused the same way.
	var row db.Bot
	h.DB.First(&row, "id = ?", id)
	for _, c := range []struct {
		name string
		args map[string]any
	}{
		{"click", map[string]any{"x": 1, "y": 2}},
		{"exec_python", map[string]any{"code": "from silo_runtime import chrome_page\npage = chrome_page()"}},
	} {
		d, ok := h.App.Tools.ByName(id, c.name)
		if !ok {
			t.Fatalf("no %s tool", c.name)
		}
		_, err := h.App.Tools.Run(h.Ctx(), &tool.Call{Surface: tool.Completion, Bot: &row, Def: d, Args: c.args})
		if err == nil || !strings.Contains(err.Error(), "in control") {
			t.Fatalf("paused %s from chat: %v", c.name, err)
		}
	}
	var asked int64
	h.DB.Model(&db.Approval{}).Where("bot_id = ?", id).Count(&asked)
	if asked != 0 {
		t.Fatalf("a paused desktop raised %d approvals", asked)
	}

	setPaused(false)
	if res := pyCall(t, h, id, "", "desktop", "click", `{"x":1,"y":2}`); res.GetError() != "" {
		t.Fatalf("click after resume: %+v", res)
	}
}

// Only the owner holds the switch.
func TestPauseIsTheOwners(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Mine")
	other, _ := h.SignedInUser("other@example.com")
	if _, err := other.SetDesktopPaused(h.Ctx(), connect.NewRequest(&v1.SetDesktopPausedRequest{BotId: bot.GetId(), Paused: true})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("another user paused it: %v", err)
	}
	if h.App.Pause.Paused(bot.GetId()) {
		t.Fatal("paused by a stranger")
	}
}
