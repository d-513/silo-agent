package app

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/skills"
)

// A Bot moves to another Workspace with what is its own (its box, SOUL,
// memories, chats, rules, secrets) and without what was its Workspace's:
// grants and Workspace skills do not cross, and people who are not in the
// target lose it.

// moveTarget checks the caller may move the Bot to the Workspace: a manager
// of it who may build there.
func (a *App) moveTarget(ctx context.Context, botID, spaceID string) (*db.Bot, *db.Space, error) {
	b, err := a.bot(ctx, botID, access.Manage)
	if err != nil {
		return nil, nil, err
	}
	sp, _, err := access.Space(ctx, a.DB, spaceID, access.Build)
	if err != nil {
		return nil, nil, err
	}
	if sp.ID == b.SpaceID {
		return nil, nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the Bot is in that workspace already"))
	}
	return b, sp, nil
}

// leaving are the people on the Bot who are not in the target Workspace.
func (a *App) leaving(botID, spaceID string) []string {
	var out []string
	for _, id := range access.People(a.DB, botID) {
		if access.SpaceRole(a.DB, spaceID, id) == "" {
			out = append(out, id)
		}
	}
	return out
}

func (a *App) PlanMoveBot(ctx context.Context, req *connect.Request[v1.MoveBotRequest]) (*connect.Response[v1.MoveBotPlan], error) {
	b, sp, err := a.moveTarget(ctx, req.Msg.GetBotId(), req.Msg.GetSpaceId())
	if err != nil {
		return nil, err
	}
	out := &v1.MoveBotPlan{Connectors: a.Connectors.GrantNames(b.ID), Drives: a.Drives.GrantNames(b.ID)}
	a.DB.Model(&db.BotSkill{}).Where("bot_id = ? AND kind = ? AND enabled", b.ID, skills.KindSpace).Order("name").Pluck("name", &out.Skills)
	for _, id := range a.leaving(b.ID, sp.ID) {
		out.People = append(out.People, access.Email(a.DB, id))
	}
	return connect.NewResponse(out), nil
}

func (a *App) MoveBot(ctx context.Context, req *connect.Request[v1.MoveBotRequest]) (*connect.Response[v1.Bot], error) {
	b, sp, err := a.moveTarget(ctx, req.Msg.GetBotId(), req.Msg.GetSpaceId())
	if err != nil {
		return nil, err
	}
	from := b.SpaceID
	a.Connectors.RevokeAll(ctx, b.ID)
	a.Drives.RevokeAll(b.ID)
	a.DB.Where("bot_id = ? AND kind = ?", b.ID, skills.KindSpace).Delete(&db.BotSkill{})
	for _, id := range a.leaving(b.ID, sp.ID) {
		a.DB.Where("bot_id = ? AND user_id = ?", b.ID, id).Delete(&db.BotAccess{})
		a.Gone.End(b.ID, id)
	}
	if err := a.DB.Model(&db.Bot{}).Where("id = ?", b.ID).Update("space_id", sp.ID).Error; err != nil {
		return nil, err
	}
	b.SpaceID = sp.ID
	go a.Skills.Push(b.ID)
	me := access.User(ctx).ID
	access.Activity(a.DB, from, me, access.BotDeleted, b.Name, "", "moved to "+sp.Name)
	access.Activity(a.DB, sp.ID, me, access.BotCreated, b.Name, "", "moved in")
	return connect.NewResponse(a.viewBot(ctx, b)), nil
}
