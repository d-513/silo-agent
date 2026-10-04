package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/masker"
	"silo.agent/internal/rpcx"
)

func main() {
	cp := os.Getenv("SILO_CP_URL")
	tok := os.Getenv("SILO_BOT_TOKEN")
	if cp == "" || tok == "" {
		log.Fatal("SILO_CP_URL and SILO_BOT_TOKEN required")
	}
	ws := os.Getenv("SILO_WORKSPACE")
	if ws == "" {
		ws = "/workspace"
	}
	sock := os.Getenv("SILO_WORKER_SOCK")
	if sock == "" {
		sock = "/var/run/silo/worker.sock"
	}
	_ = os.MkdirAll(filepath.Dir(sock), 0o755)
	_ = os.MkdirAll(ws, 0o755)

	client := silov1connect.NewBotWorkerClient(rpcx.H2CClient(2*time.Second), cp, connect.WithInterceptors(rpcx.Bearer(tok)))
	w := &worker{
		workspace: ws,
		mask:      masker.New(),
		pending:   map[string]*job{},
		rpc:       client,
	}
	w.mask.Add(tok)
	ensureToolsDir()
	go serveLocal(sock, w)
	go retryLoop("vnc", func() error { return w.vncOnce(client) })
	go retryLoop("console", func() error { return w.consoleOnce(client) })
	retryLoop("commands", func() error { return w.commands(client) })
}

type job struct {
	cancel context.CancelFunc
}

type worker struct {
	workspace string
	mask      *masker.Masker
	mu        sync.Mutex
	sendMu    sync.Mutex
	pending   map[string]*job
	rpc       silov1connect.BotWorkerClient
	// shotSeq counts desktop captures. A command that bumps it (a Python or
	// terminal `look`) reports the fresh screen back on CmdDone.image so the
	// CP can attach pixels without a second round trip.
	shotSeq atomic.Int64
}

// screenDataURL reads the latest model-facing screenshot as a data: URL.
func (w *worker) screenDataURL() string {
	full, err := w.resolve(screenShot)
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(full)
	if err != nil || len(raw) == 0 {
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(raw)
}

func (w *worker) send(st *connect.BidiStreamForClient[v1.CmdEvent, v1.Cmd], ev *v1.CmdEvent) error {
	w.sendMu.Lock()
	defer w.sendMu.Unlock()
	return st.Send(ev)
}

func (w *worker) cancelJob(id string) bool {
	w.mu.Lock()
	j := w.pending[id]
	w.mu.Unlock()
	if j == nil {
		return false
	}
	j.cancel()
	return true
}

func (w *worker) commands(client silov1connect.BotWorkerClient) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := client.Commands(ctx)
	// ConnectRPC does not send bidi headers until the first Send. Without this
	// the CP never sees the worker until the 10s heartbeat tick.
	if err := w.send(st, &v1.CmdEvent{Body: &v1.CmdEvent_Heartbeat{Heartbeat: &v1.Heartbeat{}}}); err != nil {
		return err
	}
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = w.send(st, &v1.CmdEvent{Body: &v1.CmdEvent_Heartbeat{Heartbeat: &v1.Heartbeat{}}})
			}
		}
	}()
	for {
		cmd, err := st.Receive()
		if err != nil {
			return err
		}
		go w.run(st, cmd)
	}
}

func (w *worker) run(st *connect.BidiStreamForClient[v1.CmdEvent, v1.Cmd], cmd *v1.Cmd) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	w.mu.Lock()
	w.pending[cmd.GetId()] = &job{cancel: cancel}
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		delete(w.pending, cmd.GetId())
		w.mu.Unlock()
	}()

	send := func(ev *v1.CmdEvent) {
		ev.Id = cmd.GetId()
		_ = w.send(st, ev)
	}
	shots := w.shotSeq.Load()
	out, err := w.exec(ctx, cmd, func(chunk string) {
		send(&v1.CmdEvent{Body: &v1.CmdEvent_Chunk{Chunk: &v1.OutputChunk{Text: validUTF8(w.mask.Apply(chunk))}}})
	})
	if err != nil {
		send(&v1.CmdEvent{Body: &v1.CmdEvent_Error{Error: &v1.CmdError{Message: validUTF8(err.Error())}}})
		return
	}
	done := &v1.CmdDone{Result: validUTF8(w.mask.Apply(out))}
	if w.shotSeq.Load() != shots && cmdTakesShot(cmd) {
		done.Image = w.screenDataURL()
	}
	send(&v1.CmdEvent{Body: &v1.CmdEvent_Done{Done: done}})
}

