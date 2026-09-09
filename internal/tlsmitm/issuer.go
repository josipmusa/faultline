package tlsmitm

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"
)

// leafValidity is how long a minted leaf certificate is good for. Browsers
// reject leaves valid for more than 398 days, so this stays well under.
const leafValidity = 365 * 24 * time.Hour

// leafRenewBefore is how close to expiry a cached leaf is minted again.
const leafRenewBefore = 24 * time.Hour

// Issuer mints leaf certificates signed by the CA, one per host, on demand.
// Leaves are cached, so a busy host pays for one signature. It is safe for
// concurrent use.
type Issuer struct {
	ca *CA
	// key signs every leaf. One key for all of them: the leaves exist only to
	// carry a host name a client can match, and minting a key per host would
	// be the slow part of every first request.
	key *ecdsa.PrivateKey

	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

// NewIssuer prepares to mint leaves signed by ca.
func NewIssuer(ca *CA) (*Issuer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating leaf key: %w", err)
	}
	return &Issuer{ca: ca, key: key, cache: make(map[string]*tls.Certificate)}, nil
}

// Certificate returns the leaf for host, minting it on first use. host is a
// bare host name or IP address, without a port; case does not matter.
func (i *Issuer) Certificate(host string) (*tls.Certificate, error) {
	host = strings.ToLower(host)

	i.mu.Lock()
	defer i.mu.Unlock()

	if cert, ok := i.cache[host]; ok && time.Now().Before(cert.Leaf.NotAfter.Add(-leafRenewBefore)) {
		return cert, nil
	}
	cert, err := i.mint(host)
	if err != nil {
		return nil, err
	}
	i.cache[host] = cert
	return cert, nil
}

// ServerConfig is a TLS server configuration that presents the leaf for the
// host the client asked for by SNI, or for fallbackHost when the client sent
// none, as clients connecting to an IP address do. fallbackHost may carry a
// port, which is dropped.
func (i *Issuer) ServerConfig(fallbackHost string) *tls.Config {
	if h, _, err := net.SplitHostPort(fallbackHost); err == nil {
		fallbackHost = h
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// HTTP/1.1 only: the intercepted requests are served by a plain
		// http.Server over the connection, one request at a time.
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			host := hello.ServerName
			if host == "" {
				host = fallbackHost
			}
			return i.Certificate(host)
		},
	}
}

// mint signs a new leaf for host. The caller holds the lock.
func (i *Issuer) mint(host string) (*tls.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating serial number: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"Faultline"}},
		// A little in the past so a client whose clock is slightly behind
		// still accepts a leaf minted a moment ago.
		NotBefore:   now.Add(-time.Hour),
		NotAfter:    now.Add(leafValidity),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}

	der, err := x509.CreateCertificate(rand.Reader, template, i.ca.Cert, &i.key.PublicKey, i.ca.Key)
	if err != nil {
		return nil, fmt.Errorf("signing leaf for %s: %w", host, err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parsing leaf for %s: %w", host, err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{der, i.ca.Cert.Raw},
		PrivateKey:  i.key,
		Leaf:        leaf,
	}, nil
}
