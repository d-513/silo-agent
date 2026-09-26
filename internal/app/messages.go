package app

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// chatEvent is one persisted RunEvent together with its parent Run, used to
// order a conversation across runs.
type chatEvent struct {
	run db.Run
	ev  db.RunEvent
}

// orderedChatEvents flattens a chat's Runs and RunEvents into conversational
// order. RunEvent IDs are random, so created_at with an id tiebreak is the only
// stable ordering.
func (a *App) orderedChatEvents(chatID string) []chatEvent {
	var runs []db.Run
	a.DB.Where("chat_id = ?", chatID).Order("created_at").Order("id").Find(&runs)
	var out []chatEvent
	for _, run := range runs {
		var evs []db.RunEvent
		a.DB.Where("run_id = ?", run.ID).Order("seq").Find(&evs)
		for _, ev := range evs {
			out = append(out, chatEvent{run: run, ev: ev})
		}
	}
	return out
}

// lastUserEventID is the most recent user message in a chat, or "" if none.
func (a *App) lastUserEventID(chatID string) string {
	last := ""
	for _, e := range a.orderedChatEvents(chatID) {
		if e.ev.Kind == "user" {
			last = e.ev.ID
		}
	}
	return last
}

// truncateChatFrom deletes the anchor event and everything after it, dropping
// runs left with no events. It reports whether the anchor was found.
func (a *App) truncateChatFrom(chatID, eventID string) (bool, error) {
	events := a.orderedChatEvents(chatID)
	idx := -1
	for i, e := range events {
		if e.ev.ID == eventID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, nil
	}
	ids := make([]string, 0, len(events)-idx)
	runs := map[string]bool{}
	for _, e := range events[idx:] {
		ids = append(ids, e.ev.ID)
		runs[e.ev.RunID] = true
	}
	if err := a.DB.Where("id IN ?", ids).Delete(&db.RunEvent{}).Error; err != nil {
		return false, err
	}
	for runID := range runs {
		var n int64
		a.DB.Model(&db.RunEvent{}).Where("run_id = ?", runID).Count(&n)
		if n == 0 {
			a.DB.Where("id = ?", runID).Delete(&db.Run{})
		}
	}
	return true, nil
}

// copyChatPrefix creates a new chat holding a copy of every event before the
// anchor, with fresh IDs so the original chat is untouched. It reports a nil
// chat when the anchor is not in this chat.
func (a *App) copyChatPrefix(chatID, eventID string) (*db.Chat, error) {
	var src db.Chat
	if err := a.DB.First(&src, "id = ?", chatID).Error; err != nil {
		return nil, err
	}
	events := a.orderedChatEvents(chatID)
	cut := -1
	for i, e := range events {
		if e.ev.ID == eventID {
			cut = i
			break
		}
	}
	if cut < 0 {
		return nil, nil
	}
	now := time.Now()
	nc := db.Chat{ID: ids.New(), BotID: src.BotID, Title: src.Title, Model: src.Model, CreatedAt: now, UpdatedAt: now}
	if err := a.DB.Create(&nc).Error; err != nil {
		return nil, err
	}
	runIDs := map[string]string{}
	for _, e := range events[:cut] {
		newRunID, ok := runIDs[e.ev.RunID]
		if !ok {
			newRunID = ids.New()
			runIDs[e.ev.RunID] = newRunID
			status := e.run.Status
			if status == "" || status == "running" {
				status = "done"
			}
			nr := db.Run{ID: newRunID, BotID: e.run.BotID, ChatID: nc.ID, ChannelID: e.run.ChannelID, Origin: e.run.Origin, Status: status, CreatedAt: e.run.CreatedAt}
			if err := a.DB.Create(&nr).Error; err != nil {
				return nil, err
			}
		}
		ne := db.RunEvent{ID: ids.New(), RunID: newRunID, Kind: e.ev.Kind, Body: e.ev.Body, Tool: e.ev.Tool, Meta: e.ev.Meta, CreatedAt: e.ev.CreatedAt}
		if err := a.DB.Create(&ne).Error; err != nil {
			return nil, err
		}
	}
	return &nc, nil
}

// publishReset tells every viewer of the chat to drop its cached events and
// replay from scratch. It is transient: nothing is persisted.
func (a *App) publishReset(botID, chatID string) {
	a.Bus.Publish(botID, &v1.RunEvent{ChatId: chatID, Kind: "reset"})
}

// stopChatLive stops any live run for the chat and waits for its goroutine to
// exit, so history truncation is not raced by a final write.
func (a *App) stopChatLive(botID, chatID string) {
	lr := a.liveRunFor(botID, chatID)
	if lr == nil {
		return
	}
	a.stopChat(botID, chatID)
	select {
	case <-lr.done:
	case <-time.After(5 * time.Second):
	}
}

// EditMessage truncates the chat at the last user message and re-sends the
// edited text as a fresh turn.
func (a *App) EditMessage(ctx context.Context, req *connect.Request[v1.EditMessageRequest]) (*connect.Response[v1.SendResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	ch, err := a.ownChat(ctx, b.ID, req.Msg.GetChatId())
	if err != nil {
		return nil, err
	}
	text := req.Msg.GetText()
	atts := cleanAttachments(req.Msg.GetAttachments())
	if text == "" && len(atts) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("text required"))
	}
	a.convMu.Lock()
	defer a.convMu.Unlock()
	if last := a.lastUserEventID(ch.ID); last == "" || last != req.Msg.GetEventId() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("only the last user message can be edited"))
	}
	a.stopChatLive(b.ID, ch.ID)
	found, err := a.truncateChatFrom(ch.ID, req.Msg.GetEventId())
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("message not found"))
	}
	a.publishReset(b.ID, ch.ID)
	runID, err := a.startOrInjectLocked(b.ID, ch.ID, text, atts, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1.SendResponse{RunId: runID, ChatId: ch.ID}), nil
}

// DeleteMessage truncates the chat at the last user message and stops there.
func (a *App) DeleteMessage(ctx context.Context, req *connect.Request[v1.DeleteMessageRequest]) (*connect.Response[v1.DeleteMessageResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	ch, err := a.ownChat(ctx, b.ID, req.Msg.GetChatId())
	if err != nil {
		return nil, err
	}
	a.convMu.Lock()
	defer a.convMu.Unlock()
	if last := a.lastUserEventID(ch.ID); last == "" || last != req.Msg.GetEventId() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("only the last user message can be deleted"))
	}
	a.stopChatLive(b.ID, ch.ID)
	found, err := a.truncateChatFrom(ch.ID, req.Msg.GetEventId())
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("message not found"))
	}
	a.publishReset(b.ID, ch.ID)
	return connect.NewResponse(&v1.DeleteMessageResponse{}), nil
}

// DivergeChat copies the context before a message into a new chat.
func (a *App) DivergeChat(ctx context.Context, req *connect.Request[v1.DivergeChatRequest]) (*connect.Response[v1.DivergeChatResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	ch, err := a.ownChat(ctx, b.ID, req.Msg.GetChatId())
	if err != nil {
		return nil, err
	}
	nc, err := a.copyChatPrefix(ch.ID, req.Msg.GetEventId())
	if err != nil {
		return nil, err
	}
	if nc == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("message not found"))
	}
	return connect.NewResponse(&v1.DivergeChatResponse{Chat: protoChat(nc)}), nil
}
