package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	v1 "silo.agent/gen/silo/v1"
)

// historyWorker is a worker with a workspace and a history store of its own.
func historyWorker(t *testing.T) *worker {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	return &worker{workspace: t.TempDir(), history: filepath.Join(t.TempDir(), "history")}
}

type testChange struct {
	ID      string          `json:"id"`
	At      int64           `json:"at"`
	Base    string          `json:"base"`
	Head    string          `json:"head"`
	Files   int             `json:"files"`
	Added   int             `json:"added"`
	Deleted int             `json:"deleted"`
	Note    json.RawMessage `json:"note"`
}

type testChanges struct {
	Changes  []testChange `json:"changes"`
	Pending  *testChange  `json:"pending"`
	Since    int64        `json:"since"`
	Indexing bool         `json:"indexing"`
	Warn     string       `json:"warn"`
}

type testFile struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary"`
	Large   bool   `json:"large"`
	OldSize int64  `json:"old_size"`
	NewSize int64  `json:"new_size"`
}

func decode[T any](t *testing.T, raw string, err error) T {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("%v in %s", err, raw)
	}
	return v
}

func listChanges(t *testing.T, w *worker, lim *v1.HistoryLimits) testChanges {
	t.Helper()
	raw, err := w.changes(context.Background(), &v1.ChangesCmd{Limits: lim})
	return decode[testChanges](t, raw, err)
}

func takeCheckpoint(t *testing.T, w *worker, note string, lim *v1.HistoryLimits) checkpointResult {
	t.Helper()
	raw, err := w.checkpoint(context.Background(), &v1.CheckpointCmd{Note: note, Limits: lim})
	return decode[checkpointResult](t, raw, err)
}

func filesOf(t *testing.T, w *worker, c testChange) map[string]testFile {
	t.Helper()
	raw, err := w.changeFiles(context.Background(), &v1.ChangeFilesCmd{Base: c.Base, Head: c.Head})
	res := decode[struct {
		Files []testFile `json:"files"`
	}](t, raw, err)
	out := map[string]testFile{}
	for _, f := range res.Files {
		out[f.Path] = f
	}
	return out
}

func patchOf(t *testing.T, w *worker, c testChange, path, old string) (patch string, binary bool) {
	t.Helper()
	raw, err := w.changePatch(context.Background(), &v1.ChangePatchCmd{Base: c.Base, Head: c.Head, Path: path, OldPath: old})
	res := decode[struct {
		Patch  string `json:"patch"`
		Binary bool   `json:"binary"`
	}](t, raw, err)
	return res.Patch, res.Binary
}

func rm(t *testing.T, w *worker, rel string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(w.workspace, rel)); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryFirstLookIsTheBaseline(t *testing.T) {
	w := historyWorker(t)
	writeTree(t, w.workspace, map[string]string{"notes.md": "one\n", "src/app.py": "print(1)\n"})

	got := listChanges(t, w, nil)
	if len(got.Changes) != 0 || got.Pending != nil || got.Since == 0 {
		t.Fatalf("first look = %+v, want an empty history that starts now", got)
	}
	// Looking again takes no second checkpoint.
	again := listChanges(t, w, nil)
	if again.Since != got.Since || len(again.Changes) != 0 || again.Pending != nil {
		t.Fatalf("second look = %+v", again)
	}
	if _, err := os.Stat(filepath.Join(w.workspace, ".git")); err == nil {
		t.Fatal("the history store must not live in the workspace")
	}
}

