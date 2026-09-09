package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/admin"
	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/rules"
)

// instance is a running Faultline the CLI can talk to: the real admin server
// on a real socket, so a command test covers the wire as well as the printing.
type instance struct {
	url       string
	rules     *rules.Store
	scenarios *rules.Scenarios
	events    *events.Recorder
}

func newInstance(t *testing.T) instance {
	t.Helper()

	rec := events.NewRecorder(events.DefaultSize)
	t.Cleanup(rec.Close)

	store := rules.New()
	scenarios := rules.NewScenarios(store)

	bypass, err := forward.NewBypass(nil)
	if err != nil {
		t.Fatalf("bypass: %v", err)
	}

	api := admin.NewServer(store, scenarios, rec, bypass, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { _ = api.Shutdown(context.Background()) })

	ts := httptest.NewServer(api)
	t.Cleanup(ts.Close)

	return instance{url: ts.URL, rules: store, scenarios: scenarios, events: rec}
}

// run executes a command against this instance, failing the test if it errors.
func (i instance) run(t *testing.T, args ...string) string {
	t.Helper()
	return runCmd(t, append(args, "--admin", i.url)...)
}

// runErr executes a command against this instance and returns its error.
func (i instance) runErr(t *testing.T, args ...string) error {
	t.Helper()
	return runCmdErr(t, append(args, "--admin", i.url)...)
}

// record puts one event in the ring, the way a proxied request would.
func (i instance) record(t *testing.T, id, host string, faulted bool) {
	t.Helper()
	i.events.Record(events.Event{
		ID:         id,
		Timestamp:  time.Now(),
		Host:       host,
		Method:     "GET",
		Path:       "/get",
		Status:     200,
		DurationMS: 12,
		Faulted:    faulted,
		Tier:       events.TierPlain,
	})
}

// runCmdCtx is runCmd with a context and a writer of the caller's own, for a
// command that runs until it is stopped and is watched while it does.
func runCmdCtx(ctx context.Context, out io.Writer, args ...string) error {
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)

	return root.ExecuteContext(ctx)
}

// syncBuffer is a buffer a test can read while the command it belongs to is
// still writing.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func decodeJSON[T any](t *testing.T, out string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("decoding %q: %v", out, err)
	}
	return v
}

func wantLine(t *testing.T, out string, words ...string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if hasAll(line, words) {
			return
		}
	}
	t.Errorf("no line has all of %v:\n%s", words, out)
}

func hasAll(line string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(line, w) {
			return false
		}
	}
	return true
}

func TestACommandAgainstNothingSaysTheInstanceIsNotRunning(t *testing.T) {
	// A closed listener gives an address nothing is on, without guessing one.
	ts := httptest.NewServer(http.NotFoundHandler())
	addr := ts.URL
	ts.Close()

	err := runCmdErr(t, "rule", "list", "--admin", addr)
	if err == nil {
		t.Fatal("rule list against nothing = nil error, want one")
	}
	if !strings.Contains(err.Error(), "no Faultline is listening at "+addr) {
		t.Errorf("error = %q, want one line saying nothing is listening", err)
	}
	if strings.Contains(err.Error(), "dial tcp") {
		t.Errorf("error = %q, want no Go dial detail in it", err)
	}
}

func TestAnAdminAddressThatIsNotAnAddressNamesTheFlag(t *testing.T) {
	err := runCmdErr(t, "rule", "list", "--admin", "ftp://localhost:9000")
	if err == nil || !strings.Contains(err.Error(), "--admin") {
		t.Fatalf("error = %v, want it to name --admin", err)
	}
}
