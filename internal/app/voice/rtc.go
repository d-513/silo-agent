package voice

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/pion/webrtc/v4"

	"silo.agent/internal/config"
)

// rtc is the one WebRTC engine every call shares: it listens on the
// configured UDP and TCP ports once, and each call is a peer on them.
type rtc struct {
	once sync.Once
	api  *webrtc.API
	err  error
	// ports are what it ended up listening on, for the logs and the tests.
	udp, tcp int
}

// engine builds the shared WebRTC API on first use. A port that cannot be
// bound (another control plane on it, a test beside the running one) falls
// back to a port per call, which works on one machine and is logged.
func (r *rtc) engine(cfg config.Config) (*webrtc.API, error) {
	r.once.Do(func() {
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
		}
		r.api = webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(se))
	})
	return r.api, r.err
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
	api, err := s.rtc.engine(cfg)
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
