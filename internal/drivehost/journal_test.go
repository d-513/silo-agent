package drivehost

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

// rcloneLines is what `rclone mount --log-level DEBUG --use-json-log` (v1.75.1,
// the image's) wrote while a shell did, on the mount:
//
//	echo new > new.txt; rm del.txt; rm nope.txt; mv mv.txt moved.txt
//	mkdir dir; rmdir dir; mv sub sub2; cat keep.txt; rm -r sub2
//	echo changed >> keep.txt
//
// with the reads and lookups in between cut down to one of each kind.
const rcloneLines = `{"time":"2026-10-10T14:27:02.69364606Z","level":"notice","msg":"Config file \"/root/.config/rclone/rclone.conf\" not found - using defaults","source":"config/config.go:374"}
{"time":"2026-10-10T14:27:02.694218173Z","level":"info","msg":"poll-interval is not supported by this remote","object":"Local file system at /src","objectType":"*local.Fs","source":"vfs/vfs.go:257"}
{"time":"2026-10-10T14:27:04.688505481Z","level":"info","msg":"vfs cache: queuing for upload in 1s","object":"new.txt","objectType":"string","source":"vfscache/item.go:825"}
{"time":"2026-10-10T14:27:24.733142489Z","level":"debug","msg":"Remove: name=\"del.txt\"","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:185"}
{"time":"2026-10-10T14:27:24.733178905Z","level":"debug","msg":"Remove: ","object":"del.txt","objectType":"string","source":"vfs/file.go:659"}
{"time":"2026-10-10T14:27:24.733339152Z","level":"debug","msg":">Remove: err=<nil>","object":"del.txt","objectType":"string","source":"vfs/file.go:697"}
{"time":"2026-10-10T14:27:24.733400442Z","level":"debug","msg":">Remove: err=<nil>","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:191"}
{"time":"2026-10-10T14:27:24.735000000Z","level":"debug","msg":"Remove: name=\"nope.txt\"","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:185"}
{"time":"2026-10-10T14:27:24.735100000Z","level":"debug","msg":">Remove: err=file does not exist","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:191"}
{"time":"2026-10-10T14:27:24.737848562Z","level":"debug","msg":"Rename: oldName=\"mv.txt\", newName=\"moved.txt\", newDir=/","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:207"}
{"time":"2026-10-10T14:27:24.738015058Z","level":"info","msg":"Moved (server-side) to: moved.txt","object":"mv.txt","objectType":"*local.Object","source":"operations/operations.go:493"}
{"time":"2026-10-10T14:27:24.738246887Z","level":"debug","msg":">Rename: err=<nil>","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:225"}
{"time":"2026-10-10T14:27:24.739624693Z","level":"debug","msg":"Mkdir: name=\"dir\"","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:169"}
{"time":"2026-10-10T14:27:24.739777523Z","level":"debug","msg":">Mkdir: node=dir/, err=<nil>","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:177"}
{"time":"2026-10-10T14:27:24.740817711Z","level":"debug","msg":"Remove: name=\"dir\"","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:185"}
{"time":"2026-10-10T14:27:24.740948875Z","level":"debug","msg":">Remove: err=<nil>","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:191"}
{"time":"2026-10-10T14:27:24.743035792Z","level":"debug","msg":"Rename: oldName=\"sub\", newName=\"sub2\", newDir=/","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:207"}
{"time":"2026-10-10T14:27:24.743257537Z","level":"debug","msg":">Rename: err=<nil>","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:225"}
{"time":"2026-10-10T14:27:24.744000000Z","level":"debug","msg":"Read: len=4096, offset=0","object":"keep.txt","objectType":"*mount.FileHandle","source":"mount/handle.go:28"}
{"time":"2026-10-10T14:27:24.746884298Z","level":"debug","msg":"Remove: name=\"in.txt\"","object":"sub2/","objectType":"*mount.Dir","source":"mount/dir.go:185"}
{"time":"2026-10-10T14:27:24.746905339Z","level":"debug","msg":"Remove: ","object":"sub2/in.txt","objectType":"string","source":"vfs/file.go:659"}
{"time":"2026-10-10T14:27:24.747012296Z","level":"debug","msg":">Remove: err=<nil>","object":"sub2/in.txt","objectType":"string","source":"vfs/file.go:697"}
{"time":"2026-10-10T14:27:24.747049962Z","level":"debug","msg":">Remove: err=<nil>","object":"sub2/","objectType":"*mount.Dir","source":"mount/dir.go:191"}
{"time":"2026-10-10T14:27:24.747183501Z","level":"debug","msg":"Remove: name=\"sub2\"","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:185"}
{"time":"2026-10-10T14:27:24.747255832Z","level":"debug","msg":">Remove: err=<nil>","object":"/","objectType":"*mount.Dir","source":"mount/dir.go:191"}
{"time":"2026-10-10T14:27:25.736478436Z","level":"debug","msg":"renamed to: new.txt","object":"new.txt.4cf792de.partial","objectType":"*local.Object","source":"operations/copy.go:372"}
{"time":"2026-10-10T14:27:25.736503269Z","level":"info","msg":"Copied (new)","size":4,"object":"new.txt","objectType":"*local.Object","source":"operations/copy.go:380"}
{"time":"2026-10-10T14:27:25.693999094Z","level":"info","msg":"vfs cache: upload succeeded try #1","object":"new.txt","objectType":"string","source":"writeback/writeback.go:382"}
{"time":"2026-10-10T14:27:25.706359305Z","level":"info","msg":"Copied (replaced existing)","size":12,"object":"keep.txt","objectType":"*local.Object","source":"operations/copy.go:380"}
{"time":"2026-10-10T14:27:26.000000000Z","level":"error","msg":"Failed to copy: 401 Unauthorized","object":"taxes.pdf","objectType":"*local.Object","source":"operations/copy.go:400"}
fusermount3: failed to access mountpoint /mnt/d: Permission denied
`

