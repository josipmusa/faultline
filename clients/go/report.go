package client

import "context"

// Report reads the current session's report: what Faultline has seen since it
// started, or since the events were last cleared.
func (c *Client) Report(ctx context.Context) (Report, error) {
	var out Report
	if err := c.get(ctx, "/api/sessions/current/report", &out); err != nil {
		return Report{}, err
	}
	return out, nil
}
