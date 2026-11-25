package main

import (
	"fmt"
	"log"
	"os"

	"silo.agent/internal/app"
	"silo.agent/internal/auth"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/version"
	"silo.agent/internal/webui"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version":
			fmt.Println("silo", version.String())
			return
		case "healthcheck":
			os.Exit(healthcheck())
		}
	}
	store, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	cfg := store.Config()
	gdb, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	if err := auth.EnsureBootstrap(gdb, cfg.Bootstrap.Email, cfg.Bootstrap.Password); err != nil {
		log.Fatal(err)
	}
	if err := auth.EnsureAdmin(gdb); err != nil {
		log.Fatal(err)
	}
	eng, err := dockerx.New(store)
	if err != nil {
		log.Fatal(err)
	}
	a := app.New(store, gdb, eng)
	log.Printf("silo %s listening %s docker=%s cp_url=%s", version.String(), cfg.HTTPAddr, cfg.DockerHost, cfg.CPURL)
	if webui.Embedded() {
		log.Printf("silo: serving the web console at %s", cfg.HTTPAddr)
	}
	log.Printf("silo images: bot=%s stdio=%s drive=%s egress=%s pull=%s", cfg.BotImage, cfg.MCPStdioImage, cfg.DriveImage(), cfg.EgressImage(), cfg.PullPolicy())
	if err := app.ListenAndServe(&cfg, a.Handler(), a.WorkerHandler(), a.Shutdown); err != nil {
		log.Fatal(err)
	}
}
