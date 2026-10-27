package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/app/access"
	"silo.agent/internal/apptest"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// homeSpace is the id of the harness user's first Workspace.
func homeSpace(t *testing.T, h *apptest.H) string {
	t.Helper()
	res, err := h.Client.ListSpaces(h.Ctx(), connect.NewRequest(&v1.ListSpacesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetSpaces()[0].GetId()
}

// invite makes an invite link in the Workspace as from and returns its token.
func invite(t *testing.T, from silov1connect.UIClient, spaceID, role string) string {
	t.Helper()
	res, err := from.CreateSpaceInvite(context.Background(), connect.NewRequest(&v1.CreateSpaceInviteRequest{SpaceId: spaceID, Role: role}))
	if err != nil {
		t.Fatalf("invite %s: %v", role, err)
	}
	u := res.Msg.GetUrl()
	return u[strings.LastIndex(u, "/")+1:]
}

// joinSpace brings a new person into the harness user's home Workspace with role.
func joinSpace(t *testing.T, h *apptest.H, spaceID, email, role string) (silov1connect.UIClient, *db.User) {
	t.Helper()
	c, _, u := h.Person(email)
	if _, err := c.JoinSpace(h.Ctx(), connect.NewRequest(&v1.JoinSpaceRequest{Token: invite(t, h.Client, spaceID, role)})); err != nil {
		t.Fatalf("%s joins: %v", email, err)
	}
	return c, u
}

func TestInviteIsSingleUse(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	tok := invite(t, h.Client, sp, access.RoleMember)

	info, err := h.Client.GetSpaceInvite(h.Ctx(), connect.NewRequest(&v1.GetSpaceInviteRequest{Token: tok}))
	if err != nil || info.Msg.GetState() != "ok" || info.Msg.GetRole() != access.RoleMember {
		t.Fatalf("invite info = %+v, %v", info, err)
	}
	bob, _, _ := h.Person("bob@test.local")
	res, err := bob.JoinSpace(h.Ctx(), connect.NewRequest(&v1.JoinSpaceRequest{Token: tok}))
	if err != nil || res.Msg.GetSpace().GetRole() != access.RoleMember {
		t.Fatalf("join = %+v, %v", res, err)
	}
	carol, _, _ := h.Person("carol@test.local")
	if _, err := carol.JoinSpace(h.Ctx(), connect.NewRequest(&v1.JoinSpaceRequest{Token: tok})); err == nil {
		t.Fatal("a used link let a second person in")
	}
	if info, _ := h.Client.GetSpaceInvite(h.Ctx(), connect.NewRequest(&v1.GetSpaceInviteRequest{Token: tok})); info.Msg.GetState() != "used" {
		t.Fatalf("a used link reads %q", info.Msg.GetState())
	}

	// An admin invites members and guests, never admins.
	admin, _ := joinSpace(t, h, sp, "wsadmin@test.local", access.RoleAdmin)
	if _, err := admin.CreateSpaceInvite(h.Ctx(), connect.NewRequest(&v1.CreateSpaceInviteRequest{SpaceId: sp, Role: access.RoleAdmin})); code(err) != connect.CodePermissionDenied {
		t.Fatalf("an admin made an admin invite: %v", err)
	}
	if _, err := bob.CreateSpaceInvite(h.Ctx(), connect.NewRequest(&v1.CreateSpaceInviteRequest{SpaceId: sp, Role: access.RoleGuest})); code(err) != connect.CodePermissionDenied {
		t.Fatalf("a member made an invite: %v", err)
	}

	// A guest is never a manager.
	_, guest := joinSpace(t, h, sp, "guest@test.local", access.RoleGuest)
	bot := h.CreateBot("Scout")
	if _, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: bot.Id, UserId: guest.ID, Role: access.RoleManage})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("a guest was made a manager: %v", err)
	}
	if _, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: bot.Id, UserId: guest.ID, Role: access.RoleUse})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.SetBotRole(h.Ctx(), connect.NewRequest(&v1.SetBotRoleRequest{BotId: bot.Id, UserId: guest.ID, Role: access.RoleManage})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("a guest became a manager: %v", err)
	}
	// Deleting a link spends it.
	tok2 := invite(t, h.Client, sp, access.RoleGuest)
	list, _ := h.Client.ListSpaceInvites(h.Ctx(), connect.NewRequest(&v1.ListSpaceInvitesRequest{SpaceId: sp}))
	if len(list.Msg.GetInvites()) != 1 {
		t.Fatalf("open invites = %d, want 1", len(list.Msg.GetInvites()))
	}
	if _, err := h.Client.DeleteSpaceInvite(h.Ctx(), connect.NewRequest(&v1.DeleteSpaceInviteRequest{SpaceId: sp, Id: list.Msg.GetInvites()[0].GetId()})); err != nil {
		t.Fatal(err)
	}
	if info, _ := h.Client.GetSpaceInvite(h.Ctx(), connect.NewRequest(&v1.GetSpaceInviteRequest{Token: tok2})); info.Msg.GetState() != "expired" {
		t.Fatalf("a deleted link reads %q", info.Msg.GetState())
	}
}

