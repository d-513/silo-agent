package app_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/run"
	"silo.agent/internal/apptest"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
	"silo.agent/internal/skills"
)

func boolp(b bool) *bool { return &b }

func TestUserAdminIsAdminOnly(t *testing.T) {
	h := apptest.New(t)
	user, _ := h.SignedInUser("user@test.local")
	var admin db.User
	h.DB.First(&admin, "email = ?", h.Email)
	for name, err := range map[string]error{
		"ListUsers":    second(user.ListUsers(h.Ctx(), rq(&v1.ListUsersRequest{}))),
		"UpdateUser":   second(user.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: admin.ID, Disabled: boolp(true)}))),
		"DeleteUser":   second(user.DeleteUser(h.Ctx(), rq(&v1.DeleteUserRequest{Id: admin.ID}))),
		"CreateInvite": second(user.CreateInvite(h.Ctx(), rq(&v1.CreateInviteRequest{Email: "friend@test.local", Admin: true}))),
		"DeleteInvite": second(user.DeleteInvite(h.Ctx(), rq(&v1.DeleteInviteRequest{Id: "x"}))),
		"CheckOIDC":    second(user.CheckOIDC(h.Ctx(), rq(&v1.CheckOIDCRequest{}))),
	} {
		if code(err) != connect.CodePermissionDenied {
			t.Errorf("%s as a non-admin: %v", name, err)
		}
	}
}

func second[T any](_ T, err error) error { return err }

func TestInviteLinkMakesAnAccount(t *testing.T) {
	h := apptest.New(t)
	for _, bad := range []string{"", "not-an-email", "a@b", "two words@test.local"} {
		if _, err := h.Client.CreateInvite(h.Ctx(), rq(&v1.CreateInviteRequest{Email: bad})); code(err) != connect.CodeInvalidArgument {
			t.Errorf("invite for %q: %v", bad, err)
		}
	}
	if _, err := h.Client.CreateInvite(h.Ctx(), rq(&v1.CreateInviteRequest{Email: "Admin@Test.Local"})); code(err) != connect.CodeAlreadyExists {
		t.Fatalf("invite for an existing user: %v", err)
	}

	token := inviteLink(t, h, " Ann@Test.Local ", false)
	// Only the hash of the link is kept.
	var row db.Invite
	h.DB.First(&row, "email = ?", "ann@test.local")
	if row.ID == token || row.ID == "" {
		t.Fatalf("invite row id %q", row.ID)
	}
	list, err := h.Client.ListUsers(h.Ctx(), rq(&v1.ListUsersRequest{}))
	if err != nil || len(list.Msg.GetUsers()) != 1 || len(list.Msg.GetInvites()) != 1 || list.Msg.GetInvites()[0].GetUrl() != "" {
		t.Fatalf("ListUsers with an invite out: %+v %v", list, err)
	}

	guest, _ := h.Browser()
	info, err := guest.GetInvite(h.Ctx(), rq(&v1.GetInviteRequest{Token: token}))
	if err != nil || info.Msg.GetEmail() != "ann@test.local" || info.Msg.GetPasswordReset() {
		t.Fatalf("GetInvite: %+v %v", info, err)
	}
	if _, err := guest.AcceptInvite(h.Ctx(), rq(&v1.AcceptInviteRequest{Token: token, Password: "short"})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("short password: %v", err)
	}
	res, err := guest.AcceptInvite(h.Ctx(), rq(&v1.AcceptInviteRequest{Token: token, Password: "anns-password"}))
	if err != nil || !res.Msg.GetSignedIn() || res.Msg.GetUser().GetEmail() != "ann@test.local" || res.Msg.GetUser().GetAdmin() {
		t.Fatalf("AcceptInvite: %+v %v", res, err)
	}
	if me, err := whoAmI(guest); err != nil || me.GetEmail() != "ann@test.local" {
		t.Fatalf("not signed in after accepting: %+v %v", me, err)
	}

	// The link worked once.
	other, _ := h.Browser()
	if _, err := other.GetInvite(h.Ctx(), rq(&v1.GetInviteRequest{Token: token})); code(err) != connect.CodeNotFound {
		t.Fatalf("a used link: %v", err)
	}
	if _, err := other.AcceptInvite(h.Ctx(), rq(&v1.AcceptInviteRequest{Token: token, Password: "another-password"})); code(err) != connect.CodeNotFound {
		t.Fatalf("accepting a used link: %v", err)
	}
	list, _ = h.Client.ListUsers(h.Ctx(), rq(&v1.ListUsersRequest{}))
	if len(list.Msg.GetUsers()) != 2 || len(list.Msg.GetInvites()) != 0 {
		t.Fatalf("ListUsers after accepting: %+v", list.Msg)
	}

	// An admin invite makes an admin.
	boss, _ := h.Browser()
	res, err = boss.AcceptInvite(h.Ctx(), rq(&v1.AcceptInviteRequest{Token: inviteLink(t, h, "boss@test.local", true), Password: "boss-password"}))
	if err != nil || !res.Msg.GetUser().GetAdmin() {
		t.Fatalf("admin invite: %+v %v", res, err)
	}
	if _, err := boss.ListUsers(h.Ctx(), rq(&v1.ListUsersRequest{})); err != nil {
		t.Fatalf("the invited admin: %v", err)
	}
}

