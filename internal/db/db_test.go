package db_test

import (
	"testing"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
)

func TestScrubsNULAndInvalidUTF8(t *testing.T) {
	gdb := dbtest.New(t)
	ev := db.RunEvent{ID: "e1", RunID: "r1", Kind: "tool_result", Body: "a\x00b\xffc", CreatedAt: time.Now()}
	if err := gdb.Create(&ev).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got db.RunEvent
	gdb.First(&got, "id = ?", "e1")
	if got.Body != "ab�c" {
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

func TestMigrateRenamesMemoryRule(t *testing.T) {
	gdb := dbtest.New(t)
	gdb.Create(&db.Rule{ID: "r1", BotID: "b1", Connector: "bot", Action: "memory", Decision: "deny"})
	if err := db.Migrate(gdb); err != nil {
		t.Fatal(err)
	}
	var got db.Rule
	gdb.First(&got, "id = ?", "r1")
	if got.Action != "core_memory" || got.Decision != "deny" {
		t.Fatalf("rule %+v", got)
	}
}
