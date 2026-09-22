package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/masker"
)

func TestResolveStripsWorkspacePrefix(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "workspace")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(ws, "twilio_doc.md")
	if err := os.WriteFile(want, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &worker{workspace: ws}
	for _, p := range []string{"twilio_doc.md", "/workspace/twilio_doc.md", "workspace/twilio_doc.md", want} {
		got, err := w.resolve(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if got != want {
			t.Fatalf("%s -> %s want %s", p, got, want)
		}
	}
}

func TestReconnectWaitUnauth(t *testing.T) {
	if reconnectWait(nil) != 200*time.Millisecond {
		t.Fatal("ok")
	}
	if reconnectWait(errors.New("x")) != 200*time.Millisecond {
		t.Fatal("other")
	}
	if reconnectWait(connect.NewError(connect.CodeUnauthenticated, nil)) != 15*time.Second {
		t.Fatal("unauth")
	}
}

func TestCancelJob(t *testing.T) {
	w := &worker{pending: map[string]*job{}, mask: masker.New()}
	ctx, cancel := context.WithCancel(context.Background())
	w.pending["a"] = &job{cancel: cancel}
	if !w.cancelJob("a") {
		t.Fatal("miss")
	}
	if ctx.Err() == nil {
		t.Fatal("not canceled")
	}
	if w.cancelJob("missing") {
		t.Fatal("ghost")
	}
}

func TestChildEnvRunID(t *testing.T) {
	t.Setenv("SILO_BOT_TOKEN", "secret-token-value")
	t.Setenv("SILO_CP_URL", "http://cp")
	w := &worker{mask: masker.New()}
	env := w.childEnv("run-1")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "SILO_BOT_TOKEN=") || strings.Contains(joined, "SILO_CP_URL=") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "SILO_RUN_ID=run-1") {
		t.Fatal(joined)
	}
	n := 0
	for _, e := range env {
		if strings.HasPrefix(e, "PYTHONPATH=") {
			n++
			if e != "PYTHONPATH=/opt/silo" {
				t.Fatal(e)
			}
		}
	}
	if n != 1 {
		t.Fatalf("PYTHONPATH count %d", n)
	}
}

func TestSendSerializes(t *testing.T) {
	w := &worker{pending: map[string]*job{}}
	started := make(chan struct{})
	done := make(chan struct{})
	w.sendMu.Lock()
	go func() {
		close(started)
		w.sendMu.Lock()
		w.sendMu.Unlock()
		close(done)
	}()
	<-started
	select {
	case <-done:
		t.Fatal("send was not serialized")
	default:
	}
	w.sendMu.Unlock()
	<-done
}

