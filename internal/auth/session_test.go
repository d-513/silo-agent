package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
	"silo.agent/internal/ids"
)

func TestCheckPassword(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "correct horse") {
		t.Fatal("correct password rejected")
	}
	if CheckPassword(hash, "wrong horse") {
		t.Fatal("wrong password accepted")
	}
	if CheckPassword("short", "x") {
		t.Fatal("short hash accepted")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	gdb := dbtest.New(t)
	hash, _ := HashPassword("pw")
	u := db.User{ID: "u1", Email: "a@b.c", PasswordHash: hash}
	if err := gdb.Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	made, err := NewSession(gdb, u.ID, rec, SessionMeta{UserAgent: "Firefox", IP: "203.0.113.9", Method: "password"})
	if err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie set")
	}
	if !cookies[0].HttpOnly || cookies[0].Secure {
		t.Fatalf("session cookie flags: %+v", cookies[0])
	}
	// The table holds the hash of the cookie, never the cookie.
	var row db.Session
	gdb.First(&row)
	if row.ID != ids.Hash(cookies[0].Value) || row.ID == cookies[0].Value || made.ID != row.ID {
		t.Fatalf("session id %q for cookie %q", row.ID, cookies[0].Value)
	}
	if row.UserAgent != "Firefox" || row.IP != "203.0.113.9" || row.Method != "password" || row.LastSeenAt.IsZero() {
		t.Fatalf("session row %+v", row)
	}
	var signedIn db.User
	gdb.First(&signedIn, "id = ?", u.ID)
	if signedIn.LastSignInAt == nil {
		t.Fatal("last sign-in was not recorded")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookies[0])
	got, err := UserFromRequest(gdb, req)
	if err != nil || got.ID != u.ID {
		t.Fatalf("UserFromRequest: %v %+v", err, got)
	}
}

func TestUserFromRequestExpiredAndMissing(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Create(&db.User{ID: "u1", Email: "a@b.c"})
	gdb.Create(&db.Session{ID: ids.Hash("old"), UserID: "u1", ExpiresAt: time.Now().Add(-time.Hour)})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := UserFromRequest(gdb, req); err == nil {
		t.Fatal("missing cookie should fail")
	}
	req.AddCookie(&http.Cookie{Name: "silo_session", Value: "old"})
	if _, err := UserFromRequest(gdb, req); err == nil {
		t.Fatal("expired session should fail")
	}
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: "silo_session", Value: "ghost"})
	if _, err := UserFromRequest(gdb, req2); err == nil {
		t.Fatal("unknown session should fail")
	}
}

// A store failure is not a sign-out: the UI must not bounce to the sign-in
// screen because Postgres blinked.
func TestUserFromRequestStoreErrorIsNotAuth(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Create(&db.User{ID: "u1", Email: "a@b.c"})
	gdb.Create(&db.Session{ID: ids.Hash("s1"), UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "silo_session", Value: "s1"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := UserFromRequest(gdb.WithContext(ctx), req)
	if err == nil || errors.Is(err, ErrAuth) {
		t.Fatalf("store error should not be ErrAuth: %v", err)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: "silo_session", Value: "ghost"})
	if _, err := UserFromRequest(gdb, req2); !errors.Is(err, ErrAuth) {
		t.Fatalf("unknown session should be ErrAuth: %v", err)
	}
}

func TestClearSessionExpiresCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	ClearSession(rec)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("cookies %+v", cookies)
	}
}

func TestEnsureBootstrapIdempotentAndPromotes(t *testing.T) {
	gdb := dbtest.New(t)
	hash, _ := HashPassword("pw")
	gdb.Create(&db.User{ID: "u1", Email: "boss@local", PasswordHash: hash})
	if err := EnsureBootstrap(gdb, "boss@local", "pw"); err != nil {
		t.Fatal(err)
	}
	var u db.User
	gdb.First(&u, "email = ?", "boss@local")
	if !u.Admin {
		t.Fatal("existing user should be promoted to admin")
	}
	if err := EnsureBootstrap(gdb, "", ""); err != nil {
		t.Fatalf("empty bootstrap should be a no-op: %v", err)
	}
}

