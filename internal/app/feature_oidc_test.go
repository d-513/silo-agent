package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/apptest"
	"silo.agent/internal/auth/oidctest"
	"silo.agent/internal/db"
)

// oidcHarness is a CP that signs people in with a provider in memory.
func oidcHarness(t *testing.T, extraYAML string) (*apptest.H, *oidctest.Provider) {
	t.Helper()
	idp := oidctest.New(t)
	dir := t.TempDir()
	yaml := apptest.DefaultYAML(dir) + "oidc:\n  issuer: " + idp.Issuer() + "\n  client_id: " + idp.ClientID + "\n  client_secret: " + idp.ClientSecret + "\n" + extraYAML
	return apptest.New(t, apptest.WithDataDir(dir), apptest.WithYAML(yaml)), idp
}

// oidcSignIn walks a browser through the round trip (start, the provider, the
// callback) and returns where the callback sent it and the UI client that
// shares the browser's cookies.
func oidcSignIn(t *testing.T, h *apptest.H, rd string) (string, silov1connect.UIClient) {
	t.Helper()
	c, hc := h.Browser()
	return oidcRoundTrip(t, h, hc, rd), c
}

func oidcRoundTrip(t *testing.T, h *apptest.H, hc *http.Client, rd string) string {
	t.Helper()
	cp, _ := url.Parse(h.URL)
	// Follow the redirects as a browser would, but stop where the callback
	// lands: that page is the web app's, which this server does not serve.
	hc.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if req.URL.Host == cp.Host && !strings.HasPrefix(req.URL.Path, "/auth/") {
			return http.ErrUseLastResponse
		}
		return nil
	}
	start := h.URL + "/auth/oidc/start"
	if rd != "" {
		start += "?rd=" + url.QueryEscape(rd)
	}
	res, err := hc.Get(start)
	if err != nil {
		t.Fatalf("oidc round trip: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("oidc round trip ended with %d at %s", res.StatusCode, res.Request.URL)
	}
	return res.Header.Get("Location")
}

func TestOIDCOptionsAndCheck(t *testing.T) {
	h, _ := oidcHarness(t, "  label: Company login\n")
	opts, err := h.NewClient().AuthOptions(h.Ctx(), rq(&v1.AuthOptionsRequest{}))
	if err != nil || !opts.Msg.GetOidc() || opts.Msg.GetOidcLabel() != "Company login" || !opts.Msg.GetPassword() {
		t.Fatalf("AuthOptions = %+v %v", opts, err)
	}
	check, err := h.Client.CheckOIDC(h.Ctx(), rq(&v1.CheckOIDCRequest{}))
	if err != nil || !strings.HasSuffix(check.Msg.GetAuthorizationEndpoint(), "/auth") || check.Msg.GetIssuer() == "" {
		t.Fatalf("CheckOIDC = %+v %v", check, err)
	}
	set, err := h.Client.GetSettings(h.Ctx(), rq(&v1.GetSettingsRequest{}))
	if err != nil || set.Msg.GetOidcRedirectUrl() != h.URL+"/auth/oidc/callback" {
		t.Fatalf("redirect url %q %v", set.Msg.GetOidcRedirectUrl(), err)
	}
	// The secret is a secret in Settings.
	for _, f := range set.Msg.GetFields() {
		if f.GetKey() == "oidc.client_secret" && !f.GetSecret() {
			t.Fatal("oidc.client_secret is shown in the clear")
		}
	}

	// Not configured: no button, the routes turn people back, the check says why.
	plain := apptest.New(t)
	if _, err := plain.Client.CheckOIDC(plain.Ctx(), rq(&v1.CheckOIDCRequest{})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("CheckOIDC with nothing configured: %v", err)
	}
	if got, _ := oidcSignIn(t, plain, ""); got != "/signin?error=oidc_off" {
		t.Fatalf("start with nothing configured went to %q", got)
	}

	// A provider that is not there is the provider's fault, not Silo being down.
	dir := t.TempDir()
	dead := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(dir)+"oidc:\n  issuer: http://127.0.0.1:1/nope\n  client_id: silo\n  client_secret: hunter2\n"))
	_, err = dead.Client.CheckOIDC(dead.Ctx(), rq(&v1.CheckOIDCRequest{}))
	if code(err) != connect.CodeUnknown || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("CheckOIDC against a dead provider: %v", err)
	}
	if got, _ := oidcSignIn(t, dead, ""); got != "/signin?error=oidc_unavailable" {
		t.Fatalf("start against a dead provider went to %q", got)
	}
}