func TestInviteLinksAreReplacedRevokedAndExpire(t *testing.T) {
	h := apptest.New(t)
	guest, _ := h.Browser()

	// A second link for the same person replaces the first.
	first := inviteLink(t, h, "ann@test.local", false)
	secondLink := inviteLink(t, h, "ann@test.local", false)
	if _, err := guest.GetInvite(h.Ctx(), rq(&v1.GetInviteRequest{Token: first})); code(err) != connect.CodeNotFound {
		t.Fatalf("the replaced link: %v", err)
	}
	if _, err := guest.GetInvite(h.Ctx(), rq(&v1.GetInviteRequest{Token: secondLink})); err != nil {
		t.Fatalf("the new link: %v", err)
	}

	list, _ := h.Client.ListUsers(h.Ctx(), rq(&v1.ListUsersRequest{}))
	if len(list.Msg.GetInvites()) != 1 {
		t.Fatalf("invites out: %+v", list.Msg.GetInvites())
	}
	if _, err := h.Client.DeleteInvite(h.Ctx(), rq(&v1.DeleteInviteRequest{Id: list.Msg.GetInvites()[0].GetId()})); err != nil {
		t.Fatal(err)
	}
	if _, err := guest.GetInvite(h.Ctx(), rq(&v1.GetInviteRequest{Token: secondLink})); code(err) != connect.CodeNotFound {
		t.Fatalf("a revoked link: %v", err)
	}
	if _, err := h.Client.DeleteInvite(h.Ctx(), rq(&v1.DeleteInviteRequest{Id: "gone"})); code(err) != connect.CodeNotFound {
		t.Fatalf("deleting a missing invite: %v", err)
	}

	old := inviteLink(t, h, "bob@test.local", false)
	h.DB.Model(&db.Invite{}).Where("email = ?", "bob@test.local").Update("expires_at", time.Now().Add(-time.Minute))
	if _, err := guest.AcceptInvite(h.Ctx(), rq(&v1.AcceptInviteRequest{Token: old, Password: "bobs-password"})); code(err) != connect.CodeNotFound {
		t.Fatalf("an expired link: %v", err)
	}
	list, _ = h.Client.ListUsers(h.Ctx(), rq(&v1.ListUsersRequest{}))
	if len(list.Msg.GetInvites()) != 0 {
		t.Fatalf("an expired invite is still listed: %+v", list.Msg.GetInvites())
	}
}

// Guessing at invite links is slowed down like guessing at passwords.
func TestInviteLinkGuessesAreThrottled(t *testing.T) {
	h := apptest.New(t)
	guest, _ := h.Browser()
	for i := range 20 {
		if _, err := guest.GetInvite(h.Ctx(), rq(&v1.GetInviteRequest{Token: "guess"})); code(err) != connect.CodeNotFound {
			t.Fatalf("guess %d: %v", i+1, err)
		}
	}
	if _, err := guest.GetInvite(h.Ctx(), rq(&v1.GetInviteRequest{Token: "guess"})); code(err) != connect.CodeResourceExhausted {
		t.Fatalf("after 20 guesses: %v", err)
	}
}

