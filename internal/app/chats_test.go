package app

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
)

func TestCleanTitle(t *testing.T) {
	if got := cleanTitle(`  "Invoice download"  `); got != "Invoice download" {
		t.Fatal(got)
	}
	if cleanTitle("New chat") != "" {
		t.Fatal("placeholder")
	}
}

func TestApplyGeneratedTitleSkipsRename(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Chat{ID: "c1", BotID: "b1", Title: "Work stuff"})
	if a.applyGeneratedTitle("c1", "Invoice download") {
		t.Fatal("overwrote a human title")
	}
	a.DB.Create(&db.Chat{ID: "c2", BotID: "b1", Title: "New chat"})
	if !a.applyGeneratedTitle("c2", "Invoice download") {
		t.Fatal("expected apply")
	}
	var c db.Chat
	a.DB.First(&c, "id = ?", "c2")
	if c.Title != "Invoice download" {
		t.Fatal(c.Title)
	}
}

func TestRenameChat(t *testing.T) {
	a := testApp(t, nil)
	u := &db.User{ID: "u", Email: "a@b.c"}
	a.DB.Create(u)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	a.DB.Create(&db.Chat{ID: "c1", BotID: "b1", Title: "New chat"})
	ctx := context.WithValue(context.Background(), userKey, u)
	got, err := a.RenameChat(ctx, connect.NewRequest(&v1.RenameChatRequest{BotId: "b1", Id: "c1", Title: "  Personal  "}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Msg.Title != "Personal" {
		t.Fatal(got.Msg.Title)
	}
	_, err = a.RenameChat(ctx, connect.NewRequest(&v1.RenameChatRequest{BotId: "b1", Id: "c1", Title: "   "}))
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("%v", err)
	}
}
