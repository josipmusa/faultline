package client

import (
	"context"
	"net/url"
	"strconv"
)

// EventQuery narrows what Events returns, the same way the query string on
// GET /api/events does. The zero value asks for everything the ring holds.
type EventQuery struct {
	// Limit keeps the most recent N events. Zero means no limit.
	Limit int
	// Host keeps only events for one upstream.
	Host string
	// Faulted, when set, keeps only faulted or only unfaulted events.
	Faulted *bool
}

func (q EventQuery) values() url.Values {
	v := url.Values{}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Host != "" {
		v.Set("host", q.Host)
	}
	if q.Faulted != nil {
		v.Set("faulted", strconv.FormatBool(*q.Faulted))
	}
	return v
}

// Events reads what the ring buffer holds, oldest first.
func (c *Client) Events(ctx context.Context, q EventQuery) ([]Event, error) {
	path := "/api/events"
	if query := q.values().Encode(); query != "" {
		path += "?" + query
	}

	var out []Event
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Upstreams lists the hosts Faultline has seen, with the tier it could see
// them at and how many of their requests were faulted.
func (c *Client) Upstreams(ctx context.Context) ([]Upstream, error) {
	var out []Upstream
	if err := c.get(ctx, "/api/upstreams", &out); err != nil {
		return nil, err
	}
	return out, nil
}
