package mcpx

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fakeMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var msg struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		http.Error(w, "bad json", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch msg.Method {
	case "initialize":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": msg.ID,
			"result": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "fake", "version": "1"},
			},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": msg.ID,
			"result": map[string]any{
				"tools": []map[string]any{{
					"name":        "echo",
					"description": "echo",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
				}},
			},
		})
	case "tools/call":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": msg.ID,
			"result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "pong"}},
			},
		})
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": msg.ID,
			"error": map[string]any{"code": -32601, "message": msg.Method},
		})
	}
}

func TestConnectListCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(fakeMCP))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := Connect(ctx, Dial{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	tools, err := ListAll(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("%v", tools)
	}
	out, err := Call(ctx, sess, "echo", map[string]any{"q": "hi"})
	if err != nil || out != "pong" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestConnectKeepsPOSTOnRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mcp/", http.StatusFound)
	})
	mux.HandleFunc("/mcp/", fakeMCP)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := Connect(ctx, Dial{URL: srv.URL + "/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	sess.Close()
}

func TestConnectRetriesTrailingSlashOn404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "session not found", http.StatusNotFound)
	})
	mux.HandleFunc("/mcp/", fakeMCP)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := Connect(ctx, Dial{URL: srv.URL + "/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	sess.Close()
}

func TestWolframLive(t *testing.T) {
	d := net.Dialer{Timeout: 3 * time.Second}
	c, err := d.Dial("tcp", "agenttools.wolfram.com:443")
	if err != nil {
		t.Skip("wolfram unreachable: ", err)
	}
	_ = c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	sess, err := Connect(ctx, Dial{URL: "https://agenttools.wolfram.com/mcp"})
	if err != nil {
		t.Skip("wolfram mcp initialize: ", err)
	}
	defer sess.Close()
	tools, err := ListAll(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) == 0 {
		t.Fatal("no tools")
	}
	name := tools[0].Name
	for _, tl := range tools {
		if tl.Name == "WolframAlpha" {
			name = tl.Name
			break
		}
	}
	out, err := Call(ctx, sess, name, map[string]any{"query": "2+2"})
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("empty result")
	}
}

func TestTwilioDocsLive(t *testing.T) {
	d := net.Dialer{Timeout: 3 * time.Second}
	c, err := d.Dial("tcp", "mcp.twilio.com:443")
	if err != nil {
		t.Skip("twilio unreachable: ", err)
	}
	_ = c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	sess, err := Connect(ctx, Dial{URL: "https://mcp.twilio.com/docs"})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	tools, err := ListAll(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) == 0 {
		t.Fatal("no tools")
	}
}

func TestHTML404IsNotMCP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `<!DOCTYPE HTML PUBLIC "-//IETF//DTD HTML 2.0//EN"><html><head><title>404 Not Found</title></head><body><h1>Not Found</h1></body></html>`)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Connect(ctx, Dial{URL: srv.URL + "/mcp"})
	if err == nil || !strings.Contains(err.Error(), "not an MCP endpoint") {
		t.Fatal(err)
	}
}

func TestHeadersFromJSON(t *testing.T) {
	h, err := HeadersFromJSON(`{"X-A":"b"}`)
	if err != nil || h["X-A"] != "b" {
		t.Fatal(h, err)
	}
}
