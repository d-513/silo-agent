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
	sock := os.Getenv("SILO_MCP_SOCK")
	if sock == "" {
		sock = "/run/silo/" + mcpbridge.SockFile
	}
	var args []string
	if raw := os.Getenv("SILO_MCP_ARGS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			log.Fatalf("SILO_MCP_ARGS: %v", err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := mcpbridge.Run(ctx, mcpbridge.Config{Command: cmd, Args: args, Sock: sock}); err != nil && !errorsIsSignal(err, ctx) {
		log.Fatal(err)
	}
}

func errorsIsSignal(err error, ctx context.Context) bool {
	return err == nil || ctx.Err() != nil
}
