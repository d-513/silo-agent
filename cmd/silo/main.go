package main

import (
	"log"

	"silo.agent/internal/app"
	"silo.agent/internal/auth"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
)

func main() {
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
	log.Printf("silo listening %s docker=%s cp_url=%s", cfg.HTTPAddr, cfg.DockerHost, cfg.CPURL)
	if err := app.ListenAndServe(&cfg, a.Handler(), a.Shutdown); err != nil {
		log.Fatal(err)
	}
}
