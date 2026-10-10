package app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// changesFixture is a Bot with a real worker, so the snapshots are real git.
type changesFixture struct {
	h     *apptest.H
	botID string
	w     *apptest.Worker
}

func newChanges(t *testing.T, opts ...apptest.Option) *changesFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := apptest.New(t, opts...)
	bot := h.CreateBot("Tracked")
	return &changesFixture{h: h, botID: bot.GetId(), w: h.StartWorker(bot.GetId())}
}

func (f *changesFixture) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(f.w.Workspace, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *changesFixture) list(t *testing.T) *v1.ListChangesResponse {
	t.Helper()
	res, err := f.h.Client.ListChanges(f.h.Ctx(), connect.NewRequest(&v1.ListChangesRequest{BotId: f.botID}))
	if err != nil {
		t.Fatalf("ListChanges: %v", err)
	}
	return res.Msg
}

// waitChanges polls until n changes are listed: a run's closing snapshot is
// taken just after its done event.
func (f *changesFixture) waitChanges(t *testing.T, n int) *v1.ListChangesResponse {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		got := f.list(t)
		if len(got.GetChanges()) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %d changes, have %+v (pending %+v)", n, got.GetChanges(), got.GetPending())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *changesFixture) files(t *testing.T, c *v1.Change) map[string]*v1.ChangeFile {
	t.Helper()
	res, err := f.h.Client.ListChangeFiles(f.h.Ctx(), connect.NewRequest(&v1.ListChangeFilesRequest{BotId: f.botID, Base: c.GetBase(), Head: c.GetHead()}))
	if err != nil {
		t.Fatalf("ListChangeFiles: %v", err)
	}
	out := map[string]*v1.ChangeFile{}
	for _, file := range res.Msg.GetFiles() {
		out[file.GetPath()] = file
	}
	return out
}

func (f *changesFixture) patch(t *testing.T, c *v1.Change, path string) *v1.GetChangePatchResponse {
	t.Helper()
	res, err := f.h.Client.GetChangePatch(f.h.Ctx(), connect.NewRequest(&v1.GetChangePatchRequest{BotId: f.botID, Base: c.GetBase(), Head: c.GetHead(), Path: path}))
	if err != nil {
		t.Fatalf("GetChangePatch %s: %v", path, err)
	}
	return res.Msg
}

