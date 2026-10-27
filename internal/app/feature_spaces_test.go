package app_test

import (
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/space"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
)

func TestEveryUserHasAWorkspace(t *testing.T) {
	h := apptest.New(t)
	res, err := h.Client.ListSpaces(h.Ctx(), rq(&v1.ListSpacesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(res.Msg.GetSpaces()); n != 1 {
		t.Fatalf("a new user has %d workspaces, want their own one", n)
	}
	home := res.Msg.GetSpaces()[0]
	if home.GetRole() != access.RoleOwner || home.GetName() == "" || home.GetMark() == "" {
		t.Fatalf("home workspace: %+v", home)
	}
	// Asking again does not make another.
	again, _ := h.Client.ListSpaces(h.Ctx(), rq(&v1.ListSpacesRequest{}))
	if len(again.Msg.GetSpaces()) != 1 || again.Msg.GetSpaces()[0].GetId() != home.GetId() {
		t.Fatalf("second list: %+v", again.Msg.GetSpaces())
	}
	// A Bot made with no Workspace named lands in it, managed by its maker.
	bot := h.CreateBot("Scout")
	if bot.GetSpaceId() != home.GetId() || bot.GetRole() != access.RoleManage {
		t.Fatalf("bot space %q role %q, want %q manage", bot.GetSpaceId(), bot.GetRole(), home.GetId())
	}
}

func TestBotsLiveInTheirWorkspace(t *testing.T) {
	h := apptest.New(t)
	home := h.CreateBot("Home bot")
	work, err := h.Client.CreateSpace(h.Ctx(), rq(&v1.CreateSpaceRequest{Name: "Acme labs"}))
	if err != nil {
		t.Fatal(err)
	}
	if work.Msg.GetMark() != "AL" || work.Msg.GetRole() != access.RoleOwner {
		t.Fatalf("new workspace: %+v", work.Msg)
	}
	made, err := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "Work bot", SpaceId: work.Msg.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if made.Msg.GetSpaceId() != work.Msg.GetId() {
		t.Fatalf("bot made in %q, want %q", made.Msg.GetSpaceId(), work.Msg.GetId())
	}

	list := func(spaceID string) []string {
		t.Helper()
		res, err := h.Client.ListBots(h.Ctx(), rq(&v1.ListBotsRequest{SpaceId: spaceID}))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, b := range res.Msg.GetBots() {
			names = append(names, b.GetName())
		}
		return names
	}
	if got := list(work.Msg.GetId()); len(got) != 1 || got[0] != "Work bot" {
		t.Fatalf("work lists %v", got)
	}
	if got := list(home.GetSpaceId()); len(got) != 1 || got[0] != "Home bot" {
		t.Fatalf("home lists %v", got)
	}
	if got := list(""); len(got) != 2 {
		t.Fatalf("no workspace named lists every Bot of theirs, got %v", got)
	}

	// Another person sees none of it and cannot make a Bot there.
	other, _, _ := h.Person("other@test.local")
	if _, err := other.ListBots(h.Ctx(), rq(&v1.ListBotsRequest{SpaceId: work.Msg.GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("a stranger listing my workspace: %v", err)
	}
	if _, err := other.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "Intruder", SpaceId: work.Msg.GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("a stranger making a Bot in my workspace: %v", err)
	}
	if res, err := other.ListBots(h.Ctx(), rq(&v1.ListBotsRequest{})); err != nil || len(res.Msg.GetBots()) != 0 {
		t.Fatalf("a stranger's own list: %v %v", res, err)
	}
	mine, err := other.ListSpaces(h.Ctx(), rq(&v1.ListSpacesRequest{}))
	if err != nil || len(mine.Msg.GetSpaces()) != 1 || mine.Msg.GetSpaces()[0].GetId() == work.Msg.GetId() {
		t.Fatalf("a stranger has their own workspace only: %v %v", mine, err)
	}
}

func TestWorkspaceSettingsAndDelete(t *testing.T) {
	h := apptest.New(t)
	// The console lists first, which makes the user's own one.
	if _, err := h.Client.ListSpaces(h.Ctx(), rq(&v1.ListSpacesRequest{})); err != nil {
		t.Fatal(err)
	}
	ws, err := h.Client.CreateSpace(h.Ctx(), rq(&v1.CreateSpaceRequest{Name: "Side project", Mark: "sp!"}))
	if err != nil {
		t.Fatal(err)
	}
	if ws.Msg.GetMark() != "SP" {
		t.Fatalf("mark %q, want SP", ws.Msg.GetMark())
	}
	if _, err := h.Client.CreateSpace(h.Ctx(), rq(&v1.CreateSpaceRequest{Name: "   "})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a blank name: %v", err)
	}
	up, err := h.Client.UpdateSpace(h.Ctx(), rq(&v1.UpdateSpaceRequest{Id: ws.Msg.GetId(), Name: "Garden"}))
	if err != nil || up.Msg.GetName() != "Garden" || up.Msg.GetMark() != "GA" {
		t.Fatalf("rename: %v %v", up, err)
	}

	bot, err := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "Gardener", SpaceId: ws.Msg.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.DeleteSpace(h.Ctx(), rq(&v1.DeleteSpaceRequest{Id: ws.Msg.GetId()})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("deleting a workspace with a Bot in it: %v", err)
	}
	if _, err := h.Client.DeleteBot(h.Ctx(), rq(&v1.GetBotRequest{Id: bot.Msg.GetId()})); err != nil {
		t.Fatal(err)
	}
	var left int64
	h.DB.Model(&db.BotAccess{}).Where("bot_id = ?", bot.Msg.GetId()).Count(&left)
	if left != 0 {
		t.Fatalf("a deleted Bot keeps %d access rows", left)
	}
	if _, err := h.Client.DeleteSpace(h.Ctx(), rq(&v1.DeleteSpaceRequest{Id: ws.Msg.GetId()})); err != nil {
		t.Fatalf("deleting an empty workspace: %v", err)
	}
	res, _ := h.Client.ListSpaces(h.Ctx(), rq(&v1.ListSpacesRequest{}))
	if len(res.Msg.GetSpaces()) != 1 {
		t.Fatalf("after delete: %v", res.Msg.GetSpaces())
	}
	// The last one cannot go: everyone owns at least one.
	if _, err := h.Client.DeleteSpace(h.Ctx(), rq(&v1.DeleteSpaceRequest{Id: res.Msg.GetSpaces()[0].GetId()})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("deleting the last workspace: %v", err)
	}
}

func TestDeletingAUserDropsTheWorkspacesTheyAreAloneIn(t *testing.T) {
	h := apptest.New(t)
	other, _, u := h.Person("leaver@test.local")
	res, err := other.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "Theirs"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.DeleteUser(h.Ctx(), rq(&v1.DeleteUserRequest{Id: u.ID})); err != nil {
		t.Fatal(err)
	}
	var bots, spaces, members int64
	h.DB.Model(&db.Bot{}).Where("id = ?", res.Msg.GetId()).Count(&bots)
	h.DB.Model(&db.Space{}).Where("id = ?", res.Msg.GetSpaceId()).Count(&spaces)
	h.DB.Model(&db.SpaceMember{}).Where("user_id = ?", u.ID).Count(&members)
	if bots+spaces+members != 0 {
		t.Fatalf("left behind: %d bots, %d workspaces, %d memberships", bots, spaces, members)
	}
}

func TestMark(t *testing.T) {
	for _, c := range []struct{ name, given, want string }{
		{"Acme labs", "", "AL"},
		{"garden", "", "GA"},
		{"Żółw ninja", "", "ŻN"},
		{"x", "", "X"},
		{"!!!", "", "W"},
		{"Acme", "q", "Q"},
		{"Acme", "  -- ", "AC"},
	} {
		if got := space.Mark(c.name, c.given); got != c.want {
			t.Errorf("Mark(%q, %q) = %q, want %q", c.name, c.given, got, c.want)
		}
	}
}
