package app_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

func userEvents(events []db.RunEvent) []db.RunEvent {
	var out []db.RunEvent
	for _, ev := range events {
		if ev.Kind == "user" {
			out = append(out, ev)
		}
	}
	return out
}

func TestEditLastUserMessage(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_60",
		dummy.Turn{Text: "first answer"},
		dummy.Turn{Text: "second answer"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Edit")
	chat := h.FirstChat(bot.GetId())

	run1, _ := h.Send(bot.GetId(), chat, "Test_60 one")
	h.WaitRun(run1)
	run2, _ := h.Send(bot.GetId(), chat, "Test_60 two")
	h.WaitRun(run2)

	last := h.LastUserEvent(chat)
	if last.Body != "Test_60 two" {
		t.Fatalf("last user %q", last.Body)
	}
	newRun := h.EditMessage(bot.GetId(), chat, last.ID, "Test_60 two edited")
	if newRun == run2 {
		t.Fatal("edit should start a replacement run")
	}
	h.WaitRun(newRun)

	users := userEvents(h.ChatEvents(chat))
	if len(users) != 2 || users[0].Body != "Test_60 one" || users[1].Body != "Test_60 two edited" {
		t.Fatalf("users after edit: %+v", users)
	}
	var old int64
	h.DB.Model(&db.RunEvent{}).Where("run_id = ?", run2).Count(&old)
	if old != 0 {
		t.Fatalf("old run events not truncated: %d", old)
	}
	var runs int64
	h.DB.Model(&db.Run{}).Where("chat_id = ?", chat).Count(&runs)
	if runs != 2 {
		t.Fatalf("expected 2 runs after edit, got %d", runs)
	}
}

func TestDeleteLastUserMessage(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_61",
		dummy.Turn{Text: "answer one"},
		dummy.Turn{Text: "answer two"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Delete")
	chat := h.FirstChat(bot.GetId())

	run1, _ := h.Send(bot.GetId(), chat, "Test_61 one")
	h.WaitRun(run1)
	run2, _ := h.Send(bot.GetId(), chat, "Test_61 two")
	h.WaitRun(run2)

	last := h.LastUserEvent(chat)
	h.DeleteMessage(bot.GetId(), chat, last.ID)

	users := userEvents(h.ChatEvents(chat))
	if len(users) != 1 || users[0].Body != "Test_61 one" {
		t.Fatalf("users after delete: %+v", users)
	}
	var runs int64
	h.DB.Model(&db.Run{}).Where("chat_id = ?", chat).Count(&runs)
	if runs != 1 {
		t.Fatalf("expected 1 run after delete, got %d", runs)
	}
	var gone int64
	h.DB.Model(&db.RunEvent{}).Where("run_id = ?", run2).Count(&gone)
	if gone != 0 {
		t.Fatalf("second run events remain: %d", gone)
	}
}

func TestEditRejectsEarlierMessage(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_62",
		dummy.Turn{Text: "a"},
		dummy.Turn{Text: "b"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Reject")
	chat := h.FirstChat(bot.GetId())

	run1, _ := h.Send(bot.GetId(), chat, "Test_62 one")
	h.WaitRun(run1)
	run2, _ := h.Send(bot.GetId(), chat, "Test_62 two")
	h.WaitRun(run2)

	first := userEvents(h.ChatEvents(chat))[0]
	_, err := h.Client.EditMessage(h.Ctx(), connect.NewRequest(&v1.EditMessageRequest{
		BotId: bot.GetId(), ChatId: chat, EventId: first.ID, Text: "nope",
	}))
	if err == nil {
		t.Fatal("editing a non-last message should fail")
	}
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("code = %v", connect.CodeOf(err))
	}

	_, err = h.Client.DeleteMessage(h.Ctx(), connect.NewRequest(&v1.DeleteMessageRequest{
		BotId: bot.GetId(), ChatId: chat, EventId: first.ID,
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("delete code = %v", connect.CodeOf(err))
	}
}

func TestDivergeChat(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_63",
		dummy.Turn{Text: "a"},
		dummy.Turn{Text: "b"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Diverge")
	chat := h.FirstChat(bot.GetId())

	run1, _ := h.Send(bot.GetId(), chat, "Test_63 one")
	h.WaitRun(run1)
	run2, _ := h.Send(bot.GetId(), chat, "Test_63 two")
	h.WaitRun(run2)

	srcBefore := h.ChatEvents(chat)
	last := h.LastUserEvent(chat)

	nc := h.DivergeChat(bot.GetId(), chat, last.ID)
	if nc.GetId() == "" || nc.GetId() == chat {
		t.Fatalf("bad new chat: %+v", nc)
	}
	prefix := h.ChatEvents(nc.GetId())
	if len(prefix) == 0 {
		t.Fatal("expected a non-empty copied prefix")
	}
	users := userEvents(prefix)
	if len(users) != 1 || users[0].Body != "Test_63 one" {
		t.Fatalf("prefix users: %+v", users)
	}
	var assistants int
	for _, ev := range prefix {
		if ev.Kind == "assistant" {
			assistants++
		}
	}
	if assistants != 1 {
		t.Fatalf("prefix assistant events = %d", assistants)
	}
	if prefix[0].ID == srcBefore[0].ID {
		t.Fatal("copied events must get fresh ids")
	}
	if got := h.ChatEvents(chat); len(got) != len(srcBefore) {
		t.Fatalf("source chat changed: %d -> %d", len(srcBefore), len(got))
	}

	first := userEvents(srcBefore)[0]
	empty := h.DivergeChat(bot.GetId(), chat, first.ID)
	if n := len(h.ChatEvents(empty.GetId())); n != 0 {
		t.Fatalf("diverging the first message should be empty, got %d events", n)
	}
}

func TestDeleteStopsLiveRun(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_64",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"sleep 2"}`}}},
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("StopLive")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())

	h.Send(bot.GetId(), chat, "Test_64 go")

	var ev db.RunEvent
	deadline := time.Now().Add(5 * time.Second)
	for {
		ev = h.LastUserEvent(chat)
		if ev.Body == "Test_64 go" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("user event was not persisted")
		}
		time.Sleep(10 * time.Millisecond)
	}

	h.DeleteMessage(bot.GetId(), chat, ev.ID)

	if got := len(h.ChatEvents(chat)); got != 0 {
		t.Fatalf("expected empty chat after deleting the only message, got %d events", got)
	}
	// Give a stray final write a moment to show up.
	time.Sleep(300 * time.Millisecond)
	if got := len(h.ChatEvents(chat)); got != 0 {
		t.Fatalf("stopped run wrote after truncation: %d events", got)
	}
}

func TestEditMessageOwnership(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_65", dummy.Turn{Text: "hi"})
	h := apptest.New(t)
	bot := h.CreateBot("Own")
	chat := h.FirstChat(bot.GetId())
	run, _ := h.Send(bot.GetId(), chat, "Test_65 hi")
	h.WaitRun(run)

	last := h.LastUserEvent(chat)
	if _, err := h.Client.EditMessage(h.Ctx(), connect.NewRequest(&v1.EditMessageRequest{
		BotId: bot.GetId(), ChatId: "does-not-exist", EventId: last.ID, Text: "x",
	})); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown chat err = %v", err)
	}
}
