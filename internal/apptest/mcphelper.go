package apptest

import (
	"context"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPHelperEnv selects the STDIO MCP child a test binary can act as. The real
// bridge spawns the test binary with this set, mirroring how the sidecar spawns
// a real MCP process.
const MCPHelperEnv = "SILO_MCP_HELPER"

// RunMCPHelper runs a tiny STDIO MCP server when MCPHelperEnv=echo and reports
// whether it did. TestMain must call it before m.Run, then exit.
func RunMCPHelper() bool {
	if os.Getenv(MCPHelperEnv) != "echo" {
		return false
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "echo", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "echo", Description: "echo the q argument"},
		func(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.Q}}}, nil, nil
		})
	_ = srv.Run(context.Background(), &mcp.StdioTransport{})
	return true
}

type echoIn struct {
	Q string `json:"q"`
}

// NewEchoServer is a streamable-HTTP MCP server with an echo tool, mounted on
// an httptest server the harness can hand to an HTTP connector.
func NewEchoServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "echo-http", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "echo", Description: "echo the q argument"},
		func(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.Q}}}, nil, nil
		})
	return srv
}