func TestInviteSignupFollowsTheSetting(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	visitor, _ := h.Browser()
	tok := invite(t, h.Client, sp, access.RoleMember)
	if info, _ := visitor.GetSpaceInvite(h.Ctx(), connect.NewRequest(&v1.GetSpaceInviteRequest{Token: tok})); info.Msg.GetSignup() {
		t.Fatal("signup offered while spaces.invite_signup is off")
	}
	if _, err := visitor.JoinSpace(h.Ctx(), connect.NewRequest(&v1.JoinSpaceRequest{Token: tok, Email: "new@test.local", Password: "a long password"})); err == nil {
		t.Fatal("a link made an account with signup off")
	}

	h2 := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(t.TempDir())+"\nspaces:\n  invite_signup: true\n"))
	sp2 := homeSpace(t, h2)
	tok2 := invite(t, h2.Client, sp2, access.RoleMember)
	v2, _ := h2.Browser()
	res, err := v2.JoinSpace(h2.Ctx(), connect.NewRequest(&v1.JoinSpaceRequest{Token: tok2, Email: "New@Test.local", Password: "a long password"}))
	if err != nil || res.Msg.GetEmail() != "new@test.local" {
		t.Fatalf("signup join = %+v, %v", res, err)
	}
	// Signed in now, and in the Workspace.
	if _, err := v2.GetSpace(h2.Ctx(), connect.NewRequest(&v1.GetSpaceRequest{Id: sp2})); err != nil {
		t.Fatalf("the new account is not in the workspace: %v", err)
	}
}

func TestBotPeople(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	bob, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	bot := h.CreateBot("Scout")
	add := func(role string) error {
		_, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: bot.Id, UserId: bu.ID, Role: role}))
		return err
	}
	if err := add(access.RoleUse); err != nil {
		t.Fatal(err)
	}
	// Bob uses it now: he sees it, and may not change who is on it.
	if _, err := bob.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.Id})); err != nil {
		t.Fatalf("bob cannot open the bot he uses: %v", err)
	}
	people, err := bob.ListBotPeople(h.Ctx(), connect.NewRequest(&v1.ListBotPeopleRequest{BotId: bot.Id}))
	if err != nil || len(people.Msg.GetPeople()) != 2 || len(people.Msg.GetCandidates()) != 0 {
		t.Fatalf("bob's people = %+v, %v", people, err)
	}
	if _, err := bob.SetBotRole(h.Ctx(), connect.NewRequest(&v1.SetBotRoleRequest{BotId: bot.Id, UserId: bu.ID, Role: access.RoleManage})); code(err) != connect.CodePermissionDenied {
		t.Fatalf("a user made himself a manager: %v", err)
	}
	// The last manager stays.
	owner := h.AdminUser()
	if _, err := h.Client.RemoveBotPerson(h.Ctx(), connect.NewRequest(&v1.RemoveBotPersonRequest{BotId: bot.Id, UserId: owner.ID})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("the last manager left: %v", err)
	}
	if _, err := h.Client.SetBotRole(h.Ctx(), connect.NewRequest(&v1.SetBotRoleRequest{BotId: bot.Id, UserId: owner.ID, Role: access.RoleUse})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("the last manager stepped down: %v", err)
	}
	if _, err := h.Client.SetBotRole(h.Ctx(), connect.NewRequest(&v1.SetBotRoleRequest{BotId: bot.Id, UserId: bu.ID, Role: access.RoleManage})); err != nil {
		t.Fatal(err)
	}
	// Bob leaves on his own.
	if _, err := bob.RemoveBotPerson(h.Ctx(), connect.NewRequest(&v1.RemoveBotPersonRequest{BotId: bot.Id, UserId: bu.ID})); err != nil {
		t.Fatal(err)
	}
	if _, err := bob.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.Id})); code(err) != connect.CodeNotFound {
		t.Fatalf("bob still sees the bot he left: %v", err)
	}
	// Nobody outside the Workspace is added.
	_, _, stranger := h.Person("stranger@test.local")
	if _, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: bot.Id, UserId: stranger.ID, Role: access.RoleUse})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("a stranger was added: %v", err)
	}
}

