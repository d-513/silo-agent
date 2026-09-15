// Package mcpbridge runs one STDIO MCP process and tunnels its raw JSON-RPC
// traffic to the control plane over a reverse ConnectRPC stream. The sidecar
// dials the CP (rather than the CP dialing a published sidecar port) and
// authenticates with a per-attachment bridge token.
package mcpbridge

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
)

const (
	MaxResultBytes = 8 << 20
	reconnectMin   = 200 * time.Millisecond
	reconnectMax   = 15 * time.Second
	dialTimeout    = 10 * time.Second
)

// Config describes one sidecar: the child process to supervise, plus the CP
// endpoint and bridge token to reach.
type Config struct {
	Command string
	Args    []string
	Env     []string
	CPURL   string
	Token   string
}

// errChildExited means the child process stopped on its own while a tunnel was
// live; the bridge should exit so the container's restart policy replaces it.
var errChildExited = errors.New("stdio mcp process exited")

// Serve supervises the child and keeps a reverse tunnel to the control plane
// open. The child is restarted for each tunnel session, so a CP restart does
// not require recreating the container (npm's cache lives in the container
// filesystem). A child crash exits the bridge so the container restarts.
func Serve(ctx context.Context, cfg Config) error {
	if err := ValidCommand(cfg.Command); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.CPURL) == "" {
		return errors.New("cp url required")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return errors.New("bridge token required")
	}
	client := newClient(cfg.CPURL, cfg.Token)
	wait := reconnectMin
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		sctx, cancel := context.WithCancel(ctx)
		conn := client.Tunnel(sctx)
		err := session(sctx, cfg, conn)
		_ = conn.CloseRequest()
		_ = conn.CloseResponse()
		cancel()
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, errChildExited):
			return err
		}
		time.Sleep(wait)
		wait *= 2
		if wait > reconnectMax {
			wait = reconnectMax
		}
	}
}

// session runs one child against one tunnel. It returns errChildExited if the
// child stopped while the tunnel was live, or nil when the tunnel closed and
// the next session should start a fresh child.
func session(ctx context.Context, cfg Config, conn *connect.BidiStreamForClient[v1.BridgeFrame, v1.BridgeFrame]) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd, err := child(cfg)
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	_ = conn.Send(&v1.BridgeFrame{Body: &v1.BridgeFrame_Status{Status: "starting"}})

	childDone := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64<<10), MaxResultBytes)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			if len(line) == 0 {
				continue
			}
			if err := conn.Send(&v1.BridgeFrame{Body: &v1.BridgeFrame_Data{Data: line}}); err != nil {
				return
			}
		}
		childDone <- sc.Err()
	}()

	recvErr := make(chan error, 1)
	go func() {
		for {
			frame, err := conn.Receive()
			if err != nil {
				recvErr <- err
				return
			}
			data := frame.GetData()
			if len(data) == 0 {
				continue
			}
			if _, err := stdin.Write(append(data, '\n')); err != nil {
				recvErr <- err
				return
			}
		}
	}()

	var cause error
	select {
	case <-ctx.Done():
		cause = ctx.Err()
	case err := <-childDone:
		cause = fmt.Errorf("%w: %v", errChildExited, err)
	case <-recvErr:
		cause = nil
	}
	cancel()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
	_ = stdin.Close()
	return cause
}

func child(cfg Config) (*exec.Cmd, error) {
	for _, a := range cfg.Args {
		if err := ValidArg(a); err != nil {
			return nil, err
		}
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

func newClient(cpURL, token string) silov1connect.MCPHostClient {
	dialer := &net.Dialer{Timeout: dialTimeout, FallbackDelay: 100 * time.Millisecond}
	hc := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		},
	}
	return silov1connect.NewMCPHostClient(hc, cpURL, connect.WithInterceptors(tokenInterceptor{token}))
}

type tokenInterceptor struct{ tok string }

func (t tokenInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+t.tok)
		return next(ctx, req)
	}
}

func (t tokenInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Bearer "+t.tok)
		return conn
	}
}

func (t tokenInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func stripBridgeEnv(env []string) []string {
	strip := []string{
		"SILO_MCP_CMD=", "SILO_MCP_ARGS=", "SILO_MCP_SOCK=",
		"SILO_MCP_TCP=", "SILO_MCP_TOKEN=",
		"SILO_CP_URL=", "SILO_BRIDGE_TOKEN=",
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		drop := false
		for _, p := range strip {
			if strings.HasPrefix(e, p) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, e)
		}
	}
	return out
}

// ParseArgs and the validators below are shared with the control plane: the CP
// validates connector edits and persists the structured argv the bridge runs.

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