// The issuer has to match the provider's own spelling exactly, and providers
// disagree about a trailing slash; either spelling in the config works.
func TestOIDCIssuerTrailingSlash(t *testing.T) {
	idp := oidctest.New(t)
	dir := t.TempDir()
	h := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(dir)+"oidc:\n  issuer: "+idp.Issuer()+"/\n  client_id: silo\n  client_secret: s3cret\n"))
	h.DB.Model(&db.User{}).Where("email = ?", h.Email).Update("email", "admin@test.local")
	idp.SignInAs(oidctest.Identity{Subject: "sub-admin", Email: h.Email, Verified: oidctest.Yes})
	if got, _ := oidcSignIn(t, h, ""); got != "/" {
		t.Fatalf("landed at %q", got)
	}
}

func TestOIDCLinksAnExistingAccountByVerifiedEmail(t *testing.T) {
	h, idp := oidcHarness(t, "")
	_, ann := join(t, h, "ann@test.local", "first-password")

	// An email the provider does not vouch for opens nobody's account.
	for name, who := range map[string]oidctest.Identity{
		"unverified":     {Subject: "sub-ann", Email: "ann@test.local", Verified: oidctest.No},
		"no claim":       {Subject: "sub-ann", Email: "ann@test.local"},
		"no email":       {Subject: "sub-ann", Verified: oidctest.Yes},
		"someone else's": {Subject: "sub-ann", Email: "stranger@test.local", Verified: oidctest.Yes},
	} {
		idp.SignInAs(who)
		got, c := oidcSignIn(t, h, "")
		want := "/signin?error=oidc_unverified"
		if name == "someone else's" {
			want = "/signin?error=oidc_no_account"
		}
		if got != want {
			t.Errorf("%s: landed at %q, want %q", name, got, want)
		}
		if _, err := whoAmI(c); code(err) != connect.CodeUnauthenticated {
			t.Errorf("%s: signed in (%v)", name, err)
		}
	}
	if n := count(h, &db.User{}, "oidc_subject <> ''"); n != 0 {
		t.Fatalf("%d accounts were linked by a refused sign-in", n)
	}

	idp.SignInAs(oidctest.Identity{Subject: "sub-ann", Email: "Ann@Test.Local", Verified: oidctest.Yes})
	got, c := oidcSignIn(t, h, "/bots/abc/run?x=1")
	if got != "/bots/abc/run?x=1" {
		t.Fatalf("landed at %q", got)
	}
	me, err := whoAmI(c)
	if err != nil || me.GetId() != ann.GetId() || !me.GetOidc() || !me.GetHasPassword() {
		t.Fatalf("Me = %+v %v", me, err)
	}
	list, _ := c.ListSessions(h.Ctx(), rq(&v1.ListSessionsRequest{}))
	if list.Msg.GetSessions()[0].GetMethod() != "oidc" {
		t.Fatalf("session method %q", list.Msg.GetSessions()[0].GetMethod())
	}

	// From now on it is the identity that counts, whatever email it carries.
	idp.SignInAs(oidctest.Identity{Subject: "sub-ann", Email: "renamed@test.local", Verified: oidctest.No})
	_, c = oidcSignIn(t, h, "")
	if me, err := whoAmI(c); err != nil || me.GetId() != ann.GetId() {
		t.Fatalf("the linked identity: %+v %v", me, err)
	}
	// And nobody else gets in under that email.
	idp.SignInAs(oidctest.Identity{Subject: "sub-impostor", Email: "ann@test.local", Verified: oidctest.Yes})
	if got, _ := oidcSignIn(t, h, ""); got != "/signin?error=oidc_conflict" {
		t.Fatalf("a second identity for the same email landed at %q", got)
	}
	// The password still works beside it.
	if _, _, err := signInAs(h, "ann@test.local", "first-password", ""); err != nil {
		t.Fatalf("password sign-in after linking: %v", err)
	}
}

func TestOIDCEmailFromUserInfo(t *testing.T) {
	h, idp := oidcHarness(t, "")
	idp.EmailOnlyInUserInfo()
	idp.SignInAs(oidctest.Identity{Subject: "sub-admin", Email: h.Email, Verified: oidctest.Yes})
	got, c := oidcSignIn(t, h, "")
	if me, err := whoAmI(c); got != "/" || err != nil || me.GetEmail() != h.Email {
		t.Fatalf("landed at %q as %+v (%v)", got, me, err)
	}
}

