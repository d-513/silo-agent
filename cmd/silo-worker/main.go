package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/masker"
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

	httpClient := &http.Client{
		Timeout: 0,
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		},
	}
	client := silov1connect.NewBotWorkerClient(httpClient, cp, connect.WithInterceptors(tokenI{tok}))
	w := &worker{
		workspace: ws,
		mask:      masker.New(),
		pending:   map[string]*job{},
		rpc:       client,
	}
	w.mask.Add(tok)
	go serveLocal(sock, w)
	go w.vncLoop(client)

	for {
		if err := w.commands(client); err != nil {
			log.Printf("commands: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
}

type tokenI struct{ tok string }

func (t tokenI) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+t.tok)
		return next(ctx, req)
	}
}

func (t tokenI) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Bearer "+t.tok)
		return conn
	}
}

func (t tokenI) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
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
	out, err := w.exec(ctx, cmd, func(chunk string) {
		send(&v1.CmdEvent{Body: &v1.CmdEvent_Chunk{Chunk: &v1.OutputChunk{Text: w.mask.Apply(chunk)}}})
	})
	if err != nil {
		send(&v1.CmdEvent{Body: &v1.CmdEvent_Error{Error: &v1.CmdError{Message: err.Error()}}})
		return
	}
	send(&v1.CmdEvent{Body: &v1.CmdEvent_Done{Done: &v1.CmdDone{Result: w.mask.Apply(out)}}})
}

