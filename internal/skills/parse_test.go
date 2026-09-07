package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndName(t *testing.T) {
	raw := "---\nname: pdf-processing\ndescription: Extract PDF text. Use when handling PDFs.\n---\n\n# PDF\n"
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "pdf-processing" || !strings.Contains(m.Body, "# PDF") {
		t.Fatalf("%+v", m)
	}
	if err := MatchDir(m.Name, "pdf-processing"); err != nil {
		t.Fatal(err)
	}
	if err := MatchDir(m.Name, "other"); err == nil {
		t.Fatal("dir mismatch")
	}
	for _, bad := range []string{"PDF", "-pdf", "pdf-", "pdf--x", ""} {
		if ValidName(bad) == nil {
			t.Fatal(bad)
		}
	}
	if _, err := Parse("# no frontmatter\n"); err == nil {
		t.Fatal("frontmatter")
	}
}

func TestDiscoverAndSkipExisting(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "one"), "one", "Does one. Use for one.")
	writeSkill(t, filepath.Join(root, "skills", "two"), "two", "Does two. Use for two.")
	dirs, err := Discover(root, "")
	if err != nil || len(dirs) != 2 {
		t.Fatalf("%v %+v", err, dirs)
	}
	lib := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(lib, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CopyTree(filepath.Join(root, "skills", "one"), filepath.Join(lib, "one")); err != nil {
		t.Fatal(err)
	}
	if !Exists(lib, "one") || Exists(lib, "two") {
		t.Fatal("exists")
	}
}

func writeSkill(t *testing.T, dir, name, desc string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
