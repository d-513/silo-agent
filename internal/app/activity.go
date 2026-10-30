package app

import (
	"context"
	"slices"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
)

// What a Bot is doing is worked out when it is read, for whoever is reading:
// nothing about it is stored, so it cannot go stale or be overwritten by the
// next run that starts.
const (
	machineStopped  = "stopped"
	machineStarting = "starting"
	machineOnline   = "online"

	stateWorking  = "working"
	stateNeedsYou = "needs_you"
	stateWaiting  = "waiting"
	stateOnCall   = "on_call"

	kindHidden = "hidden"

	// activityTaskMax keeps a task to what a status line can show.
	activityTaskMax = 160
)

// machine is the Bot's box: the worker and the engine are the truth.
func (a *App) machine(ctx context.Context, b *db.Bot) string {
	running := a.live(ctx, b)
	return machineState(a.Hub.Connected(b.ID), running)
}

func machineState(connected, running bool) string {
	switch {
	case connected:
		return machineOnline
	case running:
		return machineStarting
	default:
		return machineStopped
	}
}

// botStatus is the one word for a Bot as a viewer sees it, the most pressing
// first: what they must answer beats work, and work beats the box.
func botStatus(machine string, rows []*v1.BotActivity, needsYou bool) string {
	if needsYou {
		return stateNeedsYou
	}
	for _, want := range []string{stateNeedsYou, stateWorking, stateWaiting, stateOnCall} {
		for _, r := range rows {
			if r.GetState() == want {
				return want
			}
		}
	}
	return machine
}

// setStep records the tool a live run is in now, for the Bot's status line;
// "" while the model is writing or between turns.
func (a *App) setStep(runID, step string) {
	a.mu.Lock()
	if lr := a.runs[runID]; lr != nil {
		lr.step = step
	}
	a.mu.Unlock()
}

// paused is what stopped a run: an approval, and who it waits for.
type paused struct {
	mine bool
	who  string
}

// pendingFor reads a Bot's open approvals for one person: which runs they
// pause, and whether any is theirs to answer (a background run's has no run).
func (a *App) pendingFor(botID, userID string, manager bool) (map[string]paused, bool) {
	var rows []db.Approval
	a.DB.Select("run_id, connector, action").Where("bot_id = ? AND status = ?", botID, "pending").Find(&rows)
	out := make(map[string]paused, len(rows))
	needsYou := false
	for i := range rows {
		r := &rows[i]
		person := a.runPerson(r.RunID)
		mine := mayAnswer(r, person, userID, manager)
		needsYou = needsYou || mine
		if r.RunID == "" || out[r.RunID].mine {
			continue
		}
		p := paused{mine: mine}
		if !mine && person != "" && !security.ManagerOnly(r.Connector, r.Action) {
			p.who = access.Email(a.DB, person)
		}
		out[r.RunID] = p
	}
	return out, needsYou
}

// NeedsYou says a run of a chat, or of a subagent it leads, is paused on an
// approval this person may answer.
func (a *App) NeedsYou(botID, chatID, userID string) bool {
	var rows []db.Approval
	a.DB.Select("run_id, connector, action").Where("bot_id = ? AND status = ?", botID, "pending").Find(&rows)
	manager := access.BotRole(a.DB, botID, userID) == access.RoleManage
	for i := range rows {
		r := &rows[i]
		if r.RunID != "" && a.slipChat(r.RunID) == chatID && mayAnswer(r, a.runPerson(r.RunID), userID, manager) {
			return true
		}
	}
	return false
}