func TestResetLinkSetsANewPassword(t *testing.T) {
	h := apptest.New(t)
	ann, u := join(t, h, "ann@test.local", "forgotten-password")
	res, err := h.Client.CreateInvite(h.Ctx(), rq(&v1.CreateInviteRequest{UserId: u.GetId()}))
	if err != nil || res.Msg.GetUserId() != u.GetId() || res.Msg.GetEmail() != "ann@test.local" {
		t.Fatalf("reset link: %+v %v", res, err)
	}
	token := linkToken(t, h, res.Msg.GetUrl())
	if _, err := h.Client.CreateInvite(h.Ctx(), rq(&v1.CreateInviteRequest{UserId: "nobody"})); code(err) != connect.CodeNotFound {
		t.Fatalf("reset link for a missing user: %v", err)
	}

	guest, _ := h.Browser()
	info, err := guest.GetInvite(h.Ctx(), rq(&v1.GetInviteRequest{Token: token}))
	if err != nil || !info.Msg.GetPasswordReset() || info.Msg.GetEmail() != "ann@test.local" {
		t.Fatalf("GetInvite: %+v %v", info, err)
	}
	done, err := guest.AcceptInvite(h.Ctx(), rq(&v1.AcceptInviteRequest{Token: token, Password: "remembered-password"}))
	if err != nil || done.Msg.GetSignedIn() || done.Msg.GetUser().GetId() != u.GetId() {
		t.Fatalf("AcceptInvite: %+v %v", done, err)
	}
	// A reset does not sign in (a second factor still stands), and it signs
	// out whoever held the old password.
	if _, err := whoAmI(guest); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("the reset signed in: %v", err)
	}
	if _, err := whoAmI(ann); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("the old session: %v", err)
	}
	if _, _, err := signInAs(h, "ann@test.local", "forgotten-password", ""); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("the old password: %v", err)
	}
	if _, _, err := signInAs(h, "ann@test.local", "remembered-password", ""); err != nil {
		t.Fatalf("the new password: %v", err)
	}
	var users int64
	h.DB.Model(&db.User{}).Count(&users)
	if users != 2 {
		t.Fatalf("%d users after a reset", users)
	}
}

func TestAdminChangesRoleAndPassword(t *testing.T) {
	h := apptest.New(t)
	ann, u := join(t, h, "ann@test.local", "first-password")
	me, _ := whoAmI(h.Client)

	// An admin cannot lock themselves out, so one admin always remains.
	for name, req := range map[string]*v1.UpdateUserRequest{
		"demote":  {Id: me.GetId(), Admin: boolp(false)},
		"disable": {Id: me.GetId(), Disabled: boolp(true)},
	} {
		if _, err := h.Client.UpdateUser(h.Ctx(), rq(req)); code(err) != connect.CodeInvalidArgument {
			t.Errorf("%s self: %v", name, err)
		}
	}
	if _, err := h.Client.DeleteUser(h.Ctx(), rq(&v1.DeleteUserRequest{Id: me.GetId()})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("delete self: %v", err)
	}
	if _, err := h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: "nobody", Admin: boolp(true)})); code(err) != connect.CodeNotFound {
		t.Fatalf("missing user: %v", err)
	}

	res, err := h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: u.GetId(), Admin: boolp(true)}))
	if err != nil || !res.Msg.GetAdmin() {
		t.Fatalf("promote: %+v %v", res, err)
	}
	if _, err := ann.ListUsers(h.Ctx(), rq(&v1.ListUsersRequest{})); err != nil {
		t.Fatalf("the promoted user: %v", err)
	}
	// Leaving a field out leaves it alone.
	res, err = h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: u.GetId()}))
	if err != nil || !res.Msg.GetAdmin() || res.Msg.GetDisabled() {
		t.Fatalf("empty update: %+v %v", res, err)
	}
	res, err = h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: u.GetId(), Admin: boolp(false)}))
	if err != nil || res.Msg.GetAdmin() {
		t.Fatalf("demote: %+v %v", res, err)
	}
	if _, err := ann.ListUsers(h.Ctx(), rq(&v1.ListUsersRequest{})); code(err) != connect.CodePermissionDenied {
		t.Fatalf("the demoted user: %v", err)
	}

	if _, err := h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: u.GetId(), Password: "short"})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("short password: %v", err)
	}
	if _, err := h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: u.GetId(), Password: "set-by-admin"})); err != nil {
		t.Fatal(err)
	}
	if _, err := whoAmI(ann); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("the user's session after an admin set their password: %v", err)
	}
	if _, _, err := signInAs(h, "ann@test.local", "set-by-admin", ""); err != nil {
		t.Fatalf("the password the admin set: %v", err)
	}
	// Setting your own password through the admin page keeps you signed in.
	if _, err := h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: me.GetId(), Password: "admins-new-password"})); err != nil {
		t.Fatal(err)
	}
	if _, err := whoAmI(h.Client); err != nil {
		t.Fatalf("the admin after setting their own password: %v", err)
	}
}

