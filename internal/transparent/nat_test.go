package transparent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// recorder stands in for the iptables binary and remembers what it was asked
// to do, so the rules themselves can be asserted without root or a kernel.
type recorder struct {
	calls [][]string
	// failAt makes the call at this index fail, to see what is left behind
	// when an install stops half way.
	failAt int
	err    error
}

func newRecorder() *recorder { return &recorder{failAt: -1} }

func (r *recorder) run(_ context.Context, args ...string) error {
	r.calls = append(r.calls, args)
	if r.failAt >= 0 && len(r.calls)-1 == r.failAt {
		return r.err
	}
	return nil
}

func (r *recorder) lines() []string {
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

func natFor(t *testing.T, rec *recorder) *NAT {
	t.Helper()
	n, err := New(Options{HTTPPort: 9002, HTTPSPort: 9003, ExemptUID: 65532})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	n.run = rec.run
	return n
}

func TestInstallWritesTheRedirectRules(t *testing.T) {
	rec := newRecorder()
	nat := natFor(t, rec)

	if err := nat.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}

	want := []string{
		// A chain left behind by a crash is taken down first, so a restart
		// cannot append a second copy of every rule.
		"-t nat -D OUTPUT -p tcp -j FAULTLINE",
		"-t nat -F FAULTLINE",
		"-t nat -X FAULTLINE",
		"-t nat -N FAULTLINE",
		"-t nat -A FAULTLINE -m owner --uid-owner 65532 -j RETURN",
		"-t nat -A FAULTLINE -d 127.0.0.0/8 -j RETURN",
		"-t nat -A FAULTLINE -p tcp --dport 80 -j REDIRECT --to-ports 9002",
		"-t nat -A FAULTLINE -p tcp --dport 443 -j REDIRECT --to-ports 9003",
		"-t nat -A OUTPUT -p tcp -j FAULTLINE",
	}
	got := rec.lines()
	if len(got) != len(want) {
		t.Fatalf("ran %d commands, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("command %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
}

// The exemption has to come before the redirects, or Faultline's own calls to
// the upstream are sent straight back into Faultline.
func TestTheUIDExemptionPrecedesTheRedirects(t *testing.T) {
	rec := newRecorder()
	nat := natFor(t, rec)
	if err := nat.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}

	lines := rec.lines()
	exempt, redirect := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "--uid-owner") {
			exempt = i
		}
		if strings.Contains(l, "REDIRECT") && redirect < 0 {
			redirect = i
		}
	}
	if exempt < 0 || redirect < 0 {
		t.Fatalf("missing rules:\n%s", strings.Join(lines, "\n"))
	}
	if exempt > redirect {
		t.Errorf("the uid exemption is at %d, after the first redirect at %d", exempt, redirect)
	}
}

func TestInstallUndoesItselfWhenARuleFails(t *testing.T) {
	rec := newRecorder()
	rec.failAt, rec.err = 6, errors.New("iptables: no chain/target/match by that name")
	nat := natFor(t, rec)

	err := nat.Install(context.Background())
	if err == nil {
		t.Fatal("Install succeeded with a failing rule")
	}
	if !strings.Contains(err.Error(), "no chain/target") {
		t.Errorf("error lost the reason: %v", err)
	}

	// The OUTPUT jump is the last thing installed, so a failure before it
	// leaves nothing catching traffic; what matters is that the chain is gone.
	tail := rec.lines()[7:]
	for _, want := range []string{"-t nat -F FAULTLINE", "-t nat -X FAULTLINE"} {
		if !containsLine(tail, want) {
			t.Errorf("teardown did not run %q; after the failure it ran:\n%s", want, strings.Join(tail, "\n"))
		}
	}
}

func TestRemoveIsQuietAboutRulesThatAreNotThere(t *testing.T) {
	rec := newRecorder()
	rec.failAt, rec.err = 0, errors.New("iptables: Bad rule")
	nat := natFor(t, rec)

	// Removing what was never installed is how shutdown runs after a failed
	// start, so a rule that is not there stops nothing.
	nat.Remove(context.Background())
	if len(rec.calls) != 3 {
		t.Errorf("Remove stopped at the first error: ran %d commands", len(rec.calls))
	}
}

func TestNewRejectsPortsItWouldRedirectToNowhere(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"no http port", Options{HTTPSPort: 9003}},
		{"no https port", Options{HTTPPort: 9002}},
		{"same port twice", Options{HTTPPort: 9002, HTTPSPort: 9002}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.opts); err == nil {
				t.Fatal("New accepted it")
			}
		})
	}
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}
