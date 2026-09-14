package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// loopbackAddr fails the test unless addr is on an address only this machine
// can reach.
func loopbackAddr(t *testing.T, what, addr string) {
	t.Helper()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("%s listening on %q: %v", what, addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Errorf("%s bound %q, want loopback: Faultline sees an application's credentials and can rewrite its traffic, so reaching it from off the machine has to be asked for", what, host)
	}
}

// Every listener is on localhost unless --bind says otherwise. This is the
// flag's default as cobra applies it, not the constant, because the constant
// being right is no comfort if the flag does not use it.
func TestServeBindsLocalhostByDefault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncWriter{}
	served := make(chan error, 1)
	go func() {
		served <- runCmdCtx(ctx, out, "serve", "--admin-port", "0", "--proxy-port", "0", "--intercept=false")
	}()

	loopbackAddr(t, "admin", waitForAddr(t, out, "admin: http://"))
	loopbackAddr(t, "proxy", waitForAddr(t, out, "proxy: http://"))

	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// `run` has no --bind at all: an application wrapped on a developer's machine
// is never a reason to expose the control surface to the network.
func TestRunBindsLocalhostAlways(t *testing.T) {
	var out, errOut syncWriter
	code, err := run(context.Background(), &out, &errOut, nil, nil, 0, 0, nil, nil, nil, session{},
		[]string{"sh", "-c", "exit 0"}, true)
	if err != nil || code != 0 {
		t.Fatalf("run = %d, %v", code, err)
	}

	for _, prefix := range []string{"admin: http://", "proxy: http://"} {
		line := lineWith(t, errOut.String(), prefix)
		loopbackAddr(t, strings.TrimSuffix(prefix, ": http://"), strings.TrimPrefix(line, prefix))
	}

	if strings.Contains(runCmd(t, "run", "--help"), "--bind") {
		t.Error("run help offers --bind: wrapping an application is not a reason to widen the listeners")
	}
}

// lineWith returns the banner line starting with prefix.
func lineWith(t *testing.T, out, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("no line starting %q in:\n%s", prefix, out)
	return ""
}

// The agent interface is served on the admin server's own mux, so it is
// reached at the admin address and nowhere else: whatever --bind says about
// the API says the same about /mcp, with no second rule to keep in step.
func TestMCPIsServedOnTheAdminListenerOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncWriter{}
	served := make(chan error, 1)
	go func() { served <- serve(ctx, out, nil, DefaultBind, 0, 0, nil, nil, nil, false, true) }()

	adminAddr := waitForAddr(t, out, "admin: http://")
	proxyAddr := waitForAddr(t, out, "proxy: http://")
	loopbackAddr(t, "admin", adminAddr)

	// Answering on the admin port is the claim; a bare GET is enough to show
	// the endpoint is mounted there, protocol aside.
	resp, err := http.Get("http://" + adminAddr + "/mcp") //nolint:noctx // a listener on this machine
	if err != nil {
		t.Fatalf("GET /mcp on the admin address: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf("/mcp on the admin address answered 404, want the agent interface")
	}

	// And nowhere else: the forward proxy is a proxy, not a second door to
	// the control surface. It answers a non-proxy request as a proxy would.
	proxied := &http.Client{Timeout: 5 * time.Second}
	direct, err := proxied.Get("http://" + proxyAddr + "/mcp") //nolint:noctx // a listener on this machine
	if err != nil {
		return // refusing outright is fine too
	}
	body, _ := io.ReadAll(direct.Body)
	_ = direct.Body.Close()
	if direct.StatusCode == http.StatusOK && strings.Contains(string(body), "jsonrpc") {
		t.Errorf("the forward proxy port served the agent interface: %d %s", direct.StatusCode, body)
	}

	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
}
