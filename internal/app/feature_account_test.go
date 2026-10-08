package app_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
)

// rq shortens connect.NewRequest in the account tests.
func rq[T any](m *T) *connect.Request[T] { return connect.NewRequest(m) }

// inviteLink has the admin invite email and returns the link's token.
func inviteLink(t *testing.T, h *apptest.H, email string, admin bool) string {
	t.Helper()
	res, err := h.Client.CreateInvite(h.Ctx(), rq(&v1.CreateInviteRequest{Email: email, Admin: admin}))
	if err != nil {
		t.Fatalf("CreateInvite(%s): %v", email, err)
	}
	return linkToken(t, h, res.Msg.GetUrl())
}

func linkToken(t *testing.T, h *apptest.H, link string) string {
	t.Helper()
	token, ok := strings.CutPrefix(link, h.URL+"/invite/")
	if !ok || token == "" {
		t.Fatalf("invite url %q is not under %s/invite/", link, h.URL)
	}
	return token
}

// join makes an account the way people get one: an admin's invite link, then
// their own password. It returns them signed in.
func join(t *testing.T, h *apptest.H, email, password string) (silov1connect.UIClient, *v1.User) {
	t.Helper()
	c, _ := h.Browser()
	res, err := c.AcceptInvite(h.Ctx(), rq(&v1.AcceptInviteRequest{Token: inviteLink(t, h, email, false), Password: password}))
	if err != nil {
		t.Fatalf("AcceptInvite(%s): %v", email, err)
	}
	return c, res.Msg.GetUser()
}

// signInAs signs in on a fresh browser.
func signInAs(h *apptest.H, email, password, code string) (silov1connect.UIClient, *v1.SignInResponse, error) {
	c, _ := h.Browser()
	res, err := c.SignIn(h.Ctx(), rq(&v1.SignInRequest{Email: email, Password: password, Code: code}))
	if err != nil {
		return c, nil, err
	}
	return c, res.Msg, nil
}

func whoAmI(c silov1connect.UIClient) (*v1.User, error) {
	res, err := c.Me(context.Background(), rq(&v1.MeRequest{}))
	if err != nil {
		return nil, err
	}
	return res.Msg.GetUser(), nil
}

func TestSignInOptionsAndPublicCalls(t *testing.T) {
	h := apptest.New(t)
	anon := h.NewClient()
	opts, err := anon.AuthOptions(h.Ctx(), rq(&v1.AuthOptionsRequest{}))
	if err != nil || !opts.Msg.GetPassword() || opts.Msg.GetOidc() || opts.Msg.GetOidcLabel() != "" {
		t.Fatalf("AuthOptions = %+v %v", opts, err)
	}
	// Everything else still needs a session.
	if _, err := anon.ListUsers(h.Ctx(), rq(&v1.ListUsersRequest{})); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("anonymous ListUsers: %v", err)
	}
	if _, err := anon.ListSessions(h.Ctx(), rq(&v1.ListSessionsRequest{})); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("anonymous ListSessions: %v", err)
	}
}

func TestSignInFindsTheEmailWhateverItsCase(t *testing.T) {
	h := apptest.New(t)
	c, res, err := signInAs(h, "  ADMIN@Test.Local ", h.Password, "")
	if err != nil || res.GetUser().GetEmail() != h.Email || res.GetNeedsCode() {
		t.Fatalf("sign in: %+v %v", res, err)
	}
	me, err := whoAmI(c)
	if err != nil || !me.GetHasPassword() || me.GetTotp() || me.GetOidc() || me.GetLastSignInAt() == "" {
		t.Fatalf("Me = %+v %v", me, err)
	}
	// A wrong password and an unknown email look the same from outside.
	_, _, wrongPass := signInAs(h, h.Email, "not-the-password", "")
	_, _, noUser := signInAs(h, "nobody@test.local", "not-the-password", "")
	if code(wrongPass) != connect.CodeUnauthenticated || code(noUser) != connect.CodeUnauthenticated || wrongPass.Error() != noUser.Error() {
		t.Fatalf("wrong password: %v; unknown email: %v", wrongPass, noUser)
	}
}

func TestChangePasswordEndsTheOtherSessions(t *testing.T) {
	h := apptest.New(t)
	here, u := join(t, h, "ann@test.local", "first-password")
	elsewhere, _, err := signInAs(h, "ann@test.local", "first-password", "")
	if err != nil {
		t.Fatal(err)
	}

	// The wrong current password is a mistake in the form, not a sign-out.
	if _, err := here.ChangePassword(h.Ctx(), rq(&v1.ChangePasswordRequest{Current: "nope-nope", Password: "second-password"})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("wrong current password: %v", err)
	}
	if _, err := here.ChangePassword(h.Ctx(), rq(&v1.ChangePasswordRequest{Current: "first-password", Password: "short"})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("short password: %v", err)
	}
	if _, err := whoAmI(elsewhere); err != nil {
		t.Fatalf("a failed change signed the other session out: %v", err)
	}

	res, err := here.ChangePassword(h.Ctx(), rq(&v1.ChangePasswordRequest{Current: "first-password", Password: "second-password"}))
	if err != nil || res.Msg.GetId() != u.GetId() {
		t.Fatalf("ChangePassword: %+v %v", res, err)
	}
	if _, err := whoAmI(here); err != nil {
		t.Fatalf("the session that changed the password: %v", err)
	}
	if _, err := whoAmI(elsewhere); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("the other session: %v", err)
	}
	if _, _, err := signInAs(h, "ann@test.local", "first-password", ""); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("the old password: %v", err)
	}
	if _, _, err := signInAs(h, "ann@test.local", "second-password", ""); err != nil {
		t.Fatalf("the new password: %v", err)
	}
	// The admin's session is someone else's and untouched.
	if _, err := whoAmI(h.Client); err != nil {
		t.Fatalf("admin: %v", err)
	}
}