// feedLines runs rclone's stderr through the sink and the journal, as the
// supervisor does, and returns the changes and what reached the plain log.
func feedLines(t *testing.T, lines string, chunk int) ([]*v1.DriveChange, string) {
	t.Helper()
	var plain bytes.Buffer
	var got []*v1.DriveChange
	j := newJournal()
	sink := &logSink{out: &plain, onEntry: func(e LogEntry) {
		if c := j.feed(e); c != nil {
			got = append(got, c)
		}
	}}
	// The pipe hands over arbitrary slices, not lines.
	raw := []byte(lines)
	for len(raw) > 0 {
		n := min(chunk, len(raw))
		if _, err := sink.Write(raw[:n]); err != nil {
			t.Fatal(err)
		}
		raw = raw[n:]
	}
	return got, plain.String()
}

func TestJournalReadsRcloneLines(t *testing.T) {
	for _, chunk := range []int{1 << 20, 7} {
		got, plain := feedLines(t, rcloneLines, chunk)
		var have []string
		for _, c := range got {
			line := c.GetOp() + " " + c.GetPath()
			if c.GetOldPath() != "" {
				line += " <- " + c.GetOldPath()
			}
			if c.GetSize() != 0 {
				line += fmt.Sprintf(" (%d)", c.GetSize())
			}
			if c.GetAt() == 0 {
				t.Fatalf("%s has no time", line)
			}
			have = append(have, line)
		}
		want := []string{
			"deleted del.txt",
			// rm nope.txt failed: nothing changed.
			"renamed moved.txt <- mv.txt",
			"deleted dir",
			"renamed sub2 <- sub",
			"deleted sub2/in.txt",
			"deleted sub2",
			"added new.txt (4)",
			"modified keep.txt (12)",
		}
		if strings.Join(have, "\n") != strings.Join(want, "\n") {
			t.Fatalf("chunk %d: changes =\n%s\nwant\n%s", chunk, strings.Join(have, "\n"), strings.Join(want, "\n"))
		}
		// The time is the log line's, not now.
		if at := time.UnixMilli(got[0].GetAt()).UTC(); at.Year() != 2026 || at.Second() != 24 {
			t.Fatalf("time = %v", at)
		}

		// Only notices, errors and what is not a log line leave the sidecar,
		// in rclone's plain shape, so the error tail still reads.
		lines := strings.Split(strings.TrimSpace(plain), "\n")
		if len(lines) != 3 {
			t.Fatalf("chunk %d: plain log =\n%s", chunk, plain)
		}
		if !strings.HasSuffix(lines[0], "NOTICE : Config file \"/root/.config/rclone/rclone.conf\" not found - using defaults") {
			t.Fatalf("notice = %q", lines[0])
		}
		if lines[1] != "2026/10/10 14:27:26 ERROR : taxes.pdf: Failed to copy: 401 Unauthorized" {
			t.Fatalf("error = %q", lines[1])
		}
		if lines[2] != "fusermount3: failed to access mountpoint /mnt/d: Permission denied" {
			t.Fatalf("raw line = %q", lines[2])
		}
		if got := CleanError(plain); got != "taxes.pdf: Failed to copy: 401 Unauthorized" {
			t.Fatalf("CleanError = %q", got)
		}
		if strings.Contains(plain, "debug") || strings.Contains(plain, "Remove") || strings.Contains(plain, "vfs cache") {
			t.Fatalf("chatter leaked into the plain log:\n%s", plain)
		}
	}
}

func line(level, msg, object, objectType string) string {
	b, _ := json.Marshal(map[string]any{"time": "2026-10-10T10:00:00Z", "level": level, "msg": msg, "object": object, "objectType": objectType})
	return string(b) + "\n"
}

