package auth

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("127.0.0.1/32")}
	for _, c := range []struct {
		name    string
		remote  string
		xff     []string
		trusted []netip.Prefix
		want    string
	}{
		{"no proxy", "203.0.113.9:4000", nil, nil, "203.0.113.9"},
		// Without the setting the header is anyone's to write, so it is ignored.
		{"spoofed header, nothing trusted", "203.0.113.9:4000", []string{"1.2.3.4"}, nil, "203.0.113.9"},
		{"spoofed header, peer not a proxy", "203.0.113.9:4000", []string{"1.2.3.4"}, proxies, "203.0.113.9"},
		{"behind the proxy", "10.0.0.2:4000", []string{"198.51.100.7"}, proxies, "198.51.100.7"},
		// The client can prepend whatever it likes; only what our proxy appended counts.
		{"client lies in front", "10.0.0.2:4000", []string{"1.2.3.4, 198.51.100.7"}, proxies, "198.51.100.7"},
		{"two proxies", "127.0.0.1:4000", []string{"198.51.100.7, 10.0.0.5"}, proxies, "198.51.100.7"},
		{"split headers", "127.0.0.1:4000", []string{"198.51.100.7", "10.0.0.5"}, proxies, "198.51.100.7"},
		{"port in the header", "10.0.0.2:4000", []string{"198.51.100.7:5555"}, proxies, "198.51.100.7"},
		{"ipv6", "10.0.0.2:4000", []string{"[2001:db8::1]:443"}, proxies, "2001:db8::1"},
		{"garbage stops the walk", "10.0.0.2:4000", []string{"1.2.3.4, nonsense"}, proxies, "10.0.0.2"},
		{"proxy sent nothing", "10.0.0.2:4000", nil, proxies, "10.0.0.2"},
		{"all trusted", "10.0.0.2:4000", []string{"10.0.0.9"}, proxies, "10.0.0.9"},
		{"mapped v4 peer", "[::ffff:10.0.0.2]:4000", []string{"198.51.100.7"}, proxies, "198.51.100.7"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		for _, v := range c.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := ClientIP(r, c.trusted); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
