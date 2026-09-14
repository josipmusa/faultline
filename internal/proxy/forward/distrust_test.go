package forward

import (
	"strings"
	"testing"
)

func TestDistrustHintNamesTheVariablesThatWereSet(t *testing.T) {
	got := distrustHintOn("linux", []string{"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS"})

	if !strings.Contains(got, "SSL_CERT_FILE, NODE_EXTRA_CA_CERTS") {
		t.Errorf("hint %q does not name the variables Faultline set", got)
	}
	if strings.Contains(got, "ca install") {
		t.Errorf("hint %q sends a Linux reader to the keychain", got)
	}
}

// Go on macOS reads the system trust store and ignores SSL_CERT_FILE, so the
// variable the hint names as already set is not the fix there.
func TestDistrustHintOnMacOSSaysToInstallTheCA(t *testing.T) {
	got := distrustHintOn("darwin", []string{"SSL_CERT_FILE"})

	if !strings.Contains(got, "faultline ca install") {
		t.Errorf("hint %q does not say how a Go client on macOS gets to trust the CA", got)
	}
}
