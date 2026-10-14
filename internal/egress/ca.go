package egress

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"
)

// CALifetime is how long a generated CA is valid. Rotation is a button.
const CALifetime = 5 * 365 * 24 * time.Hour

// leafLifetime is how long a minted site certificate lives; they are cached
// and re-minted well before they lapse.
const leafLifetime = 7 * 24 * time.Hour

// GenerateCA makes a new CA: the certificate and key, PEM.
func GenerateCA(name string, now time.Time) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name, Organization: []string{"Silo network filter"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(CALifetime),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}), nil
}

// ParseCACert reads a CA certificate, PEM.
func ParseCACert(certPEM []byte) (*x509.Certificate, error) {
	b, _ := pem.Decode(certPEM)
	if b == nil || b.Type != "CERTIFICATE" {
		return nil, errors.New("no certificate in the PEM")
	}
	return x509.ParseCertificate(b.Bytes)
}

// CA mints site certificates for the hosts the filter decrypts.
type CA struct {
	cert *x509.Certificate
	key  crypto.Signer
	// leafKey signs every leaf: one key is enough, and keeps minting cheap.
	leafKey *ecdsa.PrivateKey

	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

// LoadCA reads a CA from its certificate and key, PEM.
func LoadCA(certPEM, keyPEM []byte) (*CA, error) {
	cert, err := ParseCACert(certPEM)
	if err != nil {
		return nil, err
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil {
		return nil, errors.New("no key in the PEM")
	}
	var key crypto.Signer
	switch kb.Type {
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(kb.Bytes)
	default:
		var k any
		k, err = x509.ParsePKCS8PrivateKey(kb.Bytes)
		if err == nil {
			var ok bool
			if key, ok = k.(crypto.Signer); !ok {
				err = errors.New("the key cannot sign")
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("ca key: %w", err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &CA{cert: cert, key: key, leafKey: leafKey, cache: map[string]*tls.Certificate{}}, nil
}

// Cert returns a certificate for host, minting one when none is cached or the
// cached one is near its end.
func (c *CA) Cert(host string, now time.Time) (*tls.Certificate, error) {
	host = NormHost(host)
	if host == "" {
		return nil, errors.New("no host to mint a certificate for")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if crt := c.cache[host]; crt != nil && crt.Leaf != nil && now.Add(24*time.Hour).Before(crt.Leaf.NotAfter) {
		return crt, nil
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(leafLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	if tmpl.NotAfter.After(c.cert.NotAfter) {
		tmpl.NotAfter = c.cert.NotAfter
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &c.leafKey.PublicKey, c.key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	crt := &tls.Certificate{Certificate: [][]byte{der, c.cert.Raw}, PrivateKey: c.leafKey, Leaf: leaf}
	if len(c.cache) > 4096 {
		c.cache = map[string]*tls.Certificate{}
	}
	c.cache[host] = crt
	return crt, nil
}

// Pool is a pool holding only the CA, for tests and clients that trust it.
func (c *CA) Pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(c.cert)
	return p
}
