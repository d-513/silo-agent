package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	"silo.agent/internal/mcpbridge"
)

func main() {
	cmd := os.Getenv("SILO_MCP_CMD")
	if cmd == "" {
		log.Fatal("SILO_MCP_CMD required")
	}
	var args []string
	if raw := os.Getenv("SILO_MCP_ARGS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			log.Fatalf("SILO_MCP_ARGS: %v", err)
		}
	}
	cp := os.Getenv("SILO_CP_URL")
	token := os.Getenv("SILO_BRIDGE_TOKEN")
	if cp == "" || token == "" {
		log.Fatal("SILO_CP_URL and SILO_BRIDGE_TOKEN required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := mcpbridge.Serve(ctx, mcpbridge.Config{
		Command: cmd,
		Args:    args,
		CPURL:   cp,
		Token:   token,
	}); err != nil {
		log.Fatalf("mcp bridge: %v", err)
	}
}
