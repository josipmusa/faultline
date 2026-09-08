package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/rules"
)

const streamTestTimeout = 3 * time.Second

// streamServer serves s on a real listener, because a websocket upgrade needs a
// hijackable connection, and returns the URL of its event stream.
func streamServer(t *testing.T, s *Server) (string, context.Context) {
	t.Helper()

	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), streamTestTimeout)
	t.Cleanup(cancel)

	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/events/stream", ctx
}

func dialStream(ctx context.Context, t *testing.T, url string) *websocket.Conn {
	t.Helper()

	//nolint:bodyclose // Dial nils out resp.Body on a successful handshake and documents that it must not be closed.
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dialing %s: %v", url, err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func readMessage(ctx context.Context, t *testing.T, c *websocket.Conn) Message {
	t.Helper()

	typ, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("reading from stream: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("message type = %v, want text", typ)
	}
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decoding %q: %v", data, err)
	}
	return m
}

// readEvent reads the next message and asserts it is an event envelope.
func readEvent(ctx context.Context, t *testing.T, c *websocket.Conn) events.Event {
	t.Helper()

	m := readMessage(ctx, t, c)
	if m.Type != MessageEvent {
		t.Fatalf("message type = %q, want %q", m.Type, MessageEvent)
	}
	if m.Event == nil {
		t.Fatal("event message carries no event")
	}
	return *m.Event
}

// readRulesChanged reads the next message and asserts it is a rules_changed
// notice carrying no payload.
func readRulesChanged(ctx context.Context, t *testing.T, c *websocket.Conn) {
	t.Helper()

	m := readMessage(ctx, t, c)
	if m.Type != MessageRulesChanged {
		t.Fatalf("message type = %q, want %q", m.Type, MessageRulesChanged)
	}
	if m.Event != nil {
		t.Errorf("rules_changed carries an event %+v, want none", *m.Event)
	}
}

func TestStreamSendsEachNewEvent(t *testing.T) {
	s := newTestServer(t)
	url, ctx := streamServer(t, s)
	c := dialStream(ctx, t, url)

	s.events.Record(events.Event{ID: "e1", Host: "api.stripe.com", Tier: events.TierPlain})
	s.events.Record(events.Event{ID: "e2", Host: "api.stripe.com", Faulted: true, Tier: events.TierPlain})

	for _, want := range []string{"e1", "e2"} {
		got := readEvent(ctx, t, c)
		if got.ID != want {
			t.Fatalf("event id = %q, want %q", got.ID, want)
		}
		if got.Host != "api.stripe.com" {
			t.Errorf("event host = %q, want api.stripe.com", got.Host)
		}
	}
}

func TestStreamSendsRulesChangedOnEveryMutation(t *testing.T) {
	s := newTestServer(t)
	url, ctx := streamServer(t, s)
	c := dialStream(ctx, t, url)

	if err := s.rules.Add(rules.Rule{ID: "slow", Match: rules.Match{Host: "api.stripe.com"}}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}
	readRulesChanged(ctx, t, c)

	if err := s.rules.Delete("slow"); err != nil {
		t.Fatalf("deleting rule: %v", err)
	}
	readRulesChanged(ctx, t, c)
}

// The store's change channel has a single reader, so the admin server is what
// fans a mutation out to every open stream.
func TestStreamFansOutToEveryClient(t *testing.T) {
	s := newTestServer(t)
	url, ctx := streamServer(t, s)

	conns := make([]*websocket.Conn, 2)
	for i := range conns {
		conns[i] = dialStream(ctx, t, url)
	}

	// One source at a time: the stream makes no ordering promise between an
	// event and a notice, only that every client gets both.
	if err := s.rules.Add(rules.Rule{ID: "slow", Match: rules.Match{Host: "api.stripe.com"}}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}
	for _, c := range conns {
		readRulesChanged(ctx, t, c)
	}

	s.events.Record(events.Event{ID: "e1", Host: "api.stripe.com", Tier: events.TierPlain})
	for i, c := range conns {
		if got := readEvent(ctx, t, c); got.ID != "e1" {
			t.Fatalf("client %d: event id = %q, want e1", i, got.ID)
		}
	}
}

