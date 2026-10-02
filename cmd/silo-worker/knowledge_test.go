package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func walkPaths(t *testing.T, w *worker, path string, max int) (paths []string, truncated bool) {
	t.Helper()
	raw, err := w.walk(path, max)
	if err != nil {
		t.Fatalf("walk %q: %v", path, err)
	}
	var out struct {
		Files []struct {
			Path  string `json:"path"`
			Size  int64  `json:"size"`
			Mtime int64  `json:"mtime"`
		} `json:"files"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	for _, f := range out.Files {
		if f.Mtime == 0 {
			t.Fatalf("%s has no mtime", f.Path)
		}
		paths = append(paths, f.Path)
	}
	return paths, out.Truncated
}

func TestWalkListsRelativePathsAndPrunes(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{
		"docs/a.md":               "a",
		"docs/sub/b.txt":          "bb",
		"docs/.hidden/c.md":       "c",
		"docs/node_modules/d.md":  "d",
		"docs/.dotfile":           "e",
		"docs/__pycache__/f.pyc":  "f",
		"other/g.md":              "g",
		"drives/gdrive/h.md":      "h",
		"docs/sub/deeper/i.md":    "i",
		"docs/build/j.md":         "j",
		"docs/chrome-profile/k.x": "k",
	})
	w := &worker{workspace: ws}

	got, trunc := walkPaths(t, w, "docs", 0)
	if trunc {
		t.Fatal("unexpected truncation")
	}
	want := "docs/a.md,docs/sub/b.txt,docs/sub/deeper/i.md"
	if strings.Join(got, ",") != want {
		t.Fatalf("walk docs = %v, want %s", got, want)
	}

	// Walking the whole workspace skips drives, like grep does.
	all, _ := walkPaths(t, w, "", 0)
	for _, p := range all {
		if strings.HasPrefix(p, "drives/") {
			t.Fatalf("workspace walk descended into drives: %v", all)
		}
	}
	// A drive folder is walked when it is the root.
	d, _ := walkPaths(t, w, "/workspace/drives/gdrive", 0)
	if len(d) != 1 || d[0] != "drives/gdrive/h.md" {
		t.Fatalf("drive walk = %v", d)
	}
}

func TestWalkMissingRootIsAnError(t *testing.T) {
	ws := t.TempDir()
	w := &worker{workspace: ws}
	if _, err := w.walk("nope", 0); err == nil {
		t.Fatal("a missing folder must be an error, not an empty list")
	}
	writeTree(t, ws, map[string]string{"file.txt": "x"})
	if _, err := w.walk("file.txt", 0); err == nil {
		t.Fatal("a file is not a folder")
	}
	if _, err := w.walk("../etc", 0); err == nil {
		t.Fatal("escaping the workspace must fail")
	}
}

func TestWalkTruncates(t *testing.T) {
	ws := t.TempDir()
	files := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		files["dir/"+n+".txt"] = n
	}
	writeTree(t, ws, files)
	w := &worker{workspace: ws}
	got, trunc := walkPaths(t, w, "dir", 3)
	if len(got) != 3 || !trunc {
		t.Fatalf("got %v truncated=%v, want 3 files and truncated", got, trunc)
	}
}

func TestWalkSkipsSymlinksAndSpecials(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"dir/real.md": "x", "outside/secret.md": "s"})
	if err := os.Symlink(filepath.Join(ws, "outside"), filepath.Join(ws, "dir", "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := os.Symlink(filepath.Join(ws, "outside", "secret.md"), filepath.Join(ws, "dir", "flink.md")); err != nil {
		t.Skip("symlinks unavailable")
	}
	w := &worker{workspace: ws}
	got, _ := walkPaths(t, w, "dir", 0)
	if strings.Join(got, ",") != "dir/real.md" {
		t.Fatalf("walk followed a symlink: %v", got)
	}
}

type extractOut struct {
	Text      string `json:"text"`
	Sha256    string `json:"sha256"`
	Kind      string `json:"kind"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Detail    string `json:"detail"`
}

func runExtract(t *testing.T, w *worker, path string, max int64) extractOut {
	t.Helper()
	raw, err := w.extract(context.Background(), path, max)
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	var out extractOut
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestExtractTextHashesBytes(t *testing.T) {
	ws := t.TempDir()
	body := "# Title\n\nHello wörld\n"
	writeTree(t, ws, map[string]string{"n.md": body})
	w := &worker{workspace: ws}
	out := runExtract(t, w, "n.md", 0)
	sum := sha256.Sum256([]byte(body))
	if out.Sha256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 = %s", out.Sha256)
	}
	if out.Text != body || out.Kind != "text" || out.Truncated || out.Size != int64(len(body)) {
		t.Fatalf("out = %+v", out)
	}
}

func TestExtractSniffsUnknownExtensions(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{
		"Makefile":  "all:\n\techo hi\n",
		"notes.xyz": "plain notes\n",
		"bin.dat":   "abc\x00\x01\x02def",
		"bad.txt":   strings.Repeat("good text ", 100) + "\xff\xfe tail",
	})
	w := &worker{workspace: ws}
	if o := runExtract(t, w, "Makefile", 0); o.Kind != "text" || !strings.Contains(o.Text, "echo hi") {
		t.Fatalf("Makefile = %+v", o)
	}
	if o := runExtract(t, w, "notes.xyz", 0); o.Kind != "text" {
		t.Fatalf("notes.xyz = %+v", o)
	}
	bin := runExtract(t, w, "bin.dat", 0)
	if bin.Kind != "skipped" || bin.Text != "" || bin.Sha256 == "" {
		t.Fatalf("binary should be skipped but still hashed: %+v", bin)
	}
	if o := runExtract(t, w, "bad.txt", 0); o.Kind != "text" || !utf8.ValidString(o.Text) || !strings.Contains(o.Text, "tail") {
		t.Fatalf("invalid UTF-8 must be scrubbed: %+v", o)
	}
}

