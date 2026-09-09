package faults

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// waitUntil holds until due, or gives up early if the caller's context is done
// and returns its error. Every fault that spends time in a request path waits
// here, so none of them sleeps through a client that has already gone away.
func waitUntil(ctx context.Context, due time.Time) error {
	delay := time.Until(due)
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// measureBody reads a response body into memory and declares its real length,
// for the faults that cannot act without knowing how long the body is. It is
// only reached when the upstream declared no length of its own, which for a
// fault meant to break or slow a single response is a fair trade.
func measureBody(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		return fmt.Errorf("faultline: reading the response body: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("faultline: closing the response body: %w", closeErr)
	}

	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.TransferEncoding = nil
	return nil
}
