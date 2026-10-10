package app_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/changes"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/drivehost"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// journalFixture is a Bot with one mounted drive ("files") on the fake rclone.
type journalFixture struct {
	h     *apptest.H
	botID string
	drive *v1.Drive
}

func newJournal(t *testing.T) *journalFixture {
	t.Helper()
	h := driveHarness(t)
	bot := h.CreateBot("Keeper")
	draft, err := saveDrive(h, &v1.SaveDriveRequest{BotId: bot.GetId(), Template: "s3", Draft: true, Options: map[string]string{
		"access_key_id": "AKIA", "secret_access_key": "s3cret", "region": "eu-west-1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.GetId(), Name: "files", Options: map[string]string{"access_key_id": "AKIA", "region": "eu-west-1"}})
	if err != nil {
		t.Fatalf("save drive: %v", err)
	}
	waitDrive(t, h, bot.GetId(), d.GetId(), "mounted")
	return &journalFixture{h: h, botID: bot.GetId(), drive: d}
}

func (f *journalFixture) list(t *testing.T) *v1.ListDriveChangesResponse {
	t.Helper()
	res, err := f.h.Client.ListDriveChanges(f.h.Ctx(), connect.NewRequest(&v1.ListDriveChangesRequest{BotId: f.botID}))
	if err != nil {
		t.Fatalf("ListDriveChanges: %v", err)
	}
	return res.Msg
}

func (f *journalFixture) wait(t *testing.T, n int) []*v1.DriveChangeEntry {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := f.list(t).GetChanges()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %d journal lines, have %+v", n, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// log makes the drive's (fake) rclone log what the real one logs for a change.
func (f *journalFixture) log(t *testing.T, entries ...drivehost.LogEntry) {
	t.Helper()
	if !f.h.Drives.Runner.Log("files", entries...) {
		t.Fatal("the drive is not mounted")
	}
}

func uploaded(path string, size int64) drivehost.LogEntry {
	return drivehost.LogEntry{Time: time.Now(), Level: "info", Msg: "Copied (new)", Object: path, Size: size}
}

func removed(dir, name string) []drivehost.LogEntry {
	return []drivehost.LogEntry{
		{Time: time.Now(), Level: "debug", Msg: fmt.Sprintf("Remove: name=%q", name), Object: dir, ObjectType: "*mount.Dir"},
		{Time: time.Now(), Level: "debug", Msg: ">Remove: err=<nil>", Object: dir, ObjectType: "*mount.Dir"},
	}
}

func TestDriveJournalRecordsWhatTheMountSaw(t *testing.T) {
	f := newJournal(t)
	if got := f.list(t); got.GetState() != "ok" || !got.GetHasDrives() || len(got.GetChanges()) != 0 || got.GetKeep() != changes.DriveKeep {
		t.Fatalf("empty journal = %+v", got)
	}

	f.log(t, uploaded("reports/q3.pdf", 2048))
	f.log(t, removed("reports/", "old.pdf")...)
	f.log(t,
		drivehost.LogEntry{Time: time.Now(), Level: "debug", Msg: `Rename: oldName="a.txt", newName="b.txt", newDir=archive/`, Object: "/", ObjectType: "*mount.Dir"},
		drivehost.LogEntry{Time: time.Now(), Level: "debug", Msg: ">Rename: err=<nil>", Object: "/", ObjectType: "*mount.Dir"},
		// Reads are not changes.
		drivehost.LogEntry{Time: time.Now(), Level: "debug", Msg: "Read: len=4096", Object: "reports/q3.pdf", ObjectType: "*mount.FileHandle"},
	)

	// Newest first.
	got := f.wait(t, 3)
	if len(got) != 3 {
		t.Fatalf("journal = %+v", got)
	}
	ren, del, add := got[0], got[1], got[2]
	if add.GetOp() != "added" || add.GetPath() != "reports/q3.pdf" || add.GetSize() != 2048 || add.GetDrive() != "files" || add.GetAt() == "" || add.GetId() == "" {
		t.Fatalf("upload = %+v", add)
	}
	if del.GetOp() != "deleted" || del.GetPath() != "reports/old.pdf" {
		t.Fatalf("delete = %+v", del)
	}
	if ren.GetOp() != "renamed" || ren.GetOldPath() != "a.txt" || ren.GetPath() != "archive/b.txt" {
		t.Fatalf("rename = %+v", ren)
	}
	// Nobody's run was at work.
	if len(add.GetSources()) != 0 {
		t.Fatalf("sources = %+v", add.GetSources())
	}

	// A stranger sees none of it.
	stranger, _ := f.h.SignedInUser("eve@test.local")
	if _, err := stranger.ListDriveChanges(f.h.Ctx(), connect.NewRequest(&v1.ListDriveChangesRequest{BotId: f.botID})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("stranger = %v", err)
	}

	// The journal outlives its drive (the line keeps the drive's name) and
	// goes with the Bot.
	if _, err := f.h.Client.DeleteDrive(f.h.Ctx(), connect.NewRequest(&v1.DeleteDriveRequest{Id: f.drive.GetId()})); err != nil {
		t.Fatal(err)
	}
	if after := f.list(t); len(after.GetChanges()) != 3 || after.GetChanges()[0].GetDrive() != "files" || after.GetHasDrives() {
		t.Fatalf("after the drive is removed = %+v", after)
	}
	if _, err := f.h.Client.DeleteBot(f.h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: f.botID})); err != nil {
		t.Fatal(err)
	}
	var n int64
	f.h.DB.Model(&db.DriveChange{}).Where("bot_id = ?", f.botID).Count(&n)
	if n != 0 {
		t.Fatalf("%d journal lines left after the Bot was deleted", n)
	}
}

func TestDriveJournalNamesTheRunAtWork(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dummy.Reset()
	dummy.Script("Test_journal_run",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"touch started && sleep 1"}`}}},
		dummy.Turn{Text: "copied"},
	)
	f := newJournal(t)
	w := f.h.StartWorker(f.botID)
	chat := f.h.FirstChat(f.botID)
	runID, _ := f.h.Send(f.botID, chat, "Test_journal_run")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(w.Workspace, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the run never reached its script")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The script is running: what lands on the drive now is the run's.
	f.log(t, removed("/", "during.txt")...)
	during := f.wait(t, 1)[0]
	if len(during.GetSources()) != 1 || during.GetSources()[0].GetChatId() != chat || during.GetSources()[0].GetKind() != "chat" {
		t.Fatalf("during the run: sources = %+v", during.GetSources())
	}

	// rclone uploads a file a few seconds after it is closed, so an upload
	// that lands just after the run is still the run's.
	f.h.WaitRun(runID)
	f.log(t, uploaded("after.txt", 10))
	after := f.wait(t, 2)[0]
	if after.GetPath() != "after.txt" || len(after.GetSources()) != 1 || after.GetSources()[0].GetChatId() != chat {
		t.Fatalf("just after the run: %+v", after)
	}
}

func TestDriveJournalIsBoundedAndDistrustsTheSidecar(t *testing.T) {
	f := newJournal(t)
	var d db.Drive
	if err := f.h.DB.First(&d, "id = ?", f.drive.GetId()).Error; err != nil {
		t.Fatal(err)
	}
	rec := f.h.App.Changes.RecordDrive

	// What a sidecar sends is cleaned, not believed: an unknown op and an
	// empty path are dropped, a wild clock is set to now.
	rec(&d, []*v1.DriveChange{
		{Op: "formatted", Path: "everything"},
		{Op: "deleted", Path: ""},
		{Op: "deleted", Path: "/x/y/", At: time.Now().Add(240 * time.Hour).UnixMilli(), Size: -5},
	})
	got := f.list(t).GetChanges()
	if len(got) != 1 || got[0].GetPath() != "x/y" || got[0].GetSize() != 0 {
		t.Fatalf("after bad input = %+v", got)
	}
	at, err := time.Parse(time.RFC3339, got[0].GetAt())
	if err != nil || time.Since(at) > time.Minute || time.Until(at) > time.Minute {
		t.Fatalf("time = %q", got[0].GetAt())
	}

	// A burst far past the cap leaves the newest DriveKeep lines.
	total := changes.DriveKeep + 700
	for start := 0; start < total; start += 200 {
		batch := make([]*v1.DriveChange, 0, 200)
		for i := start; i < min(start+200, total); i++ {
			batch = append(batch, &v1.DriveChange{Op: "added", Path: fmt.Sprintf("f%05d", i), At: time.Now().UnixMilli()})
		}
		rec(&d, batch)
	}
	var n int64
	f.h.DB.Model(&db.DriveChange{}).Where("bot_id = ?", f.botID).Count(&n)
	// Trimming runs every couple of hundred lines, so the table sits between
	// the cap and a little over it.
	if n < int64(changes.DriveKeep) || n > int64(changes.DriveKeep+200) {
		t.Fatalf("%d lines kept, want about %d", n, changes.DriveKeep)
	}
	if newest := f.list(t).GetChanges()[0]; newest.GetPath() != fmt.Sprintf("f%05d", total-1) {
		t.Fatalf("newest = %+v", newest)
	}
}

func TestDriveJournalOff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "silo.yaml")
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)+"changes:\n  enabled: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := apptest.New(t, apptest.WithConfigPath(path), apptest.WithDriveGuest())
	bot := h.CreateBot("Quiet")
	h.App.Changes.RecordDrive(&db.Drive{ID: "d1", BotID: bot.GetId(), Name: "files"}, []*v1.DriveChange{{Op: "added", Path: "x"}})
	res, err := h.Client.ListDriveChanges(h.Ctx(), connect.NewRequest(&v1.ListDriveChangesRequest{BotId: bot.GetId()}))
	if err != nil || res.Msg.GetState() != "off" || len(res.Msg.GetChanges()) != 0 {
		t.Fatalf("off = %+v (%v)", res.Msg, err)
	}
	var n int64
	h.DB.Model(&db.DriveChange{}).Count(&n)
	if n != 0 {
		t.Fatalf("%d lines recorded with change tracking off", n)
	}
}