func TestHistoryCheckpointDiffsWhatAScriptDid(t *testing.T) {
	w := historyWorker(t)
	body := strings.Repeat("a line that stays\n", 20)
	writeTree(t, w.workspace, map[string]string{
		"notes.md":   "one\ntwo\nthree\n",
		"old.txt":    "going away\n",
		"report.txt": body,
	})
	listChanges(t, w, nil)

	// Nothing here goes through a worker file command: a script did it.
	sh := exec.Command("/bin/sh", "-c", "printf 'one\\n2\\nthree\\nfour\\n' > notes.md && rm old.txt && mv report.txt final.txt && mkdir -p out && echo hi > out/new.txt")
	sh.Dir = w.workspace
	if out, err := sh.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	pending := listChanges(t, w, nil)
	if pending.Pending == nil || pending.Pending.Files != 4 || len(pending.Changes) != 0 {
		t.Fatalf("before the checkpoint = %+v, want 4 pending files", pending)
	}
	if got := filesOf(t, w, *pending.Pending); got["notes.md"].Status != "modified" {
		t.Fatalf("pending files = %+v", got)
	}

	cp := takeCheckpoint(t, w, `{"kind":"run_end","sources":[{"kind":"chat","name":"Tidy up"}]}`, nil)
	if !cp.Changed || cp.Files != 4 || cp.ID == "" {
		t.Fatalf("checkpoint = %+v", cp)
	}
	got := listChanges(t, w, nil)
	if len(got.Changes) != 1 || got.Pending != nil {
		t.Fatalf("after the checkpoint = %+v", got)
	}
	c := got.Changes[0]
	if c.ID != cp.ID || c.Files != 4 || c.Added != 3 || c.Deleted != 2 || !strings.Contains(string(c.Note), "Tidy up") {
		t.Fatalf("change = %+v note %s", c, c.Note)
	}

	files := filesOf(t, w, c)
	if f := files["notes.md"]; f.Status != "modified" || f.Added != 2 || f.Deleted != 1 || f.OldSize != 14 || f.NewSize != 17 {
		t.Fatalf("notes.md = %+v", f)
	}
	if f := files["old.txt"]; f.Status != "deleted" || f.Deleted != 1 || f.NewSize != -1 {
		t.Fatalf("old.txt = %+v", f)
	}
	if f := files["final.txt"]; f.Status != "renamed" || f.OldPath != "report.txt" || f.Added != 0 {
		t.Fatalf("final.txt = %+v", f)
	}
	if f := files["out/new.txt"]; f.Status != "added" || f.Added != 1 || f.OldSize != -1 {
		t.Fatalf("out/new.txt = %+v", f)
	}

	patch, _ := patchOf(t, w, c, "notes.md", "")
	if !strings.HasPrefix(patch, "@@ ") || !strings.Contains(patch, "-two\n") || !strings.Contains(patch, "+2\n") || !strings.Contains(patch, "+four\n") {
		t.Fatalf("patch = %q", patch)
	}
	if strings.Contains(patch, "diff --git") {
		t.Fatalf("patch keeps its file header: %q", patch)
	}
	// A pure rename has no hunks.
	if patch, binary := patchOf(t, w, c, "final.txt", "report.txt"); patch != "" || binary {
		t.Fatalf("rename patch = %q binary %v", patch, binary)
	}
}

func TestHistoryUnchangedTreeMakesNoCheckpoint(t *testing.T) {
	w := historyWorker(t)
	writeTree(t, w.workspace, map[string]string{"a.txt": "a\n"})
	first := takeCheckpoint(t, w, "", nil)
	second := takeCheckpoint(t, w, "", nil)
	if !first.Changed || second.Changed || second.ID != first.ID {
		t.Fatalf("first %+v second %+v", first, second)
	}
	refs, err := w.checkpoints(context.Background())
	if err != nil || len(refs) != 1 {
		t.Fatalf("checkpoints = %d (%v), want 1", len(refs), err)
	}
}

func TestHistoryNeverReadsDrivesScratchOrHeavyTrees(t *testing.T) {
	w := historyWorker(t)
	writeTree(t, w.workspace, map[string]string{"keep.txt": "k\n", ".gitignore": "*.log\n"})
	listChanges(t, w, nil)
	writeTree(t, w.workspace, map[string]string{
		"drives/gdrive/taxes.pdf":   "remote",
		"tmp/upload.bin":            "upload",
		"bot/screen.jpg":            "scratch",
		"web/node_modules/x/i.js":   "dep",
		"py/.venv/lib/site.py":      "dep",
		"py/__pycache__/m.pyc":      "cache",
		"build.log":                 "ignored by the workspace's own .gitignore",
		"web/src/index.ts":          "export {}\n",
		".env":                      "KEY=1\n",
		"nested/drives/not-a-mount": "an ordinary folder called drives\n",
	})
	got := listChanges(t, w, nil)
	if got.Pending == nil {
		t.Fatal("no pending change")
	}
	files := filesOf(t, w, *got.Pending)
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	for _, want := range []string{"web/src/index.ts", ".env", "nested/drives/not-a-mount"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("%s is missing from %v", want, paths)
		}
	}
	if len(files) != 3 {
		t.Fatalf("tracked %v, want only the three real files", paths)
	}
}

