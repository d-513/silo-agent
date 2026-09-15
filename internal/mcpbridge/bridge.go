// Package mcpbridge runs one STDIO MCP process and exposes it over streamable HTTP.
package mcpbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	SockFile        = "mcp.sock"
	MaxResultBytes  = 8 << 20
	childPoll       = 100 * time.Millisecond
	emptyObjectJSON = `{"type":"object"}`
)

type Config struct {
	Command string
	Args    []string
	Sock    string
	Env     []string
}

func Run(ctx context.Context, cfg Config) error {
	cmd, err := child(cfg)
	if err != nil {
		return err
	}
	return Serve(ctx, cfg.Sock, cmd)
}

func child(cfg Config) (*exec.Cmd, error) {
	if err := ValidCommand(cfg.Command); err != nil {
		return nil, err
	}
	for _, a := range cfg.Args {
		if err := ValidArg(a); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(cfg.Sock) == "" {
		return nil, errors.New("socket path required")
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	if cfg.Env != nil {
		cmd.Env = cfg.Env
	} else {
		cmd.Env = stripBridgeEnv(os.Environ())
	}
	cmd.Stderr = os.Stderr
	return cmd, nil
}

func Serve(ctx context.Context, sock string, cmd *exec.Cmd) error {
	return serve(ctx, sock, "", cmd)
}

// ServeTCP runs one STDIO MCP process and exposes it over TCP. addr is
// host:port to bind; token, when non-empty, is required as "Bearer <token>"
// on every request (a TCP listener is reachable from other containers on the
// shared bridge network, unlike a 0600 unix socket).
func ServeTCP(ctx context.Context, addr, token string, cfg Config) error {
	cmd, err := child(cfg)
	if err != nil {
		return err
	}
	return serve(ctx, "", token+":"+addr, cmd)
}

func serve(ctx context.Context, sock, tcpAddrToken string, cmd *exec.Cmd) error {
	if cmd == nil || cmd.Path == "" && cmd.Args == nil {
		return errors.New("command required")
	}
	if tcpAddrToken == "" {
		if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
			return err
		}
		_ = os.Remove(sock)
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "silo-mcp-bridge", Version: "v1"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return err
	}
	defer cs.Close()

	fwd := newForwarder(cs)
	srv := mcp.NewServer(&mcp.Implementation{Name: "silo-mcp-bridge", Version: "v1"}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})
	for t, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return err
		}
		srv.AddTool(&mcp.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: objectSchema(t.InputSchema),
		}, fwd.handle)
	}

	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		DisableLocalhostProtection:   true,
		MaxRequestBodyBytes:          MaxResultBytes,
		PropagateRequestCancellation: true,
	})
	var handler http.Handler = h
	var token string
	if strings.Contains(tcpAddrToken, ":") {
		token, tcpAddrToken, _ = strings.Cut(tcpAddrToken, ":")
		if token != "" {
			handler = requireToken(h, token)
		}
	}
	var ln net.Listener
	if tcpAddrToken != "" {
		var err error
		ln, err = net.Listen("tcp", tcpAddrToken)
		if err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
			return err
		}
		_ = os.Remove(sock)
		var err error
		ln, err = net.Listen("unix", sock)
		if err != nil {
			return err
		}
		_ = os.Chmod(sock, 0o600)
	}
	defer func() {
		_ = ln.Close()
		if tcpAddrToken == "" {
			_ = os.Remove(sock)
		}
	}()

	httpSrv := &http.Server{Handler: handler}
	dead := make(chan struct{})
	if cmd.Process != nil {
		pid := cmd.Process.Pid
		go watchPID(ctx, pid, dead)
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-dead:
		}
		_ = httpSrv.Close()
	}()
	err = httpSrv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		select {
		case <-dead:
			return errors.New("stdio mcp process exited")
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	return err
}

func watchPID(ctx context.Context, pid int, dead chan struct{}) {
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			close(dead)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(childPoll):
		}
	}
}

// requireToken rejects requests without "Authorization: Bearer <token>".
func requireToken(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type forwarder struct {
	mu sync.Mutex
	cs *mcp.ClientSession
}

func newForwarder(cs *mcp.ClientSession) *forwarder { return &forwarder{cs: cs} }

func (f *forwarder) handle(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// skip: stdio MCP is one client; concurrent HTTP sessions queue on this lock.
	f.mu.Lock()
	defer f.mu.Unlock()
	var args any
	if len(req.Params.Arguments) > 0 {
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
	}
	res, err := f.cs.CallTool(ctx, &mcp.CallToolParams{Name: req.Params.Name, Arguments: args})
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	if len(b) > MaxResultBytes {
		return nil, fmt.Errorf("tool result exceeds %d bytes", MaxResultBytes)
	}
	return res, nil
}

func objectSchema(v any) any {
	if v == nil {
		return json.RawMessage(emptyObjectJSON)
	}
	var m map[string]any
	switch t := v.(type) {
	case map[string]any:
		m = t
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return json.RawMessage(emptyObjectJSON)
		}
		if json.Unmarshal(b, &m) != nil || m == nil {
			return json.RawMessage(emptyObjectJSON)
		}
	}
	if typ, _ := m["type"].(string); typ == "object" {
		return v
	}
	return json.RawMessage(emptyObjectJSON)
}

func stripBridgeEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "SILO_MCP_CMD=") || strings.HasPrefix(e, "SILO_MCP_ARGS=") || strings.HasPrefix(e, "SILO_MCP_SOCK=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

func ValidCommand(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return errors.New("stdio_command required")
	}
	if strings.ContainsAny(cmd, "\x00\n\r`$;&|<>(){}!*?[]\\\"'") {
		return errors.New("stdio_command must be an executable, not a shell line")
	}
	if strings.Contains(cmd, "..") {
		return errors.New("stdio_command path invalid")
	}
	switch filepath.Base(cmd) {
	case "sh", "bash", "dash", "zsh", "fish", "csh", "ksh":
		return errors.New("stdio_command cannot be a shell")
	}
	return nil
}

func ValidArg(a string) error {
	if strings.ContainsRune(a, 0) {
		return errors.New("stdio arg contains NUL")
	}
	return nil
}

func ValidImage(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if len(s) > 256 {
		return errors.New("stdio_image too long")
	}
	if strings.ContainsAny(s, " \t\n\r`$;&|<>(){}!*?\\\"'") || strings.Contains(s, "..") {
		return errors.New("stdio_image invalid")
	}
	return nil
}

func ValidEnvName(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("env name required")
	}
	for i, r := range s {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return fmt.Errorf("invalid env name %q", s)
	}
	return nil
}

// ParseArgs normalizes stored stdio args. Each element is one argument, but
// users paste "-y package" into a single field; npm exec would then fall back
// to a shell for the multi-word spec. Split on unquoted whitespace so the
// child always gets structured argv; quote an argument to keep it whole.
func ParseArgs(raw string) []string {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "null" {
		return []string{}
	}
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil || out == nil {
		return []string{}
	}
	split := make([]string, 0, len(out))
	for _, a := range out {
		split = append(split, SplitArg(a)...)
	}
	return split
}

// SplitArg turns one stored argument into zero or more argv elements.
// Whitespace separates; single or double quotes group.
func SplitArg(a string) []string {
	var words []string
	var cur strings.Builder
	inWord, quote := false, rune(0)
	flush := func() {
		if inWord {
			words = append(words, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for _, r := range a {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	flush()
	return words
}
