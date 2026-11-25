package voice

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"

	"silo.agent/internal/config"
)

// A control plane on a container network offers a call the machine's
// addresses in place of its own; one on the host's network changes nothing.
func TestBehindEngineOffersTheMachinesAddresses(t *testing.T) {
	own := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("10.212.0.2"), net.ParseIP("10.212.1.2")}
	via, rule, ok := behindEngine(own, []string{"192.168.10.51", "fd00::1", "100.64.0.7"})
	if !ok || via.String() != "10.212.0.2" || strings.Join(rule.External, ",") != "192.168.10.51,100.64.0.7" || rule.Local != "10.212.0.2" {
		t.Fatalf("via %v rule %+v ok %v", via, rule, ok)
	}
	if _, _, ok := behindEngine([]net.IP{net.ParseIP("192.168.10.51")}, []string{"192.168.10.51"}); ok {
		t.Fatal("a control plane on the host's network was given a rewrite")
	}
	if _, _, ok := behindEngine(own, nil); ok {
		t.Fatal("a rewrite with no machine address")
	}
}

// The rule is one pion takes, and what a peer then offers is the machine's
// addresses and not the container's.
func TestBehindEngineRuleIsWhatAPeerOffers(t *testing.T) {
	var local net.IP
	for _, ip := range ownAddrs() {
		if ip.To4() != nil && !ip.IsLoopback() {
			local = ip
			break
		}
	}
	if local == nil {
		t.Skip("no address but loopback here")
	}
	via, rule, ok := behindEngine(ownAddrs(), []string{"203.0.113.7", "203.0.113.8"})
	if !ok {
		t.Fatal("no rule")
	}
	// One shared port, as a call has (voice.webrtc.udp_port).
	conn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port
	se := webrtc.SettingEngine{}
	se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, conn))
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	se.SetIPFilter(func(ip net.IP) bool { return ip.Equal(via) })
	if err := se.SetICEAddressRewriteRules(rule); err != nil {
		t.Fatal(err)
	}
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if _, err := pc.CreateDataChannel("x", nil); err != nil {
		t.Fatal(err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	done := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-done
	sdp := pc.LocalDescription().SDP
	for _, want := range rule.External {
		if !strings.Contains(sdp, fmt.Sprintf(" %s %d typ host", want, port)) {
			t.Errorf("%s is not offered:\n%s", want, sdp)
		}
	}
	if strings.Contains(sdp, " "+via.String()+" ") {
		t.Errorf("the container's own %s is offered:\n%s", via, sdp)
	}
}

// An engine is not made while the machine's addresses cannot be read: it is
// made once, and would offer every later call the container's own. The call
// fails in words, and the next one asks again.
func TestEngineWaitsForTheMachinesAddresses(t *testing.T) {
	var r rtc
	asked := 0
	down := func() ([]string, error) { asked++; return nil, errors.New("siloosd is not answering") }
	if _, err := r.engine(config.Config{}, down); err == nil || !strings.Contains(err.Error(), "siloosd") {
		t.Fatalf("an engine was made without the machine's addresses: %v", err)
	}
	up := func() ([]string, error) { asked++; return []string{"203.0.113.7"}, nil }
	if _, err := r.engine(config.Config{}, up); err != nil {
		t.Fatal(err)
	}
	// Made: nothing is asked again.
	if _, err := r.engine(config.Config{}, down); err != nil || asked != 2 {
		t.Fatalf("after the engine was made: err %v, asked %d times", err, asked)
	}
}
