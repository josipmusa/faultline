package admin

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/josipmusa/faultline/internal/events"
)

// A stalled reader must not pin a goroutine and its subscriptions forever.
const streamWriteTimeout = 10 * time.Second

// How long to wait for a websocket close to finish before walking away from it.
const streamCloseTimeout = time.Second

// Message types on the event stream.
const (
	// MessageEvent carries one proxied request.
	MessageEvent = "event"
	// MessageRulesChanged says a client's cached rule list is stale and it
	// should re-read GET /api/rules. Rules never travel over the socket.
	MessageRulesChanged = "rules_changed"
)

// Message is one frame on the event stream. Type says which payload field is
// set, so a client switches on it rather than sniffing for fields.
type Message struct {
	Type  string        `json:"type"`
	Event *events.Event `json:"event,omitempty"`
}

// streamEvents serves GET /api/events/stream: every event as it is recorded,
// plus a rules_changed message after every rule store mutation. The socket is
// read-only and accepts no commands.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	if !s.enterStream() {
		s.fail(w, unavailable("faultline is shutting down"))
		return
	}
	defer s.leaveStream()

	// Subscribe before upgrading so nothing recorded during the handshake is
	// lost between the client believing it is connected and this loop starting.
	sub := s.events.Subscribe()
	defer sub.Close()

	changed, unsubscribe := s.watcher.subscribe()
	defer unsubscribe()

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept has already written the failure to w.
		s.log.Warn("stream: upgrade failed", "remote", r.RemoteAddr, "err", err)
		return
	}
	defer closeBounded(func() { _ = conn.CloseNow() })

	// CloseRead discards whatever the client sends, closes the connection with
	// a policy violation if that is a data message, and cancels ctx when the
	// client goes away.
	ctx := conn.CloseRead(r.Context())

	for {
		select {
		case <-ctx.Done():
			return

		case e, ok := <-sub.C:
			if !ok {
				return
			}
			if err := writeMessage(ctx, conn, Message{Type: MessageEvent, Event: &e}); err != nil {
				s.log.Debug("stream: write failed", "remote", r.RemoteAddr, "err", err)
				return
			}

		case _, ok := <-changed:
			if !ok { // the watcher closed, so the server is shutting down
				s.flushQueued(ctx, conn, sub, r.RemoteAddr)
				closeBounded(func() {
					_ = conn.Close(websocket.StatusGoingAway, "faultline is shutting down")
				})
				return
			}
			if err := writeMessage(ctx, conn, Message{Type: MessageRulesChanged}); err != nil {
				s.log.Debug("stream: write failed", "remote", r.RemoteAddr, "err", err)
				return
			}
		}
	}
}

// flushQueued writes the events already waiting in sub, and is what a stream
// does before it closes on shutdown.
//
// The proxies stop before the admin server, so anything queued here is the
// tail of the session: the last requests of a run, already recorded. Without
// this the close frame would race them, and select picks at random among
// ready cases, so those events would be dropped whenever the handler had not
// been scheduled since they arrived. That is rare on an idle laptop and
// common on a loaded CI runner, which is exactly the kind of difference that
// should not decide whether a developer sees their last request.
//
// Only what is already queued is written. Nothing new can arrive, since the
// proxies that record are gone by now, and waiting for more would hold up a
// shutdown that has promised to be prompt.
func (s *Server) flushQueued(ctx context.Context, conn *websocket.Conn, sub *events.Subscription, remote string) {
	for {
		select {
		case e, ok := <-sub.C:
			if !ok {
				return
			}
			if err := writeMessage(ctx, conn, Message{Type: MessageEvent, Event: &e}); err != nil {
				s.log.Debug("stream: write failed while flushing", "remote", remote, "err", err)
				return
			}
		default:
			return
		}
	}
}

// closeBounded runs a websocket close and stops waiting for it after
// streamCloseTimeout. Both close paths in the library end in waitGoroutines,
// which wedges for fifteen seconds once CloseRead has started a close of its
// own — and shutdown waits on this handler. The close frame is written before
// that wait begins, so the client still sees a clean close either way, and the
// abandoned goroutine dies with the process.
func closeBounded(closer func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		closer()
	}()

	select {
	case <-done:
	case <-time.After(streamCloseTimeout):
	}
}

// enterStream registers an open stream so shutdown can wait for it. It returns
// false once the server is draining, so a late client is turned away rather
// than handed a socket that is about to close under it.
func (s *Server) enterStream() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.draining {
		return false
	}
	s.streams.Add(1)
	return true
}

func (s *Server) leaveStream() { s.streams.Done() }

// waitFor blocks until wg is done or ctx expires, so one stuck stream cannot
// hold shutdown open forever.
func waitFor(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("admin: waiting for open event streams: %w", ctx.Err())
	}
}

func writeMessage(ctx context.Context, conn *websocket.Conn, m Message) error {
	ctx, cancel := context.WithTimeout(ctx, streamWriteTimeout)
	defer cancel()

	return wsjson.Write(ctx, conn, m)
}

// ruleWatcher fans the rule store's single change channel out to every open
// stream. Signals coalesce the same way the store's do: a subscriber learns
// that rules changed, never how many times, and re-reads the store itself.
type ruleWatcher struct {
	stop    chan struct{}
	stopped chan struct{}
	once    sync.Once

	mu     sync.Mutex
	subs   []chan struct{}
	closed bool
}

func newRuleWatcher(changes <-chan struct{}) *ruleWatcher {
	w := &ruleWatcher{stop: make(chan struct{}), stopped: make(chan struct{})}
	go w.run(changes)
	return w
}

func (w *ruleWatcher) run(changes <-chan struct{}) {
	defer close(w.stopped)

	for {
		select {
		case <-w.stop:
			return
		case <-changes:
			w.broadcast()
		}
	}
}

func (w *ruleWatcher) broadcast() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, ch := range w.subs {
		select {
		case ch <- struct{}{}:
		default: // a signal is already pending; the two coalesce
		}
	}
}

// subscribe returns a channel signalled after every rule store mutation and the
// function that releases it. The channel is closed when the watcher closes.
func (w *ruleWatcher) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		close(ch)
		return ch, func() {}
	}
	w.subs = append(w.subs, ch)
	return ch, func() { w.unsubscribe(ch) }
}

func (w *ruleWatcher) unsubscribe(ch chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if i := slices.Index(w.subs, ch); i >= 0 {
		w.subs = slices.Delete(w.subs, i, i+1)
		close(ch)
	}
}

// close stops the fan-out and closes every subscriber channel, which is how
// open streams learn the server is going away. It is safe to call twice.
func (w *ruleWatcher) close() {
	w.once.Do(func() {
		w.mu.Lock()
		w.closed = true
		subs := w.subs
		w.subs = nil
		w.mu.Unlock()

		for _, ch := range subs {
			close(ch)
		}

		close(w.stop)
		<-w.stopped
	})
}
