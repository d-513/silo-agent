package egress

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	utls "github.com/refraction-networking/utls"
)

// TLS record and handshake types we read off the wire.
const (
	recordHandshake    = 0x16
	handshakeHello     = 0x01
	extServerName      = 0x0000
	maxClientHelloSize = 1 << 16
)

// readClientHello reads TLS records from r until a whole ClientHello is
// buffered. It returns every byte it read (so the caller can replay them to the
// real handshake) and the SNI the hello carries.
func readClientHello(r io.Reader) (raw []byte, sni string, err error) {
	var hs []byte
	for {
		var hdr [5]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return nil, "", err
		}
		if hdr[0] != recordHandshake {
			return nil, "", fmt.Errorf("not a TLS handshake (record type %d)", hdr[0])
		}
		n := int(binary.BigEndian.Uint16(hdr[3:5]))
		if n <= 0 || n > maxClientHelloSize {
			return nil, "", errors.New("bad TLS record length")
		}
		rec := make([]byte, n)
		if _, err := io.ReadFull(r, rec); err != nil {
			return nil, "", err
		}
		raw = append(raw, hdr[:]...)
		raw = append(raw, rec...)
		hs = append(hs, rec...)
		if len(hs) > maxClientHelloSize {
			return nil, "", errors.New("the TLS hello is too large")
		}
		if len(hs) < 4 {
			continue
		}
		if hs[0] != handshakeHello {
			return nil, "", errors.New("not a ClientHello")
		}
		hlen := int(hs[1])<<16 | int(hs[2])<<8 | int(hs[3])
		if hlen > maxClientHelloSize {
			return nil, "", errors.New("the TLS hello is too large")
		}
		if len(hs) >= 4+hlen {
			return raw, sniFromClientHello(hs[4 : 4+hlen]), nil
		}
	}
}

// sniFromClientHello reads the server_name extension out of a ClientHello body.
func sniFromClientHello(b []byte) string {
	p := 2 + 32 // legacy_version + random
	if p >= len(b) {
		return ""
	}
	p += 1 + int(b[p]) // session id
	if p+2 > len(b) {
		return ""
	}
	p += 2 + int(binary.BigEndian.Uint16(b[p:])) // cipher suites
	if p >= len(b) {
		return ""
	}
	p += 1 + int(b[p]) // compression methods
	if p+2 > len(b) {
		return ""
	}
	extLen := int(binary.BigEndian.Uint16(b[p:]))
	p += 2
	end := min(p+extLen, len(b))
	for p+4 <= end {
		et := binary.BigEndian.Uint16(b[p:])
		el := int(binary.BigEndian.Uint16(b[p+2:]))
		p += 4
		if p+el > end {
			break
		}
		if et == extServerName {
			d := b[p : p+el]
			if len(d) >= 5 {
				nl := int(binary.BigEndian.Uint16(d[3:]))
				if 5+nl <= len(d) {
					return string(d[5 : 5+nl])
				}
			}
		}
		p += el
	}
	return ""
}

// mirrorSpec turns the client's ClientHello into a spec the gateway can send
// upstream, keeping the client's fingerprint except where the gateway speaks
// differently: ALPN offers only http/1.1 (all the gateway relays), in the
// extension's own place, and ALPS goes, since it carries HTTP/2 settings for a
// protocol no longer offered. A hello that offers ALPS without the ALPN it
// belongs to is no browser's, which is what bot walls look for. The SNI is
// filled from host by the caller.
func mirrorSpec(raw []byte, host string) *utls.ClientHelloSpec {
	var spec *utls.ClientHelloSpec
	if s, err := (&utls.Fingerprinter{}).RawClientHello(raw); err == nil {
		spec = s
	} else {
		// An unusual hello: a Chrome one is the closest thing to a browser.
		if s, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto); err == nil {
			spec = &s
		}
	}
	if spec == nil {
		return nil
	}
	out := *spec
	exts := make([]utls.TLSExtension, 0, len(spec.Extensions))
	for _, e := range spec.Extensions {
		switch e.(type) {
		case *utls.ALPNExtension:
			exts = append(exts, &utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}})
		case *utls.ApplicationSettingsExtension, *utls.ApplicationSettingsExtensionNew:
		default:
			exts = append(exts, e)
		}
	}
	out.Extensions = exts
	return &out
}

// dialTLS opens a TLS connection to host that mirrors the client's hello, and
// verifies the origin's certificate against roots (nil: the system's). The
// box only ever sees the gateway's certificate, so this is the one place the
// origin is checked at all; skipping it would hand the real value of every
// surrogate to whoever answers on the way.
func dialTLS(conn net.Conn, host string, spec *utls.ClientHelloSpec, roots *x509.CertPool) (*utls.UConn, error) {
	cfg := &utls.Config{ServerName: host, RootCAs: roots, NextProtos: []string{"http/1.1"}}
	uconn := utls.UClient(conn, cfg, utls.HelloCustom)
	if spec == nil {
		uconn = utls.UClient(conn, cfg, utls.HelloChrome_Auto)
	} else if err := uconn.ApplyPreset(spec); err != nil {
		uconn = utls.UClient(conn, cfg, utls.HelloChrome_Auto)
	} else {
		uconn.SetSNI(host)
	}
	if err := uconn.Handshake(); err != nil {
		return nil, err
	}
	// A mirrored hello can name an ALPN or a version the gateway does not
	// speak; HTTP/1.1 is all it relays.
	if p := uconn.ConnectionState().NegotiatedProtocol; p != "" && p != "http/1.1" {
		return nil, fmt.Errorf("the origin chose %s", p)
	}
	return uconn, nil
}

// serverTLSConfig serves the box a certificate minted for the SNI it asked
// for. It speaks HTTP/1.1 (no ALPN) so the gateway's HTTP handling fits.
func serverTLSConfig(ca *CA) *tls.Config {
	return &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			host := NormHost(hello.ServerName)
			if host == "" {
				return nil, errors.New("no SNI to mint a certificate for")
			}
			return ca.Cert(host, time.Now())
		},
	}
}
