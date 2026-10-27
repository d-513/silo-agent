package app_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

func TestMoveBotToAnotherWorkspace(t *testing.T) {
	h := apptest.New(t)
	home := homeSpace(t, h)
	bot, bob, bu := team(t, h)
	conn := lent(t, h, home, "", "Docs", false, bot.Id)
	h.DB.Create(&db.BotSkill{ID: ids.New(), BotID: bot.Id, Kind: "space", Name: "notes", Enabled: true, CreatedAt: time.Now()})
	res, err := h.Client.CreateSpace(h.Ctx(), connect.NewRequest(&v1.CreateSpaceRequest{Name: "Lab"}))
	if err != nil {
		t.Fatal(err)
	}
	lab := res.Msg.GetId()

	plan, err := h.Client.PlanMoveBot(h.Ctx(), connect.NewRequest(&v1.MoveBotRequest{BotId: bot.Id, SpaceId: lab}))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plan.Msg.GetConnectors(), "Docs") || len(plan.Msg.GetSkills()) != 1 || len(plan.Msg.GetPeople()) != 1 {
		t.Fatalf("plan = %+v; want Docs, notes and bob", plan.Msg)
	}
	// Bob uses it but cannot move it.
	if _, err := bob.MoveBot(h.Ctx(), connect.NewRequest(&v1.MoveBotRequest{BotId: bot.Id, SpaceId: lab})); err == nil {
		t.Fatal("a user moved the bot")
	}
	moved, err := h.Client.MoveBot(h.Ctx(), connect.NewRequest(&v1.MoveBotRequest{BotId: bot.Id, SpaceId: lab}))
	if err != nil || moved.Msg.GetSpaceId() != lab {
		t.Fatalf("move = %+v, %v", moved, err)
	}
	var n int64
	h.DB.Model(&db.BotConnector{}).Where("bot_id = ? AND connector_id = ?", bot.Id, conn.ID).Count(&n)
	if n != 0 {
		t.Fatal("the bot kept a grant from the old workspace")
	}
	h.DB.Model(&db.BotSkill{}).Where("bot_id = ? AND kind = ?", bot.Id, "space").Count(&n)
	if n != 0 {
		t.Fatal("the bot kept the old workspace's skills")
	}
	if access.BotRole(h.DB, bot.Id, bu.ID) != "" {
		t.Fatal("bob, who is not in the target, is still on the bot")
	}
	// Nobody moves a Bot into a Workspace they cannot build in.
	if _, err := h.Client.MoveBot(h.Ctx(), connect.NewRequest(&v1.MoveBotRequest{BotId: bot.Id, SpaceId: home + "x"})); code(err) != connect.CodeNotFound {
		t.Fatalf("move to an unknown workspace: %v", err)
	}
}