func TestHistoryLargeFilesAreRecordedBySizeOnly(t *testing.T) {
	w := historyWorker(t)
	lim := &v1.HistoryLimits{MaxFileBytes: 1000}
	secret := "LARGE-FILE-CONTENT-MARKER"
	big := func(n int) string { return secret + strings.Repeat("x", n) }
	writeTree(t, w.workspace, map[string]string{"video.bin": big(5000), "small.txt": "s\n", "grows.txt": "tiny\n"})
	listChanges(t, w, lim)

	writeTree(t, w.workspace, map[string]string{"video.bin": big(7000), "grows.txt": big(3000), "new.bin": big(2000)})
	got := listChanges(t, w, lim)
	if got.Pending == nil || got.Pending.Files != 3 {
		t.Fatalf("pending = %+v, want 3 files", got.Pending)
	}
	files := filesOf(t, w, *got.Pending)
	want := map[string][2]int64{
		"video.bin": {int64(len(big(5000))), int64(len(big(7000)))},
		"grows.txt": {5, int64(len(big(3000)))},
		"new.bin":   {-1, int64(len(big(2000)))},
	}
	for p, sizes := range want {
		f := files[p]
		if !f.Large || f.OldSize != sizes[0] || f.NewSize != sizes[1] || f.Added != 0 || f.Deleted != 0 {
			t.Fatalf("%s = %+v, want large %v", p, f, sizes)
		}
	}
	if files["new.bin"].Status != "added" || files["video.bin"].Status != "modified" {
		t.Fatalf("statuses = %+v", files)
	}

	// The content of a file over the cap is nowhere in the store.
	takeCheckpoint(t, w, "", lim)
	out, err := exec.Command("grep", "-r", "-l", "-a", secret, w.history).CombinedOutput()
	if err == nil {
		t.Fatalf("large file content was stored: %s", out)
	}
	objs, _ := w.git(context.Background(), nil, nil, "cat-file", "--batch-all-objects", "--batch")
	if strings.Contains(string(objs), secret) {
		t.Fatal("large file content is in an object")
	}

	// Shrinking back under the cap makes it an ordinary tracked file again.
	writeTree(t, w.workspace, map[string]string{"grows.txt": "tiny again\n"})
	got = listChanges(t, w, lim)
	if f := filesOf(t, w, *got.Pending)["grows.txt"]; !f.Large || f.NewSize != 11 || f.Status != "modified" {
		t.Fatalf("shrunk file = %+v", f)
	}
}

func TestHistoryNestedRepositoryIsOneEntry(t *testing.T) {
	w := historyWorker(t)
	writeTree(t, w.workspace, map[string]string{"readme.md": "r\n"})
	listChanges(t, w, nil)

	repo := filepath.Join(w.workspace, "project")
	writeTree(t, repo, map[string]string{"main.go": "package main\n", "go.mod": "module x\n"})
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "--quiet")
	run("add", "-A")
	run("commit", "--quiet", "-m", "first")

	got := listChanges(t, w, nil)
	if got.Pending == nil || got.Pending.Files != 1 {
		t.Fatalf("pending = %+v, want the repository as one entry", got.Pending)
	}
	if f := filesOf(t, w, *got.Pending)["project"]; f.Status != "repo" {
		t.Fatalf("project = %+v", f)
	}
	takeCheckpoint(t, w, "", nil)

	// A new commit inside it is one change; an uncommitted edit is none.
	writeTree(t, repo, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	if got := listChanges(t, w, nil); got.Pending != nil {
		t.Fatalf("an uncommitted edit in a nested repository showed up: %+v", got.Pending)
	}
	run("commit", "--quiet", "-am", "second")
	got = listChanges(t, w, nil)
	if got.Pending == nil || filesOf(t, w, *got.Pending)["project"].Status != "repo" {
		t.Fatalf("pending after a commit = %+v", got.Pending)
	}
}

func TestHistoryWorkspaceThatIsItselfARepository(t *testing.T) {
	w := historyWorker(t)
	writeTree(t, w.workspace, map[string]string{"a.txt": "a\n"})
	c := exec.Command("git", "init", "--quiet")
	c.Dir = w.workspace
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	listChanges(t, w, nil)
	writeTree(t, w.workspace, map[string]string{"a.txt": "b\n"})
	got := listChanges(t, w, nil)
	if got.Pending == nil || got.Pending.Files != 1 {
		t.Fatalf("pending = %+v", got.Pending)
	}
	// The Bot's own repository is untouched: nothing staged, no commits.
	st := exec.Command("git", "status", "--porcelain")
	st.Dir = w.workspace
	out, _ := st.CombinedOutput()
	if !strings.Contains(string(out), "?? a.txt") {
		t.Fatalf("the workspace repository was touched: %s", out)
	}
}

