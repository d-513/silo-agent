package app_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/account"
	"silo.agent/internal/apptest"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// The first admin's first sign-in is told to open Admin, once, and leaves the
// marker SiloOS's console banner reads to stop showing the first password.
func TestFirstAdminLandsInAdminOnce(t *testing.T) {
	h := apptest.New(t)
	hash, err := auth.HashPassword("first-pass")
	if err != nil {
		t.Fatal(err)
	}
	u := db.User{ID: ids.New(), Email: "first@test.local", PasswordHash: hash, Admin: true, Welcome: true, CreatedAt: time.Now()}
	if err := h.DB.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(h.DataDir, account.FirstAdminMarker)
	_ = os.Remove(marker) // the harness's own admin signed in already

	signIn := func() bool {
		t.Helper()
		c, _ := h.Browser()
		r, err := c.SignIn(h.Ctx(), connect.NewRequest(&v1.SignInRequest{Email: "first@test.local", Password: "first-pass"}))
		if err != nil {
			t.Fatal(err)
		}
		return r.Msg.Welcome
	}
	if !signIn() {
		t.Fatal("the first sign-in was not welcomed")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("no marker after the first sign-in: %v", err)
	}
	if signIn() {
		t.Error("welcomed twice")
	}

	// Anyone else is never welcomed.
	_, _, other := h.Person("someone@test.local")
	var row db.User
	h.DB.First(&row, "id = ?", other.ID)
	if row.Welcome {
		t.Error("a person who is not the first admin is marked")
	}
}
