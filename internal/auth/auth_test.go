package auth

import (
	"testing"

	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
)

func TestEnsureBootstrapCreatesMissingUser(t *testing.T) {
	gdb := dbtest.New(t)

	if err := EnsureBootstrap(gdb, "admin@local", "password"); err != nil {
		t.Fatal(err)
	}

	var u db.User
	if err := gdb.Where("email = ?", "admin@local").First(&u).Error; err != nil {
		t.Fatal(err)
	}
	if !u.Admin {
		t.Fatal("bootstrap user is not an admin")
	}
}