func TestHistoryUnreadableFileDoesNotCostTheCheckpoint(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	w := historyWorker(t)
	writeTree(t, w.workspace, map[string]string{"ok.txt": "ok\n", "locked.txt": "no\n"})
	listChanges(t, w, nil)
	writeTree(t, w.workspace, map[string]string{"ok.txt": "changed\n", "locked.txt": "changed too\n"})
	if err := os.Chmod(filepath.Join(w.workspace, "locked.txt"), 0); err != nil {
		t.Fatal(err)
	}
	got := listChanges(t, w, nil)
	if got.Pending == nil {
		t.Fatalf("no pending change (warn %q)", got.Warn)
	}
	if _, ok := filesOf(t, w, *got.Pending)["ok.txt"]; !ok || got.Warn == "" {
		t.Fatalf("pending = %+v warn %q", got.Pending, got.Warn)
	}
}

func TestHistoryBigWorkspaceIsIndexedInSlices(t *testing.T) {
	w := historyWorker(t)
	old := checkpointMaxFiles
	checkpointMaxFiles = 3
	t.Cleanup(func() { checkpointMaxFiles = old })

	files := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		files[n+".txt"] = n + "\n"
	}
	writeTree(t, w.workspace, files)

	// Each look takes the next slice; none of them is a change.
	looks := 0
	for {
		got := listChanges(t, w, nil)
		if len(got.Changes) != 0 || got.Pending != nil {
			t.Fatalf("catching up showed as a change: %+v", got)
		}
		if looks++; !got.Indexing {
			break
		}
		if looks > 10 {
			t.Fatal("indexing never finished")
		}
	}
	if looks < 3 {
		t.Fatalf("indexed in %d looks, want several slices", looks)
	}

	// From here on changes show as usual.
	writeTree(t, w.workspace, map[string]string{"a.txt": "changed\n"})
	got := listChanges(t, w, nil)
	if got.Pending == nil || got.Pending.Files != 1 {
		t.Fatalf("pending after indexing = %+v", got.Pending)
	}
}

func TestHistoryExpiresOldCheckpoints(t *testing.T) {
	w := historyWorker(t)
	lim := &v1.HistoryLimits{Keep: 3}
	for i := range 6 {
		writeTree(t, w.workspace, map[string]string{"n.txt": strings.Repeat("x\n", i+1)})
		takeCheckpoint(t, w, "", lim)
	}
	refs, err := w.checkpoints(context.Background())
	if err != nil || len(refs) != 3 {
		t.Fatalf("checkpoints = %d (%v), want 3", len(refs), err)
	}
	got := listChanges(t, w, lim)
	if len(got.Changes) != 2 {
		t.Fatalf("changes = %+v, want the two newest", got.Changes)
	}
	if got.Changes[0].At <= got.Changes[1].At {
		t.Fatal("changes are not newest first")
	}
	// The newest change still diffs.
	if f := filesOf(t, w, got.Changes[0])["n.txt"]; f.Status != "modified" || f.Added != 1 {
		t.Fatalf("n.txt = %+v", f)
	}
}

func TestHistoryRejectsObjectsItDidNotName(t *testing.T) {
	w := historyWorker(t)
	writeTree(t, w.workspace, map[string]string{"a.txt": "a\n"})
	listChanges(t, w, nil)
	ctx := context.Background()
	for _, bad := range []string{"", "HEAD", "--output=/tmp/x", "refs/silo/cp/1"} {
		if _, err := w.changeFiles(ctx, &v1.ChangeFilesCmd{Base: bad, Head: bad}); err == nil {
			t.Fatalf("changeFiles accepted %q", bad)
		}
		if _, err := w.changePatch(ctx, &v1.ChangePatchCmd{Base: bad, Head: bad, Path: "a.txt"}); err == nil {
			t.Fatalf("changePatch accepted %q", bad)
		}
	}
	gone := strings.Repeat("ab", 20)
	if _, err := w.changeFiles(ctx, &v1.ChangeFilesCmd{Base: gone, Head: gone}); err == nil || !strings.Contains(err.Error(), "no longer") {
		t.Fatalf("a forgotten change = %v", err)
	}
}

func TestHistoryBinaryFileHasNoPatch(t *testing.T) {
	w := historyWorker(t)
	writeTree(t, w.workspace, map[string]string{"pic.png": "\x89PNG\x00\x01\x02"})
	listChanges(t, w, nil)
	writeTree(t, w.workspace, map[string]string{"pic.png": "\x89PNG\x00\x03\x04\x05"})
	got := listChanges(t, w, nil)
	if got.Pending == nil {
		t.Fatal("no pending change")
	}
	if f := filesOf(t, w, *got.Pending)["pic.png"]; !f.Binary || f.Large || f.NewSize != 8 {
		t.Fatalf("pic.png = %+v", f)
	}
	if patch, binary := patchOf(t, w, *got.Pending, "pic.png", ""); patch != "" || !binary {
		t.Fatalf("patch %q binary %v", patch, binary)
	}
}