func TestJournalNamesWithQuotesSpacesAndFolders(t *testing.T) {
	lines := line("debug", `Remove: name="my \"final\" report (2).pdf"`, "Tax Returns/2025/", mountDir) +
		line("debug", `>Remove: err=<nil>`, "Tax Returns/2025/", mountDir) +
		line("debug", `Rename: oldName="a, newName=b.txt", newName="ż ó ł.txt", newDir=Archive/Old Stuff/`, "Inbox/", mountDir) +
		line("debug", `>Rename: err=<nil>`, "Inbox/", mountDir)
	got, _ := feedLines(t, lines, 1<<20)
	if len(got) != 2 {
		t.Fatalf("changes = %v", got)
	}
	if got[0].GetOp() != "deleted" || got[0].GetPath() != `Tax Returns/2025/my "final" report (2).pdf` {
		t.Fatalf("delete = %v", got[0])
	}
	if got[1].GetOp() != "renamed" || got[1].GetOldPath() != "Inbox/a, newName=b.txt" || got[1].GetPath() != "Archive/Old Stuff/ż ó ł.txt" {
		t.Fatalf("rename = %v", got[1])
	}
}

func TestJournalMatchesResultsInOrderPerFolder(t *testing.T) {
	// Two deletes in one folder and one in another, results interleaved.
	lines := line("debug", `Remove: name="a"`, "x/", mountDir) +
		line("debug", `Remove: name="c"`, "y/", mountDir) +
		line("debug", `Remove: name="b"`, "x/", mountDir) +
		line("debug", `>Remove: err=permission denied`, "x/", mountDir) +
		line("debug", `>Remove: err=<nil>`, "y/", mountDir) +
		line("debug", `>Remove: err=<nil>`, "x/", mountDir) +
		// A result with no call before it (the mount restarted) is nothing.
		line("debug", `>Remove: err=<nil>`, "x/", mountDir) +
		line("debug", `>Rename: err=<nil>`, "x/", mountDir)
	got, _ := feedLines(t, lines, 1<<20)
	var paths []string
	for _, c := range got {
		paths = append(paths, c.GetPath())
	}
	if strings.Join(paths, ",") != "y/c,x/b" {
		t.Fatalf("deleted = %v, want y/c then x/b (x/a failed)", paths)
	}
}

func TestSupervisorSendsTheJournalInBatches(t *testing.T) {
	s, f, r, _ := setup(t)
	s.Apply(&v1.DriveApply{Drives: []*v1.DriveSpec{spec("abc")}})
	eventually(t, "mounted", hasState(r, "abc", StateMounted))

	log := f.proc(0).log
	if log == nil {
		t.Fatal("the mount was started without a log reader")
	}
	now := time.Now()
	log(LogEntry{Time: now, Level: "info", Msg: "Copied (new)", Object: "report.pdf", Size: 2048})
	log(LogEntry{Time: now, Level: "debug", Msg: `Remove: name="old.txt"`, Object: "/", ObjectType: mountDir})
	log(LogEntry{Time: now, Level: "debug", Msg: `>Remove: err=<nil>`, Object: "/", ObjectType: mountDir})
	eventually(t, "the journal", func() bool { return len(r.changes("abc")) == 2 })
	got := r.changes("abc")
	if got[0].GetOp() != "added" || got[0].GetPath() != "report.pdf" || got[0].GetSize() != 2048 || got[1].GetOp() != "deleted" || got[1].GetPath() != "old.txt" {
		t.Fatalf("changes = %v", got)
	}

	// A burst goes out as a few frames, in order, not one per change.
	for i := range 450 {
		log(LogEntry{Time: now, Level: "info", Msg: "Copied (new)", Object: fmt.Sprintf("f%03d", i)})
	}
	eventually(t, "the burst", func() bool { return len(r.changes("abc")) == 452 })
	all := r.changes("abc")
	if all[2].GetPath() != "f000" || all[451].GetPath() != "f449" {
		t.Fatalf("burst out of order: %s … %s", all[2].GetPath(), all[451].GetPath())
	}
	r.mu.Lock()
	frames := 0
	for _, fr := range r.frames {
		if fr.GetChanges() != nil {
			frames++
		}
	}
	r.mu.Unlock()
	if frames > 12 {
		t.Fatalf("%d frames for 452 changes: not batched", frames)
	}

	// The mount logs for the journal, and a spec cannot turn that off.
	if args := f.starts[0]; !containsRun(args, []string{"--log-level", "DEBUG", "--use-json-log"}) {
		t.Fatalf("args = %v", args)
	}
	bad := spec("xyz")
	bad.Flags = []string{"--log-level=ERROR"}
	if err := Validate(bad); err == nil {
		t.Fatal("a spec may not set the log level")
	}
}
