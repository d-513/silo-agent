package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	view, err := w.browseFile("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view, `"content":"hi"`) || !strings.Contains(view, `"data":`) {
		t.Fatal(view)
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
