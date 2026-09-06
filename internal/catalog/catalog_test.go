package catalog

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"silo.agent/internal/db"
)

func TestLoad(t *testing.T) {
	xs, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(xs) < 3 {
		t.Fatalf("got %d", len(xs))
	}
	seen := map[string]bool{}
	for _, e := range xs {
		if seen[e.Key] {
			t.Fatalf("dup key %s", e.Key)
		}
		seen[e.Key] = true
		if e.Image != "" {
			if _, _, err := imageOf(e.Image); err != nil {
				t.Fatal(e.Key, err)
			}
		}
	}
	if !seen["github"] {
		t.Fatal("github preset missing")
	}
}

func TestGuide(t *testing.T) {
	if Guide("") != "" || Guide("nope") != "" {
		t.Fatal("empty")
	}
	g := Guide("github")
	if !strings.Contains(g, "OAuth App") || strings.Contains(g, "<") {
		t.Fatal(g)
	}
	if Guide("context7") == "" || Guide("linear") == "" {
		t.Fatal("missing guides")
	}
}

func TestSeedIdempotent(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:catalog?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&db.Connector{}); err != nil {
		t.Fatal(err)
	}
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var n int64
	gdb.Model(&db.Connector{}).Count(&n)
	if n == 0 {
		t.Fatal("empty")
	}
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var n2 int64
	gdb.Model(&db.Connector{}).Count(&n2)
	if n2 != n {
		t.Fatalf("seed inserted again %d -> %d", n, n2)
	}
	var gh db.Connector
	if err := gdb.First(&gh, "seed_key = ?", "github").Error; err != nil {
		t.Fatal(err)
	}
	if gh.Kind != KindLibrary || gh.Auth != "oauth" || len(gh.Image) == 0 {
		t.Fatalf("%+v", gh)
	}
	gh.Name = "GitHub (edited)"
	gdb.Save(&gh)
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var again db.Connector
	gdb.First(&again, "seed_key = ?", "github")
	if again.Name != "GitHub (edited)" {
		t.Fatal(again.Name)
	}
}
