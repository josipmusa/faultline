package main

import (
	"fmt"
	"io"
	"sync"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/proxy/forward"
)

// watchDistrust reports hosts whose client refused the interception
// certificate while the run is in progress. Those requests never reach the
// upstream and never get a status, so the child sees a bare TLS failure and
// nothing in its own output says why; this is the line that does.
//
// One line per host: the failure repeats with every request the child makes
// and the advice does not change. The returned stop drains what is left and
// waits for the writing to finish, so the caller can print after it.
func watchDistrust(rec *events.Recorder, out io.Writer, trustVars []string) func() {
	sub := rec.Subscribe()
	hint := forward.DistrustHint(trustVars)
	said := make(map[string]bool)

	var wg sync.WaitGroup
	wg.Go(func() {
		for e := range sub.C {
			if !forward.Distrusted(e) || said[e.Host] {
				continue
			}
			said[e.Host] = true
			_, _ = fmt.Fprintf(out, "trust: %s: %s\n", e.Host, hint)
		}
	})

	return func() {
		sub.Close()
		wg.Wait()
	}
}