func TestExtractTruncatesOnRuneBoundary(t *testing.T) {
	ws := t.TempDir()
	body := strings.Repeat("żółć", 500) // 2-byte runes
	writeTree(t, ws, map[string]string{"long.txt": body})
	w := &worker{workspace: ws}
	out := runExtract(t, w, "long.txt", 101)
	if !out.Truncated {
		t.Fatal("want truncated")
	}
	if len(out.Text) > 101 || !strings.HasPrefix(body, out.Text) {
		t.Fatalf("text len %d", len(out.Text))
	}
	// The hash still covers the whole file.
	sum := sha256.Sum256([]byte(body))
	if out.Sha256 != hex.EncodeToString(sum[:]) {
		t.Fatal("sha256 must cover the full file")
	}
}

func TestExtractMissingAndDirectory(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"d/x.txt": "x"})
	w := &worker{workspace: ws}
	if _, err := w.extract(context.Background(), "gone.txt", 0); err == nil {
		t.Fatal("missing file must error")
	}
	if _, err := w.extract(context.Background(), "d", 0); err == nil {
		t.Fatal("directory must error")
	}
}

func TestExtractTooBigIsSkippedUnhashed(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"big.txt": "x"})
	w := &worker{workspace: ws}
	// extractFileMax is the per-file cap; a tiny cap makes any file "big".
	old := extractFileMax
	extractFileMax = 0
	defer func() { extractFileMax = old }()
	out := runExtract(t, w, "big.txt", 0)
	if out.Kind != "skipped" || out.Sha256 != "" || !strings.Contains(out.Detail, "large") {
		t.Fatalf("out = %+v", out)
	}
}

func TestExtractPDF(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"one.pdf": minimalPDF("Quarterly revenue grew")})
	w := &worker{workspace: ws}
	out := runExtract(t, w, "one.pdf", 0)
	if out.Kind != "pdf" || !strings.Contains(out.Text, "Quarterly revenue grew") {
		t.Fatalf("out = %+v", out)
	}
}

func TestExtractDocxViaPandoc(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not installed")
	}
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"src.md": "# Heading\n\nThe warranty lasts two years.\n"})
	if b, err := exec.Command("pandoc", filepath.Join(ws, "src.md"), "-o", filepath.Join(ws, "doc.docx")).CombinedOutput(); err != nil {
		t.Fatalf("pandoc: %v %s", err, b)
	}
	w := &worker{workspace: ws}
	out := runExtract(t, w, "doc.docx", 0)
	if out.Kind != "document" || !strings.Contains(out.Text, "# Heading") || !strings.Contains(out.Text, "warranty lasts two years") {
		t.Fatalf("out = %+v", out)
	}
}

func TestExtractMissingToolIsAnError(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{"x.pdf": "%PDF-1.4 not really"})
	w := &worker{workspace: ws}
	t.Setenv("PATH", t.TempDir()) // no pdftotext anywhere
	if _, err := w.extract(context.Background(), "x.pdf", 0); err == nil {
		t.Fatal("a missing extractor must be an error the CP can show")
	}
}

// minimalPDF builds a one-page PDF whose text layer is s. pdftotext rebuilds a
// missing xref, so the offsets can stay zero.
func minimalPDF(s string) string {
	stream := "BT /F1 12 Tf 72 720 Td (" + s + ") Tj ET"
	return "%PDF-1.4\n" +
		"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
		"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj\n" +
		"4 0 obj<</Length " + itoa(len(stream)) + ">>stream\n" + stream + "\nendstream endobj\n" +
		"5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\n" +
		"trailer<</Root 1 0 R/Size 6>>\n%%EOF\n"
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
