package connector

import (
	"testing"

	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
)

// An edit is written over the row as it stands: a token a session refreshed
// after the edit loaded the row stays (a provider that rotates refresh tokens
// has already killed the old one), and a connection deleted meanwhile is not
// put back.
func TestEditKeepsTheRefreshedToken(t *testing.T) {
	gdb := dbtest.New(t)
	s := &Service{db: gdb}
	gdb.Create(&db.Connector{ID: "conn", Kind: "custom", SpaceID: "s1", Name: "GitHub", TokenJSON: "old-token"})

	var row db.Connector
	gdb.First(&row, "id = ?", "conn")
	// A session refreshes the token while the edit is being made.
	gdb.Model(&db.Connector{}).Where("id = ?", "conn").Update("token_json", "rotated-token")
	row.Description = "edited"
	if err := s.saveEdit(&row); err != nil {
		t.Fatal(err)
	}
	var got db.Connector
	gdb.First(&got, "id = ?", "conn")
	if got.TokenJSON != "rotated-token" || got.Description != "edited" {
		t.Fatalf("after the edit: token %q, description %q", got.TokenJSON, got.Description)
	}

	gdb.Delete(&db.Connector{}, "id = ?", "conn")
	if err := s.saveEdit(&row); err == nil {
		t.Fatal("an edit of a deleted connection succeeded")
	}
	var n int64
	gdb.Model(&db.Connector{}).Where("id = ?", "conn").Count(&n)
	if n != 0 {
		t.Fatal("the edit put a deleted connection back")
	}
}
