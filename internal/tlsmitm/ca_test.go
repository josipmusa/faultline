package tlsmitm

import (
	"crypto/ecdsa"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateWritesSelfSignedCAValidForTenYears(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "faultline")

	ca, err := Create(dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if ca.CertPath != filepath.Join(dir, "ca.crt") {
		t.Errorf("CertPath = %q, want %q", ca.CertPath, filepath.Join(dir, "ca.crt"))
	}
	if ca.KeyPath != filepath.Join(dir, "ca.key") {
		t.Errorf("KeyPath = %q, want %q", ca.KeyPath, filepath.Join(dir, "ca.key"))
	}

	cert := ca.Cert
	if !cert.IsCA || !cert.BasicConstraintsValid {
		t.Error("certificate is not marked as a CA")
	}
	if cert.Subject.CommonName != CommonName {
		t.Errorf("CommonName = %q, want %q", cert.Subject.CommonName, CommonName)
	}
	if got := cert.NotAfter.Sub(cert.NotBefore); got < 3650*24*time.Hour || got > 3653*24*time.Hour {
		t.Errorf("validity = %v, want about ten years", got)
	}
	if err := cert.CheckSignatureFrom(cert); err != nil {
		t.Errorf("certificate is not self-signed: %v", err)
	}
	if _, ok := ca.Key.(*ecdsa.PrivateKey); !ok {
		t.Errorf("key is %T, want *ecdsa.PrivateKey", ca.Key)
	}

	info, err := os.Stat(ca.KeyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key mode = %o, want 600", perm)
	}
}

func TestLoadReadsBackWhatCreateWrote(t *testing.T) {
	dir := t.TempDir()

	created, err := Create(dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Cert.Equal(created.Cert) {
		t.Error("loaded certificate differs from the created one")
	}
	if !loaded.Key.(*ecdsa.PrivateKey).Equal(created.Key) {
		t.Error("loaded key differs from the created one")
	}
}

func TestLoadReportsMissingCA(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load error = %v, want ErrNotFound", err)
	}
}

func TestCreateRefusesWhenCAExists(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := Create(dir)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("second Create error = %v, want ErrExists", err)
	}
}

func TestCreateRefusesPartialState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ca.key"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Create(dir)
	if err == nil || errors.Is(err, ErrExists) {
		t.Fatalf("Create error = %v, want an error naming the partial state", err)
	}
	if !strings.Contains(err.Error(), "only one of") {
		t.Errorf("error %q does not explain the partial state", err)
	}
}

func TestLoadRejectsMalformedFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "ca.key"), []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "ca.key") {
		t.Errorf("Load with a broken key: error = %v, want one naming ca.key", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "ca.crt") {
		t.Errorf("Load with a broken certificate: error = %v, want one naming ca.crt", err)
	}
}
