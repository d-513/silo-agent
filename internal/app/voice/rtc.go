package voice

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pion/webrtc/v4"

	"silo.agent/internal/config"
)

// rtc is the one WebRTC engine every call shares: it listens on the
// configured UDP and TCP ports once, and each call is a peer on them.
type rtc struct {
	once sync.Once
	made atomic.Bool
	api  *webrtc.API
	err  error
	// ports are what it ended up listening on, for the logs and the tests.
	udp, tcp int
}

// engine builds the shared WebRTC API on first use. A port that cannot be
// bound (another control plane on it, a test beside the running one) falls
// back to a port per call, which works on one machine and is logged.
func (r *rtc) engine(cfg config.Config, hostAddrs func() ([]string, error)) (*webrtc.API, error) {
	// The machine's addresses are asked before the engine is made once and
	// for all: one made while they could not be read would offer every later
	// call the container's own, which no browser can reach.
	var machine []string
	if hostAddrs != nil && len(cleanList(cfg.Voice.WebRTC.PublicIPs)) == 0 && !r.made.Load() {
		addrs, err := hostAddrs()
		if err != nil {
			return nil, fmt.Errorf("calls cannot start yet: %w", err)
		}
		machine = addrs
	}
	r.once.Do(func() {
		defer r.made.Store(true)
		me := &webrtc.MediaEngine{}
		if err := me.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"},
			PayloadType:        111,
		}, webrtc.RTPCodecTypeAudio); err != nil {
			r.err = err
			return
		}
		se := webrtc.SettingEngine{}
		w := cfg.Voice.WebRTC
		if w.UDPPort > 0 {
			if conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: w.UDPPort}); err != nil {
				log.Printf("voice: UDP port %d: %v; calls use a port each", w.UDPPort, err)
			} else {
				se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, conn))
				r.udp = w.UDPPort
			}
		}
		types := []webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6}
		if w.TCPPort > 0 {
			if ln, err := net.ListenTCP("tcp", &net.TCPAddr{Port: w.TCPPort}); err != nil {
				log.Printf("voice: TCP port %d: %v; calls have no TCP route", w.TCPPort, err)
			} else {
				se.SetICETCPMux(webrtc.NewICETCPMux(nil, ln, 8))
				types = append(types, webrtc.NetworkTypeTCP4, webrtc.NetworkTypeTCP6)
				r.tcp = w.TCPPort
			}
		}
		se.SetNetworkTypes(types)
		// A browser on the same machine (and a test) reaches the control
		// plane on loopback.
		se.SetIncludeLoopbackCandidate(true)
		if ips := cleanList(w.PublicIPs); len(ips) > 0 {
			se.SetNAT1To1IPs(ips, webrtc.ICECandidateTypeHost) //nolint:staticcheck // the rewrite rules say the same with more words
		} else if len(machine) > 0 {
			if via, rule, ok := behindEngine(ownAddrs(), machine); ok {
				se.SetIPFilter(func(ip net.IP) bool { return ip.IsLoopback() || ip.Equal(via) })
				if err := se.SetICEAddressRewriteRules(rule); err != nil {
					log.Printf("voice: offering the machine's addresses %v: %v", rule.External, err)
				} else {
					log.Printf("voice: on a container network; calls are offered the machine's addresses %v", rule.External)
				}
			}
		}
		r.api = webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(se))
	})
	return r.api, r.err
}

// behindEngine is for a control plane that is a container on an engine
// network with its call ports published (SiloOS): its own addresses mean
// nothing to a browser, so a call is offered the machine's (host, from
// siloosd) in their place. It answers the one own address to gather on (the
// first: a second network's would only repeat the same machine addresses)
// and the rule that publishes the machine's IPv4 addresses for it.
//
// Not ok, and nothing changes, when the machine's addresses are unknown or
// one of them is the control plane's own: it is on the host's network, and
// what it gathers is already right.
func behindEngine(own []net.IP, host []string) (net.IP, webrtc.ICEAddressRewriteRule, bool) {
	var ext []string
	for _, h := range host {
		ip := net.ParseIP(strings.TrimSpace(h))
		if ip == nil || ip.To4() == nil || ip.IsLoopback() {
			continue
		}
		for _, o := range own {
			if o.Equal(ip) {
				return nil, webrtc.ICEAddressRewriteRule{}, false
			}
		}
		ext = append(ext, ip.String())
	}
	var via net.IP
	for _, o := range own {
		if o.To4() != nil && !o.IsLoopback() {
			via = o
			break
		}
	}
	if len(ext) == 0 || via == nil {
		return nil, webrtc.ICEAddressRewriteRule{}, false
	}
	return via, webrtc.ICEAddressRewriteRule{
		External:        ext,
		Local:           via.String(),
		AsCandidateType: webrtc.ICECandidateTypeHost,
		Mode:            webrtc.ICEAddressRewriteReplace,
	}, true
}

// ownAddrs are the addresses of this process's own interfaces.
func ownAddrs() []net.IP {
	addrs, _ := net.InterfaceAddrs()
	var out []net.IP
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			out = append(out, n.IP)
		}
	}
	return out
}

// iceServers are the STUN/TURN servers offered to the browser.
func iceServers(cfg config.Config) []webrtc.ICEServer {
	var out []webrtc.ICEServer
	for _, u := range cleanList(cfg.Voice.WebRTC.ICEServers) {
		out = append(out, webrtc.ICEServer{URLs: []string{u}})
	}
	return out
}

// cleanList reads a list setting that may be one comma-separated value.
func cleanList(in []string) []string {
	var out []string
	for _, v := range in {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// newPeer makes a call's peer: one Opus track the Bot speaks on, answering
// the browser's offer.
func (s *Service) newPeer(cfg config.Config) (*webrtc.PeerConnection, *webrtc.TrackLocalStaticSample, error) {
	api, err := s.rtc.engine(cfg, s.HostAddrs)
	if err != nil {
		return nil, nil, fmt.Errorf("webrtc: %w", err)
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, err
	}
	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "voice", "silo")
	if err != nil {
		pc.Close()
		return nil, nil, err
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		pc.Close()
		return nil, nil, err
	}
	// RTCP has to be read for the sender to keep working.
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(buf); err != nil {
				return
			}
		}
	}()
	return pc, track, nil
}
