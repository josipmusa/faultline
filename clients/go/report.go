package client

import "context"

// ReportResult is the session report as the API answers it: the counts, plus a
// warning for every enabled rule that cannot do anything as things stand. An
// empty faulted count next to a warning means the fault never applied, not that
// the application coped with it.
type ReportResult struct {
	Report
	Warnings []string `json:"warnings,omitempty"`
}

// Report reads the current session's report: what Faultline has seen since it
// started, or since the events were last cleared.
func (c *Client) Report(ctx context.Context) (ReportResult, error) {
	var out ReportResult
	if err := c.get(ctx, "/api/sessions/current/report", &out); err != nil {
		return ReportResult{}, err
	}
	return out, nil
}