func TestAddingAPersonNamesOthersSecrets(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	_, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	bot := h.CreateBot("Scout")
	if _, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{BotId: bot.Id, Name: "BANK_PIN", Value: "123456789", Mode: "plain"})); err != nil {
		t.Fatal(err)
	}
	res, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: bot.Id, UserId: bu.ID, Role: access.RoleUse}))
	if err != nil || len(res.Msg.GetConfirmSecrets()) != 1 || res.Msg.GetPerson() != nil {
		t.Fatalf("first add = %+v, %v; want the secret named and nobody added", res, err)
	}
	if access.BotRole(h.DB, bot.Id, bu.ID) != "" {
		t.Fatal("bob was added before the confirmation")
	}
	res, err = h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: bot.Id, UserId: bu.ID, Role: access.RoleUse, Confirmed: true}))
	if err != nil || res.Msg.GetPerson().GetRole() != access.RoleUse {
		t.Fatalf("confirmed add = %+v, %v", res, err)
	}
}

// lent makes a connection in the Workspace lent by userID and grants it to
// the Bots.
func lent(t *testing.T, h *apptest.H, spaceID, userID, name string, shared bool, bots ...string) *db.Connector {
	t.Helper()
	c := db.Connector{ID: ids.New(), Kind: catalog.KindCustom, SpaceID: spaceID, Name: name, Transport: "http", HTTPURL: "http://127.0.0.1:1/mcp",
		CreatedBy: userID, LentBy: userID, Shared: shared, HeadersJSON: `{"Authorization":"Bearer typed"}`, CreatedAt: time.Now()}
	if err := h.DB.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	for _, b := range bots {
		if err := h.DB.Create(&db.BotConnector{ID: ids.New(), BotID: b, ConnectorID: c.ID, AuthStatus: "error", CreatedAt: time.Now()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &c
}

func TestPersonalIdentityStaysWithItsOwner(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	owner := h.AdminUser()
	bob, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	mine := h.CreateBot("Mine")
	gh := lent(t, h, sp, owner.ID, "GitHub", false, mine.Id)

	// Add a person: refused while the Bot holds someone's personal sign-in.
	_, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: mine.Id, UserId: bu.ID, Role: access.RoleUse, Confirmed: true}))
	if code(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "GitHub") {
		t.Fatalf("adding bob next to a personal sign-in: %v", err)
	}

	// Grant: refused to a Bot someone else uses.
	ours := h.CreateBot("Ours")
	if _, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: ours.Id, UserId: bu.ID, Role: access.RoleManage, Confirmed: true})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.GrantConnector(h.Ctx(), connect.NewRequest(&v1.GrantConnectorRequest{BotId: ours.Id, ConnectorId: gh.ID})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("a personal sign-in was granted to a shared Bot: %v", err)
	}
	// Bob, a manager of Ours, cannot grant it either, nor share it.
	if _, err := bob.GrantConnector(h.Ctx(), connect.NewRequest(&v1.GrantConnectorRequest{BotId: ours.Id, ConnectorId: gh.ID})); err == nil {
		t.Fatal("bob granted alice's personal sign-in")
	}
	if _, err := bob.SetLendShared(h.Ctx(), connect.NewRequest(&v1.SetLendSharedRequest{ConnectorId: gh.ID, Shared: true})); code(err) != connect.CodePermissionDenied {
		t.Fatalf("bob shared alice's sign-in: %v", err)
	}

	// Shared, it may go anywhere in the Workspace.
	if _, err := h.Client.SetLendShared(h.Ctx(), connect.NewRequest(&v1.SetLendSharedRequest{ConnectorId: gh.ID, Shared: true})); err != nil {
		t.Fatal(err)
	}
	if _, err := bob.GrantConnector(h.Ctx(), connect.NewRequest(&v1.GrantConnectorRequest{BotId: ours.Id, ConnectorId: gh.ID})); err != nil {
		t.Fatalf("bob could not grant a shared sign-in: %v", err)
	}
	// Back to personal: refused while Ours (Bob's too) has it, and says so.
	_, err = h.Client.SetLendShared(h.Ctx(), connect.NewRequest(&v1.SetLendSharedRequest{ConnectorId: gh.ID, Shared: false}))
	if code(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "Ours") {
		t.Fatalf("unsharing while a shared Bot has it: %v", err)
	}

	// Sign-in: whoever signs in becomes the lender, so starting one as
	// personal for a connection in a shared Bot is refused.
	oauth := db.Connector{ID: ids.New(), Kind: catalog.KindCustom, SpaceID: sp, Name: "Linear", Transport: "http", HTTPURL: "http://127.0.0.1:1/mcp", Auth: "oauth", CreatedBy: owner.ID}
	h.DB.Create(&oauth)
	h.DB.Create(&db.BotConnector{ID: ids.New(), BotID: ours.Id, ConnectorID: oauth.ID, AuthStatus: "needs_auth", CreatedAt: time.Now()})
	_, err = h.Client.StartConnectorAuth(h.Ctx(), connect.NewRequest(&v1.StartConnectorAuthRequest{ConnectorId: oauth.ID}))
	if code(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "Ours") {
		t.Fatalf("a personal sign-in for a shared Bot was started: %v", err)
	}
}