func TestOIDCMakesAccountsOnlyWhenAllowed(t *testing.T) {
	// Off by default: a stranger with a perfectly good identity is turned away.
	h, idp := oidcHarness(t, "")
	idp.SignInAs(oidctest.Identity{Subject: "sub-new", Email: "new@corp.example", Verified: oidctest.Yes})
	if got, _ := oidcSignIn(t, h, ""); got != "/signin?error=oidc_no_account" {
		t.Fatalf("landed at %q", got)
	}
	if n := count(h, &db.User{}, "1 = 1"); n != 1 {
		t.Fatalf("%d users", n)
	}

	h, idp = oidcHarness(t, "  auto_create: true\n  allowed_domains: corp.example\n")
	idp.SignInAs(oidctest.Identity{Subject: "sub-out", Email: "new@elsewhere.example", Verified: oidctest.Yes})
	if got, _ := oidcSignIn(t, h, ""); got != "/signin?error=oidc_no_account" {
		t.Fatalf("a domain off the list landed at %q", got)
	}
	idp.SignInAs(oidctest.Identity{Subject: "sub-unv", Email: "unv@corp.example", Verified: oidctest.No})
	if got, _ := oidcSignIn(t, h, ""); got != "/signin?error=oidc_unverified" {
		t.Fatalf("an unverified email landed at %q", got)
	}
	idp.SignInAs(oidctest.Identity{Subject: "sub-new", Email: "New@Corp.Example", Verified: oidctest.Yes})
	got, c := oidcSignIn(t, h, "")
	me, err := whoAmI(c)
	if got != "/" || err != nil || me.GetEmail() != "new@corp.example" || me.GetAdmin() || me.GetHasPassword() || !me.GetOidc() {
		t.Fatalf("landed at %q as %+v (%v)", got, me, err)
	}
	// A second sign-in is the same account, not another one.
	_, c = oidcSignIn(t, h, "")
	again, _ := whoAmI(c)
	if again.GetId() != me.GetId() || count(h, &db.User{}, "1 = 1") != 2 {
		t.Fatalf("second sign-in: %+v", again)
	}
	// An account with no password cannot be signed in to with one, and has
	// nothing for two-factor to protect.
	if _, _, err := signInAs(h, "new@corp.example", "", ""); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("empty password: %v", err)
	}
	if _, _, err := signInAs(h, "new@corp.example", "anything-at-all", ""); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("a password for a passwordless account: %v", err)
	}
	if _, err := c.StartTOTP(h.Ctx(), rq(&v1.StartTOTPRequest{})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("StartTOTP without a password: %v", err)
	}
	// They can give themselves a password, with no old one to ask for.
	if _, err := c.ChangePassword(h.Ctx(), rq(&v1.ChangePasswordRequest{Password: "a-first-password"})); err != nil {
		t.Fatalf("setting a first password: %v", err)
	}
	if _, _, err := signInAs(h, "new@corp.example", "a-first-password", ""); err != nil {
		t.Fatalf("the first password: %v", err)
	}
}

func TestOIDCAdminGroup(t *testing.T) {
	h, idp := oidcHarness(t, "  auto_create: true\n  admin_group: silo-admins\n")
	idp.SignInAs(oidctest.Identity{Subject: "sub-ann", Email: "ann@corp.example", Verified: oidctest.Yes, Groups: []string{"staff", "silo-admins"}})
	_, c := oidcSignIn(t, h, "")
	if me, err := whoAmI(c); err != nil || !me.GetAdmin() {
		t.Fatalf("a member of the admin group: %+v %v", me, err)
	}
	// Leaving the group takes the role away at the next sign-in.
	idp.SignInAs(oidctest.Identity{Subject: "sub-ann", Email: "ann@corp.example", Verified: oidctest.Yes, Groups: []string{"staff"}})
	_, c = oidcSignIn(t, h, "")
	if me, err := whoAmI(c); err != nil || me.GetAdmin() {
		t.Fatalf("after leaving the group: %+v %v", me, err)
	}

	// The provider cannot leave Silo with no admin at all.
	idp.SignInAs(oidctest.Identity{Subject: "sub-boss", Email: h.Email, Verified: oidctest.Yes, Groups: []string{"staff"}})
	_, c = oidcSignIn(t, h, "")
	if me, err := whoAmI(c); err != nil || !me.GetAdmin() {
		t.Fatalf("the last admin: %+v %v", me, err)
	}

	// With no admin group set, the provider has no say.
	h, idp = oidcHarness(t, "")
	idp.SignInAs(oidctest.Identity{Subject: "sub-boss", Email: h.Email, Verified: oidctest.Yes, Groups: []string{"silo-admins"}})
	_, ann := join(t, h, "ann@test.local", "first-password")
	idp.SignInAs(oidctest.Identity{Subject: "sub-ann", Email: "ann@test.local", Verified: oidctest.Yes, Groups: []string{"silo-admins"}})
	_, c = oidcSignIn(t, h, "")
	if me, err := whoAmI(c); err != nil || me.GetId() != ann.GetId() || me.GetAdmin() {
		t.Fatalf("with no admin group configured: %+v %v", me, err)
	}
}

