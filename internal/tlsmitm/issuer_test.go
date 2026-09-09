package tlsmitm

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"
)

func newIssuer(t *testing.T) (*Issuer, *CA) {
	t.Helper()
	ca, err := Create(t.TempDir())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	issuer, err := NewIssuer(ca)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	return issuer, ca
}

// verify checks that cert chains to the CA for host, the way a client trusting
// the CA would.
func verify(t *testing.T, ca *CA, cert *tls.Certificate, host string) {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)
	if _, err := cert.Leaf.Verify(x509.VerifyOptions{DNSName: host, Roots: roots}); err != nil {
		t.Fatalf("leaf for %s does not verify against the CA: %v", host, err)
	}
}

func TestIssuerMintsALeafSignedByTheCA(t *testing.T) {
	issuer, ca := newIssuer(t)

	cert, err := issuer.Certificate("api.stripe.com")
	if err != nil {
		t.Fatalf("Certificate: %v", err)
	}
	verify(t, ca, cert, "api.stripe.com")

	leaf := cert.Leaf
	if leaf.IsCA {
		t.Error("leaf is marked as a CA")
	}
	if !leaf.NotBefore.Before(time.Now()) {
		t.Errorf("NotBefore %v is in the future", leaf.NotBefore)
	}
	if limit := time.Now().AddDate(0, 0, 398); leaf.NotAfter.After(limit) {
		t.Errorf("NotAfter %v is past the 398 days browsers accept for a leaf", leaf.NotAfter)
	}
}

func TestIssuerCachesPerHost(t *testing.T) {
	issuer, _ := newIssuer(t)

	first, err := issuer.Certificate("api.stripe.com")
	if err != nil {
		t.Fatalf("Certificate: %v", err)
	}
	again, err := issuer.Certificate("API.Stripe.com")
	if err != nil {
		t.Fatalf("Certificate: %v", err)
	}
	if !first.Leaf.Equal(again.Leaf) {
		t.Error("the same host, differently cased, was minted twice")
	}

	other, err := issuer.Certificate("api.github.com")
	if err != nil {
		t.Fatalf("Certificate: %v", err)
	}
	if other.Leaf.Equal(first.Leaf) {
		t.Error("a different host got the same certificate")
	}
}

func TestIssuerMintsIPLeaves(t *testing.T) {
	issuer, ca := newIssuer(t)

	cert, err := issuer.Certificate("127.0.0.1")
	if err != nil {
		t.Fatalf("Certificate: %v", err)
	}
	if len(cert.Leaf.IPAddresses) != 1 || !cert.Leaf.IPAddresses[0].Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("IP SANs = %v, want 127.0.0.1", cert.Leaf.IPAddresses)
	}
	if len(cert.Leaf.DNSNames) != 0 {
		t.Errorf("DNS SANs = %v, want none for an IP", cert.Leaf.DNSNames)
	}
	verify(t, ca, cert, "127.0.0.1")
}

// TestServerConfigUsesSNIThenTheFallback drives a real handshake both ways: a
// client that names the host gets that host's leaf, one that does not (an IP
// URL, say) gets the leaf for the host the CONNECT named.
func TestServerConfigUsesSNIThenTheFallback(t *testing.T) {
	issuer, ca := newIssuer(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)

	tests := []struct {
		name        string
		sni         string
		fallback    string
		wantSubject string
	}{
		{"sni wins", "api.stripe.com", "ignored.example", "api.stripe.com"},
		{"fallback without sni", "", "127.0.0.1", "127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientEnd, serverEnd := net.Pipe()
			defer func() { _ = clientEnd.Close() }()

			// The raw pipe ends are closed rather than the TLS conns: a TLS
			// close waits for the peer to read its alert, and a pipe write
			// blocks until it is read, so two of them would sit out their
			// deadlines.
			server := tls.Server(serverEnd, issuer.ServerConfig(tt.fallback))
			go func() {
				_ = server.Handshake()
				_ = serverEnd.Close()
			}()

			client := tls.Client(clientEnd, &tls.Config{
				ServerName: tt.sni,
				RootCAs:    roots,
				MinVersion: tls.VersionTLS12,
				// With no SNI, verify by hand against the fallback host.
				InsecureSkipVerify: tt.sni == "",
				VerifyConnection: func(cs tls.ConnectionState) error {
					_, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{DNSName: tt.wantSubject, Roots: roots})
					return err
				},
			})
			if err := client.Handshake(); err != nil {
				t.Fatalf("handshake: %v", err)
			}
		})
	}
}
