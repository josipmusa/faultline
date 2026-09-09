package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/admin"
)

func TestEventsExportWritesOneJSONObjectPerLine(t *testing.T) {
	i := newInstance(t)
	i.record(t, "1", "httpbin.org", false)
	i.record(t, "2", "httpbin.org", true)

	out := i.run(t, "events", "export")

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("export printed %d lines, want 2:\n%s", len(lines), out)
	}
	for _, line := range lines {
		var e client.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("line %q is not one event: %v", line, err)
		}
		if e.Host != "httpbin.org" {
			t.Errorf("host = %q, want httpbin.org", e.Host)
		}
	}
}

func TestEventsExportFiltersAndWritesToAFile(t *testing.T) {
	i := newInstance(t)
	i.record(t, "1", "httpbin.org", false)
	i.record(t, "2", "httpbin.org", true)
	i.record(t, "3", "example.com", true)

	path := filepath.Join(t.TempDir(), "events.ndjson")
	out := i.run(t, "events", "export", "--host", "httpbin.org", "--faulted", "--out", path)

	if !strings.Contains(out, path) {
		t.Errorf("export printed %q, want it to name the file it wrote", out)
	}

	written, err := os.ReadFile(path) //nolint:gosec // the path is the test's own temp dir
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(written)), "\n")
	if len(lines) != 1 {
		t.Fatalf("file holds %d lines, want only the faulted httpbin event:\n%s", len(lines), written)
	}
}

func TestEventsExportJSONIsTheAPIsOwnArray(t *testing.T) {
	i := newInstance(t)
	i.record(t, "1", "httpbin.org", false)

	list := decodeJSON[[]client.Event](t, i.run(t, "events", "export", "--json"))

	if len(list) != 1 || list[0].ID != "1" {
		t.Fatalf("export --json = %+v, want the one event", list)
	}
}

func TestEventsTailFollowsTheStreamAndStopsCleanly(t *testing.T) {
	i := newInstance(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- runCmdCtx(ctx, &out, "events", "tail", "--admin", i.url) }()

	// The stream subscribes during the handshake, so keep recording until one
	// arrives rather than racing the connection.
	waitFor(ctx, t, done, func() bool {
		i.record(t, "1", "httpbin.org", true)
		return strings.Contains(out.String(), "httpbin.org")
	})

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("events tail = %v, want a clean stop when it is interrupted", err)
	}
	wantLine(t, out.String(), "GET", "httpbin.org", "/get", "200")
}

func TestEventsTailJSONPrintsTheStreamEnvelope(t *testing.T) {
	i := newInstance(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- runCmdCtx(ctx, &out, "events", "tail", "--json", "--admin", i.url) }()

	waitFor(ctx, t, done, func() bool {
		i.record(t, "1", "httpbin.org", true)
		return strings.Contains(out.String(), "httpbin.org")
	})

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("events tail --json = %v, want a clean stop", err)
	}

	first := strings.SplitN(strings.TrimSpace(out.String()), "\n", 2)[0]
	m := decodeJSON[admin.Message](t, first)
	if m.Type != admin.MessageEvent || m.Event == nil || m.Event.Host != "httpbin.org" {
		t.Fatalf("first line = %q, want an event envelope for httpbin.org", first)
	}
}

// waitFor polls until ready reports true, failing the test if the command
// under test returns first or the context runs out.
func waitFor(ctx context.Context, t *testing.T, done <-chan error, ready func() bool) {
	t.Helper()

	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()

	for {
		if ready() {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("the command returned early: %v", err)
		case <-ctx.Done():
			t.Fatal("timed out waiting for the command")
		case <-tick.C:
		}
	}
}

func TestEventsTailAgainstNothingPrintsOnlyTheError(t *testing.T) {
	// A closed listener gives an address nothing is on, without guessing one.
	ts := httptest.NewServer(http.NotFoundHandler())
	addr := ts.URL
	ts.Close()

	var out syncBuffer
	err := runCmdCtx(context.Background(), &out, "events", "tail", "--admin", addr)

	if err == nil || !strings.Contains(err.Error(), "no Faultline is listening") {
		t.Fatalf("error = %v, want one saying nothing is listening", err)
	}
	if strings.Contains(out.String(), "METHOD") {
		t.Errorf("printed %q, want no table header for a stream that never opened", out.String())
	}
}