func TestChangesTellARunFromWhatHappenedOutsideIt(t *testing.T) {
	dummy.Reset()
	// A script edits the files: no file tool is involved.
	dummy.Script("Test_changes_1",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"printf 'one\\n2\\nthree\\n' > notes.md && echo made > out.txt"}`}}},
		dummy.Turn{Text: "edited"},
	)
	f := newChanges(t)
	f.write(t, "notes.md", "one\ntwo\nthree\n")
	if got := f.list(t); got.GetState() != "ok" || len(got.GetChanges()) != 0 || got.GetPending() != nil || got.GetSince() == "" {
		t.Fatalf("first look = %+v, want an empty history that starts now", got)
	}

	// The owner changes a file by hand: it is pending, and nobody's run.
	f.write(t, "mine.txt", "by hand\n")
	got := f.list(t)
	if p := got.GetPending(); p == nil || p.GetFiles() != 1 || !p.GetPending() || len(p.GetSources()) != 0 {
		t.Fatalf("pending = %+v, want one file outside a run", got.GetPending())
	}
	if file := f.files(t, got.GetPending())["mine.txt"]; file.GetStatus() != "added" {
		t.Fatalf("mine.txt = %+v", file)
	}

	chat := f.h.FirstChat(f.botID)
	runID, _ := f.h.Send(f.botID, chat, "Test_changes_1")
	f.h.WaitRun(runID)

	got = f.waitChanges(t, 2)
	if got.GetPending() != nil {
		t.Fatalf("pending after the run = %+v", got.GetPending())
	}
	run, before := got.GetChanges()[0], got.GetChanges()[1]
	if len(before.GetSources()) != 0 || before.GetFiles() != 1 {
		t.Fatalf("the change before the run = %+v, want the owner's one file and no source", before)
	}
	if _, ok := f.files(t, before)["mine.txt"]; !ok {
		t.Fatalf("the owner's edit is not its own change: %+v", f.files(t, before))
	}
	if len(run.GetSources()) != 1 || run.GetSources()[0].GetKind() != "chat" || run.GetSources()[0].GetChatId() != chat || run.GetSources()[0].GetName() == "" {
		t.Fatalf("the run's change sources = %+v, want the chat", run.GetSources())
	}
	if run.GetFiles() != 2 || run.GetAdded() != 2 || run.GetDeleted() != 1 || run.GetAt() == "" || run.GetId() == "" {
		t.Fatalf("the run's change = %+v", run)
	}
	files := f.files(t, run)
	if files["notes.md"].GetStatus() != "modified" || files["out.txt"].GetStatus() != "added" || len(files) != 2 {
		t.Fatalf("the run's files = %+v", files)
	}
	p := f.patch(t, run, "notes.md")
	if !strings.Contains(p.GetPatch(), "-two\n") || !strings.Contains(p.GetPatch(), "+2\n") || p.GetBinary() {
		t.Fatalf("patch = %+v", p)
	}
}

func TestChangesNameEveryRunAtWorkOnTheBot(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_changes_slow",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"echo a > a.txt && sleep 1.5 && echo a2 > a2.txt"}`}}},
		dummy.Turn{Text: "slow done"},
	)
	dummy.Script("Test_changes_fast",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "write", Arguments: `{"path":"b.txt","content":"b\n"}`}}},
		dummy.Turn{Text: "fast done"},
	)
	f := newChanges(t)
	f.list(t)
	slowChat := f.h.FirstChat(f.botID)
	c, err := f.h.Client.CreateChat(f.h.Ctx(), connect.NewRequest(&v1.CreateChatRequest{BotId: f.botID}))
	if err != nil {
		t.Fatal(err)
	}
	fastChat := c.Msg.GetId()

	slow, _ := f.h.Send(f.botID, slowChat, "Test_changes_slow")
	// Let the slow run reach its script before the fast one starts.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(f.w.Workspace, "a.txt")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the slow run never wrote a.txt")
		}
		time.Sleep(10 * time.Millisecond)
	}
	fast, _ := f.h.Send(f.botID, fastChat, "Test_changes_fast")
	f.h.WaitRun(fast)
	f.h.WaitRun(slow)

	// Every file is accounted for, and the change holding the fast run's file
	// names both runs: either could have written it.
	deadline = time.Now().Add(15 * time.Second)
	for {
		got := f.list(t)
		seen := map[string]bool{}
		var shared *v1.Change
		for _, ch := range got.GetChanges() {
			for p := range f.files(t, ch) {
				seen[p] = true
				if p == "b.txt" {
					shared = ch
				}
			}
		}
		if seen["a.txt"] && seen["a2.txt"] && seen["b.txt"] && got.GetPending() == nil {
			chats := map[string]bool{}
			for _, s := range shared.GetSources() {
				chats[s.GetChatId()] = true
			}
			if !chats[slowChat] || !chats[fastChat] {
				t.Fatalf("the change with b.txt names %+v, want both chats", shared.GetSources())
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("changes never settled: %+v pending %+v", got.GetChanges(), got.GetPending())
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func TestChangesMaskSecretsInADiff(t *testing.T) {
	f := newChanges(t)
	const secret = "sk-very-secret-value-123456"
	if _, err := f.h.Client.AddSecret(f.h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{BotId: f.botID, Name: "api", Value: secret})); err != nil {
		t.Fatal(err)
	}
	f.list(t)
	f.write(t, ".env", "API_KEY="+secret+"\n")
	got := f.list(t)
	if got.GetPending() == nil {
		t.Fatal("no pending change")
	}
	p := f.patch(t, got.GetPending(), ".env")
	if strings.Contains(p.GetPatch(), secret) || !strings.Contains(p.GetPatch(), "+API_KEY=") {
		t.Fatalf("patch = %q", p.GetPatch())
	}
}

func TestChangesOffTakesNoSnapshots(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_changes_off",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "write", Arguments: `{"path":"x.txt","content":"x\n"}`}}},
		dummy.Turn{Text: "written"},
	)
	dir := t.TempDir()
	f := newChanges(t, apptest.WithYAML(apptest.DefaultYAML(dir)+"changes:\n  enabled: false\n"))
	if got := f.list(t); got.GetState() != "off" || got.GetPending() != nil || len(got.GetChanges()) != 0 {
		t.Fatalf("off = %+v", got)
	}
	runID, _ := f.h.Send(f.botID, f.h.FirstChat(f.botID), "Test_changes_off")
	f.h.WaitRun(runID)
	time.Sleep(200 * time.Millisecond) // a closing snapshot would have landed by now
	if _, err := os.Stat(f.w.History); err == nil {
		t.Fatal("a history store was made with change tracking off")
	}
}

func TestChangesBelongToTheOwner(t *testing.T) {
	f := newChanges(t)
	f.list(t)
	f.write(t, "a.txt", "a\n")
	mine := f.list(t).GetPending()
	if mine == nil {
		t.Fatal("no pending change")
	}
	stranger, _ := f.h.SignedInUser("eve@test.local")
	if _, err := stranger.ListChanges(f.h.Ctx(), connect.NewRequest(&v1.ListChangesRequest{BotId: f.botID})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("stranger ListChanges = %v", err)
	}
	if _, err := stranger.ListChangeFiles(f.h.Ctx(), connect.NewRequest(&v1.ListChangeFilesRequest{BotId: f.botID, Base: mine.GetBase(), Head: mine.GetHead()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("stranger ListChangeFiles = %v", err)
	}
	if _, err := stranger.GetChangePatch(f.h.Ctx(), connect.NewRequest(&v1.GetChangePatchRequest{BotId: f.botID, Base: mine.GetBase(), Head: mine.GetHead(), Path: "a.txt"})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("stranger GetChangePatch = %v", err)
	}
}

func TestChangesNeedTheMachineAndAKnownChange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := apptest.New(t)
	bot := h.CreateBot("Asleep")
	// No worker: the history lives on the machine, so there is nothing to read.
	if _, err := h.Client.ListChanges(h.Ctx(), connect.NewRequest(&v1.ListChangesRequest{BotId: bot.GetId()})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("ListChanges with no worker = %v", err)
	}
	h.StartWorker(bot.GetId())
	gone := strings.Repeat("ab", 20)
	for _, pair := range [][2]string{{gone, gone}, {"HEAD", "--help"}} {
		_, err := h.Client.ListChangeFiles(h.Ctx(), connect.NewRequest(&v1.ListChangeFilesRequest{BotId: bot.GetId(), Base: pair[0], Head: pair[1]}))
		if connect.CodeOf(err) != connect.CodeNotFound {
			t.Fatalf("ListChangeFiles %v = %v, want NotFound", pair, err)
		}
	}
}

// TestContainerChanges is change tracking against a real box: git from the bot
// image, the history store bound from beside the workspace, and a script's
// edits showing up as the run's change. It also survives a container reset,
// because the store is not in the container.
func TestContainerChanges(t *testing.T) {
	if !apptest.ContainersEnabled(t) {
		return
	}
	dummy.Reset()
	dummy.Script("Test_changes_box",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"mkdir -p site && printf '<h1>hi</h1>\\n' > site/index.html && echo scratch > bot/notes.txt"}`}}},
		dummy.Turn{Text: "built"},
	)
	h := newContainerHarness(t)
	id := h.SeedBot("Tracked box")
	h.StartSeededBot(id)
	f := &changesFixture{h: h, botID: id}
	if got := f.list(t); got.GetState() != "ok" || len(got.GetChanges()) != 0 {
		t.Fatalf("first look in the box = %+v", got)
	}

	chat := h.FirstChat(id)
	runID, _ := h.Send(id, chat, "Test_changes_box")
	h.WaitRun(runID)
	got := f.waitChanges(t, 1)
	c := got.GetChanges()[0]
	if len(c.GetSources()) != 1 || c.GetSources()[0].GetChatId() != chat {
		t.Fatalf("sources = %+v", c.GetSources())
	}
	// bot/ is the Bot's scratch: it is not tracked.
	files := f.files(t, c)
	if len(files) != 1 || files["site/index.html"].GetStatus() != "added" {
		t.Fatalf("files = %+v", files)
	}
	if p := f.patch(t, c, "site/index.html"); !strings.Contains(p.GetPatch(), "+<h1>hi</h1>") {
		t.Fatalf("patch = %q", p.GetPatch())
	}

	// The store is on the host beside the workspace, not inside it.
	if _, err := os.Stat(filepath.Join(h.DataDir, "bots", id, "history", "objects")); err != nil {
		t.Fatalf("history store on the host: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.DataDir, "bots", id, "workspace", ".git")); err == nil {
		t.Fatal("the history store leaked into the workspace")
	}

	// Undoing the run with the image's git: its file and the folder it made
	// are gone, and the restore is a change of its own.
	if res := f.restore(t, c, ""); res.GetRestored() != 1 || res.GetSkippedTotal() != 0 {
		t.Fatalf("restore in the box = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(h.DataDir, "bots", id, "workspace", "site")); err == nil {
		t.Fatal("the run's folder is still there after the restore")
	}

	if _, err := h.Client.ResetContainer(h.Ctx(), botReq(id)); err != nil {
		t.Fatalf("ResetContainer: %v", err)
	}
	h.StartSeededBot(id)
	after := f.list(t).GetChanges()
	if len(after) != 2 || !after[0].GetRestore() || after[1].GetId() != c.GetId() {
		t.Fatalf("history after a reset = %+v", after)
	}
}

func (f *changesFixture) restore(t *testing.T, c *v1.Change, path string) *v1.RestoreChangeResponse {
	t.Helper()
	res, err := f.h.Client.RestoreChange(f.h.Ctx(), connect.NewRequest(&v1.RestoreChangeRequest{BotId: f.botID, Base: c.GetBase(), Head: c.GetHead(), Path: path}))
	if err != nil {
		t.Fatalf("RestoreChange %q: %v", path, err)
	}
	return res.Msg
}

func (f *changesFixture) read(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.w.Workspace, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestChangesRestorePutsARunsWorkBack(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_changes_restore",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"printf 'one\\n2\\nthree\\n' > notes.md && echo made > out.txt && rm keep.txt"}`}}},
		dummy.Turn{Text: "edited"},
	)
	f := newChanges(t)
	f.write(t, "notes.md", "one\ntwo\nthree\n")
	f.write(t, "keep.txt", "keep me\n")
	f.list(t)
	chat := f.h.FirstChat(f.botID)
	runID, _ := f.h.Send(f.botID, chat, "Test_changes_restore")
	f.h.WaitRun(runID)
	run := f.waitChanges(t, 1).GetChanges()[0]
	if run.GetRestore() {
		t.Fatalf("a run's change reads as a restore: %+v", run)
	}

	// One file first: the rest of the run's work stays.
	if res := f.restore(t, run, "notes.md"); res.GetRestored() != 1 || res.GetSkippedTotal() != 0 {
		t.Fatalf("restore of one file = %+v", res)
	}
	if f.read(t, "notes.md") != "one\ntwo\nthree\n" || f.read(t, "out.txt") != "made\n" {
		t.Fatal("want notes.md back and out.txt left alone")
	}
	got := f.list(t)
	if len(got.GetChanges()) != 2 || got.GetPending() != nil {
		t.Fatalf("after a restore = %+v pending %+v", got.GetChanges(), got.GetPending())
	}
	back := got.GetChanges()[0]
	if !back.GetRestore() || back.GetFiles() != 1 || len(back.GetSources()) != 0 {
		t.Fatalf("the restore's change = %+v, want it marked as a restore by nobody's run", back)
	}

	// Then the whole change.
	if res := f.restore(t, run, ""); res.GetRestored() != 2 {
		t.Fatalf("restore of the change = %+v", res)
	}
	if f.read(t, "keep.txt") != "keep me\n" {
		t.Fatal("keep.txt is not back")
	}
	if _, err := os.Stat(filepath.Join(f.w.Workspace, "out.txt")); err == nil {
		t.Fatal("out.txt is still there")
	}

	// A restore is a change like any other: undoing it brings the file back.
	undo := f.list(t).GetChanges()[0]
	if !undo.GetRestore() {
		t.Fatalf("newest change = %+v", undo)
	}
	f.restore(t, undo, "")
	if f.read(t, "out.txt") != "made\n" {
		t.Fatal("undoing the restore did not bring out.txt back")
	}
}

