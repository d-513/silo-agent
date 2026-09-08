package mcpbridge

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"silo.agent/internal/mcpx"
)

func TestMain(m *testing.M) {
	switch os.Getenv("SILO_MCP_HELPER") {
	case "echo":
		runEcho()
		os.Exit(0)
	case "garbage":
		os.Stdout.WriteString("this is not jsonrpc\n")
		os.Exit(0)
	case "exit":
		runExit()
		os.Exit(0)
	case "sleep":
		runSleep()
		os.Exit(0)
	case "big":
		runBig()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type echoIn struct {
	Q string `json:"q"`
}

func runEcho() {
	srv := mcp.NewServer(&mcp.Implementation{Name: "echo", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "echo", Description: "echo"},
		func(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.Q}}}, nil, nil
		})
	_ = srv.Run(context.Background(), &mcp.StdioTransport{})
}

func runExit() {
	srv := mcp.NewServer(&mcp.Implementation{Name: "exit", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "die", Description: "exit"},
		func(context.Context, *mcp.CallToolRequest, echoIn) (*mcp.CallToolResult, any, error) {
			os.Exit(1)
			return nil, nil, nil
		})
	_ = srv.Run(context.Background(), &mcp.StdioTransport{})
}

func runSleep() {
	srv := mcp.NewServer(&mcp.Implementation{Name: "sleep", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "sleep", Description: "block"},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ echoIn) (*mcp.CallToolResult, any, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		})
	_ = srv.Run(context.Background(), &mcp.StdioTransport{})
}

func runBig() {
	srv := mcp.NewServer(&mcp.Implementation{Name: "big", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "big", Description: "huge"},
		func(context.Context, *mcp.CallToolRequest, echoIn) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("x", MaxResultBytes+1)}}}, nil, nil
		})
	_ = srv.Run(context.Background(), &mcp.StdioTransport{})
}

func helper(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "SILO_MCP_HELPER="+mode)
	return cmd
}

func serveBridge(t *testing.T, mode string) (context.CancelFunc, string) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, SockFile)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- Serve(ctx, sock, helper(t, mode)) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errc:
		case <-time.After(5 * time.Second):
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			c, err := net.Dial("unix", sock)
			if err == nil {
				c.Close()
				return cancel, sock
			}
		}
		select {
		case err := <-errc:
			t.Fatalf("bridge: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("socket not ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func connectSock(t *testing.T, sock string) *mcpx.Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	sess, err := mcpx.Connect(ctx, mcpx.Dial{URL: "http://localhost/mcp", Sock: sock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	return sess
}

func TestBridgeEcho(t *testing.T) {
	_, sock := serveBridge(t, "echo")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess := connectSock(t, sock)
	tools, err := mcpx.ListAll(ctx, sess)
	if err != nil || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("%v %v", tools, err)
	}
	out, err := mcpx.Call(ctx, sess, "echo", map[string]any{"q": "hi"})
	if err != nil || out != "hi" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestBridgeConcurrent(t *testing.T) {
	_, sock := serveBridge(t, "echo")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess := connectSock(t, sock)
	errc := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			_, err := mcpx.Call(ctx, sess, "echo", map[string]any{"q": "x"})
			errc <- err
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
	}
}

func TestBridgeMalformedChild(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, SockFile)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := Serve(ctx, sock, helper(t, "garbage"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBridgeChildExit(t *testing.T) {
	_, sock := serveBridge(t, "exit")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess := connectSock(t, sock)
	_, err := mcpx.Call(ctx, sess, "die", map[string]any{"q": "x"})
	if err == nil {
		t.Fatal("expected die")
	}
}

func TestBridgeCancel(t *testing.T) {
	_, sock := serveBridge(t, "sleep")
	sess := connectSock(t, sock)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := mcpx.Call(ctx, sess, "sleep", map[string]any{})
	if err == nil {
		t.Fatal("expected cancel")
	}
}

func TestBridgeOversized(t *testing.T) {
	_, sock := serveBridge(t, "big")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess := connectSock(t, sock)
	_, err := mcpx.Call(ctx, sess, "big", map[string]any{"q": "x"})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("got %v", err)
	}
}

func TestBridgeShutdown(t *testing.T) {
	cancel, sock := serveBridge(t, "echo")
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(sock); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("socket lingered")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestValidCommand(t *testing.T) {
	if err := ValidCommand("npx"); err != nil {
		t.Fatal(err)
	}
	if err := ValidCommand("bash"); err == nil {
		t.Fatal("shell")
	}
	if err := ValidCommand("npx; rm -rf /"); err == nil {
		t.Fatal("meta")
	}
	if err := ValidCommand("../npx"); err == nil {
		t.Fatal("dotdot")
	}
	if err := ValidImage("node:22"); err != nil {
		t.Fatal(err)
	}
	if err := ValidImage("x;y"); err == nil {
		t.Fatal("image")
	}
	if err := ValidEnvName("GITHUB_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if err := ValidEnvName("1x"); err == nil {
		t.Fatal("env")
	}
}

func TestStripBridgeEnv(t *testing.T) {
	got := stripBridgeEnv([]string{"FOO=1", "SILO_MCP_CMD=npx", "BAR=2"})
	raw, _ := json.Marshal(got)
	if string(raw) != `["FOO=1","BAR=2"]` {
		t.Fatal(got)
	}
}
