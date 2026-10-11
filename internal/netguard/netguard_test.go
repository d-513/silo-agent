package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestInternalAddresses(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1":            true,
		"127.8.9.10":           true,
		"10.1.2.3":             true,
		"172.16.0.1":           true,
		"172.31.255.255":       true,
		"172.32.0.1":           false,
		"192.168.1.10":         true,
		"169.254.169.254":      true, // cloud metadata
		"100.64.0.1":           true, // carrier-grade NAT, Tailscale
		"0.0.0.0":              true,
		"255.255.255.255":      true,
		"224.0.0.1":            true,
		"240.0.0.1":            true,
		"192.0.0.8":            true,
		"198.18.0.1":           true,
		"::1":                  true,
		"::":                   true,
		"fe80::1":              true,
		"fc00::1":              true,
		"fd12:3456::1":         true,
		"ff02::1":              true,
		"::ffff:10.0.0.1":      true, // a v4 address in v6 clothes
		"::ffff:8.8.8.8":       false,
		"64:ff9b::a00:1":       true, // NAT64 of 10.0.0.1
		"64:ff9b::808:808":     false,
		"2002:a00:1::1":        true, // 6to4 of 10.0.0.1
		"2002:808:808::1":      false,
		"8.8.8.8":              false,
		"1.1.1.1":              false,
		"93.184.216.34":        false,
		"2606:4700:4700::1111": false,
		"2001:db8::1":          true, // documentation range: nothing lives there
		"192.0.2.1":            true,
	} {
		if got := Internal(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Internal(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestParseAllow(t *testing.T) {
	got, err := ParseAllow("127.0.0.1:9223, 10.0.0.0/8  192.168.1.5 [::1]:8080\nfd00::/8")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("entries %v", got)
	}
	for _, bad := range []string{"localhost", "10.0.0.1:99999", "10.0.0.0/40", "example.com:443", "10.0.0.1:0"} {
		if _, err := ParseAllow(bad); err == nil {
			t.Errorf("%q was taken as an exception", bad)
		}
	}
	if got, err := ParseAllow("  "); err != nil || len(got) != 0 {
		t.Fatalf("empty list: %v %v", got, err)
	}
}

func TestPolicyCheck(t *testing.T) {
	allow, _ := ParseAllow("127.0.0.1:9223, 10.9.0.0/16")
	p := Policy{Block: true, Allow: allow}
	for addr, ok := range map[string]bool{
		"8.8.8.8:443":        true,
		"127.0.0.1:9223":     true,  // the one port the operator opened
		"127.0.0.1:5433":     false, // not the database beside it
		"10.9.4.4:25":        true,
		"10.8.4.4:25":        false,
		"169.254.169.254:80": false,
		"[::1]:9223":         false,
	} {
		err := p.Check(netip.MustParseAddrPort(addr))
		if (err == nil) != ok {
			t.Errorf("Check(%s) = %v, want allowed=%v", addr, err, ok)
		}
		if err != nil && !errors.Is(err, ErrBlocked) {
			t.Errorf("Check(%s) error is not ErrBlocked: %v", addr, err)
		}
	}
	// Off, nothing is refused.
	if err := (Policy{}).Check(netip.MustParseAddrPort("127.0.0.1:5433")); err != nil {
		t.Fatalf("an idle policy refused: %v", err)
	}
}

// The check is made on the address the socket is about to connect to, so a
// name that resolves inside is refused like the address itself, and so is a
// redirect that leads inside.
func TestDialRefusesWhatANameResolvesTo(t *testing.T) {
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("secret")) }))
	defer inside.Close()
	port := inside.Listener.Addr().(*net.TCPAddr).Port
	p := Policy{Block: true}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// localhost is a name; it is the loopback address that gets refused.
	if _, err := p.DialContext(ctx, "tcp", net.JoinHostPort("localhost", itoa(port))); !errors.Is(err, ErrBlocked) {
		t.Fatalf("dial by name: %v", err)
	}
	if _, err := p.DialContext(ctx, "tcp", inside.Listener.Addr().String()); !errors.Is(err, ErrBlocked) {
		t.Fatalf("dial by address: %v", err)
	}
	res, err := p.Client(5 * time.Second).Get(inside.URL)
	if err == nil {
		res.Body.Close()
		t.Fatal("the HTTP client reached a loopback server")
	}
	if !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "private") {
		t.Fatalf("http error %v", err)
	}

	// With that one port opened, it works.
	allow, _ := ParseAllow(inside.Listener.Addr().String())
	open := Policy{Block: true, Allow: allow}
	res, err = open.Client(5 * time.Second).Get(inside.URL)
	if err != nil {
		t.Fatalf("an allowed address was refused: %v", err)
	}
	res.Body.Close()
	// And with the policy off, as before.
	res, err = Policy{}.Client(5 * time.Second).Get(inside.URL)
	if err != nil {
		t.Fatalf("an idle policy refused: %v", err)
	}
	res.Body.Close()
}

func itoa(n int) string {
	return netip.AddrPortFrom(netip.IPv4Unspecified(), uint16(n)).String()[len("0.0.0.0:"):]
}