func TestDisabledUserIsLockedOutAndTheirBotsStop(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	ann, u := join(t, h, "ann@test.local", "first-password")
	bot, err := ann.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "Anns"}))
	if err != nil {
		t.Fatal(err)
	}
	au, err := ann.CreateAutomation(h.Ctx(), rq(&v1.CreateAutomationRequest{BotId: bot.Msg.GetId(), Name: "Hourly", Prompt: "Test_81_Input", Schedule: "0 * * * *", Enabled: true}))
	if err != nil {
		t.Fatal(err)
	}
	adminBot := h.CreateBot("Admins")

	res, err := h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: u.GetId(), Disabled: boolp(true)}))
	if err != nil || !res.Msg.GetDisabled() {
		t.Fatalf("disable: %+v %v", res, err)
	}
	if _, err := whoAmI(ann); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("a disabled user's session: %v", err)
	}
	if _, _, err := signInAs(h, "ann@test.local", "first-password", ""); code(err) != connect.CodePermissionDenied {
		t.Fatalf("a disabled user signing in: %v", err)
	}
	// Whether the account is disabled is only said to someone who knows the password.
	if _, _, err := signInAs(h, "ann@test.local", "wrong-password", ""); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("a wrong password on a disabled account: %v", err)
	}

	// Nothing of theirs runs: not a message, not a due automation.
	if _, err := h.App.StartRun(run.Request{BotID: bot.Msg.GetId(), ChatID: "c", Text: "hi"}); err == nil {
		t.Fatal("a disabled user's Bot started a run")
	}
	h.DB.Model(&db.Automation{}).Where("id = ?", au.Msg.GetId()).Update("next_run_at", time.Now().Add(-time.Minute))
	if n := h.App.Automations.FireDue(time.Now()); n != 0 {
		t.Fatalf("a disabled user's automation fired (%d)", n)
	}
	var runs int64
	h.DB.Model(&db.Run{}).Where("bot_id = ?", bot.Msg.GetId()).Count(&runs)
	if runs != 0 {
		t.Fatalf("%d runs on a disabled user's Bot", runs)
	}
	// Everyone else's Bots carry on.
	runID, _ := h.Send(adminBot.GetId(), "", "Test_81_Input")
	h.WaitRun(runID)

	// Nothing was deleted, and enabling brings it all back.
	res, err = h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: u.GetId(), Disabled: boolp(false)}))
	if err != nil || res.Msg.GetDisabled() {
		t.Fatalf("enable: %+v %v", res, err)
	}
	back, _, err := signInAs(h, "ann@test.local", "first-password", "")
	if err != nil {
		t.Fatalf("signing in again: %v", err)
	}
	bots, err := back.ListBots(h.Ctx(), rq(&v1.ListBotsRequest{}))
	if err != nil || len(bots.Msg.GetBots()) != 1 {
		t.Fatalf("their Bots after enabling: %+v %v", bots, err)
	}
	h.DB.Model(&db.Automation{}).Where("id = ?", au.Msg.GetId()).Update("next_run_at", time.Now().Add(-time.Minute))
	if n := h.App.Automations.FireDue(time.Now()); n != 1 {
		t.Fatalf("their automation after enabling fired %d times", n)
	}
	var row db.Automation
	h.DB.First(&row, "id = ?", au.Msg.GetId())
	h.WaitRun(row.LastRunID)
}

func TestDisabledUsersTunnelsAreClosed(t *testing.T) {
	e := newProxyEnv(t, "")
	pub := e.tunnel(t, true)
	b := e.browser()
	if res, _ := b.do(t, "GET", tunnelURL(pub, "/"), nil, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("public tunnel: %d", res.StatusCode)
	}
	h := e.h
	h.DB.Model(&db.User{}).Where("email = ?", h.Email).Update("disabled", true)
	if res, _ := b.do(t, "GET", tunnelURL(pub, "/"), nil, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("a disabled user's public tunnel: %d", res.StatusCode)
	}
	h.DB.Model(&db.User{}).Where("email = ?", h.Email).Update("disabled", false)
	if res, _ := b.do(t, "GET", tunnelURL(pub, "/"), nil, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("after enabling: %d", res.StatusCode)
	}
}

