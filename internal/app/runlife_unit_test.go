package app

import (
	"context"
	"testing"
	"time"

	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
	"silo.agent/internal/ids"
	"silo.agent/internal/masker"
)

// lifeApp is an App with just the run bookkeeping and a database.
func lifeApp(t *testing.T) *App {
	t.Helper()
	return &App{DB: dbtest.New(t), Bus: newBus(), runs: map[string]*liveRun{}, approvals: map[string]*waiter{}, cmdRun: map[string]string{}, mask: map[string]*masker.Masker{}}
}

// liveFor tracks a run of chat on bot, as StartRun does, with its row.
func liveFor(t *testing.T, a *App, bot, chat string) (string, *liveRun, context.Context) {
	t.Helper()
	id := ids.New()
	ctx, cancel := context.WithCancel(context.Background())
	lr := &liveRun{cancel: cancel, botID: bot, chatID: chat, inbox: make(chan inboxMsg, 4), done: make(chan struct{})}
	if !a.track(id, lr) {
		cancel()
		return "", nil, nil
	}
	if err := a.DB.Create(&db.Run{ID: id, BotID: bot, ChatID: chat, Origin: "chat", Status: "running", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	return id, lr, ctx
}

func rowStatus(a *App, id string) string {
	var r db.Run
	a.DB.Select("status").Where("id = ?", id).Limit(1).Find(&r)
	return r.Status
}

// A conversation has one live run: a second is refused, not run beside it,
// until the first retires.
func TestOneLiveRunPerConversation(t *testing.T) {
	a := lifeApp(t)
	first, lr, _ := liveFor(t, a, "b", "c")
	if id, _, _ := liveFor(t, a, "b", "c"); id != "" {
		t.Fatal("a second live run was tracked for the conversation")
	}
	if id, _, _ := liveFor(t, a, "b", "other"); id == "" {
		t.Fatal("another conversation's run was refused")
	}
	// Retiring needs an empty inbox when the run asks for it.
	lr.inbox <- inboxMsg{text: "waiting"}
	if a.retire(first, lr, true) {
		t.Fatal("a run with a message waiting retired")
	}
	<-lr.inbox
	if !a.retire(first, lr, true) {
		t.Fatal("an empty inbox did not retire")
	}
	if a.inject("b", "c", first, "late", run.Extra{}, "") {
		t.Fatal("a retired run took a message")
	}
	if a.LiveRunID("b", "c") != "" {
		t.Fatal("a retired run is still the conversation's live run")
	}
	if a.trackedRun(first) != lr {
		t.Fatal("a retired run is no longer tracked while it is ending")
	}
	if id, _, _ := liveFor(t, a, "b", "c"); id == "" {
		t.Fatal("a new run was refused while the old one was only ending")
	}
	a.runExited(first)
	if a.trackedRun(first) != nil {
		t.Fatal("an exited run is still tracked")
	}
}

// Stop settles the runs it stops and nothing else: a run already past its
// last turn finishes with the status it earned.
func TestStopSparesAnEndingRun(t *testing.T) {
	a := lifeApp(t)
	old, oldLR, oldCtx := liveFor(t, a, "b", "c")
	a.retire(old, oldLR, true)
	cur, curLR, curCtx := liveFor(t, a, "b", "c")
	a.cancelRuns("b", "c", "stopped")
	if curCtx.Err() == nil || curLR.stoppedAs() != "stopped" || rowStatus(a, cur) != "stopped" {
		t.Fatalf("the live run: ctx %v, stoppedAs %q, row %q", curCtx.Err(), curLR.stoppedAs(), rowStatus(a, cur))
	}
	if oldCtx.Err() != nil || oldLR.stoppedAs() != "" || rowStatus(a, old) != "running" {
		t.Fatalf("the ending run was touched: ctx %v, stoppedAs %q, row %q", oldCtx.Err(), oldLR.stoppedAs(), rowStatus(a, old))
	}
	// The first reason stands.
	curLR.stopWith("interrupted")
	if curLR.stoppedAs() != "stopped" {
		t.Fatalf("stoppedAs %q", curLR.stoppedAs())
	}
}

// A stop that lands between an approval's waiter and its row settles the
// waiter before the row exists; the row must not stay pending with nobody
// waiting on it.
func TestCanceledWaiterSettlesItsApproval(t *testing.T) {
	a := lifeApp(t)
	bot := &db.Bot{ID: ids.New()}
	errc := make(chan error, 1)
	go func() {
		_, err := a.AuthorizeAction(context.Background(), bot, "", "automations", "create", `{}`, "")
		errc <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.mu.Lock()
		var w *waiter
		for id, x := range a.approvals {
			w = x
			delete(a.approvals, id)
		}
		a.mu.Unlock()
		if w != nil {
			// What cancelRuns does when its database update came first.
			w.ch <- "canceled"
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no approval was asked")
		}
		time.Sleep(time.Millisecond)
	}
	if err := <-errc; err == nil || err.Error() != "canceled" {
		t.Fatalf("err = %v", err)
	}
	var rows []db.Approval
	a.DB.Where("bot_id = ?", bot.ID).Find(&rows)
	if len(rows) != 1 || rows[0].Status != "canceled" {
		t.Fatalf("approvals %+v", rows)
	}
}