func TestOIDCRefusesADisabledUser(t *testing.T) {
	h, idp := oidcHarness(t, "")
	_, ann := join(t, h, "ann@test.local", "first-password")
	idp.SignInAs(oidctest.Identity{Subject: "sub-ann", Email: "ann@test.local", Verified: oidctest.Yes})
	oidcSignIn(t, h, "")
	if _, err := h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: ann.GetId(), Disabled: boolp(true)})); err != nil {
		t.Fatal(err)
	}
	got, c := oidcSignIn(t, h, "")
	if got != "/signin?error=oidc_disabled" {
		t.Fatalf("landed at %q", got)
	}
	if _, err := whoAmI(c); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("a disabled user signed in: %v", err)
	}
}

func TestOIDCRoundTripIsBoundToTheBrowser(t *testing.T) {
	h, idp := oidcHarness(t, "")
	idp.SignInAs(oidctest.Identity{Subject: "sub-admin", Email: h.Email, Verified: oidctest.Yes})

	// A callback nobody started, and one started in another browser.
	c, hc := h.Browser()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for name, q := range map[string]string{"no state": "?code=x", "made-up state": "?code=x&state=made-up"} {
		res, err := hc.Get(h.URL + "/auth/oidc/callback" + q)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.Header.Get("Location"); got != "/signin?error=oidc_expired" {
			t.Errorf("%s: landed at %q", name, got)
		}
	}
	// Start in one browser, then hand the provider's answer to another.
	_, victim := h.Browser()
	victim.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if strings.HasPrefix(req.URL.Path, "/auth/oidc/callback") {
			return http.ErrUseLastResponse
		}
		return nil
	}
	res, err := victim.Get(h.URL + "/auth/oidc/start")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	callback := res.Header.Get("Location")
	if !strings.Contains(callback, "/auth/oidc/callback?") {
		t.Fatalf("the provider sent the browser to %q", callback)
	}
	res, err = hc.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Get("Location"); got != "/signin?error=oidc_expired" {
		t.Fatalf("someone else's callback landed at %q", got)
	}
	if _, err := whoAmI(c); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("signed in with someone else's callback: %v", err)
	}
	// And that answer is spent: the browser that started cannot use it now either.
	victim.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err = victim.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Get("Location"); got != "/signin?error=oidc_expired" {
		t.Fatalf("a replayed callback landed at %q", got)
	}
}

func TestOIDCRefusesWhatTheProviderGotWrong(t *testing.T) {
	h, idp := oidcHarness(t, "")
	idp.Deny("access_denied")
	if got, _ := oidcSignIn(t, h, ""); got != "/signin?error=oidc_denied" {
		t.Fatalf("a refused sign-in landed at %q", got)
	}
	idp.SignInAs(oidctest.Identity{Subject: "sub-admin", Email: h.Email, Verified: oidctest.Yes})
	idp.WrongNonce()
	got, c := oidcSignIn(t, h, "")
	if got != "/signin?error=oidc_failed" {
		t.Fatalf("a token with the wrong nonce landed at %q", got)
	}
	if _, err := whoAmI(c); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("signed in on a bad token: %v", err)
	}

	// A wrong client secret fails at the provider, and never shows the secret.
	idp2 := oidctest.New(t)
	dir := t.TempDir()
	bad := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(dir)+"oidc:\n  issuer: "+idp2.Issuer()+"\n  client_id: silo\n  client_secret: wrong\n"))
	idp2.SignInAs(oidctest.Identity{Subject: "sub-admin", Email: bad.Email, Verified: oidctest.Yes})
	if got, _ := oidcSignIn(t, bad, ""); got != "/signin?error=oidc_failed" {
		t.Fatalf("a wrong client secret landed at %q", got)
	}
}

