package app

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/prompts"
	"silo.agent/internal/textx"
) // --- prompt ---

// subagentSections is the per-run note that tells a subagent who it is.
func (a *App) subagentSections(pc promptContext) []promptSection {
	if pc.subagent == nil {
		return nil
	}
	body := strings.ReplaceAll(strings.TrimSpace(prompts.Subagent), "{{NAME}}", pc.subagent.Name)
	return []promptSection{{title: "This run", body: body, trailing: true}}
}

// --- RPC ---

func (a *App) protoSubagent(sa *db.Subagent) *v1.Subagent {
	out := &v1.Subagent{
		Id: sa.ID, BotId: sa.BotID, ParentChatId: sa.ParentChatID, ChatId: sa.ChatID, Name: sa.Name,
		Goal: sa.Goal, Context: sa.Context, Model: sa.Model, Status: sa.Status, Result: sa.Result,
		Running: a.subagentLive(sa), CreatedAt: sa.CreatedAt.Format(time.RFC3339), FinishedAt: textx.RFC3339(sa.FinishedAt),
	}
	if out.Running {
		out.Status = "running"
		out.Activity = a.subagentActivity(sa)
	}
	var run db.Run
	a.DB.Select("id").Where("chat_id = ?", sa.ChatID).Order("created_at desc").Limit(1).Find(&run)
	out.RunId = run.ID
	return out
}

func (a *App) ownSubagent(ctx context.Context, botID, id string) (*db.Subagent, error) {
	return access.OwnBotRow[db.Subagent](ctx, a.DB, botID, id, "subagent")
}

func (a *App) ListSubagents(ctx context.Context, req *connect.Request[v1.ListSubagentsRequest]) (*connect.Response[v1.ListSubagentsResponse], error) {
	if _, err := a.ownChat(ctx, req.Msg.GetBotId(), req.Msg.GetChatId()); err != nil {
		return nil, err
	}
	out := &v1.ListSubagentsResponse{}
	rows := a.subagentsOf(req.Msg.GetChatId())
	for i := range rows {
		out.Subagents = append(out.Subagents, a.protoSubagent(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (a *App) GetSubagent(ctx context.Context, req *connect.Request[v1.GetSubagentRequest]) (*connect.Response[v1.Subagent], error) {
	sa, err := a.ownSubagent(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(a.protoSubagent(sa)), nil
}

// StopSubagent is the human stopping one subagent. The lead is told (woken if
// idle) so it can adjust its plan.
func (a *App) StopSubagent(ctx context.Context, req *connect.Request[v1.StopSubagentRequest]) (*connect.Response[v1.StopSubagentResponse], error) {
	sa, err := a.ownSubagent(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	a.stopChat(sa.BotID, sa.ChatID)
	return connect.NewResponse(&v1.StopSubagentResponse{}), nil
}

func (a *App) protoBoard(chatID string) *v1.Taskboard {
	out := &v1.Taskboard{ChatId: chatID}
	for _, it := range a.boardItems(chatID) {
		out.Items = append(out.Items, &v1.TaskItem{
			N: int32(it.N), Text: it.Text, Assignee: it.Assignee, Done: it.Done, DoneBy: it.DoneBy, Note: it.Note, CreatedBy: it.CreatedBy,
		})
	}
	return out
}

func (a *App) GetTaskboard(ctx context.Context, req *connect.Request[v1.GetTaskboardRequest]) (*connect.Response[v1.Taskboard], error) {
	if _, err := a.ownChat(ctx, req.Msg.GetBotId(), req.Msg.GetChatId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(a.protoBoard(a.boardChat(req.Msg.GetChatId()))), nil
}

func (a *App) ClearTaskboard(ctx context.Context, req *connect.Request[v1.ClearTaskboardRequest]) (*connect.Response[v1.Taskboard], error) {
	if _, err := a.ownChat(ctx, req.Msg.GetBotId(), req.Msg.GetChatId()); err != nil {
		return nil, err
	}
	board := a.boardChat(req.Msg.GetChatId())
	a.DB.Where("chat_id = ?", board).Delete(&db.TaskItem{})
	a.pingLead(req.Msg.GetBotId(), board, "board")
	return connect.NewResponse(a.protoBoard(board)), nil
}
