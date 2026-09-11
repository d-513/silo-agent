package auth

import (
	"testing"

	"silo.agent/internal/db"
)

func TestEnsureBootstrapCreatesMissingUser(t *testing.T) {
	gdb, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

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