// After signing in the browser goes only to a plain path on this site.
func TestOIDCLandsOnlyOnThisSite(t *testing.T) {
	h, idp := oidcHarness(t, "")
	idp.SignInAs(oidctest.Identity{Subject: "sub-admin", Email: h.Email, Verified: oidctest.Yes})
	for rd, want := range map[string]string{
		"":                             "/",
		"/admin/users":                 "/admin/users",
		"/tunnels/auth?name=a&rd=%2Fx": "/tunnels/auth?name=a&rd=%2Fx",
		"https://evil.example/":        "/",
		"//evil.example/":              "/",
		"/\\evil.example":              "/",
		"/signin?from=/x":              "/",
		"/auth/oidc/start":             "/",
	} {
		if got, _ := oidcSignIn(t, h, rd); got != want {
			t.Errorf("rd=%q landed at %q, want %q", rd, got, want)
		}
	}
}

func TestPasswordSignInCanBeTurnedOffOnceOIDCWorks(t *testing.T) {
	idp := oidctest.New(t)
	dir := t.TempDir()
	h := apptest.New(t, apptest.WithSignedOut(), apptest.WithYAML(apptest.DefaultYAML(dir)+
		"auth:\n  password: false\noidc:\n  issuer: "+idp.Issuer()+"\n  client_id: silo\n  client_secret: s3cret\n"))
	opts, err := h.NewClient().AuthOptions(h.Ctx(), rq(&v1.AuthOptionsRequest{}))
	if err != nil || opts.Msg.GetPassword() || !opts.Msg.GetOidc() {
		t.Fatalf("AuthOptions = %+v %v", opts, err)
	}
	if _, _, err := signInAs(h, h.Email, h.Password, ""); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("password sign-in while off: %v", err)
	}
	idp.SignInAs(oidctest.Identity{Subject: "sub-admin", Email: h.Email, Verified: oidctest.Yes})
	_, c := oidcSignIn(t, h, "")
	if _, err := whoAmI(c); err != nil {
		t.Fatalf("OIDC sign-in: %v", err)
	}
}

// Nobody confirms an email a user types in for themselves, so single sign-on
// does not match an account by one. Otherwise a user could take a colleague's
// address, wait for the colleague to sign in, and share their identity; with
// an admin group, their admin role too.
func TestOIDCDoesNotMatchAnEmailTheUserSetThemselves(t *testing.T) {
	h, idp := oidcHarness(t, "  admin_group: silo-admins\n")
	mallory, u := join(t, h, "mallory@test.local", "mallorys-password")
	if _, err := mallory.ChangeEmail(h.Ctx(), rq(&v1.ChangeEmailRequest{Email: "boss@corp.example"})); err != nil {
		t.Fatal(err)
	}
	boss := oidctest.Identity{Subject: "sub-boss", Email: "boss@corp.example", Verified: oidctest.Yes, Groups: []string{"silo-admins"}}
	idp.SignInAs(boss)
	got, c := oidcSignIn(t, h, "")
	if got != "/signin?error=oidc_conflict" {
		t.Fatalf("landed at %q", got)
	}
	if _, err := whoAmI(c); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("signed in to the squatted account: %v", err)
	}
	if me, err := whoAmI(mallory); err != nil || me.GetAdmin() || me.GetOidc() {
		t.Fatalf("the squatter after the boss tried to sign in: %+v %v", me, err)
	}

	// An address an admin sets is vouched for, and is matched again.
	if _, err := h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: u.GetId(), Email: "boss@corp.example"})); err != nil {
		t.Fatal(err)
	}
	got, c = oidcSignIn(t, h, "")
	if me, err := whoAmI(c); got != "/" || err != nil || me.GetId() != u.GetId() || !me.GetOidc() {
		t.Fatalf("after the admin set the email: landed at %q as %+v (%v)", got, me, err)
	}

	// Someone already linked can call themselves what they like: it is the
	// identity that signs them in.
	if _, err := c.ChangeEmail(h.Ctx(), rq(&v1.ChangeEmailRequest{Email: "the.boss@corp.example"})); err != nil {
		t.Fatal(err)
	}
	_, c = oidcSignIn(t, h, "")
	if me, err := whoAmI(c); err != nil || me.GetId() != u.GetId() || me.GetEmail() != "the.boss@corp.example" {
		t.Fatalf("a linked user after changing their email: %+v %v", me, err)
	}
}
