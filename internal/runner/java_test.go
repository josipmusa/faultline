package runner

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCA writes a self-signed certificate shaped like the one tlsmitm
// creates, so the trust store tests have something real to import.
func writeCA(t *testing.T, dir string) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Faultline Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	path := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	return path
}

// requireJDK skips a test on a machine without one: the trust store is built
// with the JDK's own keytool, so there is nothing to test without it.
func requireJDK(t *testing.T) string {
	t.Helper()

	home, err := javaHome()
	if err != nil {
		t.Skipf("no JDK on this machine: %v", err)
	}
	return home
}

func TestJavaTrustStoreKeepsTheJDKRootsAndAddsTheCA(t *testing.T) {
	requireJDK(t)
	dir := t.TempDir()

	store, err := JavaTrustStore(dir, writeCA(t, dir))
	if err != nil {
		t.Fatalf("JavaTrustStore: %v", err)
	}

	listed := keytoolList(t, store)
	if !strings.Contains(listed, trustStoreAlias) {
		t.Errorf("the trust store has no %q entry:\n%s", trustStoreAlias, listed)
	}
	// The JDK ships dozens of roots; the point of copying cacerts is that the
	// child still trusts everything it trusted before.
	if entries := strings.Count(listed, "trustedCertEntry"); entries < 2 {
		t.Errorf("the trust store has %d entries, want the JDK roots as well as ours", entries)
	}
}

func TestJavaTrustStoreIsBuiltOnceAndRebuiltWhenTheCAChanges(t *testing.T) {
	requireJDK(t)
	dir := t.TempDir()
	caPath := writeCA(t, dir)

	store, err := JavaTrustStore(dir, caPath)
	if err != nil {
		t.Fatalf("JavaTrustStore: %v", err)
	}
	built := modTime(t, store)

	if _, err := JavaTrustStore(dir, caPath); err != nil {
		t.Fatalf("JavaTrustStore again: %v", err)
	}
	if again := modTime(t, store); !again.Equal(built) {
		t.Errorf("the trust store was rebuilt for an unchanged CA (%s then %s)", built, again)
	}

	// A CA created after the store means the store trusts the wrong one.
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(caPath, later, later); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if _, err := JavaTrustStore(dir, caPath); err != nil {
		t.Fatalf("JavaTrustStore after the CA changed: %v", err)
	}
	if again := modTime(t, store); again.Equal(built) {
		t.Error("the trust store was not rebuilt after the CA changed")
	}
}

// Most children are not JVMs, so a machine without a JDK is not an error the
// caller should report; it is simply nothing to do.
func TestJavaTrustStoreWithoutAJDK(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JAVA_HOME", filepath.Join(dir, "nowhere"))
	t.Setenv("PATH", filepath.Join(dir, "empty"))

	store, err := JavaTrustStore(dir, writeCA(t, dir))
	if !errors.Is(err, ErrNoJDK) {
		t.Errorf("error = %v, want ErrNoJDK", err)
	}
	if store != "" {
		t.Errorf("store = %q, want no path", store)
	}
}

// Without a CA there is no interception, so the JDK's own trust store is
// still the right one and Faultline builds nothing.
func TestJavaTrustStoreWithoutACA(t *testing.T) {
	store, err := JavaTrustStore(t.TempDir(), "")
	if err != nil {
		t.Fatalf("JavaTrustStore: %v", err)
	}
	if store != "" {
		t.Errorf("store = %q, want no path", store)
	}
}

func TestJavaHomePrefersJavaHomeWhenItHasAJDK(t *testing.T) {
	home := requireJDK(t)
	t.Setenv("JAVA_HOME", home)

	got, err := javaHome()
	if err != nil {
		t.Fatalf("javaHome: %v", err)
	}
	if got != home {
		t.Errorf("javaHome = %q, want %q", got, home)
	}
}

// A JAVA_HOME left over from an uninstalled JDK should not stop the one on
// PATH from being found.
func TestJavaHomeFallsBackToThePathWhenJavaHomeIsStale(t *testing.T) {
	requireJDK(t)
	t.Setenv("JAVA_HOME", filepath.Join(t.TempDir(), "gone"))

	home, err := javaHome()
	if err != nil {
		t.Fatalf("javaHome: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "lib", "security", "cacerts")); err != nil {
		t.Errorf("javaHome = %q, which has no cacerts: %v", home, err)
	}
}

func TestParseJavaHomeReadsTheSettingsOutput(t *testing.T) {
	output := "VM settings:\n    Stack Size (in Kbytes): 2048\n    java.home = /opt/jdk-21\n    java.io.tmpdir = /tmp\n"

	if got := parseJavaHome(output); got != "/opt/jdk-21" {
		t.Errorf("parseJavaHome = %q, want %q", got, "/opt/jdk-21")
	}
}

func modTime(t *testing.T, path string) time.Time {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime()
}

func keytoolList(t *testing.T, store string) string {
	t.Helper()

	home := requireJDK(t)
	out, err := exec.Command(filepath.Join(home, "bin", executable("keytool")),
		"-list", "-keystore", store, "-storepass", trustStorePassword).CombinedOutput()
	if err != nil {
		t.Fatalf("keytool -list: %v\n%s", err, out)
	}
	return string(out)
}