func TestRemovePersonHandsOff(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	owner := h.AdminUser()
	bob, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	// Bob's own Bot, and one he shares with the owner.
	res, err := bob.CreateBot(h.Ctx(), connect.NewRequest(&v1.CreateBotRequest{Name: "Bobs", SpaceId: sp}))
	if err != nil {
		t.Fatal(err)
	}
	bobs := res.Msg
	shared := h.CreateBot("Shared")
	if _, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: shared.Id, UserId: bu.ID, Role: access.RoleUse, Confirmed: true})); err != nil {
		t.Fatal(err)
	}
	personal := lent(t, h, sp, bu.ID, "Bob GitHub", false, bobs.Id)
	team := lent(t, h, sp, bu.ID, "Team Linear", true, bobs.Id, shared.Id)

	// Bob watches the shared Bot: his stream ends with his access.
	ctx, cancel := context.WithTimeout(h.Ctx(), 20*time.Second)
	defer cancel()
	// The stream sends nothing until there is an event, and the call
	// returns only with the first frame: run it on its own.
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		stream, err := bob.StreamRun(ctx, connect.NewRequest(&v1.StreamRunRequest{BotId: shared.Id}))
		if err != nil {
			return
		}
		for stream.Receive() {
		}
	}()
	// Let it subscribe before the access ends.
	time.Sleep(300 * time.Millisecond)
	select {
	case <-ended:
		t.Fatal("bob's stream ended before his access did")
	default:
	}

	plan, err := h.Client.PlanSpaceRemoval(h.Ctx(), connect.NewRequest(&v1.PlanSpaceRemovalRequest{SpaceId: sp, UserId: bu.ID}))
	if err != nil {
		t.Fatal(err)
	}
	if lm := plan.Msg.GetLastManaged(); len(lm) != 1 || lm[0].GetBotId() != bobs.Id {
		t.Fatalf("last managed = %+v, want Bobs", lm)
	}
	if len(plan.Msg.GetLent()) != 2 {
		t.Fatalf("lent = %+v, want both", plan.Msg.GetLent())
	}
	// No plan for Bobs: refused, naming it.
	_, err = h.Client.RemoveSpacePerson(h.Ctx(), connect.NewRequest(&v1.RemoveSpacePersonRequest{SpaceId: sp, UserId: bu.ID}))
	if code(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "Bobs") {
		t.Fatalf("removal without a plan: %v", err)
	}
	if _, err := h.Client.RemoveSpacePerson(h.Ctx(), connect.NewRequest(&v1.RemoveSpacePersonRequest{SpaceId: sp, UserId: bu.ID,
		Plan: []*v1.Handoff{{BotId: bobs.Id, Action: "take"}}})); err != nil {
		t.Fatal(err)
	}
	if access.BotRole(h.DB, bobs.Id, owner.ID) != access.RoleManage {
		t.Fatal("take did not make the remover Bobs's manager")
	}
	var n int64
	h.DB.Model(&db.BotAccess{}).Where("user_id = ?", bu.ID).Count(&n)
	if n != 0 {
		t.Fatalf("bob is still on %d bots", n)
	}
	if access.SpaceRole(h.DB, sp, bu.ID) != "" {
		t.Fatal("bob is still in the workspace")
	}
	// His personal sign-in went with him; the shared one stays, signed out.
	h.DB.Model(&db.Connector{}).Where("id = ?", personal.ID).Count(&n)
	if n != 0 {
		t.Fatal("bob's personal connection outlived him")
	}
	var c db.Connector
	h.DB.Where("id = ?", team.ID).Find(&c)
	if c.ID == "" || c.LentBy != "" || strings.Contains(c.HeadersJSON, "typed") {
		t.Fatalf("the shared connection = %+v; want it kept, signed out", c)
	}
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("bob's stream outlived his access")
	}
}