func TestChangesRestoreMarksKnowledgeDirty(t *testing.T) {
	f := newChanges(t)
	f.write(t, "docs/a.md", "first\n")
	f.list(t)
	f.write(t, "docs/a.md", "second\n")
	pending := f.list(t).GetPending()
	if pending == nil {
		t.Fatal("no pending change")
	}
	folder := db.KnowledgeFolder{ID: "kf-restore", BotID: f.botID, Path: "docs"}
	if err := f.h.DB.Create(&folder).Error; err != nil {
		t.Fatal(err)
	}
	f.restore(t, pending, "docs/a.md")
	var after db.KnowledgeFolder
	f.h.DB.First(&after, "id = ?", folder.ID)
	if after.DirtyAt == nil {
		t.Fatal("a restore under a knowledge folder should mark it dirty")
	}
}

func TestChangesRestoreBelongsToTheOwnerAndNeedsTracking(t *testing.T) {
	f := newChanges(t)
	f.write(t, "a.txt", "a\n")
	f.list(t)
	f.write(t, "a.txt", "a2\n")
	mine := f.list(t).GetPending()
	if mine == nil {
		t.Fatal("no pending change")
	}
	req := &v1.RestoreChangeRequest{BotId: f.botID, Base: mine.GetBase(), Head: mine.GetHead()}
	stranger, _ := f.h.SignedInUser("eve@test.local")
	if _, err := stranger.RestoreChange(f.h.Ctx(), connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("stranger RestoreChange = %v", err)
	}
	// Ids the pane never listed are refused, and nothing is touched.
	gone := strings.Repeat("ab", 20)
	for _, pair := range [][2]string{{gone, gone}, {"HEAD", "--help"}, {"", ""}} {
		_, err := f.h.Client.RestoreChange(f.h.Ctx(), connect.NewRequest(&v1.RestoreChangeRequest{BotId: f.botID, Base: pair[0], Head: pair[1]}))
		if connect.CodeOf(err) != connect.CodeNotFound {
			t.Fatalf("RestoreChange %v = %v, want NotFound", pair, err)
		}
	}
	if f.read(t, "a.txt") != "a2\n" {
		t.Fatal("a refused restore changed the workspace")
	}

	dir := t.TempDir()
	off := newChanges(t, apptest.WithYAML(apptest.DefaultYAML(dir)+"changes:\n  enabled: false\n"))
	_, err := off.h.Client.RestoreChange(off.h.Ctx(), connect.NewRequest(&v1.RestoreChangeRequest{BotId: off.botID, Base: gone, Head: gone}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("RestoreChange with tracking off = %v", err)
	}
}
