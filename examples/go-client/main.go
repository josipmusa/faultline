// Command go-client calls one URL over and over and logs what happened, so
// there is always some traffic to look at while working on Faultline. It uses
// nothing but the standard library and reads no Faultline configuration: the
// proxy and CA variables the environment carries are enough.
//
// By default a failed call is logged and the loop moves on, which makes the
// gap between two calls the poll interval rather than a backoff. With -retries
// it repeats a failed call instead, waiting longer each time, so the report's
// retry numbers describe a client that genuinely backs off.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func main() {
	url := flag.String("url", "https://httpbin.org/get", "url to call")
	every := flag.Duration("every", 2*time.Second, "wait this long between calls")
	count := flag.Int("count", 0, "stop after this many calls, 0 to keep going until interrupted")
	timeout := flag.Duration("timeout", 30*time.Second, "give up on a single call after this long")
	retries := flag.Int("retries", 0, "repeat a failed call this many times, backing off between attempts")
	backoff := flag.Duration("backoff", 250*time.Millisecond, "wait this long after the first failure, doubling after each one")
	flag.Parse()

	// Interrupts end the loop rather than the process, so the last call gets
	// to finish and the summary still prints.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := &http.Client{Timeout: *timeout}
	log.Printf("calling %s every %s", *url, *every)

	loop(ctx, client, *url, *every, *count, retry{times: *retries, first: *backoff})
}

// retry is how hard the loop tries before it counts a call as failed. The zero
// value repeats nothing, which is the poll loop this example has always been.
type retry struct {
	times int
	first time.Duration
}

// loop calls url until ctx is cancelled or count calls have been made. A call
// that fails every attempt is logged and the loop carries on: a fault injected
// upstream is the point of the exercise, not a reason to stop.
func loop(ctx context.Context, client *http.Client, url string, every time.Duration, count int, r retry) {
	for n := 1; count == 0 || n <= count; n++ {
		if n > 1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(every):
			}
		}

		started := time.Now()
		status, size, attempts, err := r.do(ctx, client, url)
		elapsed := time.Since(started).Round(time.Millisecond)

		switch {
		case ctx.Err() != nil:
			// Interrupted mid-call; the failure is ours, not the upstream's.
			return
		case err != nil:
			log.Printf("call %d failed after %s and %d attempts: %v", n, elapsed, attempts, err)
		case attempts > 1:
			log.Printf("call %d: %d, %d bytes, %s, %d attempts", n, status, size, elapsed, attempts)
		default:
			log.Printf("call %d: %d, %d bytes, %s", n, status, size, elapsed)
		}
	}
}

// do makes the call, repeating it while it fails and there are attempts left,
// and waits longer before each repeat. It reports how many attempts it took,
// so a caller can say what a failure cost as well as that there was one.
//
// A 5xx is a failure here as much as a refused connection is: both are the
// upstream not answering, and both are worth trying again.
func (r retry) do(ctx context.Context, client *http.Client, url string) (int, int64, int, error) {
	// With no retries asked for, this is the single call the example has always
	// made, and a 5xx is a status to log like any other rather than a failure.
	if r.times == 0 {
		status, size, err := call(ctx, client, url)
		return status, size, 1, err
	}

	wait := r.first

	for attempt := 1; ; attempt++ {
		status, size, err := call(ctx, client, url)
		if err == nil && status < 500 {
			return status, size, attempt, nil
		}
		if err == nil {
			err = fmt.Errorf("upstream answered %d", status)
		}
		if attempt > r.times || ctx.Err() != nil {
			return status, size, attempt, err
		}

		log.Printf("  attempt %d failed (%v), retrying in %s", attempt, err, wait)
		select {
		case <-ctx.Done():
			return status, size, attempt, err
		case <-time.After(wait):
		}
		wait *= 2
	}
}

// call makes one request and reads the whole body, so the time it reports
// covers the response arriving and not just its headers.
func call(ctx context.Context, client *http.Client, url string) (int, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("build request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	size, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return resp.StatusCode, size, fmt.Errorf("read body: %w", err)
	}
	return resp.StatusCode, size, nil
}
