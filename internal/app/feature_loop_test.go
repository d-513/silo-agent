package app_test

import (
	"strings"
	"testing"
	"time"

	"silo.agent/internal/apptest"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// A message sent while the model writes its final reply is answered in the
// same run, not left in an inbox nobody reads, and the next run replays the
// conversation exactly as this one sent it.
func TestMessageDuringFinalReplyIsAnswered(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_FT",
		dummy.Turn{Text: "Test_FT first reply", Delay: 600 * time.Millisecond},
		dummy.Turn{Text: "Test_FT second reply"},
	)
	h := apptest.New(t)
	id := h.CreateBot("Latecomer").GetId()
	chat := h.FirstChat(id)
	r1, _ := h.Send(id, chat, "Test_FT_Input start")
	deadline := time.Now().Add(10 * time.Second)
	for len(dummy.Streamed()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the first turn never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got, _ := h.Send(id, chat, "one more thing"); got != r1 {
		t.Fatalf("the message started run %s instead of joining %s", got, r1)
	}
	h.WaitRun(r1)
	var replies []string
	for _, e := range h.Events(r1) {
		if e.Kind == "assistant" {
			replies = append(replies, e.Body)
		}
	}
	if strings.Join(replies, "|") != "Test_FT first reply|Test_FT second reply" {
		t.Fatalf("replies %q", replies)
	}
	reqs := dummy.Streamed()
	last := reqs[len(reqs)-1].Messages
	if n := len(last); n < 3 || last[n-1].Role != llm.RoleUser || !strings.Contains(last[n-1].Text, "one more thing") {
		t.Fatalf("the second turn did not read the message last: %+v", last)
	}

	r2, _ := h.Send(id, chat, "afterwards")
	h.WaitRun(r2)
	assertAppendOnly(t, dummy.Streamed())
}

// A run that finished is not live: a message after it starts a new run.
func TestMessageAfterFinishStartsANewRun(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Twice").GetId()
	chat := h.FirstChat(id)
	r1, _ := h.Send(id, chat, "hello")
	h.WaitRun(r1)
	r2, _ := h.Send(id, chat, "again")
	if r2 == r1 {
		t.Fatal("a finished run took the message")
	}
	h.WaitRun(r2)
}
