package app_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/run"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

func retry(h *apptest.H, botID, chatID string) (string, error) {
	res, err := h.Client.RetryRun(h.Ctx(), connect.NewRequest(&v1.RetryRunRequest{BotId: botID, ChatId: chatID}))
	if err != nil {
		return "", err
	}
	return res.Msg.GetRunId(), nil
}

// A reply that failed is answered again by Retry, from the history as it
// stands: no message is written twice and nothing is taken back, so the
// model is sent what the failed turn was sent.
func TestRetryAnswersAFailedReply(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_rt_once", dummy.Turn{Fail: "provider fell over", Fails: 1, Text: "answered after all"})
	h := apptest.New(t)
	bot := h.CreateBot("Retry")
	chat := h.FirstChat(bot.GetId())
	failed, _ := h.Send(bot.GetId(), chat, "Test_rt_once please")
	h.WaitRun(failed)
	if got := runStatus(t, h, failed); got != "error" {
		t.Fatalf("first run = %q, want error", got)
	}
	before := dummy.Streamed()

	again, err := retry(h, bot.GetId(), chat)
	if err != nil {
		t.Fatal(err)
	}
	evs := h.WaitRun(again)
	if got := doneBody(evs); got != "done" {
		t.Fatalf("retry done = %q", got)
	}
	if !strings.Contains(h.RunBody(again), "answered after all") {
		t.Fatalf("the retry did not answer: %v", kinds(evs))
	}
	if _, ok := find(evs, "user"); ok {
		t.Fatal("the retry wrote the message again")
	}
	var r db.Run
	h.DB.Where("id = ?", again).Find(&r)
	if r.Trigger != run.TriggerRetry || r.Origin != "chat" {
		t.Fatalf("retry run: trigger %q origin %q", r.Trigger, r.Origin)
	}
	after := dummy.Streamed()
	if len(after) != len(before)+1 {
		t.Fatalf("model asked %d times by the retry", len(after)-len(before))
	}
	was, now := before[len(before)-1].Messages, after[len(after)-1].Messages
	if len(was) != len(now) {
		t.Fatalf("the retry sent %d messages, the failed turn %d", len(now), len(was))
	}
	for i := range was {
		if was[i].Role != now[i].Role || was[i].Text != now[i].Text {
			t.Fatalf("message %d differs: %q vs %q", i, was[i].Text, now[i].Text)
		}
	}

	// It answered: there is nothing left to retry.
	if _, err := retry(h, bot.GetId(), chat); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("a second retry: %v", err)
	}
}

// Retry is for a reply that failed, not a way to make the Bot speak again.
func TestRetryRefusedWhenNothingFailed(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("NoRetry")
	chat := h.FirstChat(bot.GetId())
	if _, err := retry(h, bot.GetId(), chat); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("retry of an empty chat: %v", err)
	}
	ok, _ := h.Send(bot.GetId(), chat, "Test_rt_fine hello")
	h.WaitRun(ok)
	if _, err := retry(h, bot.GetId(), chat); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("retry after a reply: %v", err)
	}
}

// A model switched after the failure (/model, the picker) answers the retry:
// the command's run asks no model and does not hide the failure.
func TestRetryAfterSwitchingModel(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_rt_switch", dummy.Turn{Fail: "provider fell over", Fails: 1, Text: "the other model answered"})
	h := apptest.New(t)
	bot := h.CreateBot("RetrySwitch")
	chat := h.FirstChat(bot.GetId())
	failed, _ := h.Send(bot.GetId(), chat, "Test_rt_switch go")
	h.WaitRun(failed)
	if _, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: bot.GetId(), ChatId: chat, Text: "/model"})); err != nil {
		t.Fatal(err)
	}
	again, err := retry(h, bot.GetId(), chat)
	if err != nil {
		t.Fatal(err)
	}
	h.WaitRun(again)
	if !strings.Contains(h.RunBody(again), "the other model answered") {
		t.Fatal("the retry did not answer")
	}
}
