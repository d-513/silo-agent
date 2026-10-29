package egress

import (
	"net"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

func TestReadClientHelloSNI(t *testing.T) {
	cli, srv := net.Pipe()
	defer cli.Close()
	defer srv.Close()

	go func() {
		cfg := &utls.Config{ServerName: "example.com", InsecureSkipVerify: true, NextProtos: []string{"h2", "http/1.1"}}
		u := utls.UClient(cli, cfg, utls.HelloChrome_Auto)
		_ = u.Handshake()
	}()

	_ = srv.SetReadDeadline(time.Now().Add(5 * time.Second))
	raw, sni, err := readClientHello(srv)
	if err != nil {
		t.Fatal(err)
	}
	if sni != "example.com" {
		t.Fatalf("sni = %q", sni)
	}
	spec := mirrorSpec(raw, "example.com")
	if spec == nil {
		t.Fatal("no mirrored spec")
	}
	// The client offered h2; upstream only http/1.1 is offered (all the
	// gateway relays), and ALPS, which belongs to h2, goes with it.
	alpn := 0
	for _, e := range spec.Extensions {
		switch e := e.(type) {
		case *utls.ALPNExtension:
			alpn++
			if len(e.AlpnProtocols) != 1 || e.AlpnProtocols[0] != "http/1.1" {
				t.Fatalf("ALPN offers %v, want only http/1.1", e.AlpnProtocols)
			}
		case *utls.ApplicationSettingsExtension, *utls.ApplicationSettingsExtensionNew:
			t.Fatal("ALPS was kept without the h2 it settles")
		}
	}
	if alpn != 1 {
		t.Fatalf("%d ALPN extensions, want the client's one in place", alpn)
	}
}

func TestSNIFromChromeHello(t *testing.T) {
	if got := sniFromClientHello(nil); got != "" {
		t.Fatalf("empty body = %q", got)
	}
	if got := sniFromClientHello([]byte{3, 3}); got != "" {
		t.Fatalf("truncated body = %q", got)
	}
}
