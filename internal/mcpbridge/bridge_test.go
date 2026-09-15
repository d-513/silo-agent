package mcpbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
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

// hostFake plays the control plane: it accepts the sidecar's tunnel and drives
// a standard MCP client over it.
type hostFake struct {
	addr string
	got  chan *mcp.ClientSession
	errc chan error
}

func (h *hostFake) Tunnel(ctx context.Context, stream *connect.BidiStream[v1.BridgeFrame, v1.BridgeFrame]) error {
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "cp", Version: "1"}, nil).Connect(ctx, &frameTransport{stream: stream}, nil)
	if err != nil {
		return err
	}
	h.got <- cs
	<-ctx.Done()
	return nil
}

func startHost(t *testing.T) *hostFake {
	t.Helper()
	h := &hostFake{got: make(chan *mcp.ClientSession, 1), errc: make(chan error, 1)}
	path, handler := silov1connect.NewMCPHostHandler(h)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewUnstartedServer(h2c.NewHandler(mux, &http2.Server{}))
	srv.Start()
	t.Cleanup(srv.Close)
	h.addr = srv.Listener.Addr().String()
	return h
}

func (h *hostFake) session(t *testing.T) *mcp.ClientSession {
	t.Helper()
	select {
	case cs := <-h.got:
		return cs
	case err := <-h.errc:
		t.Fatalf("bridge serve exited: %v", err)
		return nil
	case <-time.After(10 * time.Second):
		t.Fatal("control plane never got a session")
		return nil
	}
}

type frameTransport struct {
	stream *connect.BidiStream[v1.BridgeFrame, v1.BridgeFrame]
}

func (t *frameTransport) Connect(context.Context) (mcp.Connection, error) {
	return &frameConn{stream: t.stream}, nil
}

type frameConn struct {
	stream *connect.BidiStream[v1.BridgeFrame, v1.BridgeFrame]
}

func (c *frameConn) Read(context.Context) (jsonrpc.Message, error) {
	for {
		frame, err := c.stream.Receive()
		if err != nil {
			return nil, err
		}
		data := frame.GetData()
		if len(data) == 0 {
			continue
		}
		return jsonrpc.DecodeMessage(data)
	}
}

func (c *frameConn) Write(_ context.Context, msg jsonrpc.Message) error {
	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return err
	}
	return c.stream.Send(&v1.BridgeFrame{Body: &v1.BridgeFrame_Data{Data: data}})
}

func (c *frameConn) Close() error      { return nil }
func (c *frameConn) SessionID() string { return "" }

func bridgeConfig(h *hostFake, mode string) Config {
	return Config{
		Command: os.Args[0],
		Args:    []string{"-test.run=^$"},
		Env:     append(os.Environ(), "SILO_MCP_HELPER="+mode),
		CPURL:   "http://" + h.addr,
		Token:   "test-token",
	}
}

func startBridge(t *testing.T, mode string) (*hostFake, context.CancelFunc) {
	t.Helper()
	h := startHost(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	h.errc = errc
	go func() { errc <- Serve(ctx, bridgeConfig(h, mode)) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errc:
		case <-time.After(5 * time.Second):
		}
	})
	return h, cancel
}

func TestBridgeEcho(t *testing.T) {
	h, _ := startBridge(t, "echo")
	cs := h.session(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var names []string
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	if len(names) != 1 || names[0] != "echo" {
		t.Fatalf("tools %v", names)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"q": "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content %+v", res.Content)
	}
	if txt, ok := res.Content[0].(*mcp.TextContent); !ok || txt.Text != "hi" {
		t.Fatalf("content %+v", res.Content[0])
	}
}

func TestBridgeChildExit(t *testing.T) {
	h, _ := startBridge(t, "exit")
	cs := h.session(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "die", Arguments: map[string]any{"q": "x"}})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("expected die to fail the call: %+v %v", res, err)
	}
}

func TestBridgeCancel(t *testing.T) {
	h, _ := startBridge(t, "sleep")
	cs := h.session(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "sleep", Arguments: map[string]any{"q": "x"}})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("expected cancel: %+v %v", res, err)
	}
}

func TestBridgeMalformedChild(t *testing.T) {
	h := startHost(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Serve(ctx, bridgeConfig(h, "garbage")); err == nil {
		t.Fatal("expected error")
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
	got := stripBridgeEnv([]string{
		"FOO=1", "SILO_MCP_CMD=npx", "BAR=2",
		"SILO_CP_URL=http://x", "SILO_BRIDGE_TOKEN=secret",
	})
	raw, _ := json.Marshal(got)
	if string(raw) != `["FOO=1","BAR=2"]` {
		t.Fatal(got)
	}
}

func TestParseArgs(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{`["-y @modelcontextprotocol/server-fetch"]`, []string{"-y", "@modelcontextprotocol/server-fetch"}},
		{`["-y", "@modelcontextprotocol/server-fetch"]`, []string{"-y", "@modelcontextprotocol/server-fetch"}},
		{`["--flag", "\"hello world\""]`, []string{"--flag", "hello world"}},
		{`["-p \"a b\""]`, []string{"-p", "a b"}},
		{`["-p 'x y'"]`, []string{"-p", "x y"}},
		{`[]`, []string{}},
		{``, []string{}},
		{`null`, []string{}},
		{`not json`, []string{}},
	}
	for _, c := range cases {
		got := ParseArgs(c.raw)
		r1, _ := json.Marshal(got)
		r2, _ := json.Marshal(c.want)
		if string(r1) != string(r2) {
			t.Errorf("ParseArgs(%q) = %s, want %s", c.raw, r1, r2)
		}
	}
}
