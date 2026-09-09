package events

import (
	"testing"
	"time"
)

// later is a moment far enough past every attempt below that all of their retry
// windows have closed.
var later = origin.Add(time.Hour)

func TestReportOfAnEmptySessionIsAllZero(t *testing.T) {
	if got := Reported(nil, later); got != (Report{}) {
		t.Errorf("Reported(nil) = %+v, want every count zero", got)
	}
}

func TestReportCountsTrafficFaultsAndRetries(t *testing.T) {
	got := Reported(replay(t,
		faultedAt("a", 0),
		faultedAt("b", time.Second),
		at("c", 2*time.Second),
	), later)

	want := Report{Total: 3, Faulted: 2, Retries: 2, MaxRetryWaitMS: 990}
	if got != want {
		t.Errorf("Reported() = %+v, want %+v", got, want)
	}
}

func TestReportMaxRetryWaitIsTheLongestBackoff(t *testing.T) {
	got := Reported(replay(t,
		faultedAt("a", 0),
		faultedAt("b", 100*time.Millisecond),
		at("c", 2*time.Second),
	), later)

	// b waited 90ms after a, c waited 1.89s after b.
	if got.MaxRetryWaitMS != 1890 {
		t.Errorf("MaxRetryWaitMS = %d, want 1890, the longest of the two waits", got.MaxRetryWaitMS)
	}
}

func TestReportCountsNoRetryWaitWhenNothingRetried(t *testing.T) {
	got := Reported(replay(t, faultedAt("a", 0)), later)

	if got.Retries != 0 || got.MaxRetryWaitMS != 0 {
		t.Errorf("Reported() = %+v, want no retries and no wait", got)
	}
}

func TestReportAbandonedCountsFaultsNobodyRetried(t *testing.T) {
	got := Reported(replay(t,
		faultedAt("a", 0),
		at("b", time.Second),          // retried a
		faultedAt("c", 2*time.Second), // nobody came back
	), later)

	if got.Abandoned != 1 {
		t.Errorf("Abandoned = %d, want 1; only c was given up on", got.Abandoned)
	}
}

func TestReportWaitsForTheWindowBeforeCallingAnAttemptAbandoned(t *testing.T) {
	all := replay(t, faultedAt("a", 0))

	if got := Reported(all, origin.Add(time.Second)); got.Abandoned != 0 {
		t.Errorf("Abandoned = %d one second in, want 0; the application may still retry", got.Abandoned)
	}
	if got := Reported(all, origin.Add(retryWindow+time.Second)); got.Abandoned != 1 {
		t.Errorf("Abandoned = %d once the window closed, want 1", got.Abandoned)
	}
}

func TestReportIgnoresASuccessfulAttemptForAbandoned(t *testing.T) {
	got := Reported(replay(t, at("a", 0)), later)

	if got.Abandoned != 0 {
		t.Errorf("Abandoned = %d, want 0; a call that succeeded was never given up on", got.Abandoned)
	}
}

func TestReportSurvivesARetryWhoseOriginalWasEvicted(t *testing.T) {
	r := NewRecorder(3)
	t.Cleanup(r.Close)
	r.Record(faultedAt("a", 0))
	r.Record(at("b", time.Second)) // retries a
	r.Record(at("c", 2*time.Second))
	r.Record(at("d", 3*time.Second)) // a has fallen out of the ring now

	got := r.Report()
	want := Report{Total: 3, Retries: 1}
	if got != want {
		t.Errorf("Report() = %+v, want %+v; b is still a retry, but the wait it measured is gone with a", got, want)
	}
}

func TestRecorderReportsWhatItHolds(t *testing.T) {
	r := NewRecorder(DefaultSize)
	t.Cleanup(r.Close)
	r.Record(faultedAt("a", 0))
	r.Record(at("b", time.Second))

	if got := r.Report(); got.Total != 2 || got.Faulted != 1 || got.Retries != 1 {
		t.Errorf("Report() = %+v, want 2 total, 1 faulted, 1 retry", got)
	}
}