// cmdTakesShot reports whether a command can take a desktop look as a side
// effect. Cmd_Look already returns the image in its result JSON.
func cmdTakesShot(cmd *v1.Cmd) bool {
	switch cmd.GetBody().(type) {
	case *v1.Cmd_ExecPython, *v1.Cmd_Terminal:
		return true
	default:
		return false
	}
}

// validUTF8 replaces invalid UTF-8 so protobuf string fields stay marshalable.
// Command output (a docx dump, terminal bytes) can carry arbitrary bytes.
func validUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "\uFFFD")
}

func (w *worker) exec(ctx context.Context, cmd *v1.Cmd, chunk func(string)) (string, error) {
	switch b := cmd.GetBody().(type) {
	case *v1.Cmd_Terminal:
		return w.shell(ctx, cmd.GetRunId(), b.Terminal.GetCommand(), chunk)
	case *v1.Cmd_ExecPython:
		return w.python(ctx, cmd.GetRunId(), b.ExecPython.GetCode(), chunk)
	case *v1.Cmd_FileRead:
		return w.readFileSlice(b.FileRead.GetPath(), int(b.FileRead.GetOffset()), int(b.FileRead.GetLimit()))
	case *v1.Cmd_FileWrite:
		p, err := w.resolve(b.FileWrite.GetPath())
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		return "ok", os.WriteFile(p, []byte(b.FileWrite.GetContent()), 0o644)
	case *v1.Cmd_FilePatch:
		p, err := w.resolve(b.FilePatch.GetPath())
		if err != nil {
			return "", err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		s := string(raw)
		old := b.FilePatch.GetOldText()
		n := strings.Count(s, old)
		if n == 0 {
			return "", errors.New("old_text not found")
		}
		if n > 1 {
			return "", fmt.Errorf("old_text matches %d times; make it unique", n)
		}
		s = strings.Replace(s, old, b.FilePatch.GetNewText(), 1)
		return "ok", os.WriteFile(p, []byte(s), 0o644)
	case *v1.Cmd_Grep:
		return w.grep(ctx, b.Grep)
	case *v1.Cmd_Cancel:
		w.cancelJob(b.Cancel.GetCmdId())
		return "ok", nil
	case *v1.Cmd_DirList:
		return w.listDir(b.DirList.GetPath())
	case *v1.Cmd_BrowseFile:
		return w.browseFile(b.BrowseFile.GetPath(), b.BrowseFile.GetLimit())
	case *v1.Cmd_Walk:
		return w.walk(b.Walk.GetPath(), int(b.Walk.GetMaxFiles()))
	case *v1.Cmd_Extract:
		return w.extract(ctx, b.Extract.GetPath(), b.Extract.GetMaxBytes(), b.Extract.GetOcr())
	case *v1.Cmd_Mkdir:
		return w.mkdir(b.Mkdir.GetPath())
	case *v1.Cmd_Remove:
		return w.remove(b.Remove.GetPath())
	case *v1.Cmd_PutFile:
		return w.putFile(b.PutFile.GetPath(), b.PutFile.GetData())
	case *v1.Cmd_SyncTools:
		return w.syncTools(b.SyncTools.GetStubs())
	case *v1.Cmd_SyncSkills:
		return w.syncSkills(b.SyncSkills.GetFiles())
	case *v1.Cmd_EnsureChrome:
		return w.ensureChrome(ctx)
	case *v1.Cmd_Look:
		return w.look(ctx)
	case *v1.Cmd_Click:
		return w.click(ctx, b.Click)
	case *v1.Cmd_Type:
		return w.typeText(ctx, b.Type.GetText())
	case *v1.Cmd_Key:
		return w.key(ctx, b.Key.GetName())
	case *v1.Cmd_Scroll:
		return w.scroll(ctx, b.Scroll)
	default:
		return "", errors.New("unknown cmd")
	}
}
