package config

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

const oneRule = `# The rules of this application.
rules:
  # Stripe takes its time.
  - id: slow-stripe
    name: Stripe is slow
    fault:
      type: delay
      ms: 100
`

// recorder is an Applier over a real rule store, counting how often the file
// was applied so a test can tell a reload from a quiet poll.
type recorder struct {
	store *rules.Store

	mu        sync.Mutex
	applied   int
	fail      error
	bypass    []string
	scenarios []rules.Scenario
}

func newRecorder() *recorder { return &recorder{store: rules.New()} }

func (r *recorder) Apply(cfg *Config) error {
	r.mu.Lock()
	r.applied++
	fail := r.fail
	r.mu.Unlock()

	if fail != nil {
		return fail
	}
	r.store.Replace(cfg.Rules)

	r.mu.Lock()
	r.bypass, r.scenarios = cfg.Bypass, cfg.Scenarios
	r.mu.Unlock()
	return nil
}

func (r *recorder) Rules() []rules.Rule { return r.store.List() }

func (r *recorder) Bypass() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bypass
}

func (r *recorder) Scenarios() []rules.Scenario {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scenarios
}

func (r *recorder) applies() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.applied
}

// logs is a slog handler's writer that a test can read while the watcher writes.
type logs struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// opened loads a config and builds the file to write back to, without starting
// the poll, for a test that wants to drive the timing itself.
func opened(t *testing.T, body string) (*File, *recorder, *logs) {
	t.Helper()

	path := writeConfig(t, body)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rec := newRecorder()
	rec.store.Replace(cfg.Rules) // the caller applies the first read itself

	out := &logs{}
	file := Watch(cfg, rec, slog.New(slog.NewTextHandler(out, nil)))
	file.every = time.Millisecond
	return file, rec, out
}

func watching(t *testing.T, body string) (*File, *recorder, *logs) {
	t.Helper()

	file, rec, out := opened(t, body)
	ctx, cancel := context.WithCancel(context.Background())
	file.Start(ctx)
	t.Cleanup(func() {
		cancel()
		file.Stop()
	})
	return file, rec, out
}

// rewrite replaces the file the way an editor does, so the watcher sees a new
// modification time.
func rewrite(t *testing.T, path, body string) {
	t.Helper()
	time.Sleep(2 * time.Millisecond) // a modification time has to differ to be noticed
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("rewriting the config: %v", err)
	}
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func delayOf(t *testing.T, store *rules.Store, id string) any {
	t.Helper()
	r, err := store.Get(id)
	if err != nil {
		return nil
	}
	return r.Fault.Params["ms"]
}

func TestWatchAppliesAnEditedFile(t *testing.T) {
	file, rec, _ := watching(t, oneRule)

	rewrite(t, file.Path(), strings.Replace(oneRule, "ms: 100", "ms: 2000", 1))

	waitFor(t, "the new delay", func() bool { return delayOf(t, rec.store, "slow-stripe") == 2000 })
}

func TestWatchLeavesAnUntouchedFileAlone(t *testing.T) {
	_, rec, _ := watching(t, oneRule)

	time.Sleep(50 * time.Millisecond)

	if n := rec.applies(); n != 0 {
		t.Errorf("a file nobody touched was applied %d times", n)
	}
}

func TestWatchKeepsTheOldRulesWhenTheFileBreaks(t *testing.T) {
	file, rec, out := watching(t, oneRule)

	rewrite(t, file.Path(), strings.Replace(oneRule, "    fault:", "    fault", 1))

	waitFor(t, "the error in the log", func() bool { return strings.Contains(out.String(), "line=") })

	if got := delayOf(t, rec.store, "slow-stripe"); got != 100 {
		t.Errorf("the rule in force is now %v, want the delay of 100 the broken save did not change", got)
	}
	if logged := out.String(); !strings.Contains(logged, "level=ERROR") || !strings.Contains(logged, "faultline.yaml") {
		t.Errorf("the log does not name the file at error level:\n%s", logged)
	}
	if n := rec.applies(); n != 0 {
		t.Errorf("a broken file was applied %d times", n)
	}
}

