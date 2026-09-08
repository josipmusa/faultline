package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/josipmusa/faultline/internal/proxy/reverse"
)

// syncWriter lets the test read serve's output while serve is still writing it.
type syncWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// addrs pulls the admin and route addresses out of what serve printed, once
// both are there. serve installs its signal handler before printing, so seeing
// them also means an interrupt will be caught rather than kill the test binary.
func addrs(t *testing.T, out *syncWriter, route string) (adminAddr, routeAddr string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		adminAddr, routeAddr = "", ""
		for line := range strings.SplitSeq(out.String(), "\n") {
			if rest, ok := strings.CutPrefix(line, "admin: http://"); ok {
				adminAddr = rest
			}
			if rest, ok := strings.CutPrefix(line, "route "+route+": http://"); ok {
				routeAddr, _, _ = strings.Cut(rest, " ")
			}
		}
		if adminAddr != "" && routeAddr != "" {
			return adminAddr, routeAddr
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("serve never printed both addresses:\n%s", out.String())
	return "", ""
}

// TestServeShutsDownGracefullyOnSignal is the whole of 1.8 end to end: a real
// interrupt, a request already in flight, and a live event stream.
func TestServeShutsDownGracefullyOnSignal(t *testing.T) {
	// serve installs its own handler, but hold one here for the length of the
	// test too, so an interrupt can never fall through to the default action
	// and take the test binary down with it.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, os.Interrupt)
	defer signal.Stop(guard)

	arrived, release := make(chan struct{}), make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(arrived)
		<-release
		_, _ = io.WriteString(w, "finished")
	}))
	defer up.Close()

	out := &syncWriter{}
	served := make(chan error, 1)
	go func() {
		// Port 0 everywhere: the test must not fight a real faultline for 9000.
		served <- serve(context.Background(), out, 0, []reverse.Route{{Name: "slow", Upstream: mustParse(t, up.URL)}})
	}()
	adminAddr, routeAddr := addrs(t, out, "slow")

	type reply struct {
		status int
		body   string
		err    error
	}
	inFlight := make(chan reply, 1)
	go func() {
		resp, err := http.Get("http://" + routeAddr + "/")
		if err != nil {
			inFlight <- reply{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		inFlight <- reply{status: resp.StatusCode, body: string(body), err: err}
	}()
	<-arrived

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	//nolint:bodyclose // Dial nils out resp.Body on a successful handshake and documents that it must not be closed.
	stream, _, err := websocket.Dial(ctx, "ws://"+adminAddr+"/api/events/stream", nil)
	if err != nil {
		t.Fatalf("dialing the event stream: %v", err)
	}
	defer func() { _ = stream.CloseNow() }()

	// Drain until the socket closes, counting what arrived on the way: routes
	// shut down before the admin server, so the last event must still land.
	type outcome struct {
		messages int
		status   websocket.StatusCode
	}
	closed := make(chan outcome, 1)
	go func() {
		var got outcome
		for {
			if _, _, err := stream.Read(ctx); err != nil {
				got.status = websocket.CloseStatus(err)
				closed <- got
				return
			}
			got.messages++
		}
	}()

	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("finding this process: %v", err)
	}
	if err := self.Signal(os.Interrupt); err != nil {
		t.Fatalf("signalling: %v", err)
	}
	close(release)

	if got := <-inFlight; got.err != nil {
		t.Errorf("the in-flight request failed: %v", got.err)
	} else if got.status != http.StatusOK || got.body != "finished" {
		t.Errorf("in-flight reply = %d %q, want 200 %q", got.status, got.body, "finished")
	}

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(shutdownTimeout):
		t.Fatalf("serve did not return within %v of the interrupt", shutdownTimeout)
	}

	// The stream gets a close frame, not a severed connection. Before 1.8 the
	// process could exit first and the client saw a bare EOF.
	select {
	case got := <-closed:
		if got.status != websocket.StatusGoingAway {
			t.Errorf("stream close status = %v, want %v", got.status, websocket.StatusGoingAway)
		}
		if got.messages == 0 {
			t.Error("the stream closed before the in-flight request's event reached it")
		}
	case <-time.After(time.Second):
		t.Error("the event stream never saw a close frame")
	}

	if got := out.String(); !strings.Contains(got, "shutdown complete") {
		t.Errorf("serve output has no clean exit message:\n%s", got)
	}
}
