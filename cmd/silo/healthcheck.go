package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"silo.agent/internal/config"
)

// healthcheck asks this machine's control plane for /healthz: `silo
// healthcheck` is the container image's HEALTHCHECK, so the image needs no
// curl. It reads http_addr as the control plane does and exits 0 only on an
// answer of 200.
func healthcheck() int {
	store, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	host, port, err := net.SplitHostPort(store.Config().HTTPAddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck: http_addr:", err)
		return 1
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	c := http.Client{Timeout: 3 * time.Second}
	res, err := c.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck:", res.Status)
		return 1
	}
	return 0
}
