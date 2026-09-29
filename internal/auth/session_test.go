package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
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
	if err := NewSession(gdb, u.ID, rec); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie set")
	}
	if !cookies[0].HttpOnly {
		t.Fatal("session cookie should be HttpOnly")
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
	gdb.Create(&db.Session{ID: "old", UserID: "u1", ExpiresAt: time.Now().Add(-time.Hour)})

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
	gdb.Create(&db.Session{ID: "s1", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)})
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
