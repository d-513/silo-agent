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

func TestReconnectWaitUnauth(t *testing.T) {
	if reconnectWait(nil) != 2*time.Second {
		t.Fatal("ok")
	}
	if reconnectWait(errors.New("x")) != 2*time.Second {
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
