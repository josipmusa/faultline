package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

func TestRunGivesTheChildTheProxy(t *testing.T) {
	var out, errOut bytes.Buffer
	bypass, err := bypassList(nil)
	if err != nil {
		t.Fatalf("bypassList: %v", err)
	}

	code, err := run(context.Background(), &out, &errOut, nil, 0, 0, nil, bypass,
		[]string{"sh", "-c", "echo $HTTP_PROXY; echo $https_proxy; echo $NO_PROXY"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("child printed %q, want three lines", out.String())
	}

	proxy := strings.TrimPrefix(bannerValue(t, errOut.String(), "proxy: "), "http://")
	if proxy == "" {
		t.Fatalf("the banner did not say where the proxy is: %q", errOut.String())
	}
	for i, want := range []string{"http://" + proxy, "http://" + proxy} {
		if lines[i] != want {
			t.Errorf("child saw %q, want %q", lines[i], want)
		}
	}
	if !strings.Contains(lines[2], "localhost") {
		t.Errorf("NO_PROXY = %q, want localhost on it so the child skips the admin port", lines[2])
	}
}

func TestRunPrintsTheAdminURLBeforeTheChildRuns(t *testing.T) {
	var out, errOut bytes.Buffer
	code, err := run(context.Background(), &out, &errOut, nil, 0, 0, nil, nil, []string{"sh", "-c", "exit 0"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if url := bannerValue(t, errOut.String(), "admin: "); !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Errorf("admin line = %q, want an http url on loopback", url)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want it left to the child alone", out.String())
	}
}

func TestRunMirrorsTheChildsExitCode(t *testing.T) {
	var out, errOut bytes.Buffer
	code, err := run(context.Background(), &out, &errOut, nil, 0, 0, nil, nil, []string{"sh", "-c", "exit 7"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

func TestRunReportsACommandItCannotStart(t *testing.T) {
	var out, errOut bytes.Buffer
	if _, err := run(context.Background(), &out, &errOut, nil, 0, 0, nil, nil, []string{"faultline-no-such-command"}); err == nil {
		t.Fatal("run accepted a command that does not exist")
	}
}

func TestRunNeedsACommand(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"run"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err == nil {
		t.Fatal("run with no command was accepted")
	}
}

func TestNoProxyUsesTheNoProxySpelling(t *testing.T) {
	bypass, err := forward.NewBypass([]string{"localhost", "*.internal"})
	if err != nil {
		t.Fatalf("NewBypass: %v", err)
	}
	got := noProxy(bypass)
	want := []string{"localhost", ".internal"}
	if len(got) != len(want) {
		t.Fatalf("noProxy = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("noProxy[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// bannerValue pulls the value after a banner prefix out of what run printed.
func bannerValue(t *testing.T, banner, prefix string) string {
	t.Helper()
	for line := range strings.SplitSeq(banner, "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func TestRunGivesTheChildTheCA(t *testing.T) {
	ca, err := tlsmitm.Create(t.TempDir())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var out, errOut bytes.Buffer
	code, err := run(context.Background(), &out, &errOut, nil, 0, 0, ca, nil,
		[]string{"sh", "-c", "echo $SSL_CERT_FILE; echo $NODE_EXTRA_CA_CERTS"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line != ca.CertPath {
			t.Errorf("child saw %q, want the CA path %q", line, ca.CertPath)
		}
	}
}

func TestEnsureCACreatesOneOnFirstRunAndSaysSo(t *testing.T) {
	dir := t.TempDir()

	var notice bytes.Buffer
	ca, err := ensureCA(dir, &notice)
	if err != nil {
		t.Fatalf("ensureCA: %v", err)
	}
	if ca == nil {
		t.Fatal("ensureCA returned no CA")
	}
	if lines := strings.Count(strings.TrimSpace(notice.String()), "\n"); lines != 0 {
		t.Errorf("notice = %q, want a single line", notice.String())
	}
	if !strings.Contains(notice.String(), ca.CertPath) {
		t.Errorf("notice = %q, want it to name %q", notice.String(), ca.CertPath)
	}

	var second bytes.Buffer
	again, err := ensureCA(dir, &second)
	if err != nil {
		t.Fatalf("ensureCA on an existing CA: %v", err)
	}
	if again.CertPath != ca.CertPath {
		t.Errorf("second call loaded %q, want %q", again.CertPath, ca.CertPath)
	}
	if second.Len() != 0 {
		t.Errorf("second call printed %q, want nothing; the CA was already there", second.String())
	}
}