func TestDeleteUserRemovesEverythingTheyOwn(t *testing.T) {
	dummy.Reset()
	dir := t.TempDir()
	h := apptest.New(t, apptest.WithDataDir(dir))
	ann, u := join(t, h, "ann@test.local", "first-password")
	bot, err := ann.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "Anns"}))
	if err != nil {
		t.Fatal(err)
	}
	botDir := filepath.Join(dir, "bots", bot.Msg.GetId())
	if err := os.MkdirAll(botDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(skills.PersonalDir(dir, u.GetId()), "mine")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := h.CreateBot("Admins")
	h.Client.CreateInvite(h.Ctx(), rq(&v1.CreateInviteRequest{UserId: u.GetId()}))

	list, _ := h.Client.ListUsers(h.Ctx(), rq(&v1.ListUsersRequest{}))
	for _, row := range list.Msg.GetUsers() {
		if row.GetBots() != 1 {
			t.Fatalf("%s has %d Bots in the list", row.GetEmail(), row.GetBots())
		}
	}

	if _, err := h.Client.DeleteUser(h.Ctx(), rq(&v1.DeleteUserRequest{Id: u.GetId()})); err != nil {
		t.Fatal(err)
	}
	if _, err := whoAmI(ann); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("the deleted user's session: %v", err)
	}
	if _, _, err := signInAs(h, "ann@test.local", "first-password", ""); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("the deleted user signing in: %v", err)
	}
	for name, n := range map[string]int64{
		"users":    count(h, &db.User{}, "id = ?", u.GetId()),
		"bots":     count(h, &db.Bot{}, "user_id = ?", u.GetId()),
		"sessions": count(h, &db.Session{}, "user_id = ?", u.GetId()),
		"invites":  count(h, &db.Invite{}, "user_id = ?", u.GetId()),
	} {
		if n != 0 {
			t.Errorf("%d %s left", n, name)
		}
	}
	for _, gone := range []string{botDir, filepath.Dir(skillDir)} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s is still there (%v)", gone, err)
		}
	}
	if count(h, &db.Bot{}, "id = ?", keep.GetId()) != 1 {
		t.Fatal("the admin's Bot went with it")
	}
	if _, err := h.Client.DeleteUser(h.Ctx(), rq(&v1.DeleteUserRequest{Id: u.GetId()})); code(err) != connect.CodeNotFound {
		t.Fatalf("deleting twice: %v", err)
	}
	// The email is free again.
	inviteLink(t, h, "ann@test.local", false)
}

func count(h *apptest.H, model any, where string, args ...any) int64 {
	var n int64
	h.DB.Model(model).Where(where, args...).Count(&n)
	return n
}

// A disabled user's channels stop listening, and start again when the user is
// enabled; the channel itself is left as the owner set it up.
func TestDisabledUsersChannelsStopListening(t *testing.T) {
	dummy.Reset()
	slug := apptest.UniqueSlug("pausechan")
	adapter := apptest.NewFakeAdapter(slug)
	channels.Register(adapter)
	h := apptest.New(t)
	ann, u := join(t, h, "ann@test.local", "first-password")
	bot, err := ann.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "Anns"}))
	if err != nil {
		t.Fatal(err)
	}
	created, err := ann.CreateChannel(h.Ctx(), rq(&v1.CreateChannelRequest{
		BotId: bot.Msg.GetId(), Adapter: slug, Name: "Inbox", Enabled: true, Inbound: true, ExternalId: "chat-1", TargetTitle: "Target",
		Secrets: map[string]string{"token": "s3cret"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	host := adapter.WaitHost(t)
	var ch db.Channel
	h.DB.First(&ch, "id = ?", created.Msg.GetId())

	if _, err := h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: u.GetId(), Disabled: boolp(true)})); err != nil {
		t.Fatal(err)
	}
	h.DB.First(&ch, "id = ?", ch.ID)
	if ch.Status != "stopped" || !ch.Enabled {
		t.Fatalf("channel after disabling its owner: status=%q enabled=%v", ch.Status, ch.Enabled)
	}
	// Even a message that slips in starts nothing.
	if err := host.DeliverInbound(h.Ctx(), &ch, channels.Inbound{ExternalID: "chat-1", Text: "hello"}); err == nil {
		t.Fatal("a message to a disabled user's Bot was taken")
	}
	// A restart of the control plane does not bring the channel back either.
	h.App.Channels.Reconcile()
	h.DB.First(&ch, "id = ?", ch.ID)
	if ch.Status != "stopped" {
		t.Fatalf("Reconcile started a disabled user's channel: %q", ch.Status)
	}

	if _, err := h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: u.GetId(), Disabled: boolp(false)})); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.DB.First(&ch, "id = ?", ch.ID)
		if ch.Status != "stopped" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the channel did not start again after its owner was enabled")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
