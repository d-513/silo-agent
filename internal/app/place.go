package app

import (
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
)

// Place puts p into the Bot's main chat when it is idle (and, for a
// proactive placement, the Bot may write there unasked), else into a chat of
// its own. The conversation lock is held throughout: a run starting in the
// main chat between the check and the write would read a history without
// what was written and replay one with it.
func (a *App) Place(p run.Placement) (run.Placed, error) {
	a.convMu.Lock()
	defer a.convMu.Unlock()
	// Whose main chat: the person who pressed; for the Bot's own doing,
	// the one person on it. With several, the Bot writes in nobody's main
	// chat unasked: it opens a chat of its own, or waits on the Dashboard.
	userID := p.UserID
	if userID == "" {
		if people := access.People(a.DB, p.BotID); len(people) == 1 {
			userID = people[0]
		}
	}
	var main *db.Chat
	if userID != "" {
		m, err := chats.Main(a.DB, p.BotID, userID)
		if err != nil {
			return run.Placed{}, err
		}
		main = m
	}
	out := run.Placed{Chat: main, Main: main != nil && !p.Own && a.LiveRunID(p.BotID, main.ID) == "" && (!p.Proactive || a.Proactive(p.BotID))}
	if !out.Main {
		if p.Title == "" {
			return run.Placed{}, run.ErrNotMain
		}
		var err error
		if out.Chat, err = chats.New(a.DB, p.BotID, p.UserID, p.Title); err != nil {
			return run.Placed{}, err
		}
	}
	var ev *db.RunEvent
	var err error
	if p.Write != nil {
		if ev, err = p.Write(out.Chat); err != nil {
			a.unplace(out, nil)
			return run.Placed{}, err
		}
		a.Bus.Publish(p.BotID, a.threadEvent(ev, out.Chat.ID))
	}
	if p.Message != "" {
		if out.RunID, err = a.StartRun(run.Request{BotID: p.BotID, ChatID: out.Chat.ID, Text: p.Message, From: p.From}); err != nil {
			// A disabled owner, a Bot that is gone: nothing answers it.
			a.unplace(out, ev)
			return run.Placed{}, err
		}
	}
	return out, nil
}

// unplace takes back what Place wrote: the chat of its own, or in the main
// chat the run the event was written in.
func (a *App) unplace(out run.Placed, ev *db.RunEvent) {
	switch {
	case !out.Main:
		chats.Drop(a.DB, out.Chat.ID)
	case ev != nil:
		chats.DropRun(a.DB, ev.RunID)
	}
}
