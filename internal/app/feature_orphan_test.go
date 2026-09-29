package app_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

// A worker-side call is attributed to a chat only while its run is live; a
// finished or missing run makes it an orphan that no thread records.
func TestCallAttributionLiveVsOrphan(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Orphans")
	id := bot.GetId()
	chat := h.FirstChat(id)
	run, _ := h.Send(id, chat, "hello")
	h.WaitRun(run)

	calls := func(runID string) int64 {
		var n int64
		h.DB.Model(&db.RunEvent{}).Where("run_id = ? AND kind IN ?", runID, []string{"call", "call_result"}).Count(&n)
		return n
	}

	// Finished run (a background process that outlived its command): the call
	// still runs, but lands in no chat and is never persisted.
	if res := pyCall(t, h, id, run, "desktop", "click", `{"x":1,"y":2}`); res.GetError() != "" {
		t.Fatalf("orphan click: %+v", res)
	}
	if n := calls(run); n != 0 {
		t.Fatalf("finished run got %d call events", n)
	}
	if n := calls(""); n != 0 {
		t.Fatalf("orphan events persisted: %d", n)
	}

	// Another Bot's run id is an orphan too.
	other := h.CreateBot("Other")
	otherRun, _ := h.Send(other.GetId(), h.FirstChat(other.GetId()), "hi")
	h.WaitRun(otherRun)
	h.LiveRun(otherRun)
	if res := pyCall(t, h, id, otherRun, "desktop", "click", `{"x":1,"y":2}`); res.GetError() != "" {
		t.Fatalf("foreign run click: %+v", res)
	}
	if n := calls(otherRun); n != 0 {
		t.Fatalf("foreign run got %d call events", n)
	}

	// A live run owns its calls.
	h.LiveRun(run)
	if res := pyCall(t, h, id, run, "desktop", "click", `{"x":1,"y":2}`); res.GetError() != "" {
		t.Fatalf("live click: %+v", res)
	}
	if n := calls(run); n != 2 {
		t.Fatalf("live run call events %d, want call + call_result", n)
	}

	// An orphan cannot emit an artifact card: there is no thread to hold it.
	h.DB.Model(&db.Run{}).Where("id = ?", run).Update("status", "done")
	if res := pyCall(t, h, id, run, "artifact", "emit", `{"path":"x.txt"}`); res.GetError() == "" {
		t.Fatal("orphan artifact should fail")
	}
}

// A chat's stream never shows an orphan's live events.
func TestOrphanEventsSkipChatStream(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Stream")
	id := bot.GetId()
	chat := h.FirstChat(id)
	run, _ := h.Send(id, chat, "hello")
	h.WaitRun(run)

	ctx, cancel := context.WithTimeout(h.Ctx(), 10*time.Second)
	defer cancel()
	st, err := h.Client.StreamRun(ctx, connect.NewRequest(&v1.StreamRunRequest{BotId: id, ChatId: chat}))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// Drain the replay up to the finished run's done.
	for st.Receive() {
		if st.Msg().GetKind() == "done" {
			break
		}
	}

	pyCall(t, h, id, "", "desktop", "click", `{"x":1,"y":2}`)
	h.LiveRun(run)
	pyCall(t, h, id, run, "desktop", "scroll", `{"x":1,"y":2,"dy":1}`)

	for st.Receive() {
		ev := st.Msg()
		if ev.GetKind() != "call" {
			continue
		}
		if ev.GetTool() != "desktop.scroll" || ev.GetChatId() != chat {
			t.Fatalf("first call on the chat stream was %q (chat %q); the orphan leaked", ev.GetTool(), ev.GetChatId())
		}
		return
	}
	t.Fatalf("stream ended: %v", st.Err())
}

// Pending approvals come oldest first and say who asked; an orphan's
// approval is labelled a background process and still gates the call.
func TestApprovalSourceAndOrder(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Sources")
	id := bot.GetId()
	chat := h.FirstChat(id)
	run, _ := h.Send(id, chat, "hello")
	h.WaitRun(run)
	h.DB.Model(&db.Chat{}).Where("id = ?", chat).Update("title", "Planning")
	if _, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{BotId: id, Name: "pw", Value: "s3cret"})); err != nil {
		t.Fatal(err)
	}
	wc := h.WorkerClient(id)

	orphan := make(chan *v1.SecretRes, 1)
	go func() {
		res, err := wc.GetSecret(h.Ctx(), connect.NewRequest(&v1.SecretReq{Name: "pw", RunId: run}))
		if err != nil {
			t.Error(err)
		}
		orphan <- res.Msg
	}()
	first := h.WaitApproval(id)
	if first.GetRunId() != "" || first.GetSource() != "Background process" {
		t.Fatalf("orphan approval run=%q source=%q", first.GetRunId(), first.GetSource())
	}

	h.LiveRun(run)
	go func() {
		_, _ = wc.GetSecret(h.Ctx(), connect.NewRequest(&v1.SecretReq{Name: "pw", RunId: run}))
	}()
	deadline := time.Now().Add(10 * time.Second)
	var list []*v1.Approval
	for time.Now().Before(deadline) {
		res, err := h.Client.ListApprovals(h.Ctx(), connect.NewRequest(&v1.ListApprovalsRequest{BotId: id}))
		if err == nil && len(res.Msg.GetApprovals()) == 2 {
			list = res.Msg.GetApprovals()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(list) != 2 || list[0].GetId() != first.GetId() || list[1].GetSource() != "Chat · Planning" || list[1].GetRunId() != run {
		t.Fatalf("approvals %+v", list)
	}

	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: first.GetId(), Decision: "allow_once"})); err != nil {
		t.Fatal(err)
	}
	if got := <-orphan; got.GetValue() != "s3cret" {
		t.Fatalf("orphan secret after allow: %+v", got)
	}
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: list[1].GetId(), Decision: "deny"})); err != nil {
		t.Fatal(err)
	}
}