func TestWatchRecoversOnTheNextGoodSave(t *testing.T) {
	file, rec, _ := watching(t, oneRule)

	rewrite(t, file.Path(), "rules: [\n")
	rewrite(t, file.Path(), strings.Replace(oneRule, "ms: 100", "ms: 300", 1))

	waitFor(t, "the recovered rule", func() bool { return delayOf(t, rec.store, "slow-stripe") == 300 })
}

func TestWatchReportsTheSameBreakOnlyOnce(t *testing.T) {
	file, _, out := watching(t, oneRule)

	rewrite(t, file.Path(), "rules: [\n")
	waitFor(t, "the error in the log", func() bool { return strings.Contains(out.String(), "level=ERROR") })
	time.Sleep(30 * time.Millisecond)

	if n := strings.Count(out.String(), "level=ERROR"); n != 1 {
		t.Errorf("one broken save was reported %d times; a watcher that repeats itself buries the message", n)
	}
}

func TestWatchSaysWhenTheFileGoesAway(t *testing.T) {
	file, rec, out := watching(t, oneRule)

	if err := os.Remove(file.Path()); err != nil {
		t.Fatalf("removing the config: %v", err)
	}
	waitFor(t, "the warning in the log", func() bool { return strings.Contains(out.String(), "level=WARN") })
	time.Sleep(30 * time.Millisecond)

	if got := delayOf(t, rec.store, "slow-stripe"); got != 100 {
		t.Errorf("deleting the file threw the rules away, the store now holds %v", got)
	}
	if n := strings.Count(out.String(), "level=WARN"); n != 1 {
		t.Errorf("a missing file was reported %d times, want once", n)
	}
}

func TestChangeWritesTheStoreBackToTheFile(t *testing.T) {
	file, rec, _ := watching(t, oneRule)

	added := rules.Rule{
		ID:      "orders-503",
		Name:    "Orders answers 503",
		Enabled: true,
		Fault:   rules.Fault{Type: "status", Params: rules.Params{"code": 503}},
	}
	if err := file.Change(func() error { return rec.store.Add(added) }); err != nil {
		t.Fatalf("Change: %v", err)
	}

	body := read(t, file.Path())
	if !strings.Contains(body, "id: orders-503") {
		t.Errorf("the rule did not reach the file:\n%s", body)
	}
	if !strings.Contains(body, "# Stripe takes its time.") {
		t.Errorf("saving through the API dropped a comment:\n%s", body)
	}
}

func TestChangeIsNotSeenAsAnEdit(t *testing.T) {
	file, rec, _ := watching(t, oneRule)

	added := rules.Rule{ID: "r2", Name: "Second", Enabled: true, Fault: rules.Fault{Type: "delay", Params: rules.Params{"ms": 5}}}
	if err := file.Change(func() error { return rec.store.Add(added) }); err != nil {
		t.Fatalf("Change: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if n := rec.applies(); n != 0 {
		t.Errorf("Faultline's own write came back as %d reloads, which would reset behavior state", n)
	}
}

func TestChangeAppliesAHandEditBeforeItWrites(t *testing.T) {
	// The file gains a rule and the API adds another before the watcher has had
	// a chance to notice the first, so the poll is left out of this one.
	file, rec, _ := opened(t, oneRule)

	rewrite(t, file.Path(), oneRule+`  - id: by-hand
    name: Written by hand
    fault:
      type: status
      code: 500
`)

	added := rules.Rule{ID: "by-api", Name: "Added over the API", Enabled: true, Fault: rules.Fault{Type: "delay", Params: rules.Params{"ms": 7}}}
	if err := file.Change(func() error { return rec.store.Add(added) }); err != nil {
		t.Fatalf("Change: %v", err)
	}

	body := read(t, file.Path())
	for _, want := range []string{"id: by-hand", "id: by-api", "id: slow-stripe"} {
		if !strings.Contains(body, want) {
			t.Errorf("the saved file lost %q:\n%s", want, body)
		}
	}
	if _, err := rec.store.Get("by-hand"); err != nil {
		t.Errorf("the hand written rule never reached the store: %v", err)
	}
}

func TestChangeRefusesWhileTheFileIsBroken(t *testing.T) {
	file, rec, _ := opened(t, oneRule)
	rewrite(t, file.Path(), "rules: [\n")

	ran := false
	err := file.Change(func() error {
		ran = true
		return nil
	})

	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Change over a broken file = %v, want a *config.Error", err)
	}
	if ran {
		t.Error("the change was made even though it could not be saved")
	}
	if _, err := rec.store.Get("slow-stripe"); err != nil {
		t.Errorf("the rules in memory were thrown away: %v", err)
	}
}

