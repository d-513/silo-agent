package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
	"silo.agent/internal/skills"
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
		switch e.transportOf() {
		case "http":
			if e.HTTPURL == "" {
				t.Fatalf("%s: http entry needs http_url", e.Key)
			}
			if e.StdioCommand != "" {
				t.Fatalf("%s: http entry must not set stdio_command", e.Key)
			}
		case "stdio":
			if e.StdioCommand == "" {
				t.Fatalf("%s: stdio entry needs stdio_command", e.Key)
			}
			if e.HTTPURL != "" {
				t.Fatalf("%s: stdio entry must not set http_url", e.Key)
			}
		case "builtin":
			if e.Builtin == "" || e.HTTPURL != "" || e.StdioCommand != "" {
				t.Fatalf("%s: builtin entry needs builtin and no endpoint", e.Key)
			}
		default:
			t.Fatalf("%s: unknown transport", e.Key)
		}
	}
	if !seen["github"] {
		t.Fatal("github preset missing")
	}
}

// Lightpanda is the shared web reader: attached to every new Bot, pointed at
// the operator's LIGHTPANDA_URL, and prompted as the primary way to read pages.
func TestLightpandaPreset(t *testing.T) {
	xs, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range xs {
		if e.Key != "lightpanda" {
			continue
		}
		if !e.AutoAttach || e.transportOf() != "http" || e.HTTPURL != "${LIGHTPANDA_URL}/mcp" {
			t.Fatalf("%+v", e)
		}
		if !strings.Contains(e.Prompt, "markdown") || e.DefaultMode != "allow" {
			t.Fatalf("%+v", e)
		}
		return
	}
	t.Fatal("lightpanda preset missing")
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
	gdb := dbtest.New(t)
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
	if gh.AutoAttach || gh.Prompt != "" {
		t.Fatalf("unexpected connector defaults %+v", gh)
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

func TestSeedStdio(t *testing.T) {
	gdb := dbtest.New(t)
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var row db.Connector
	if err := gdb.First(&row, "seed_key = ?", "obsidian").Error; err != nil {
		t.Fatal(err)
	}
	if row.Transport != "stdio" || row.StdioCommand != "npx" || row.Auth != "none" {
		t.Fatalf("unexpected stdio seed %+v", row)
	}
	if !strings.Contains(row.StdioArgsJSON, "obsidian-mcp-server") {
		t.Fatalf("args not persisted: %q", row.StdioArgsJSON)
	}
	if row.HTTPURL != "" {
		t.Fatalf("stdio seed kept an http url %q", row.HTTPURL)
	}
}

func TestSeedBuiltin(t *testing.T) {
	gdb := dbtest.New(t)
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var row db.Connector
	if err := gdb.First(&row, "seed_key = ?", "email").Error; err != nil {
		t.Fatal(err)
	}
	if row.Transport != "builtin" || row.Builtin != "email" || row.Auth != "none" || row.StdioCommand != "" || row.HTTPURL != "" {
		t.Fatalf("unexpected builtin seed %+v", row)
	}
}

func TestSeedRemovedStaysGone(t *testing.T) {
	gdb := dbtest.New(t)
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var n int64
	gdb.Model(&db.Connector{}).Count(&n)
	// An admin removes a preset. The seed log keeps remembering it.
	if err := gdb.Where("seed_key = ?", "github").Delete(&db.Connector{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var count int64
	gdb.Model(&db.Connector{}).Where("seed_key = ?", "github").Count(&count)
	if count != 0 {
		t.Fatal("removed preset was re-added")
	}
	var n2 int64
	gdb.Model(&db.Connector{}).Count(&n2)
	if n2 != n-1 {
		t.Fatalf("unexpected connector count %d -> %d", n, n2)
	}
}

func TestSeedAddsUnknownKey(t *testing.T) {
	gdb := dbtest.New(t)
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	// Forget a key entirely, as if it had never been seeded upstream.
	if err := gdb.Where("key = ?", "email").Delete(&db.CatalogSeed{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var row db.Connector
	if err := gdb.First(&row, "seed_key = ?", "email").Error; err != nil {
		t.Fatalf("unknown key not seeded: %v", err)
	}
	var logged int64
	gdb.Model(&db.CatalogSeed{}).Where("key = ?", "email").Count(&logged)
	if logged != 1 {
		t.Fatal("seed key was not logged")
	}
}

func TestSeedBackfillsLog(t *testing.T) {
	gdb := dbtest.New(t)
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var n int64
	gdb.Model(&db.Connector{}).Count(&n)
	// Simulate an install seeded before the log table existed.
	if err := gdb.Where("1 = 1").Delete(&db.CatalogSeed{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var n2 int64
	gdb.Model(&db.Connector{}).Count(&n2)
	if n2 != n {
		t.Fatalf("backfill re-added presets %d -> %d", n, n2)
	}
	var logged int64
	gdb.Model(&db.CatalogSeed{}).Count(&logged)
	if logged != n {
		t.Fatalf("seed log not backfilled from rows: %d vs %d", logged, n)
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

func TestDefaultSkillsParse(t *testing.T) {
	dir := t.TempDir()
	if err := SeedSkills(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range DefaultSkills {
		info, err := skills.Load(filepath.Join(skills.LibraryDir(dir), name))
		if err != nil {
			t.Fatalf("default skill %q: %v", name, err)
		}
		if info.Name != name {
			t.Fatalf("default skill %q loaded as %q", name, info.Name)
		}
	}
}

func TestSeededName(t *testing.T) {
	if !SeededName(DefaultSkill) {
		t.Fatal("embed missing")
	}
	for _, name := range DefaultSkills {
		if !SeededName(name) {
			t.Fatalf("default skill %q missing from embed", name)
		}
	}
	if SeededName("copied-seed") || SeededName("") {
		t.Fatal("unknown name")
	}
}

func TestIdentifier(t *testing.T) {
	for in, want := range map[string]string{
		"fal.ai":           "fal_ai",
		"  Google  Drive ": "google_drive",
		"Monday.com":       "monday_com",
		"a, b; c":          "a_b_c",
		"__Git--Hub__":     "git_hub",
		"Zażółć 9":         "za_9",
		"...":              "",
	} {
		if got := Identifier(in); got != want {
			t.Errorf("Identifier(%q) = %q, want %q", in, got, want)
		}
	}
	got := Identifiers([]string{"Fal.AI", " ", "github", "fal_ai", "GitHub"})
	if len(got) != 2 || got[0] != "fal_ai" || got[1] != "github" {
		t.Fatalf("Identifiers %v", got)
	}
}

// Every catalog preset is seeded with its identifier, the name an operator
// writes in autoenable_connectors.
func TestSeedIdentifiers(t *testing.T) {
	gdb := dbtest.New(t)
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"fal": "fal_ai", "google-drive": "google_drive", "lightpanda": "lightpanda"} {
		var row db.Connector
		if err := gdb.First(&row, "seed_key = ?", key).Error; err != nil {
			t.Fatal(key, err)
		}
		if row.Identifier != want {
			t.Fatalf("%s identifier %q, want %q", key, row.Identifier, want)
		}
	}
	var blank int64
	gdb.Model(&db.Connector{}).Where("kind = ? AND COALESCE(identifier, '') = ''", KindLibrary).Count(&blank)
	if blank != 0 {
		t.Fatalf("%d presets seeded without an identifier", blank)
	}
}

// Presets from before identifiers existed get one on the next seed: the
// catalog's when it is free, otherwise the row's own id.
func TestSeedBackfillsIdentifiers(t *testing.T) {
	gdb := dbtest.New(t)
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&db.Connector{}).Where("seed_key IN ?", []string{"github", "linear"}).Update("identifier", "").Error; err != nil {
		t.Fatal(err)
	}
	// An admin's own preset from before, and one that took Linear's identifier.
	own := db.Connector{ID: "0123456789abcdef0123456789abcdef", Kind: KindLibrary, Name: "Own"}
	squat := db.Connector{ID: "fedcba9876543210fedcba9876543210", Kind: KindLibrary, Name: "Squat", Identifier: "linear"}
	for _, r := range []*db.Connector{&own, &squat} {
		if err := gdb.Create(r).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var gh, lin db.Connector
	gdb.First(&gh, "seed_key = ?", "github")
	gdb.First(&lin, "seed_key = ?", "linear")
	gdb.First(&own, "id = ?", own.ID)
	if gh.Identifier != "github" {
		t.Fatalf("github identifier %q", gh.Identifier)
	}
	if lin.Identifier != lin.ID {
		t.Fatalf("linear identifier %q, want its id %q", lin.Identifier, lin.ID)
	}
	if own.Identifier != own.ID {
		t.Fatalf("own identifier %q, want its id", own.Identifier)
	}
}

// A new catalog preset still lands when an admin's preset holds its identifier.
func TestSeedIdentifierCollision(t *testing.T) {
	gdb := dbtest.New(t)
	if err := gdb.Create(&db.Connector{ID: "0123456789abcdef0123456789abcdef", Kind: KindLibrary, Name: "Mine", Identifier: "github"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Seed(gdb); err != nil {
		t.Fatal(err)
	}
	var gh db.Connector
	if err := gdb.First(&gh, "seed_key = ?", "github").Error; err != nil {
		t.Fatal(err)
	}
	if gh.Identifier != "github_2" {
		t.Fatalf("github identifier %q", gh.Identifier)
	}
	// Two presets can never share one, whatever the case.
	if err := gdb.Create(&db.Connector{ID: "ffffffffffffffffffffffffffffffff", Kind: KindLibrary, Name: "Dup", Identifier: "github"}).Error; err == nil {
		t.Fatal("a duplicate identifier was stored")
	}
}
