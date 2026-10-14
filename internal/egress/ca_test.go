package egress

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
	"time"
)

func testCA(t *testing.T, now time.Time) *CA {
	t.Helper()
	cert, key, err := GenerateCA("Silo test", now)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := LoadCA(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

func TestGenerateCA(t *testing.T) {
	now := time.Now()
	certPEM, _, err := GenerateCA("Silo test", now)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseCACert(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsCA || !c.MaxPathLenZero || c.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Errorf("not a CA that may sign leaves only: %+v", c)
	}
	if c.Subject.CommonName != "Silo test" || c.NotAfter.Before(now.Add(CALifetime-time.Hour)) {
		t.Errorf("subject %q, until %v", c.Subject.CommonName, c.NotAfter)
	}
	if _, err := ParseCACert([]byte("nope")); err == nil {
		t.Error("garbage parsed as a certificate")
	}
}

func TestLoadCAKeyForms(t *testing.T) {
	certPEM, _, err := GenerateCA("x", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCA(certPEM, []byte("nope")); err == nil {
		t.Error("garbage key loaded")
	}
	// A PKCS#8 key loads too (what openssl writes today).
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	if _, err := LoadCA(certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})); err != nil {
		t.Errorf("PKCS#8 key: %v", err)
	}
}

func TestCAMintsTrustedLeaves(t *testing.T) {
	now := time.Now()
	ca := testCA(t, now)
	for _, host := range []string{"example.com", "API.Example.com.", "192.0.2.10"} {
		crt, err := ca.Cert(host, now)
		if err != nil {
			t.Fatal(err)
		}
		name := NormHost(host)
		_, err = crt.Leaf.Verify(x509.VerifyOptions{DNSName: name, Roots: ca.Pool(), CurrentTime: now})
		if err != nil {
			t.Errorf("%s: %v", host, err)
		}
	}
	if _, err := ca.Cert("", now); err == nil {
		t.Error("minted a certificate for no host")
	}
}

func TestCACachesAndRemints(t *testing.T) {
	now := time.Now()
	ca := testCA(t, now)
	a, _ := ca.Cert("example.com", now)
	b, _ := ca.Cert("example.com", now.Add(time.Hour))
	if a != b {
		t.Error("a fresh certificate was minted again")
	}
	c, _ := ca.Cert("example.com", now.Add(leafLifetime-time.Hour))
	if c == a {
		t.Error("a certificate a day from its end was not re-minted")
	}
}

func TestCALeafNeverOutlivesCA(t *testing.T) {
	now := time.Now()
	ca := testCA(t, now)
	crt, err := ca.Cert("example.com", now.Add(CALifetime-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if crt.Leaf.NotAfter.After(ca.cert.NotAfter) {
		t.Error("the leaf outlives its CA")
	}
}

// A real TLS handshake: a client trusting only the CA reaches a server that
// presents minted leaves.
func TestCAHandshake(t *testing.T) {
	ca := testCA(t, time.Now())
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{GetCertificate: func(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
		return ca.Cert(h.ServerName, time.Now())
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.(*tls.Conn).Handshake()
		c.Write([]byte("ok"))
	}()
	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c := tls.Client(raw, &tls.Config{ServerName: "silo.example", RootCAs: ca.Pool()})
	defer c.Close()
	buf := make([]byte, 2)
	if _, err := c.Read(buf); err != nil || string(buf) != "ok" {
		t.Fatalf("read %q: %v", buf, err)
	}
}