// activity is what a Bot is doing right now, as one person may see it: one row
// per live run and call, with where it is, what it is on, and what it waits
// for. A conversation they may not read keeps only its state. needsYou says an
// approval waits for them. It rides on the polled Bot row, so the sidebar and
// the Overview need no stream of their own.
func (a *App) activity(botID, userID string, manager bool) (out []*v1.BotActivity, needsYou bool) {
	type live struct {
		runID, chatID, step string
		started             time.Time
	}
	var runs []live
	a.mu.Lock()
	for id, lr := range a.runs {
		if lr.botID == botID && lr.chatID != "" {
			runs = append(runs, live{id, lr.chatID, lr.step, lr.started})
		}
	}
	a.mu.Unlock()
	if a.DB == nil {
		return nil, false
	}
	pending, needsYou := a.pendingFor(botID, userID, manager)
	calls := a.Voice.Live(botID)
	if len(runs) == 0 && len(calls) == 0 {
		return nil, needsYou
	}
	slices.SortFunc(runs, func(x, y live) int { return x.started.Compare(y.started) })
	ids := make([]string, 0, len(runs)+len(calls))
	for _, r := range runs {
		ids = append(ids, r.chatID)
	}
	for _, c := range calls {
		ids = append(ids, c.ChatID)
	}
	var chats []db.Chat
	a.DB.Where("id IN ?", ids).Find(&chats)
	byID := make(map[string]*db.Chat, len(chats))
	for i := range chats {
		byID[chats[i].ID] = &chats[i]
	}
	boards := a.boardsOf(chats)
	for _, r := range runs {
		c := byID[r.chatID]
		if c == nil {
			continue
		}
		row := &v1.BotActivity{ChatId: c.ID, Kind: "chat", Title: c.Title, Step: r.step, State: stateWorking}
		if p, ok := pending[r.runID]; ok {
			row.State, row.WaitingOn = stateWaiting, p.who
			if p.mine {
				row.State, row.WaitingOn = stateNeedsYou, ""
			}
		}
		var agent string
		owner := c
		board := c.ID
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
			agent, board = sa.Name, sa.ParentChatID
			owner = a.leadOf(sa.ParentChatID)
		}
		if owner == nil || !access.CanRead(owner, userID, manager) {
			out = append(out, &v1.BotActivity{Kind: kindHidden, State: row.State, WaitingOn: row.WaitingOn})
			continue
		}
		row.Task, row.TaskDone, row.TaskTotal = currentTask(boards[board], agent, r.started)
		out = append(out, row)
	}
	for _, call := range calls {
		c := byID[call.ChatID]
		if c == nil {
			continue
		}
		row := &v1.BotActivity{ChatId: c.ID, Kind: "call", Title: c.Title, State: stateOnCall}
		if !access.CanRead(c, userID, manager) {
			row = &v1.BotActivity{Kind: kindHidden, State: stateOnCall}
		}
		out = append(out, row)
	}
	return out, needsYou
}

// boardsOf loads the taskboards of these chats (a subagent's is its lead's).
func (a *App) boardsOf(chats []db.Chat) map[string][]db.TaskItem {
	ids := make([]string, 0, len(chats))
	for i := range chats {
		ids = append(ids, chats[i].ID)
		if chats[i].SubagentID != "" {
			var sa db.Subagent
			a.DB.Select("parent_chat_id").Where("id = ?", chats[i].SubagentID).Limit(1).Find(&sa)
			ids = append(ids, sa.ParentChatID)
		}
	}
	var items []db.TaskItem
	a.DB.Where("chat_id IN ?", ids).Order("chat_id, n").Find(&items)
	out := map[string][]db.TaskItem{}
	for _, it := range items {
		out[it.ChatID] = append(out[it.ChatID], it)
	}
	return out
}

// leadOf loads a subagent's lead chat, whose reader may see the subagent.
func (a *App) leadOf(chatID string) *db.Chat {
	var c db.Chat
	if a.DB.Where("id = ?", chatID).Limit(1).Find(&c); c.ID == "" {
		return nil
	}
	return &c
}

// currentTask is the task a run is on and the progress of its board. The board
// says nothing of what is in hand, so it is the first one not done: an agent's
// own by its tag, a lead's the first nobody else was given. A board a lead's
// run has not touched is an older job's, not what this run is on.
func currentTask(items []db.TaskItem, agent string, since time.Time) (task string, done, total int32) {
	mine := func(it *db.TaskItem) bool {
		if agent != "" {
			return strings.EqualFold(it.Assignee, agent)
		}
		return it.Assignee == "" || strings.EqualFold(it.Assignee, "lead")
	}
	fresh := agent != ""
	for i := range items {
		it := &items[i]
		fresh = fresh || !it.CreatedAt.Before(since) || (it.DoneAt != nil && !it.DoneAt.Before(since))
		if agent != "" && !mine(it) {
			continue
		}
		total++
		if it.Done {
			done++
		} else if task == "" && mine(it) {
			task = it.Text
		}
	}
	if !fresh || task == "" {
		return "", 0, 0
	}
	return textx.ClipRunes(task, activityTaskMax), done, total
}
