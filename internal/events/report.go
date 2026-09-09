package events

import "time"

// Report says what an application did while Faultline watched: how much
// traffic there was, how much of it Faultline broke, and how the application
// answered.
type Report struct {
	Total   int `json:"total"`
	Faulted int `json:"faulted"`
	Retries int `json:"retries"`

	// MaxRetryWaitMS is the longest any retry waited after the attempt it
	// repeats, which is the backoff the application actually used. It is zero
	// when nothing retried, and it skips a retry whose original has since been
	// dropped from the buffer, since that wait is no longer known.
	MaxRetryWaitMS int64 `json:"max_retry_wait_ms"`

	// Abandoned counts attempts worth retrying that nothing repeated before
	// their window closed. An attempt whose window is still open counts as
	// neither retried nor abandoned, so the number never accuses an
	// application that is about to come back.
	Abandoned int `json:"abandoned"`
}

// Reported summarises events, oldest first, as of now.
func Reported(all []Event, now time.Time) Report {
	r := Report{Total: len(all)}

	ends := make(map[string]time.Time, len(all))
	retried := make(map[string]bool)
	for _, e := range all {
		ends[e.ID] = e.Timestamp
		if e.RetryOf != "" {
			retried[e.RetryOf] = true
		}
	}

	for _, e := range all {
		if e.Faulted {
			r.Faulted++
		}
		if e.RetryOf != "" {
			r.Retries++
			if end, ok := ends[e.RetryOf]; ok {
				r.MaxRetryWaitMS = max(r.MaxRetryWaitMS, e.start().Sub(end).Milliseconds())
			}
		}
		if e.retryable() && !retried[e.ID] && now.Sub(e.Timestamp) > retryWindow {
			r.Abandoned++
		}
	}
	return r
}
