package catalog

import (
	"os"
	"path/filepath"
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

func TestSeedSkills(t *testing.T) {
	dir := t.TempDir()
	if err := SeedSkills(dir); err != nil {
		t.Fatal(err)
	}
	lib := filepath.Join(dir, "skills", "library", DefaultSkill, "SKILL.md")
	b, err := os.ReadFile(lib)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "name: product-self-knowledge") {
		t.Fatal(string(b))
	}
	if err := os.RemoveAll(filepath.Join(dir, "skills", "library", DefaultSkill)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "library", "keep.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedSkills(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lib); err != nil {
		t.Fatal("re-add", err)
	}
	// existing custom-looking dir is left alone if SKILL.md present
	other := filepath.Join(dir, "skills", "library", DefaultSkill)
	if err := os.WriteFile(filepath.Join(other, "SKILL.md"), []byte("---\nname: product-self-knowledge\ndescription: Edited locally. Use when testing seed skip.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedSkills(dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(other, "SKILL.md"))
	if !strings.Contains(string(got), "Edited locally") {
		t.Fatal(string(got))
	}
}

func TestSeededName(t *testing.T) {
	if !SeededName(DefaultSkill) {
		t.Fatal("embed missing")
	}
	if SeededName("copied-seed") || SeededName("") {
		t.Fatal("unknown name")
	}
}
