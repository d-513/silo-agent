package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/masker"
)

func artifactWorker(t *testing.T) *worker {
	t.Helper()
	st, err := openArtifactStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	return &worker{workspace: t.TempDir(), artifacts: st}
}

func pin(t *testing.T, w *worker, c *v1.ArtifactPinCmd) pinResult {
	t.Helper()
	raw, err := w.artifactPin(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	var res pinResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

func readBlob(t *testing.T, w *worker, id string) ([]byte, error) {
	t.Helper()
	raw, err := w.artifactRead(&v1.ArtifactReadCmd{Id: id})
	if err != nil {
		return nil, err
	}
	var out struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.DecodeString(out.Data)
}

func writeWS(t *testing.T, w *worker, rel, body string) {
	t.Helper()
	p := filepath.Join(w.workspace, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func shaOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestArtifactPinKeepsTheVersion(t *testing.T) {
	w := artifactWorker(t)
	writeWS(t, w, "report.md", "# Report\n\nfirst\n")
	v1res := pin(t, w, &v1.ArtifactPinCmd{Path: "report.md", Preview: true})
	if v1res.Kind != "file" || v1res.ID != shaOf([]byte("# Report\n\nfirst\n")) || v1res.Size != 16 {
		t.Fatalf("pin = %+v", v1res)
	}
	if v1res.Preview == nil || !strings.Contains(v1res.Preview.Text, "first") {
		t.Fatalf("preview = %+v", v1res.Preview)
	}
	writeWS(t, w, "report.md", "# Report\n\nsecond\n")
	v2res := pin(t, w, &v1.ArtifactPinCmd{Path: "report.md"})
	if v2res.ID == v1res.ID {
		t.Fatal("a changed file pinned to the same id")
	}
	// The Bot can delete the file; both versions still read as they were.
	_ = os.Remove(filepath.Join(w.workspace, "report.md"))
	got, err := readBlob(t, w, v1res.ID)
	if err != nil || string(got) != "# Report\n\nfirst\n" {
		t.Fatalf("v1 = %q, %v", got, err)
	}
	got, err = readBlob(t, w, v2res.ID)
	if err != nil || string(got) != "# Report\n\nsecond\n" {
		t.Fatalf("v2 = %q, %v", got, err)
	}
}

func TestArtifactPinFolder(t *testing.T) {
	w := artifactWorker(t)
	writeWS(t, w, "app/index.html", "<h1>hi</h1><script src=app.js></script>")
	writeWS(t, w, "app/app.js", "console.log(1)")
	writeWS(t, w, "app/node_modules/x/big.js", "skipped")
	writeWS(t, w, "app/.git/HEAD", "skipped")
	res := pin(t, w, &v1.ArtifactPinCmd{Path: "app", TreeRequires: []string{"SKILL.md", "index.html"}})
	if res.Kind != "tree" || len(res.Entries) != 2 || res.Entries[0].Path != "app.js" || res.Entries[1].Path != "index.html" {
		t.Fatalf("pin = %+v", res)
	}
	raw, err := readBlob(t, w, res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if shaOf(raw) != res.ID {
		t.Fatal("manifest id is not its hash")
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil || len(m.Entries) != 2 {
		t.Fatalf("manifest = %s, %v", raw, err)
	}
	js, err := readBlob(t, w, m.Entries[0].SHA)
	if err != nil || string(js) != "console.log(1)" {
		t.Fatalf("app.js = %q, %v", js, err)
	}
	// A folder with neither entry point is refused.
	writeWS(t, w, "plain/notes.txt", "x")
	if _, err := w.artifactPin(context.Background(), &v1.ArtifactPinCmd{Path: "plain", TreeRequires: []string{"SKILL.md", "index.html"}}); err == nil {
		t.Fatal("a plain folder was pinned")
	}
	// One file as a one-entry folder: a one-file app.
	writeWS(t, w, "game.html", "<canvas></canvas>")
	one := pin(t, w, &v1.ArtifactPinCmd{Path: "game.html", AsTree: true})
	if one.Kind != "tree" || len(one.Entries) != 1 || one.Entries[0].Path != "game.html" {
		t.Fatalf("one-file app = %+v", one)
	}
}

func TestArtifactPinRefusesWhatEscapes(t *testing.T) {
	w := artifactWorker(t)
	for _, p := range []string{"", ".", "/workspace", "../etc/passwd"} {
		if _, err := w.artifactPin(context.Background(), &v1.ArtifactPinCmd{Path: p}); err == nil {
			t.Errorf("pinned %q", p)
		}
	}
	// A link is not followed out of a folder.
	writeWS(t, w, "s/SKILL.md", "---\nname: s\n---\n")
	outside := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(outside, []byte("secret"), 0o600)
	if err := os.Symlink(outside, filepath.Join(w.workspace, "s", "link")); err != nil {
		t.Fatal(err)
	}
	res := pin(t, w, &v1.ArtifactPinCmd{Path: "s", TreeRequires: []string{"SKILL.md"}})
	if len(res.Entries) != 1 {
		t.Fatalf("entries = %+v", res.Entries)
	}
	for _, id := range []string{"", "../../x", strings.Repeat("A", 64)} {
		if _, err := w.artifactRead(&v1.ArtifactReadCmd{Id: id}); err == nil {
			t.Errorf("read %q", id)
		}
	}
}

func TestArtifactPinCaps(t *testing.T) {
	w := artifactWorker(t)
	writeWS(t, w, "big.bin", strings.Repeat("x", 2000))
	res := pin(t, w, &v1.ArtifactPinCmd{Path: "big.bin", MaxFileBytes: 1000})
	if res.TooLarge == "" || res.ID != "" {
		t.Fatalf("over the cap = %+v", res)
	}
	for i := range 5 {
		writeWS(t, w, filepath.Join("many", "f"+string(rune('a'+i))), "x")
	}
	writeWS(t, w, "many/index.html", "x")
	res = pin(t, w, &v1.ArtifactPinCmd{Path: "many", MaxTreeFiles: 3})
	if res.TooLarge == "" || res.ID != "" {
		t.Fatalf("too many files = %+v", res)
	}
	if n := len(w.artifacts.blobs()); n != 0 {
		t.Fatalf("a refused pin left %d blobs", n)
	}
}

func TestArtifactForgetAndBudget(t *testing.T) {
	w := artifactWorker(t)
	writeWS(t, w, "a.txt", strings.Repeat("a", 400))
	a := pin(t, w, &v1.ArtifactPinCmd{Path: "a.txt"})
	writeWS(t, w, "s/SKILL.md", strings.Repeat("b", 400))
	writeWS(t, w, "s/shared.txt", strings.Repeat("a", 400)) // the same bytes as a.txt
	s := pin(t, w, &v1.ArtifactPinCmd{Path: "s"})
	// Forgetting a.txt keeps its blob: the folder still uses it.
	if _, err := w.artifactForget(&v1.ArtifactForgetCmd{Ids: []string{a.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := readBlob(t, w, a.ID); err != nil {
		t.Fatalf("a blob a folder uses was removed: %v", err)
	}
	if _, err := w.artifactForget(&v1.ArtifactForgetCmd{Ids: []string{s.ID}}); err != nil {
		t.Fatal(err)
	}
	if n := len(w.artifacts.blobs()); n != 0 {
		t.Fatalf("%d blobs left after forgetting everything", n)
	}

	// Over budget, the oldest pin goes and never the new one.
	writeWS(t, w, "old.txt", strings.Repeat("o", 600))
	old := pin(t, w, &v1.ArtifactPinCmd{Path: "old.txt", MaxStoreBytes: 1000})
	past := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(w.artifacts.root, "pins", old.ID), past, past)
	writeWS(t, w, "new.txt", strings.Repeat("n", 600))
	nw := pin(t, w, &v1.ArtifactPinCmd{Path: "new.txt", MaxStoreBytes: 1000})
	if len(nw.Evicted) != 1 || nw.Evicted[0] != old.ID {
		t.Fatalf("evicted = %v, want the old pin", nw.Evicted)
	}
	if _, err := readBlob(t, w, nw.ID); err != nil {
		t.Fatalf("the new pin was evicted: %v", err)
	}
	if _, err := readBlob(t, w, old.ID); err == nil || !strings.Contains(err.Error(), "not kept") {
		t.Fatalf("old read = %v", err)
	}
}

func TestArtifactStoreSweepsHalfWrittenFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	if _, err := openArtifactStore(root); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "tmp", "blob-123"), []byte("half"), 0o600)
	if _, err := openArtifactStore(root); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(filepath.Join(root, "tmp")); len(ents) != 0 {
		t.Fatalf("tmp/ kept %d files", len(ents))
	}
}

func TestArtifactReadIsNotMasked(t *testing.T) {
	// The CP checks the hash of what it reads, so the worker must hand the
	// bytes back as stored even when they hold a masked value.
	w := artifactWorker(t)
	w.mask = masker.New()
	w.mask.Add("hunter2-secret")
	writeWS(t, w, "k.txt", "token=hunter2-secret")
	res := pin(t, w, &v1.ArtifactPinCmd{Path: "k.txt"})
	cmd := &v1.Cmd{Body: &v1.Cmd_ArtifactRead{ArtifactRead: &v1.ArtifactReadCmd{Id: res.ID}}}
	out, err := w.exec(context.Background(), cmd, nil)
	if err != nil {
		t.Fatal(err)
	}
	var blob struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(w.cmdResult(cmd, out)), &blob); err != nil {
		t.Fatal(err)
	}
	got, _ := base64.StdEncoding.DecodeString(blob.Data)
	if shaOf(got) != res.ID {
		t.Fatalf("read = %q, not the pinned bytes", got)
	}
	// Every other command's answer is still masked.
	other := &v1.Cmd{Body: &v1.Cmd_FileRead{FileRead: &v1.FileReadCmd{Path: "k.txt"}}}
	if strings.Contains(w.cmdResult(other, "token=hunter2-secret"), "hunter2-secret") {
		t.Fatal("a plain result was not masked")
	}
}