func TestPatchUnique(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("aa x aa"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &worker{workspace: dir}
	_, err := w.exec(context.Background(), &v1.Cmd{Body: &v1.Cmd_FilePatch{FilePatch: &v1.FilePatchCmd{
		Path: "f.txt", OldText: "aa", NewText: "bb",
	}}}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "2 times") {
		t.Fatalf("got %v", err)
	}
	out, err := w.exec(context.Background(), &v1.Cmd{Body: &v1.Cmd_FilePatch{FilePatch: &v1.FilePatchCmd{
		Path: "f.txt", OldText: "aa x aa", NewText: "ok",
	}}}, func(string) {})
	if err != nil || out != "ok" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestListDirAndBrowse(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := &worker{workspace: dir}
	raw, err := w.listDir("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"name":"hello.txt"`) || !strings.Contains(raw, `"name":"sub"`) {
		t.Fatal(raw)
	}
	if _, err := w.listDir("../etc"); err == nil {
		t.Fatal("escaped")
	}
	view, err := w.browseFile("hello.txt", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view, `"content":"hi"`) || !strings.Contains(view, `"data":`) {
		t.Fatal(view)
	}
}

func TestValidUTF8(t *testing.T) {
	if got := validUTF8("ok"); got != "ok" {
		t.Fatal(got)
	}
	got := validUTF8("a\xffb")
	if !utf8.ValidString(got) || !strings.Contains(got, "a") {
		t.Fatalf("%q", got)
	}
}

func TestBrowseFileLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat("a", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &worker{workspace: dir}
	raw, err := w.browseFile("big.txt", 10)
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Content) != 10 || !view.Truncated {
		t.Fatalf("content=%d truncated=%v", len(view.Content), view.Truncated)
	}
	// A limit above presentLimit falls back to the preview budget, not unlimited.
	if _, err := w.browseFile("big.txt", 1<<40); err != nil {
		t.Fatal(err)
	}
}

func TestFsMkdirPutRemove(t *testing.T) {
	dir := t.TempDir()
	w := &worker{workspace: dir}
	if _, err := w.mkdir("a/b"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.putFile("a/b/n.txt", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "a/b/n.txt"))
	if err != nil || string(raw) != "ok" {
		t.Fatalf("%q %v", raw, err)
	}
	if _, err := w.remove(""); err == nil {
		t.Fatal("removed root")
	}
	if _, err := w.remove("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := w.mkdir("../x"); err == nil {
		t.Fatal("escaped")
	}
}

func TestStartPTYInWorkspace(t *testing.T) {
	dir := t.TempDir()
	w := &worker{workspace: dir}
	ptmx, cmd, err := w.startPTY(24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()
	if _, err := ptmx.Write([]byte("pwd\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var got []byte
	tmp := make([]byte, 1024)
	for time.Now().Before(deadline) {
		_ = ptmx.SetReadDeadline(time.Now().Add(80 * time.Millisecond))
		n, _ := ptmx.Read(tmp)
		if n > 0 {
			got = append(got, tmp[:n]...)
			if strings.Contains(string(got), dir) {
				return
			}
		}
	}
	t.Fatalf("pwd not in output: %q", got)
}

func TestSyncTools(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SILO_TOOLS_DIR", dir)
	w := &worker{}
	out, err := w.syncTools([]*v1.ToolStub{{
		Connector: "Demo", Action: "Ping", Description: "ping",
		ArgsSchemaJson: `{"type":"object","properties":{"x":{"type":"string"}},"required":["x"]}`,
	}})
	if err != nil || out != "ok" {
		t.Fatalf("%q %v", out, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "demo", "ping.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `call("demo", "Ping"`) {
		t.Fatal(string(b))
	}
}

func TestSyncSkills(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SILO_SKILLS_DIR", dir)
	w := &worker{}
	out, err := w.syncSkills([]*v1.SkillFile{
		{Path: "one/SKILL.md", Data: []byte("hello")},
		{Path: "one/scripts/a.py", Data: []byte("print(1)")},
	})
	if err != nil || out != "ok" {
		t.Fatalf("%q %v", out, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "one", "SKILL.md"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("%s %v", b, err)
	}
	if _, err := w.syncSkills([]*v1.SkillFile{{Path: "../x", Data: []byte("no")}}); err == nil {
		t.Fatal("escape")
	}
}

func TestReadFileSlice(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\nc\nd\ne\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &worker{workspace: dir}
	raw, err := w.readFileSlice("f.txt", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	var v readView
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	if v.Content != "b\nc\n" || !v.Truncated || v.NextOffset != 4 {
		t.Fatalf("slice=%+v", v)
	}
	raw, err = w.readFileSlice("f.txt", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(raw), &v)
	if v.Content != "a\nb\nc\nd\ne\n" || v.TotalLines != 5 {
		t.Fatalf("full=%+v", v)
	}
	raw, err = w.readFileSlice("f.txt", 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(raw), &v)
	if v.Content != "" {
		t.Fatalf("past-eof=%+v", v)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin"), []byte("ok\x00bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.readFileSlice("bin", 1, 10); err == nil {
		t.Fatal("binary not rejected")
	}
	if err := os.WriteFile(filepath.Join(dir, "crlf.txt"), []byte("x\r\ny\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err = w.readFileSlice("crlf.txt", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(raw), &v)
	if v.Content != "x\r\ny\r\n" {
		t.Fatalf("crlf=%q", v.Content)
	}
}

func TestGrepArgs(t *testing.T) {
	args := strings.Join(grepArgs("foo|bar", "sub", "*.py", 80), " ")
	for _, want := range []string{"--line-number", "--sort path", "-e foo|bar", "-- sub", "--glob *.py", "!**/node_modules/**"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %q in %q", want, args)
		}
	}
	if strings.Contains(args, "!**/tmp/**") || strings.Contains(args, "!**/bot/**") {
		t.Fatalf("tmp/bot must stay searchable: %q", args)
	}
}

func TestGrepRG(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.md"), []byte("gamma here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &worker{workspace: dir}
	out, err := w.grep(context.Background(), &v1.GrepCmd{Pattern: "gamma"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "notes.txt:3:gamma") || !strings.Contains(out, "other.md:1:gamma") {
		t.Fatalf("grep=%q", out)
	}
	out, err = w.grep(context.Background(), &v1.GrepCmd{Pattern: "gamma", Include: "*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "notes.txt") || strings.Contains(out, "other.md") {
		t.Fatalf("include=%q", out)
	}
	out, err = w.grep(context.Background(), &v1.GrepCmd{Pattern: "nomatch_xyz"})
	if err != nil || out != "" {
		t.Fatalf("nomatch=%q %v", out, err)
	}
	out, err = w.grep(context.Background(), &v1.GrepCmd{Pattern: "gamma", MaxHits: 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("cap=%q", out)
	}
}