func TestSessionFromRequestNamesTheSession(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Create(&db.User{ID: "u1", Email: "a@b.c"})
	rec := httptest.NewRecorder()
	if _, err := NewSession(gdb, "u1", rec, SessionMeta{}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	s, u, err := SessionFromRequest(gdb, req)
	if err != nil || s.ID != ids.Hash(rec.Result().Cookies()[0].Value) || u.ID != "u1" {
		t.Fatalf("SessionFromRequest = %+v %+v %v", s, u, err)
	}
	if _, _, err := SessionFromRequest(gdb, httptest.NewRequest(http.MethodGet, "/", nil)); !errors.Is(err, ErrAuth) {
		t.Fatalf("no cookie: %v", err)
	}
}

// Signing out ends the session on the server, not just in the browser: a
// captured cookie must be worthless afterwards, and so must anything granted
// from that session.
func TestEndSessionDeletesTheSessionAndItsTunnelGrants(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Create(&db.User{ID: "u1", Email: "a@b.c"})
	rec := httptest.NewRecorder()
	made, err := NewSession(gdb, "u1", rec, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	cookie := rec.Result().Cookies()[0]
	gdb.Create(&db.TunnelGrant{ID: "g-mine", TunnelID: "t", UserID: "u1", SessionID: made.ID, ExpiresAt: time.Now().Add(time.Hour)})
	gdb.Create(&db.TunnelGrant{ID: "g-other", TunnelID: "t", UserID: "u1", SessionID: "another-session", ExpiresAt: time.Now().Add(time.Hour)})

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(cookie)
	if err := EndSession(gdb, req); err != nil {
		t.Fatal(err)
	}
	if _, err := UserFromRequest(gdb, req); !errors.Is(err, ErrAuth) {
		t.Fatalf("the old cookie still works: %v", err)
	}
	var grants []db.TunnelGrant
	gdb.Find(&grants)
	if len(grants) != 1 || grants[0].ID != "g-other" {
		t.Fatalf("grants after sign-out: %+v", grants)
	}

	// Nothing to end is not an error (signing out twice, or never signed in).
	if err := EndSession(gdb, httptest.NewRequest(http.MethodPost, "/", nil)); err != nil {
		t.Fatalf("no cookie: %v", err)
	}
	if err := EndSession(gdb, req); err != nil {
		t.Fatalf("second sign-out: %v", err)
	}
}

func signIn(t *testing.T, gdb *gorm.DB, userID string) (*db.Session, *http.Request) {
	t.Helper()
	rec := httptest.NewRecorder()
	s, err := NewSession(gdb, userID, rec, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	return s, req
}

func TestSecureCookie(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Create(&db.User{ID: "u1", Email: "a@b.c"})
	rec := httptest.NewRecorder()
	if _, err := NewSession(gdb, "u1", rec, SessionMeta{Secure: true}); err != nil {
		t.Fatal(err)
	}
	if c := rec.Result().Cookies()[0]; !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie %+v", c)
	}
}

// Disabling a user ends their access at the next request, whatever cookie they hold.
func TestDisabledUserIsSignedOut(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Create(&db.User{ID: "u1", Email: "a@b.c"})
	_, req := signIn(t, gdb, "u1")
	if _, err := UserFromRequest(gdb, req); err != nil {
		t.Fatal(err)
	}
	gdb.Model(&db.User{}).Where("id = ?", "u1").Update("disabled", true)
	if _, err := UserFromRequest(gdb, req); !errors.Is(err, ErrAuth) {
		t.Fatalf("a disabled user's session: %v", err)
	}
}

func TestLastSeenIsTouchedSparingly(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Create(&db.User{ID: "u1", Email: "a@b.c"})
	s, req := signIn(t, gdb, "u1")
	seen := func() time.Time {
		var row db.Session
		gdb.First(&row, "id = ?", s.ID)
		return row.LastSeenAt
	}
	first := seen()
	if _, err := UserFromRequest(gdb, req); err != nil {
		t.Fatal(err)
	}
	if !seen().Equal(first) {
		t.Fatal("a request right after sign-in rewrote last seen")
	}
	old := time.Now().Add(-time.Hour)
	gdb.Model(&db.Session{}).Where("id = ?", s.ID).Update("last_seen_at", old)
	if _, err := UserFromRequest(gdb, req); err != nil {
		t.Fatal(err)
	}
	if got := seen(); time.Since(got) > time.Minute {
		t.Fatalf("last seen still %v", got)
	}
}

func TestEndUserSessionsKeepsOne(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Create(&db.User{ID: "u1", Email: "a@b.c"})
	gdb.Create(&db.User{ID: "u2", Email: "d@e.f"})
	keep, keepReq := signIn(t, gdb, "u1")
	gone, goneReq := signIn(t, gdb, "u1")
	_, otherReq := signIn(t, gdb, "u2")
	exp := time.Now().Add(time.Hour)
	gdb.Create(&db.TunnelGrant{ID: "g-keep", TunnelID: "t", UserID: "u1", SessionID: keep.ID, ExpiresAt: exp})
	gdb.Create(&db.TunnelGrant{ID: "g-gone", TunnelID: "t", UserID: "u1", SessionID: gone.ID, ExpiresAt: exp})

	if err := EndUserSessions(gdb, "u1", keep.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := UserFromRequest(gdb, keepReq); err != nil {
		t.Fatalf("the kept session: %v", err)
	}
	if _, err := UserFromRequest(gdb, goneReq); !errors.Is(err, ErrAuth) {
		t.Fatalf("the other session: %v", err)
	}
	if _, err := UserFromRequest(gdb, otherReq); err != nil {
		t.Fatalf("another user's session: %v", err)
	}
	var grants []string
	gdb.Model(&db.TunnelGrant{}).Pluck("id", &grants)
	if len(grants) != 1 || grants[0] != "g-keep" {
		t.Fatalf("grants %v", grants)
	}

	// One session by id, and only its owner's.
	if ok, err := EndSessionByID(gdb, "u2", keep.ID); ok || err != nil {
		t.Fatalf("another user ended it: %v %v", ok, err)
	}
	if ok, err := EndSessionByID(gdb, "u1", keep.ID); !ok || err != nil {
		t.Fatalf("EndSessionByID: %v %v", ok, err)
	}
	if _, err := UserFromRequest(gdb, keepReq); !errors.Is(err, ErrAuth) {
		t.Fatalf("after EndSessionByID: %v", err)
	}
	if err := EndUserSessions(gdb, "u1", ""); err != nil {
		t.Fatalf("nothing left to end: %v", err)
	}
}

func TestEmailsAreFoundWhateverTheCase(t *testing.T) {
	gdb := dbtest.New(t)
	if err := EnsureBootstrap(gdb, " Boss@Local ", "password"); err != nil {
		t.Fatal(err)
	}
	u, err := FindUser(gdb, "BOSS@local")
	if err != nil || u == nil || u.Email != "boss@local" {
		t.Fatalf("FindUser = %+v %v", u, err)
	}
	if err := EnsureBootstrap(gdb, "boss@LOCAL", "password"); err != nil {
		t.Fatal(err)
	}
	var n int64
	gdb.Model(&db.User{}).Count(&n)
	if n != 1 {
		t.Fatalf("%d users after a second bootstrap", n)
	}
	if u, err := FindUser(gdb, "nobody@local"); u != nil || err != nil {
		t.Fatalf("FindUser(missing) = %+v %v", u, err)
	}
}

func TestValidPassword(t *testing.T) {
	if ValidPassword("1234567") == nil {
		t.Fatal("seven characters accepted")
	}
	if err := ValidPassword("ąęółżźćń"); err != nil {
		t.Fatalf("eight non-ASCII characters: %v", err)
	}
	if ValidPassword(strings.Repeat("x", 257)) == nil {
		t.Fatal("257 characters accepted")
	}
}

// A user with no password (OIDC only) can never be signed in to with one.
func TestCheckPasswordWithoutAHash(t *testing.T) {
	if CheckPassword("", "") || CheckPassword("", "anything") {
		t.Fatal("an empty hash accepted a password")
	}
}
