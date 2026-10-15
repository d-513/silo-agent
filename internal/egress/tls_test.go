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
	for _, e := range spec.Extensions {
		if _, ok := e.(*utls.ALPNExtension); ok {
			t.Fatal("ALPN must be dropped so the origin answers HTTP/1.1")
		}
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
