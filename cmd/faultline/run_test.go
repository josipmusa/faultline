package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/proxy/reverse"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

func TestRunGivesTheChildTheProxy(t *testing.T) {
	var out, errOut bytes.Buffer
	bypass, err := bypassList(nil)
	if err != nil {
		t.Fatalf("bypassList: %v", err)
	}

	code, err := run(context.Background(), &out, &errOut, nil, nil, 0, 0, nil, nil, bypass, session{},
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
	code, err := run(context.Background(), &out, &errOut, nil, nil, 0, 0, nil, nil, nil, session{}, []string{"sh", "-c", "exit 0"})
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
	code, err := run(context.Background(), &out, &errOut, nil, nil, 0, 0, nil, nil, nil, session{}, []string{"sh", "-c", "exit 7"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

func TestRunReportsACommandItCannotStart(t *testing.T) {
	var out, errOut bytes.Buffer
	if _, err := run(context.Background(), &out, &errOut, nil, nil, 0, 0, nil, nil, nil, session{}, []string{"faultline-no-such-command"}); err == nil {
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
	code, err := run(context.Background(), &out, &errOut, nil, nil, 0, 0, nil, ca, nil, session{},
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

func TestRunGivesTheChildAJavaTrustStore(t *testing.T) {
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("no JDK on this machine")
	}
	ca, err := tlsmitm.Create(t.TempDir())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var out, errOut bytes.Buffer
	code, err := run(context.Background(), &out, &errOut, nil, nil, 0, 0, nil, ca, nil, session{},
		[]string{"sh", "-c", "echo $JAVA_TOOL_OPTIONS"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	options := strings.TrimSpace(out.String())
	if !strings.Contains(options, "-Dhttp.proxyHost=") {
		t.Errorf("JAVA_TOOL_OPTIONS = %q, want the proxy properties", options)
	}
	store := filepath.Join(filepath.Dir(ca.CertPath), "java-truststore.p12")
	if !strings.Contains(options, `-Djavax.net.ssl.trustStore="`+store+`"`) {
		t.Errorf("JAVA_TOOL_OPTIONS = %q, want the trust store beside the CA at %q", options, store)
	}
	if _, err := os.Stat(store); err != nil {
		t.Errorf("the trust store was not built: %v", err)
	}
}

// A JDK that is there but cannot build a store is worth one line, because a
// JVM child will fail every HTTPS call and the reason is not obvious. It is
// not fatal: nothing else about the run depends on it.
func TestRunSaysSoWhenTheTrustStoreCannotBeBuilt(t *testing.T) {
	ca, err := tlsmitm.Create(t.TempDir())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Setenv("JAVA_HOME", brokenJDK(t))

	var out, errOut bytes.Buffer
	code, err := run(context.Background(), &out, &errOut, nil, nil, 0, 0, nil, ca, nil, session{},
		[]string{"sh", "-c", "echo $JAVA_TOOL_OPTIONS"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if !strings.Contains(errOut.String(), "java:") {
		t.Errorf("banner = %q, want a line about the trust store", errOut.String())
	}
	if strings.Contains(out.String(), "trustStore") {
		t.Errorf("JAVA_TOOL_OPTIONS = %q, want no trust store when none was built", out.String())
	}
	if !strings.Contains(out.String(), "-Dhttp.proxyHost=") {
		t.Errorf("JAVA_TOOL_OPTIONS = %q, want the proxy properties even so", out.String())
	}
}

// brokenJDK looks enough like a JDK to be chosen and fails when used.
func brokenJDK(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	for _, dir := range []string{"bin", filepath.Join("lib", "security")} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "lib", "security", "cacerts"), []byte("not a keystore"), 0o644); err != nil {
		t.Fatalf("write cacerts: %v", err)
	}
	keytool := filepath.Join(home, "bin", "keytool")
	if err := os.WriteFile(keytool, []byte("#!/bin/sh\necho broken >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write keytool: %v", err)
	}
	return home
}

// TestRunServesAnExplicitRoute is 3.7: a wrapped dev server points its own
// proxy at a route's port, so traffic a browser started still reaches
// Faultline even though the browser never saw the proxy variables.
func TestRunServesAnExplicitRoute(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "upstream")
	}))
	defer up.Close()

	// The child stands in for a dev server: it stays up until the test has
	// made its request through the route, then exits like a stopped server.
	done := filepath.Join(t.TempDir(), "done")
	child := []string{"sh", "-c", `while [ ! -f "$0" ]; do sleep 0.01; done`, done}

	var out bytes.Buffer
	errOut := &syncWriter{}
	routes := []reverse.Route{{Name: "api", Upstream: mustParse(t, up.URL)}}

	ran := make(chan error, 1)
	go func() {
		_, err := run(context.Background(), &out, errOut, nil, nil, 0, 0, routes, nil, nil, session{}, child)
		ran <- err
	}()
	defer func() {
		_ = os.WriteFile(done, nil, 0o600)
		if err := <-ran; err != nil {
			t.Errorf("run: %v", err)
		}
	}()

	adminAddr := waitForAddr(t, errOut, "admin: http://")
	routeAddr := waitForAddr(t, errOut, "route api: http://")

	resp, err := http.Get("http://" + routeAddr + "/orders")
	if err != nil {
		t.Fatalf("request through the route: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "upstream" {
		t.Fatalf("got %d %q, want 200 %q", resp.StatusCode, body, "upstream")
	}

	recorded, err := http.Get("http://" + adminAddr + "/api/events")
	if err != nil {
		t.Fatalf("reading events: %v", err)
	}
	seen, _ := io.ReadAll(recorded.Body)
	_ = recorded.Body.Close()
	if !strings.Contains(string(seen), `"path":"/orders"`) {
		t.Errorf("the route's request was not recorded:\n%s", seen)
	}
}

func TestRunRejectsARouteWithNoScheme(t *testing.T) {
	err := runCmdErr(t, "run", "--route", "api=api.stripe.com", "--", "true")
	if err == nil {
		t.Fatal("run accepted a route with no scheme")
	}
	if !strings.Contains(err.Error(), "scheme") {
		t.Errorf("err = %q, want it to name the missing scheme", err)
	}
}

func TestRunHelpDocumentsTheRouteFlag(t *testing.T) {
	got := runCmd(t, "run", "--help")
	for _, want := range []string{"--route", "name=url"} {
		if !strings.Contains(got, want) {
			t.Errorf("run help is missing %q:\n%s", want, got)
		}
	}
}

func TestOutcomeKeepsTheChildsExitCode(t *testing.T) {
	own := errors.New("the scenario would not turn off")

	var errOut bytes.Buffer
	code, err := outcome(&errOut, 7, nil, own)
	if code != 7 {
		t.Errorf("exit code = %d, want the child's 7", code)
	}
	if err != nil {
		t.Errorf("err = %v, want none; a problem of ours must not replace the child's code", err)
	}
	if !strings.Contains(errOut.String(), own.Error()) {
		t.Errorf("stderr = %q, want the problem said out loud", errOut.String())
	}

	if _, err := outcome(&errOut, 0, nil, own); !errors.Is(err, own) {
		t.Errorf("err = %v, want %v when the child itself succeeded", err, own)
	}

	runErr := errors.New("the command could not be started")
	if _, err := outcome(&errOut, 1, runErr, own); !errors.Is(err, runErr) {
		t.Errorf("err = %v, want the run's own failure %v", err, runErr)
	}
}