func (w *worker) exec(ctx context.Context, cmd *v1.Cmd, chunk func(string)) (string, error) {
	switch b := cmd.GetBody().(type) {
	case *v1.Cmd_Terminal:
		return w.shell(ctx, cmd.GetRunId(), b.Terminal.GetCommand(), chunk)
	case *v1.Cmd_ExecPython:
		return w.python(ctx, cmd.GetRunId(), b.ExecPython.GetCode(), chunk)
	case *v1.Cmd_FileRead:
		p, err := w.resolve(b.FileRead.GetPath())
		if err != nil {
			return "", err
		}
		raw, err := os.ReadFile(p)
		return string(raw), err
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
		if !strings.Contains(s, b.FilePatch.GetOldText()) {
			return "", errors.New("old_text not found")
		}
		s = strings.Replace(s, b.FilePatch.GetOldText(), b.FilePatch.GetNewText(), 1)
		return "ok", os.WriteFile(p, []byte(s), 0o644)
	case *v1.Cmd_Grep:
		path := b.Grep.GetPath()
		if path == "" {
			path = "."
		}
		return w.shell(ctx, cmd.GetRunId(), "grep -R -n -- "+shellQuote(b.Grep.GetPattern())+" "+shellQuote(path), chunk)
	case *v1.Cmd_Cancel:
		w.cancelJob(b.Cancel.GetCmdId())
		return "ok", nil
	case *v1.Cmd_DirList:
		return w.listDir(b.DirList.GetPath())
	case *v1.Cmd_BrowseFile:
		return w.browseFile(b.BrowseFile.GetPath())
	default:
		return "", errors.New("unknown cmd")
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func (w *worker) resolve(p string) (string, error) {
	if p == "" {
		p = "."
	}
	full := filepath.Join(w.workspace, p)
	rel, err := filepath.Rel(w.workspace, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("path escapes workspace")
	}
	return full, nil
}

const browseLimit = 512 * 1024

type dirEnt struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

type fileView struct {
	Name      string `json:"name"`
	Content   string `json:"content"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
	Size      int64  `json:"size"`
}

func (w *worker) relPath(full string) string {
	rel, err := filepath.Rel(w.workspace, full)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

func (w *worker) listDir(p string) (string, error) {
	full, err := w.resolve(p)
	if err != nil {
		return "", err
	}
	ents, err := os.ReadDir(full)
	if err != nil {
		return "", err
	}
	out := make([]dirEnt, 0, len(ents))
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		child := filepath.Join(full, e.Name())
		out = append(out, dirEnt{
			Name:     e.Name(),
			Path:     w.relPath(child),
			Dir:      e.IsDir(),
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (w *worker) browseFile(p string) (string, error) {
	full, err := w.resolve(p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", errors.New("is a directory")
	}
	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, browseLimit+1)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}
	raw := buf[:n]
	trunc := st.Size() > int64(len(raw)) || n > browseLimit
	if n > browseLimit {
		raw = raw[:browseLimit]
	}
	view := fileView{Name: filepath.Base(full), Size: st.Size(), Truncated: trunc}
	if bytes.IndexByte(raw, 0) >= 0 {
		view.Binary = true
	} else {
		view.Content = string(raw)
	}
	b, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (w *worker) childEnv(runID string) []string {
	var out []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "SILO_BOT_TOKEN=") || strings.HasPrefix(e, "SILO_CP_URL=") || strings.HasPrefix(e, "SILO_RUN_ID=") {
			continue
		}
		out = append(out, e)
	}
	sock := os.Getenv("SILO_WORKER_SOCK")
	if sock == "" {
		sock = "/var/run/silo/worker.sock"
	}
	out = append(out, "SILO_WORKER_SOCK="+sock, "PYTHONPATH=/opt/silo:"+os.Getenv("PYTHONPATH"))
	if runID != "" {
		out = append(out, "SILO_RUN_ID="+runID)
	}
	return out
}

func (w *worker) shell(ctx context.Context, runID, command string, chunk func(string)) (string, error) {
	c := exec.CommandContext(ctx, "/bin/sh", "-lc", command)
	c.Dir = w.workspace
	c.Env = w.childEnv(runID)
	var buf bytes.Buffer
	c.Stdout = &tee{w: &buf, f: chunk}
	c.Stderr = c.Stdout
	err := c.Run()
	return buf.String(), err
}

func (w *worker) python(ctx context.Context, runID, code string, chunk func(string)) (string, error) {
	c := exec.CommandContext(ctx, "python3", "-c", code)
	c.Dir = w.workspace
	c.Env = w.childEnv(runID)
	var buf bytes.Buffer
	c.Stdout = &tee{w: &buf, f: chunk}
	c.Stderr = c.Stdout
	err := c.Run()
	return buf.String(), err
}

type tee struct {
	w io.Writer
	f func(string)
}

func (t *tee) Write(p []byte) (int, error) {
	if t.f != nil {
		t.f(string(p))
	}
	return t.w.Write(p)
}

func serveLocal(sock string, w *worker) {
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		log.Printf("unix: %v", err)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/secrets/get", func(rw http.ResponseWriter, r *http.Request) {
		var body struct {
			Name  string `json:"name"`
			RunID string `json:"run_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		val, err := w.getSecret(r.Context(), body.Name, body.RunID)
		rw.Header().Set("Content-Type", "application/json")
		if err != nil {
			_ = json.NewEncoder(rw).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.mask.Add(val)
		_ = json.NewEncoder(rw).Encode(map[string]string{"value": val})
	})
	log.Printf("local tools on %s", sock)
	_ = http.Serve(ln, mux)
}

func (w *worker) getSecret(ctx context.Context, name, runID string) (string, error) {
	res, err := w.rpc.GetSecret(ctx, connect.NewRequest(&v1.SecretReq{Name: name, RunId: runID}))
	if err != nil {
		return "", err
	}
	if res.Msg.GetError() != "" {
		return "", errors.New(res.Msg.GetError())
	}
	return res.Msg.GetValue(), nil
}

func (w *worker) vncLoop(client silov1connect.BotWorkerClient) {
	for {
		if err := w.vncOnce(client); err != nil {
			log.Printf("vnc: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func (w *worker) vncOnce(client silov1connect.BotWorkerClient) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := client.VNC(ctx)
	if err := st.Send(&v1.Frame{}); err != nil {
		return err
	}
	if _, err := st.Receive(); err != nil {
		return err
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.Dial("tcp", "127.0.0.1:5900")
	if err != nil {
		return err
	}
	defer conn.Close()
	go func() {
		defer cancel()
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			data := make([]byte, n)
			copy(data, buf[:n])
			if err := st.Send(&v1.Frame{Data: data}); err != nil {
				return
			}
		}
	}()
	for {
		fr, err := st.Receive()
		if err != nil {
			return err
		}
		if len(fr.GetData()) == 0 {
			continue
		}
		if _, err := conn.Write(fr.GetData()); err != nil {
			return err
		}
	}
}