func TestLeaveAndTransfer(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	owner := h.AdminUser()
	bob, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	// The owner cannot leave, nor hand it to a member.
	if _, err := h.Client.RemoveSpacePerson(h.Ctx(), connect.NewRequest(&v1.RemoveSpacePersonRequest{SpaceId: sp, UserId: owner.ID})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("the owner left: %v", err)
	}
	if _, err := h.Client.TransferSpace(h.Ctx(), connect.NewRequest(&v1.TransferSpaceRequest{SpaceId: sp, UserId: bu.ID})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("ownership went to a member: %v", err)
	}
	// Admins: only the owner makes them.
	_, au := joinSpace(t, h, sp, "wsadmin@test.local", access.RoleMember)
	if _, err := h.Client.SetSpaceRole(h.Ctx(), connect.NewRequest(&v1.SetSpaceRoleRequest{SpaceId: sp, UserId: au.ID, Role: access.RoleAdmin})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.TransferSpace(h.Ctx(), connect.NewRequest(&v1.TransferSpaceRequest{SpaceId: sp, UserId: au.ID})); err != nil {
		t.Fatal(err)
	}
	if access.SpaceRole(h.DB, sp, au.ID) != access.RoleOwner || access.SpaceRole(h.DB, sp, owner.ID) != access.RoleAdmin {
		t.Fatal("transfer did not swap owner and admin")
	}
	// Bob leaves on his own.
	if _, err := bob.RemoveSpacePerson(h.Ctx(), connect.NewRequest(&v1.RemoveSpacePersonRequest{SpaceId: sp, UserId: bu.ID})); err != nil {
		t.Fatal(err)
	}
	if access.SpaceRole(h.DB, sp, bu.ID) != "" {
		t.Fatal("bob did not leave")
	}
}

func TestDemotingToGuestKeepsAManager(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	bob, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	res, err := bob.CreateBot(h.Ctx(), connect.NewRequest(&v1.CreateBotRequest{Name: "Bobs", SpaceId: sp}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.Client.SetSpaceRole(h.Ctx(), connect.NewRequest(&v1.SetSpaceRoleRequest{SpaceId: sp, UserId: bu.ID, Role: access.RoleGuest}))
	if code(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "Bobs") {
		t.Fatalf("demoting the last manager of Bobs: %v", err)
	}
	// With a second manager, he can be a guest: he only uses it then.
	if _, err := bob.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: res.Msg.GetId(), UserId: h.AdminUser().ID, Role: access.RoleManage, Confirmed: true})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.SetSpaceRole(h.Ctx(), connect.NewRequest(&v1.SetSpaceRoleRequest{SpaceId: sp, UserId: bu.ID, Role: access.RoleGuest})); err != nil {
		t.Fatal(err)
	}
	if access.BotRole(h.DB, res.Msg.GetId(), bu.ID) != access.RoleUse {
		t.Fatal("a guest kept Can manage")
	}
}

func TestAdminCannotOpenOthersBots(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	bob, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleAdmin)
	_ = bu
	bot := h.CreateBot("Private")
	if _, err := bob.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.Id})); code(err) != connect.CodeNotFound {
		t.Fatalf("an admin opened another's bot: %v", err)
	}
	if _, err := bob.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: bot.Id, UserId: bu.ID, Role: access.RoleUse, Confirmed: true})); code(err) != connect.CodeNotFound {
		t.Fatalf("an admin added himself: %v", err)
	}
	list, err := bob.ListBots(h.Ctx(), connect.NewRequest(&v1.ListBotsRequest{SpaceId: sp}))
	if err != nil || len(list.Msg.GetBots()) != 0 || len(list.Msg.GetOthers()) != 1 || list.Msg.GetOthers()[0].GetName() != "Private" {
		t.Fatalf("admin's list = %+v, %v", list, err)
	}
	// A member sees no others.
	carol, _ := joinSpace(t, h, sp, "carol@test.local", access.RoleMember)
	if l, _ := carol.ListBots(h.Ctx(), connect.NewRequest(&v1.ListBotsRequest{SpaceId: sp})); len(l.Msg.GetOthers()) != 0 {
		t.Fatal("a member saw other people's bots")
	}
	if _, err := bob.DeleteBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.Id})); err != nil {
		t.Fatalf("an admin could not delete a bot of the workspace: %v", err)
	}
}

