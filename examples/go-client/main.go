// Command go-client calls one URL over and over and logs what happened, so
// there is always some traffic to look at while working on Faultline. It uses
// nothing but the standard library and reads no Faultline configuration: the
// proxy and CA variables the environment carries are enough.
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
	flag.Parse()

	// Interrupts end the loop rather than the process, so the last call gets
	// to finish and the summary still prints.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := &http.Client{Timeout: *timeout}
	log.Printf("calling %s every %s", *url, *every)

	loop(ctx, client, *url, *every, *count)
}

// loop calls url until ctx is cancelled or count calls have been made. A
// failed call is logged and the loop carries on: a fault injected upstream is
// the point of the exercise, not a reason to stop.
func loop(ctx context.Context, client *http.Client, url string, every time.Duration, count int) {
	for n := 1; count == 0 || n <= count; n++ {
		if n > 1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(every):
			}
		}

		started := time.Now()
		status, size, err := call(ctx, client, url)
		elapsed := time.Since(started).Round(time.Millisecond)

		switch {
		case ctx.Err() != nil:
			// Interrupted mid-call; the failure is ours, not the upstream's.
			return
		case err != nil:
			log.Printf("call %d failed after %s: %v", n, elapsed, err)
		default:
			log.Printf("call %d: %d, %d bytes, %s", n, status, size, elapsed)
		}
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
