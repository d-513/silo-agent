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

	if _, err := h.Client.ResetContainer(h.Ctx(), botReq(id)); err != nil {
		t.Fatalf("ResetContainer: %v", err)
	}
	h.StartSeededBot(id)
	if after := f.list(t); len(after.GetChanges()) != 1 || after.GetChanges()[0].GetId() != c.GetId() {
		t.Fatalf("history after a reset = %+v", after.GetChanges())
	}
}
