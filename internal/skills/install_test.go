package skills

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallZipSkipExisting(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	add("repo/skills/one/SKILL.md", "---\nname: one\ndescription: Skill one for tests. Use when testing one.\n---\n\n# One\n")
	add("repo/skills/two/SKILL.md", "---\nname: two\ndescription: Skill two for tests. Use when testing two.\n---\n\n# Two\nscripts/ok\n")
	add("repo/skills/two/scripts/ok.py", "print(1)\n")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	HTTPClient = srv.Client()
	t.Cleanup(func() { HTTPClient = http.DefaultClient })

	dest := t.TempDir()
	writeSkill(t, filepath.Join(dest, "one"), "one", "Already there. Use when testing skip.")
	got, err := InstallURL(dest, srv.URL+"/skills.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Installed) != 1 || got.Installed[0] != "two" {
		t.Fatalf("installed %+v", got.Installed)
	}
	if len(got.Skipped) != 1 || got.Skipped[0] != "one" {
		t.Fatalf("skipped %+v", got.Skipped)
	}
	if _, err := os.Stat(filepath.Join(dest, "two", "scripts", "ok.py")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallArchive(t *testing.T) {
	body := skillZip(t, [][2]string{
		{"demo/SKILL.md", "---\nname: demo\ndescription: Uploaded zip skill for tests. Use when testing upload.\n---\n\n# Demo\n"},
		{"demo/scripts/ok.py", "print(1)\n"},
	})
	dest := t.TempDir()
	got, err := InstallArchive(dest, body, "demo.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Installed) != 1 || got.Installed[0] != "demo" {
		t.Fatalf("installed %+v", got.Installed)
	}
	if _, err := os.Stat(filepath.Join(dest, "demo", "scripts", "ok.py")); err != nil {
		t.Fatal(err)
	}
	got, err = InstallArchive(dest, body, "demo.zip")
	if err != nil || len(got.Installed) != 0 || len(got.Skipped) != 1 {
		t.Fatalf("skip %+v %v", got, err)
	}
}

func TestInstallArchiveRootSkill(t *testing.T) {
	body := skillZip(t, [][2]string{
		{"SKILL.md", "---\nname: loose\ndescription: Zip of skill files with no folder. Use when testing root upload.\n---\n\n# Loose\n"},
		{"notes.txt", "hi\n"},
	})
	dest := t.TempDir()
	got, err := InstallArchive(dest, body, "loose.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Installed) != 1 || got.Installed[0] != "loose" {
		t.Fatalf("installed %+v", got.Installed)
	}
	if _, err := os.Stat(filepath.Join(dest, "loose", "notes.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallArchiveRejectsEscape(t *testing.T) {
	body := skillZip(t, [][2]string{{"../evil/SKILL.md", "---\nname: evil\ndescription: Zip slip. Use when testing escape.\n---\n\n# x\n"}})
	if _, err := InstallArchive(t.TempDir(), body, "evil.zip"); err == nil {
		t.Fatal("escape")
	}
}

func TestInstallArchiveTooBig(t *testing.T) {
	body := make([]byte, MaxDownloadBytes+1)
	copy(body, "PK")
	if _, err := InstallArchive(t.TempDir(), body, "big.zip"); err == nil {
		t.Fatal("size")
	}
}

func skillZip(t *testing.T, files [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, f[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseGitHub(t *testing.T) {
	o, r, ref, sub, ok := parseGitHub("vercel-labs/agent-skills")
	if !ok || o != "vercel-labs" || r != "agent-skills" || ref != "" || sub != "" {
		t.Fatalf("%s %s %s %s %v", o, r, ref, sub, ok)
	}
	o, r, ref, sub, ok = parseGitHub("https://github.com/vercel-labs/agent-skills/tree/main/skills/web-design-guidelines")
	if !ok || o != "vercel-labs" || r != "agent-skills" || ref != "main" || sub != "skills/web-design-guidelines" {
		t.Fatalf("%s %s %s %s %v", o, r, ref, sub, ok)
	}
}

func TestReadRel(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "one"), "one", "Skill one for read. Use when reading.")
	b, err := ReadRel(root, "one", "")
	if err != nil || !bytes.Contains(b, []byte("name: one")) {
		t.Fatalf("%s %v", b, err)
	}
	if _, err := ReadRel(root, "one", "../other"); err == nil {
		t.Fatal("escape")
	}
}
