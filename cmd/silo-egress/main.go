// silo-egress is the network filter gateway: one per container engine, every
// Bot box and STDIO sidecar reaches the internet through it. It serves DNS,
// an HTTP proxy, transparent 80/443, SOCKS5, and the forward that carries a
// client's connection to the control plane, and reports what it did.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"silo.agent/internal/egress"
)

func main() {
	client, err := egress.NewClient(os.Getenv("SILO_CP_URL"), os.Getenv("SILO_EGRESS_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}
	gw := egress.New(egress.Config{
		DNSAddr:    env("SILO_EGRESS_DNS", egress.DefaultDNSAddr),
		ProxyAddr:  env("SILO_EGRESS_PROXY", egress.DefaultProxyAddr),
		HTTPAddr:   env("SILO_EGRESS_HTTP", egress.DefaultHTTPAddr),
		TLSAddr:    env("SILO_EGRESS_TLS", egress.DefaultTLSAddr),
		SOCKSAddr:  env("SILO_EGRESS_SOCKS", egress.DefaultSOCKSAddr),
		CPLinkAddr: env("SILO_EGRESS_CP_LINK", egress.DefaultCPLinkAddr),
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 2)
	go func() { errc <- gw.Run(ctx) }()
	go func() { errc <- egress.Serve(ctx, client, gw) }()
	if err := <-errc; err != nil && ctx.Err() == nil {
		stop()
		log.Fatalf("silo-egress: %v", err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