func TestStreamRejectsClientCommands(t *testing.T) {
	s := newTestServer(t)
	url, ctx := streamServer(t, s)
	c := dialStream(ctx, t, url)

	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"add_rule"}`)); err != nil {
		t.Fatalf("writing to stream: %v", err)
	}

	_, _, err := c.Read(ctx)
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Fatalf("close status = %v (err %v), want %v", got, err, websocket.StatusPolicyViolation)
	}
}

func TestStreamClosesWhenServerShutsDown(t *testing.T) {
	s := newTestServer(t)
	url, ctx := streamServer(t, s)
	c := dialStream(ctx, t, url)

	// Read concurrently, the way a real client does, so the closing handshake
	// can complete rather than deadlock against a client that only starts
	// reading once shutdown has already returned.
	status := make(chan websocket.StatusCode, 1)
	go func() {
		_, _, err := c.Read(ctx)
		status <- websocket.CloseStatus(err)
	}()

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("shutting down: %v", err)
	}

	select {
	case got := <-status:
		if got != websocket.StatusGoingAway {
			t.Fatalf("close status = %v, want %v", got, websocket.StatusGoingAway)
		}
	case <-ctx.Done():
		t.Fatal("the client never saw a close frame")
	}
}

func TestRuleWatcherCoalescesSignalsToASlowSubscriber(t *testing.T) {
	w := newRuleWatcher(make(chan struct{}))
	t.Cleanup(w.close)

	sub, _ := w.subscribe()
	for range 3 {
		w.broadcast() // must not block on a subscriber that has not read yet
	}

	if _, ok := <-sub; !ok {
		t.Fatal("subscriber channel closed, want a signal")
	}
	select {
	case <-sub:
		t.Fatal("got a second signal, want the three broadcasts coalesced into one")
	default:
	}
}

func TestRuleWatcherStopsSignallingAfterUnsubscribe(t *testing.T) {
	changes := make(chan struct{}, 1)
	w := newRuleWatcher(changes)
	t.Cleanup(w.close)

	sub, unsubscribe := w.subscribe()
	unsubscribe()

	if _, ok := <-sub; ok {
		t.Fatal("subscriber channel is open, want it closed by unsubscribe")
	}

	changes <- struct{}{} // must not panic by sending on the closed channel
	time.Sleep(50 * time.Millisecond)
}

// The wire format is the contract the UI and the generated clients code
// against, so pin the bytes rather than only the decoded struct.
func TestStreamWireFormat(t *testing.T) {
	s := newTestServer(t)
	url, ctx := streamServer(t, s)
	c := dialStream(ctx, t, url)

	if err := s.rules.Add(rules.Rule{ID: "slow", Match: rules.Match{Host: "api.stripe.com"}}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}
	if _, data, err := c.Read(ctx); err != nil {
		t.Fatalf("reading notice: %v", err)
	} else if got := strings.TrimSpace(string(data)); got != `{"type":"rules_changed"}` {
		t.Errorf("notice = %s, want {\"type\":\"rules_changed\"}", got)
	}

	s.events.Record(events.Event{ID: "e1", Host: "api.stripe.com", Tier: events.TierPlain})
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("reading event: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decoding %q: %v", data, err)
	}
	if len(raw) != 2 || raw["type"] == nil || raw["event"] == nil {
		t.Fatalf("event message = %s, want exactly a type and an event key", data)
	}
	if got := string(raw["type"]); got != `"event"` {
		t.Errorf("type = %s, want \"event\"", got)
	}
	if !strings.Contains(string(raw["event"]), `"id":"e1"`) {
		t.Errorf("event payload = %s, want the recorded event", raw["event"])
	}
}

func TestShutdownWaitsForOpenStreams(t *testing.T) {
	s := newTestServer(t)
	if !s.enterStream() {
		t.Fatal("a fresh server refused a stream")
	}

	done := make(chan error, 1)
	go func() { done <- s.Shutdown(context.Background()) }()

	select {
	case err := <-done:
		t.Fatalf("Shutdown returned %v while a stream was still open", err)
	case <-time.After(100 * time.Millisecond):
	}

	s.leaveStream()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(streamTestTimeout):
		t.Fatal("Shutdown did not return after the last stream closed")
	}
}

// One stuck stream must not hold shutdown open past its deadline.
func TestShutdownGivesUpOnAStuckStream(t *testing.T) {
	s := newTestServer(t)
	if !s.enterStream() {
		t.Fatal("a fresh server refused a stream")
	}
	defer s.leaveStream()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown err = %v, want it to wrap %v", err, context.DeadlineExceeded)
	}
}

func TestStreamIsRefusedWhileShuttingDown(t *testing.T) {
	s := newTestServer(t)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutting down: %v", err)
	}

	w := do(t, s, http.MethodGet, "/api/events/stream", "")

	wantError(t, w, http.StatusServiceUnavailable, "")
	if got := decodeBody[apiError](t, w); !strings.Contains(got.Message, "shutting down") {
		t.Errorf("message = %q, want it to say the server is shutting down", got.Message)
	}
}

// A client that has poked the read-only socket leaves the library's own close
// path wedged for fifteen seconds. Shutdown must stay prompt regardless.
func TestShutdownIsPromptAfterAClientCommand(t *testing.T) {
	s := newTestServer(t)
	url, ctx := streamServer(t, s)
	c := dialStream(ctx, t, url)

	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"add_rule"}`)); err != nil {
		t.Fatalf("writing to stream: %v", err)
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("close status = %v, want %v", websocket.CloseStatus(err), websocket.StatusPolicyViolation)
	}

	start := time.Now()
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutting down: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 4*streamCloseTimeout {
		t.Fatalf("shutdown took %v after a client command, want it near %v", elapsed, streamCloseTimeout)
	}
}
