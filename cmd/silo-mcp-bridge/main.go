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
	// Behind the network filter: trust its certificate before the child
	// starts. A failure is logged, not fatal: HTTPS through the gateway fails
	// then, and the gateway still filters.
	if err := mcpbridge.InstallCA(os.Getenv("SILO_EGRESS_CA")); err != nil {
		log.Printf("mcp bridge: the network filter's certificate: %v", err)
	}
	if err := mcpbridge.InstallDNS(os.Getenv("SILO_EGRESS_DNS")); err != nil {
		log.Printf("mcp bridge: the network filter's resolver: %v", err)
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
