// Package tlsmitm holds the local certificate authority Faultline uses to
// terminate TLS for intercepted traffic, and the trust setup around it.
package tlsmitm

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// CommonName is the subject of the CA certificate, chosen so it is
// recognisable in a trust store listing.
const CommonName = "Faultline Local CA"

const (
	certFile = "ca.crt"
	keyFile  = "ca.key"
)

var (
	// ErrNotFound reports that no CA has been created in the directory.
	ErrNotFound = errors.New("no certificate authority found")
	// ErrExists reports that the directory already holds a CA.
	ErrExists = errors.New("certificate authority already exists")
)

// CA is a loaded certificate authority: the certificate, its private key, and
// where both live on disk.
type CA struct {
	CertPath string
	KeyPath  string
	Cert     *x509.Certificate
	Key      crypto.Signer
}

// DefaultDir is where per-user Faultline state lives: the OS config directory
// plus "faultline", so ~/.config/faultline on Linux.
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config dir: %w", err)
	}
	return filepath.Join(base, "faultline"), nil
}

// Create generates a new self-signed CA in dir, creating the directory if
// needed. The key is written owner-readable only. It returns ErrExists when
// a CA is already there, and a descriptive error when only one of the two
// files is present.
func Create(dir string) (*CA, error) {
	certPath := filepath.Join(dir, certFile)
	keyPath := filepath.Join(dir, keyFile)

	switch present, err := filesPresent(certPath, keyPath); {
	case err != nil:
		return nil, err
	case present == 2:
		return nil, fmt.Errorf("%w in %s", ErrExists, dir)
	case present == 1:
		return nil, fmt.Errorf("%s holds only one of %s and %s; remove it and create the CA again",
			dir, certFile, keyFile)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating CA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating serial number: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   CommonName,
			Organization: []string{"Faultline"},
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		// A path length of zero means this CA signs leaves and nothing else.
		MaxPathLenZero: true,
		KeyUsage:       x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("creating CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parsing CA certificate: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("encoding CA key: %w", err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", keyPath, err)
	}
	// The certificate is public by nature: other tools and runtimes read it
	// to trust the CA, so it is world-readable on purpose.
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil { //nolint:gosec // public certificate
		return nil, fmt.Errorf("writing %s: %w", certPath, err)
	}

	return &CA{CertPath: certPath, KeyPath: keyPath, Cert: cert, Key: key}, nil
}

// Load reads the CA from dir. It returns ErrNotFound when the certificate is
// missing.
func Load(dir string) (*CA, error) {
	certPath := filepath.Join(dir, certFile)
	keyPath := filepath.Join(dir, keyFile)

	certPEM, err := os.ReadFile(certPath) //nolint:gosec // path is inside the CA dir
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w in %s", ErrNotFound, dir)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", certPath, err)
	}
	cert, err := parsePEM(certPEM, "CERTIFICATE", certPath, x509.ParseCertificate)
	if err != nil {
		return nil, err
	}

	keyPEM, err := os.ReadFile(keyPath) //nolint:gosec // path is inside the CA dir
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", keyPath, err)
	}
	key, err := parsePEM(keyPEM, "EC PRIVATE KEY", keyPath, x509.ParseECPrivateKey)
	if err != nil {
		return nil, err
	}

	return &CA{CertPath: certPath, KeyPath: keyPath, Cert: cert, Key: key}, nil
}

// parsePEM decodes the first PEM block of the expected type and parses it.
func parsePEM[T any](data []byte, blockType, path string, parse func([]byte) (T, error)) (T, error) {
	var zero T
	block, _ := pem.Decode(data)
	if block == nil || block.Type != blockType {
		return zero, fmt.Errorf("%s does not contain a PEM %s block", path, blockType)
	}
	v, err := parse(block.Bytes)
	if err != nil {
		return zero, fmt.Errorf("parsing %s: %w", path, err)
	}
	return v, nil
}

// filesPresent counts how many of the paths exist.
func filesPresent(paths ...string) (int, error) {
	n := 0
	for _, p := range paths {
		_, err := os.Stat(p)
		switch {
		case err == nil:
			n++
		case errors.Is(err, os.ErrNotExist):
		default:
			return 0, fmt.Errorf("checking %s: %w", p, err)
		}
	}
	return n, nil
}
