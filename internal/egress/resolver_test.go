package egress

import (
	"context"
	"testing"
)

// A nameserver on a client network is never asked: that network is internal
// and its DNS answers NXDOMAIN for every outside name.
func TestResolverSkipsClientNetworkDNS(t *testing.T) {
	gw := New(Config{})
	gw.Apply(testApply(t))
	dial := gw.Resolver().Dial
	if c, err := dial(context.Background(), "udp", "10.213.1.1:53"); err == nil {
		c.Close()
		t.Fatal("the resolver dialled a client network's DNS")
	}
	// Anything else is dialled as usual (UDP needs no listener).
	c, err := dial(context.Background(), "udp", "192.0.2.1:53")
	if err != nil {
		t.Fatalf("upstream dial: %v", err)
	}
	c.Close()
}
