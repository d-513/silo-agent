// silo-drive is the drive sidecar: it dials the control plane and keeps the
// Bot's rclone mounts under /mnt/drives, whose mounts propagate into the Bot.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"silo.agent/internal/drivehost"
)

func main() {
	client, err := drivehost.NewClient(os.Getenv("SILO_CP_URL"), os.Getenv("SILO_DRIVE_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}
	out, emit := drivehost.Outbox()
	sup := drivehost.New(drivehost.Config{
		MountRoot: env("SILO_DRIVE_MOUNT_ROOT", "/mnt/drives"),
		CacheRoot: env("SILO_DRIVE_CACHE_ROOT", "/cache"),
		RunDir:    env("SILO_DRIVE_RUN_DIR", "/run/silo-drive"),
	}, drivehost.Exec{}, emit)
	sup.Cleanup()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = drivehost.Serve(ctx, client, sup, out)
	sup.StopAll()
	if err != nil && ctx.Err() == nil {
		log.Fatalf("silo-drive: %v", err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
