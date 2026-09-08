package main

import (
	"log"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

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
	gdb, err := db.Open(cfg.DataDir)
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
	h2s := &http2.Server{}
	err = app.ListenAndServe(&cfg, h2c.NewHandler(a.Handler(), h2s), a.Shutdown)
	if err != nil {
		log.Fatal(err)
	}
}
