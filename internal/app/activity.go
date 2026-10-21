package app

import (
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
)

// setStep records the tool a live run is in now, for the Bot's status line.
func (a *App) setStep(runID, step string) {
	a.mu.Lock()
	if lr := a.runs[runID]; lr != nil {
		lr.step = step
	}
	a.mu.Unlock()
}

// EndTurn ends a live run after the tool calls of its current turn: the
// tool has handed the conversation to the human, and another model turn would
// only talk about the question it just asked.
func (a *App) EndTurn(runID string) {
	a.mu.Lock()
	if lr := a.runs[runID]; lr != nil {
		lr.endTurn = true
	}
	a.mu.Unlock()
}

// turnEnded reports (and clears) a run's EndTurn.
func (a *App) turnEnded(runID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	lr := a.runs[runID]
	if lr == nil || !lr.endTurn {
		return false
	}
	lr.endTurn = false
	return true
}

// activity is what a Bot is doing right now: one row per live run, with
// where it runs and the tool it is in. It rides on the polled Bot row, so the
// sidebar can say "Researching · 2 agents" without a stream of its own.
func (a *App) activity(botID string) []*v1.BotActivity {
	type live struct{ chatID, step string }
	var runs []live
	a.mu.Lock()
	for _, lr := range a.runs {
		if lr.botID == botID && lr.chatID != "" {
			runs = append(runs, live{lr.chatID, lr.step})
		}
	}
	a.mu.Unlock()
	if len(runs) == 0 || a.DB == nil {
		return nil
	}
	ids := make([]string, len(runs))
	for i, r := range runs {
		ids[i] = r.chatID
	}
	var chats []db.Chat
	a.DB.Where("id IN ?", ids).Find(&chats)
	byID := make(map[string]*db.Chat, len(chats))
	for i := range chats {
		byID[chats[i].ID] = &chats[i]
	}
	out := make([]*v1.BotActivity, 0, len(runs))
	for _, r := range runs {
		c := byID[r.chatID]
		if c == nil {
			continue
		}
		row := &v1.BotActivity{ChatId: c.ID, Kind: "chat", Title: c.Title, Step: r.step}
		switch {
		case c.Main:
			row.Kind = "main"
		case c.AutomationID != "":
			row.Kind = "automation"
			var au db.Automation
			a.DB.Select("name").Where("id = ?", c.AutomationID).Limit(1).Find(&au)
			row.Title = au.Name
		case c.ChannelID != "":
			row.Kind = "channel"
			var ch db.Channel
			a.DB.Select("name").Where("id = ?", c.ChannelID).Limit(1).Find(&ch)
			row.Title = ch.Name
		case c.SubagentID != "":
			row.Kind = "subagent"
			var sa db.Subagent
			a.DB.Select("name, parent_chat_id").Where("id = ?", c.SubagentID).Limit(1).Find(&sa)
			row.Title, row.LeadChatId = sa.Name, sa.ParentChatID
		}
		out = append(out, row)
	}
	return out
}