func TestNewBotTakesWhatItCanUse(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	owner := h.AdminUser()
	_, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	open := lent(t, h, sp, "", "Docs", false)
	mine := lent(t, h, sp, owner.ID, "Mine", false)
	res, err := h.Client.CreateBot(h.Ctx(), connect.NewRequest(&v1.CreateBotRequest{Name: "Team", SpaceId: sp, ConnectorIds: []string{open.ID}, UserIds: []string{bu.ID}}))
	if err != nil {
		t.Fatal(err)
	}
	if access.BotRole(h.DB, res.Msg.GetId(), bu.ID) != access.RoleUse {
		t.Fatal("bob is not on the new bot")
	}
	var n int64
	h.DB.Model(&db.BotConnector{}).Where("bot_id = ? AND connector_id = ?", res.Msg.GetId(), open.ID).Count(&n)
	if n != 1 {
		t.Fatal("the ticked connection was not granted")
	}
	// A personal sign-in cannot go to a Bot with Bob on it.
	if _, err := h.Client.CreateBot(h.Ctx(), connect.NewRequest(&v1.CreateBotRequest{Name: "Team 2", SpaceId: sp, ConnectorIds: []string{mine.ID}, UserIds: []string{bu.ID}})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("a personal sign-in went to a shared new bot: %v", err)
	}
}

func TestDisabledLenderSuspends(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	_, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	c := lent(t, h, sp, bu.ID, "Bob Shared", true)
	if err := access.LenderOK(h.DB, c.LentBy); err != nil {
		t.Fatalf("an enabled lender: %v", err)
	}
	h.DB.Model(&db.User{}).Where("id = ?", bu.ID).Update("disabled", true)
	if err := access.LenderOK(h.DB, c.LentBy); err == nil || !strings.Contains(err.Error(), "bob@test.local") {
		t.Fatalf("a disabled lender's sign-in still works: %v", err)
	}
}
