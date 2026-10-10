package drivehost

import (
	"bytes"
	"encoding/json"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

// The drive journal is read off rclone's own log. Everything that touches a
// drive goes through its FUSE mount, so the mount's log names every write,
// delete and rename whoever made it: a tool, a script, the owner in the
// Console. Nothing is listed or downloaded to learn it, which is why drives
// get a journal where the workspace gets snapshots.
//
// rclone logs an upload reaching the remote at INFO ("Copied (new)"), but a
// delete or a rename of a folder only at DEBUG, as the mount layer's call and
// its result. So mounts run at DEBUG with JSON lines; the lines are parsed
// here and go no further (logSink): the container's own log stays as quiet as
// it was at NOTICE.
//
// The line shapes are rclone's, pinned by the image's rclone version and by
// TestJournalReadsRcloneLines, whose fixture is a capture of the real thing.

// LogEntry is one JSON log line of rclone.
type LogEntry struct {
	Time       time.Time `json:"time"`
	Level      string    `json:"level"`
	Msg        string    `json:"msg"`
	Object     string    `json:"object"`
	ObjectType string    `json:"objectType"`
	Size       int64     `json:"size"`
}

// mountDir is the objectType of the mount layer's directory calls: the one
// place that sees a delete or a rename of a file and of a folder alike.
const mountDir = "*mount.Dir"

var renameCall = regexp.MustCompile(`^Rename: oldName=("(?:[^"\\]|\\.)*"), newName=("(?:[^"\\]|\\.)*"), newDir=(.*)$`)

type pendingCall struct {
	op            string
	path, oldPath string
}

// journal turns log entries into changes. A delete or a rename is two lines, a
// call and its result on the same directory; calls on one directory finish in
// the order they were made, so the results are matched first in, first out.
type journal struct {
	mu      sync.Mutex
	pending map[string][]pendingCall
}

func newJournal() *journal { return &journal{pending: map[string][]pendingCall{}} }

func joinDrive(dir, name string) string {
	return strings.TrimPrefix(path.Join("/", dir, name), "/")
}

// feed takes one entry and returns the change it completes, if any.
func (j *journal) feed(e LogEntry) *v1.DriveChange {
	at := e.Time.UnixMilli()
	if e.Time.IsZero() {
		at = time.Now().UnixMilli()
	}
	if e.Level == "info" {
		// An upload that reached the remote. The object is the final name (the
		// .partial it was written through is renamed first).
		if !strings.HasPrefix(e.Msg, "Copied (") || e.Object == "" {
			return nil
		}
		op := "modified"
		if strings.HasPrefix(e.Msg, "Copied (new") {
			op = "added"
		}
		return &v1.DriveChange{Op: op, Path: joinDrive("", e.Object), Size: e.Size, At: at}
	}
	if e.Level != "debug" || e.ObjectType != mountDir {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	switch {
	case strings.HasPrefix(e.Msg, "Remove: name="):
		name, err := strconv.Unquote(strings.TrimPrefix(e.Msg, "Remove: name="))
		if err != nil {
			return nil
		}
		j.push(e.Object, "Remove", pendingCall{op: "deleted", path: joinDrive(e.Object, name)})
	case strings.HasPrefix(e.Msg, "Rename: "):
		m := renameCall.FindStringSubmatch(e.Msg)
		if m == nil {
			return nil
		}
		oldName, err1 := strconv.Unquote(m[1])
		newName, err2 := strconv.Unquote(m[2])
		if err1 != nil || err2 != nil {
			return nil
		}
		j.push(e.Object, "Rename", pendingCall{op: "renamed", oldPath: joinDrive(e.Object, oldName), path: joinDrive(m[3], newName)})
	case strings.HasPrefix(e.Msg, ">Remove: err="):
		return j.pop(e.Object, "Remove", strings.TrimPrefix(e.Msg, ">Remove: err="), at)
	case strings.HasPrefix(e.Msg, ">Rename: err="):
		return j.pop(e.Object, "Rename", strings.TrimPrefix(e.Msg, ">Rename: err="), at)
	}
	return nil
}

func (j *journal) push(dir, call string, c pendingCall) {
	key := call + "\x00" + dir
	// A call whose result never came (a crashed mount) must not grow this.
	if len(j.pending[key]) < 1024 {
		j.pending[key] = append(j.pending[key], c)
	}
}

func (j *journal) pop(dir, call, result string, at int64) *v1.DriveChange {
	key := call + "\x00" + dir
	q := j.pending[key]
	if len(q) == 0 {
		return nil
	}
	c := q[0]
	if len(q) == 1 {
		delete(j.pending, key)
	} else {
		j.pending[key] = q[1:]
	}
	if result != "<nil>" {
		return nil // it failed: nothing changed
	}
	return &v1.DriveChange{Op: c.op, Path: c.path, OldPath: c.oldPath, At: at}
}

// logSink is rclone's stderr. Lines the journal can use are parsed and handed
// to onEntry; the rest of DEBUG and INFO is dropped; what is left (notices,
// errors, anything that is not JSON) is written to out as rclone's plain log
// line, so the error tail and the container log read as before.
type logSink struct {
	out     interface{ Write([]byte) (int, error) }
	onEntry func(LogEntry)
	mu      sync.Mutex
	buf     []byte
}

var (
	levelDebug = []byte(`"level":"debug"`)
	levelInfo  = []byte(`"level":"info"`)
	wantDebug  = [][]byte{[]byte(`Remove: `), []byte(`Rename: `)}
	wantInfo   = []byte(`"msg":"Copied (`)
)

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	for {
		nl := bytes.IndexByte(s.buf, '\n')
		if nl < 0 {
			break
		}
		s.line(s.buf[:nl])
		s.buf = s.buf[nl+1:]
	}
	// A line with no end in sight is not a log line: pass it on.
	if len(s.buf) > 64<<10 {
		_, _ = s.out.Write(s.buf)
		s.buf = nil
	}
	return len(p), nil
}

func (s *logSink) line(l []byte) {
	if len(l) == 0 {
		return
	}
	if l[0] != '{' {
		_, _ = s.out.Write(append(append([]byte{}, l...), '\n'))
		return
	}
	// Most lines are DEBUG chatter about reads: decide on the bytes, and parse
	// only the few that matter.
	switch {
	case bytes.Contains(l, levelDebug):
		if !bytes.Contains(l, wantDebug[0]) && !bytes.Contains(l, wantDebug[1]) {
			return
		}
	case bytes.Contains(l, levelInfo):
		if !bytes.Contains(l, wantInfo) {
			return
		}
	}
	var e LogEntry
	if err := json.Unmarshal(l, &e); err != nil {
		_, _ = s.out.Write(append(append([]byte{}, l...), '\n'))
		return
	}
	if e.Level == "debug" || e.Level == "info" {
		if s.onEntry != nil {
			s.onEntry(e)
		}
		return
	}
	_, _ = s.out.Write([]byte(plainLine(e)))
}

// plainLine writes an entry the way rclone's text log does:
// "2026/10/10 14:27:02 ERROR : object: message".
func plainLine(e LogEntry) string {
	msg := e.Msg
	if e.Object != "" {
		msg = e.Object + ": " + msg
	}
	level := strings.ToUpper(e.Level)
	if level == "WARNING" {
		level = "WARN"
	}
	t := e.Time
	if t.IsZero() {
		t = time.Now()
	}
	return t.Format("2006/01/02 15:04:05") + " " + level + " : " + msg + "\n"
}