func TestChangeReturnsTheMutationsOwnError(t *testing.T) {
	file, _, _ := watching(t, oneRule)

	want := errors.New("no such rule")
	if got := file.Change(func() error { return want }); !errors.Is(got, want) {
		t.Errorf("Change = %v, want the mutation's own error %v", got, want)
	}
}

func TestStopEndsTheWatcher(t *testing.T) {
	file, rec, _ := opened(t, oneRule)

	file.Start(context.Background())
	file.Stop()

	rewrite(t, file.Path(), strings.Replace(oneRule, "ms: 100", "ms: 900", 1))
	time.Sleep(30 * time.Millisecond)

	if n := rec.applies(); n != 0 {
		t.Errorf("the watcher applied %d files after Stop", n)
	}
}

func TestStartIsEndedByItsContext(t *testing.T) {
	file, rec, _ := opened(t, oneRule)

	ctx, cancel := context.WithCancel(context.Background())
	file.Start(ctx)
	cancel()
	file.Stop() // returns once the poll goroutine is gone

	rewrite(t, file.Path(), strings.Replace(oneRule, "ms: 100", "ms: 900", 1))
	time.Sleep(30 * time.Millisecond)

	if n := rec.applies(); n != 0 {
		t.Errorf("the watcher applied %d files after its context was cancelled", n)
	}
}

func TestChangeStillRefusesABreakTheWatchHasAlreadySeen(t *testing.T) {
	file, _, out := watching(t, oneRule)

	rewrite(t, file.Path(), "rules: [\n")
	waitFor(t, "the error in the log", func() bool { return strings.Contains(out.String(), "level=ERROR") })

	ran := false
	err := file.Change(func() error {
		ran = true
		return nil
	})

	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Change = %v, want the *config.Error the watch already found", err)
	}
	if ran {
		t.Error("the change was made even though the file it must be written to is broken")
	}
	if n := strings.Count(out.String(), "level=ERROR"); n != 1 {
		t.Errorf("the same break was reported %d times", n)
	}
}

// A save truncates the file before it writes it again, so there is a moment
// when it is zero bytes. Empty YAML is valid and means no rules, so reading it
// then would throw every rule away for a file nobody wrote.
func TestWatchIgnoresTheEmptyFileASaveLeavesBehind(t *testing.T) {
	file, rec, out := opened(t, oneRule)

	truncate(t, file.Path())

	file.reload() // notices the change
	file.reload() // and would apply it, the size having stopped moving

	if got := delayOf(t, rec.store, "slow-stripe"); got != 100 {
		t.Errorf("a half-finished save threw the rules away, the store now holds %v", got)
	}
	if n := rec.applies(); n != 0 {
		t.Errorf("an empty file was applied %d times", n)
	}
	if !strings.Contains(out.String(), "level=WARN") {
		t.Errorf("the empty file was passed over without a word:\n%s", out.String())
	}
}

// The file the truncated save goes on to write is applied as usual.
func TestWatchAppliesTheSaveThatFollowsTheEmptyFile(t *testing.T) {
	file, rec, _ := watching(t, oneRule)

	truncate(t, file.Path())
	rewrite(t, file.Path(), strings.Replace(oneRule, "ms: 100", "ms: 2000", 1))

	waitFor(t, "the new delay", func() bool { return delayOf(t, rec.store, "slow-stripe") == 2000 })
}

// truncate empties the file the way a save does on its way through.
func truncate(t *testing.T, path string) {
	t.Helper()
	time.Sleep(2 * time.Millisecond) // a modification time has to differ to be noticed
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("truncating the config: %v", err)
	}
}
