package catalog

import (
	"bytes"
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// The document skills are self-contained directories (a Bot may enable one
// without the others), so each carries its own copy of the design kit. The
// copies must not drift: pdf is the source, edit it and copy.
var designShared = []string{"DESIGN.md", "design.css", "scripts/render.py", "scripts/theme.py"}

func TestDesignKitCopiesMatch(t *testing.T) {
	for _, f := range designShared {
		src, err := fs.ReadFile(skillFiles, "skills/pdf/"+f)
		if err != nil {
			t.Fatalf("pdf/%s: %v", f, err)
		}
		for _, sk := range []string{"word", "presentations"} {
			got, err := fs.ReadFile(skillFiles, "skills/"+sk+"/"+f)
			if err != nil {
				t.Fatalf("%s/%s: %v", sk, f, err)
			}
			if !bytes.Equal(src, got) {
				t.Errorf("%s/%s differs from pdf/%s; copy it over", sk, f, f)
			}
		}
	}
}

// Every path a document skill names under /opt/silo/skills/<name>/ must ship
// with that skill, or the Bot follows instructions to a missing file.
func TestDesignKitPathsExist(t *testing.T) {
	ref := regexp.MustCompile(`/opt/silo/skills/([a-z-]+)/([A-Za-z0-9_./-]+\.(?:py|css|html|md))`)
	err := fs.WalkDir(skillFiles, "skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".html") && !strings.HasSuffix(path, ".py") {
			return nil
		}
		b, err := fs.ReadFile(skillFiles, path)
		if err != nil {
			return err
		}
		for _, m := range ref.FindAllStringSubmatch(string(b), -1) {
			if strings.Contains(m[2], "<") {
				continue
			}
			if _, err := fs.Stat(skillFiles, "skills/"+m[1]+"/"+m[2]); err != nil {
				t.Errorf("%s names %s, which is not in the catalog", path, m[0])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// WeasyPrint 57 (Debian bookworm) drops a shorthand that holds var(), so the
// whole border or background silently disappears.
func TestDesignCSSAvoidsVarInShorthand(t *testing.T) {
	bad := regexp.MustCompile(`(?:^|[;{\s])(border(?:-top|-bottom|-left|-right)?|background|font|margin|padding)\s*:[^;}]*var\(`)
	err := fs.WalkDir(skillFiles, "skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".html")) {
			return err
		}
		b, err := fs.ReadFile(skillFiles, path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if m := bad.FindStringSubmatch(line); m != nil {
				t.Errorf("%s:%d: var() inside the %q shorthand; use longhands", path, i+1, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