func TestSessionsListAndRevoke(t *testing.T) {
	h := apptest.New(t)
	here, _ := join(t, h, "ann@test.local", "first-password")
	there, _, err := signInAs(h, "ann@test.local", "first-password", "")
	if err != nil {
		t.Fatal(err)
	}
	list, err := here.ListSessions(h.Ctx(), rq(&v1.ListSessionsRequest{}))
	if err != nil || len(list.Msg.GetSessions()) != 2 {
		t.Fatalf("ListSessions: %+v %v", list, err)
	}
	mine, other := list.Msg.GetSessions()[0], list.Msg.GetSessions()[1]
	if !mine.GetCurrent() || other.GetCurrent() || mine.GetMethod() != "invite" || other.GetMethod() != "password" {
		t.Fatalf("sessions: %+v / %+v", mine, other)
	}
	if mine.GetIp() != "127.0.0.1" || mine.GetUserAgent() == "" || mine.GetLastSeenAt() == "" || mine.GetExpiresAt() == "" {
		t.Fatalf("session details: %+v", mine)
	}
	// The id shown is not the cookie: it is useless as one.
	var rows []db.Session
	h.DB.Where("id = ?", other.GetId()).Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("session id %q is not the row's id", other.GetId())
	}

	// Nobody ends a session that is not theirs, or the one they are on.
	if _, err := h.Client.RevokeSession(h.Ctx(), rq(&v1.RevokeSessionRequest{Id: other.GetId()})); code(err) != connect.CodeNotFound {
		t.Fatalf("admin revoking a user's session: %v", err)
	}
	if _, err := here.RevokeSession(h.Ctx(), rq(&v1.RevokeSessionRequest{Id: mine.GetId()})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("revoking the current session: %v", err)
	}

	after, err := here.RevokeSession(h.Ctx(), rq(&v1.RevokeSessionRequest{Id: other.GetId()}))
	if err != nil || len(after.Msg.GetSessions()) != 1 {
		t.Fatalf("RevokeSession: %+v %v", after, err)
	}
	if _, err := whoAmI(there); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("the revoked session: %v", err)
	}

	for range 2 {
		if _, _, err := signInAs(h, "ann@test.local", "first-password", ""); err != nil {
			t.Fatal(err)
		}
	}
	after, err = here.RevokeOtherSessions(h.Ctx(), rq(&v1.RevokeOtherSessionsRequest{}))
	if err != nil || len(after.Msg.GetSessions()) != 1 || !after.Msg.GetSessions()[0].GetCurrent() {
		t.Fatalf("RevokeOtherSessions: %+v %v", after, err)
	}
	if _, err := whoAmI(here); err != nil {
		t.Fatalf("the session left: %v", err)
	}
}

func TestSignInIsThrottled(t *testing.T) {
	h := apptest.New(t)
	join(t, h, "ann@test.local", "first-password")
	for i := range 5 {
		if _, _, err := signInAs(h, "ann@test.local", "wrong-password", ""); code(err) != connect.CodeUnauthenticated {
			t.Fatalf("wrong password %d: %v", i+1, err)
		}
	}
	// Now even the right password has to wait.
	_, _, err := signInAs(h, "ann@test.local", "first-password", "")
	if code(err) != connect.CodeResourceExhausted || !strings.Contains(err.Error(), "try again in") {
		t.Fatalf("after five wrong passwords: %v", err)
	}
	// Someone else is not held up by it.
	if _, _, err := signInAs(h, h.Email, h.Password, ""); err != nil {
		t.Fatalf("another account: %v", err)
	}
}

// forwarded signs in as if through a reverse proxy that saw the client at ip.
func forwarded(h *apptest.H, ip, email, password string) (silov1connect.UIClient, error) {
	c, _ := h.Browser()
	req := rq(&v1.SignInRequest{Email: email, Password: password})
	req.Header().Set("X-Forwarded-For", ip)
	_, err := c.SignIn(h.Ctx(), req)
	return c, err
}

func TestThrottleBelievesForwardedForOnlyFromATrustedProxy(t *testing.T) {
	// The tests reach the CP from 127.0.0.1. Trusted, it is a proxy and the
	// header names the client: one address is held up, the next is not.
	dir := t.TempDir()
	h := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(dir)+"auth:\n  trusted_proxies: 127.0.0.1\n"))
	join(t, h, "ann@test.local", "first-password")
	for range 5 {
		forwarded(h, "198.51.100.1", "ann@test.local", "wrong-password")
	}
	if _, err := forwarded(h, "198.51.100.1", "ann@test.local", "first-password"); code(err) != connect.CodeResourceExhausted {
		t.Fatalf("the address that guessed: %v", err)
	}
	c, err := forwarded(h, "198.51.100.2", "ann@test.local", "first-password")
	if err != nil {
		t.Fatalf("another address behind the proxy: %v", err)
	}
	list, err := c.ListSessions(h.Ctx(), rq(&v1.ListSessionsRequest{}))
	if err != nil || list.Msg.GetSessions()[0].GetIp() != "198.51.100.2" {
		t.Fatalf("session address: %+v %v", list, err)
	}

	// Not trusted, the header is anyone's to write and changes nothing.
	plain := apptest.New(t)
	join(t, plain, "ann@test.local", "first-password")
	for range 5 {
		forwarded(plain, "198.51.100.1", "ann@test.local", "wrong-password")
	}
	if _, err := forwarded(plain, "198.51.100.2", "ann@test.local", "first-password"); code(err) != connect.CodeResourceExhausted {
		t.Fatalf("a made-up address got around the limit: %v", err)
	}
}
