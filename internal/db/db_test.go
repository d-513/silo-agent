package db_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
	"silo.agent/internal/ids"
)

func TestScrubsNULAndInvalidUTF8(t *testing.T) {
	gdb := dbtest.New(t)
	ev := db.RunEvent{ID: "e1", RunID: "r1", Kind: "tool_result", Body: "a\x00b\xffc", CreatedAt: time.Now()}
	if err := gdb.Create(&ev).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got db.RunEvent
	gdb.First(&got, "id = ?", "e1")
	if got.Body != "ab\ufffdc" {
		t.Fatalf("body %q", got.Body)
	}
	if err := gdb.Model(&db.RunEvent{}).Where("id = ?", "e1").Update("body", "x\x00y").Error; err != nil {
		t.Fatalf("update map: %v", err)
	}
	gdb.First(&got, "id = ?", "e1")
	if got.Body != "xy" {
		t.Fatalf("updated body %q", got.Body)
	}
	got.Tool = "t\x00"
	if err := gdb.Save(&got).Error; err != nil {
		t.Fatalf("save: %v", err)
	}
	logs := []db.LLMLog{{ID: "l1", Request: "\x00{}"}, {ID: "l2", Response: "ok\x00"}}
	if err := gdb.Create(&logs).Error; err != nil {
		t.Fatalf("batch create: %v", err)
	}
}

func TestRunEventSeqKeepsInsertOrder(t *testing.T) {
	gdb := dbtest.New(t)
	at := time.Now()
	// Same timestamp, IDs that sort the other way round.
	for _, id := range []string{"z", "m", "a"} {
		if err := gdb.Create(&db.RunEvent{ID: id, RunID: "r", Kind: "k", CreatedAt: at}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var evs []db.RunEvent
	gdb.Where("run_id = ?", "r").Order("seq").Find(&evs)
	if len(evs) != 3 || evs[0].ID != "z" || evs[1].ID != "m" || evs[2].ID != "a" {
		t.Fatalf("order %v", evs)
	}
}

func TestMigrateDropsCoreMemoryRules(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Create(&db.Rule{ID: "r1", BotID: "b1", Connector: "bot", Action: "memory", Decision: "deny"})
	gdb.Create(&db.Rule{ID: "r2", BotID: "b1", Connector: "bot", Action: "core_memory", Decision: "deny"})
	gdb.Create(&db.Rule{ID: "r3", BotID: "b1", Connector: "bot", Action: "soul", Decision: "deny"})
	if err := db.Migrate(gdb); err != nil {
		t.Fatal(err)
	}
	var got []db.Rule
	gdb.Find(&got)
	if len(got) != 1 || got[0].Action != "soul" {
		t.Fatalf("rules %+v", got)
	}
}

// Crests from before the Stack keep their shape's place and move to the hue
// nearest their old fill, once: a second migrate leaves the new numbers alone.
func TestMigrateRestacksCrestsOnce(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Where("name = ?", "stack-crests").Delete(&db.Migration{})
	// cobalt cone, ink stocking cap, emerald bobble, pink brim.
	old := map[string]int{"b1": 7*8 + 4, "b2": 11*8 + 7, "b3": 6 * 8, "b4": 9*8 + 2}
	want := map[string]int{"b1": 3*8 + 4, "b2": 4*8 + 7, "b3": 0, "b4": 1*8 + 2}
	for id, crest := range old {
		gdb.Create(&db.Bot{ID: id, Name: id, TokenHash: id, Crest: crest})
	}
	gdb.Create(&db.AuditEvent{ID: "e1", BotID: "b1", Crest: old["b1"]})
	for range 2 {
		if err := db.Migrate(gdb); err != nil {
			t.Fatal(err)
		}
		var bots []db.Bot
		gdb.Find(&bots)
		for _, b := range bots {
			if b.Crest != want[b.ID] {
				t.Fatalf("%s: crest %d, want %d", b.ID, b.Crest, want[b.ID])
			}
		}
		var e db.AuditEvent
		gdb.Find(&e, "id = ?", "e1")
		if e.Crest != want["b1"] {
			t.Fatalf("audit line: crest %d, want %d", e.Crest, want["b1"])
		}
	}
}

// Sessions from before the id was a hash keep working: the row moves to the
// hash of the cookie it was issued with, and so do the tunnel grants made from
// it. A second migrate leaves them alone.
func TestMigrateHashesOldSessionIDs(t *testing.T) {
	gdb := dbtest.New(t)
	exp := time.Now().Add(time.Hour)
	gdb.Create(&db.Session{ID: "raw-cookie", UserID: "u1", ExpiresAt: exp})
	gdb.Exec("UPDATE sessions SET created_at = NULL WHERE id = ?", "raw-cookie")
	gdb.Create(&db.Session{ID: ids.Hash("new-cookie"), UserID: "u1", ExpiresAt: exp})
	gdb.Create(&db.TunnelGrant{ID: "g1", TunnelID: "t", UserID: "u1", SessionID: "raw-cookie", ExpiresAt: exp})
	gdb.Create(&db.TunnelGrant{ID: "g2", TunnelID: "t", UserID: "u1", SessionID: ids.Hash("new-cookie"), ExpiresAt: exp})

	for range 2 {
		if err := db.Migrate(gdb); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	gdb.Model(&db.Session{}).Order("id").Pluck("id", &got)
	want := []string{ids.Hash("raw-cookie"), ids.Hash("new-cookie")}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("session ids %v, want %v", got, want)
	}
	var g1, g2 db.TunnelGrant
	gdb.First(&g1, "id = ?", "g1")
	gdb.First(&g2, "id = ?", "g2")
	if g1.SessionID != ids.Hash("raw-cookie") || g2.SessionID != ids.Hash("new-cookie") {
		t.Fatalf("grants point at %q and %q", g1.SessionID, g2.SessionID)
	}
}

// Subagents a racing spawn named alike in one lead chat, from before the
// index, keep both logs: the later one is renamed so the index can be made.
func TestMigrateRenamesClashingSubagents(t *testing.T) {
	gdb := dbtest.New(t)
	if err := gdb.Exec("DROP INDEX subagents_name").Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i, name := range []string{"scout", "Scout", "other"} {
		sa := db.Subagent{ID: fmt.Sprint("sa", i), ParentChatID: "lead", Name: name, Status: "done", CreatedAt: now.Add(time.Duration(i) * time.Second)}
		if err := gdb.Create(&sa).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatal(err)
	}
	var names []string
	gdb.Model(&db.Subagent{}).Order("created_at").Pluck("name", &names)
	if strings.Join(names, ",") != "scout,Scout-2,other" {
		t.Fatalf("names %v", names)
	}
	if err := gdb.Create(&db.Subagent{ID: "sa9", ParentChatID: "lead", Name: "SCOUT"}).Error; err == nil {
		t.Fatal("a second scout in one lead chat was stored")
	}
}
