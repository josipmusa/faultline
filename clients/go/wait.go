package client

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// ErrWaitTimeout says nothing matching arrived before the wait ran out. It is
// an answer rather than a failure: usually it means the application never made
// the call the caller was watching for.
var ErrWaitTimeout = errors.New("no matching event arrived before the timeout")

// pollEvery is how often a wait re-reads the events. An agent waiting for a
// request to happen cannot tell a tenth of a second from none, and polling is
// what lets a wait work the same in process as over a socket, where the event
// stream cannot reach.
const pollEvery = 100 * time.Millisecond

// WaitForEvent returns the first event matching q that is recorded after the
// call begins, or ErrWaitTimeout if none arrives within timeout. Events already
// in the ring are ignored: the question is what happens next.
//
// q.Limit is not used. The wait is for one event, and trimming the list to the
// most recent few would hide an earlier match.
func (c *Client) WaitForEvent(ctx context.Context, q EventQuery, timeout time.Duration) (Event, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	after, err := c.latestID(ctx)
	if err != nil {
		return Event{}, err
	}

	q.Limit = 0
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for {
		// Read before the first tick, so an event recorded between the
		// watermark and here is found without waiting for the interval.
		list, err := c.Events(ctx, q)
		if err != nil {
			return Event{}, waitErr(ctx, err)
		}
		for _, e := range list {
			if idAfter(e.ID, after) {
				return e, nil
			}
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return Event{}, waitErr(ctx, ctx.Err())
		}
	}
}

// latestID is the id of the newest event in the ring, which is the watermark a
// wait counts from. The read is unfiltered: any event advances the counter, so
// the newest one is the highest id whether it matches the filter or not.
func (c *Client) latestID(ctx context.Context) (uint64, error) {
	list, err := c.Events(ctx, EventQuery{Limit: 1})
	if err != nil {
		return 0, err
	}
	if len(list) == 0 {
		return 0, nil
	}
	n, _ := strconv.ParseUint(list[0].ID, 10, 64)
	return n, nil
}

// idAfter reports whether id was minted after the watermark. Ids are decimal
// counters, so they are compared as numbers: "10" is after "9", which string
// order gets backwards. An id that is not a number cannot be placed, and counts
// as new rather than being skipped, so a wait never stalls on one.
func idAfter(id string, after uint64) bool {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return true
	}
	return n > after
}

// waitErr turns the end of a wait into the right error. A deadline this call
// set itself is a timeout, and anything else, including a cancellation from the
// caller, belongs to the caller.
func waitErr(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrWaitTimeout
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}
