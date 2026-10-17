package scheduler

import (
	"errors"
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
)

func TestBackoff(t *testing.T) {
	want := map[int]time.Duration{0: 30 * time.Second, 1: 30 * time.Second, 2: time.Minute, 3: 2 * time.Minute, 5: 8 * time.Minute, 8: time.Hour, 50: time.Hour}
	for attempt, d := range want {
		if got := Backoff(attempt); got != d {
			t.Errorf("Backoff(%d) = %s, want %s", attempt, got, d)
		}
	}
}

func TestNextRun(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if got := nextRun(t0, time.Minute, t0.Add(10*time.Second)); !got.Equal(t0.Add(time.Minute)) {
		t.Errorf("on schedule: got %s", got)
	}
	// Missed runs are skipped, not caught up.
	now := t0.Add(time.Hour)
	if got := nextRun(t0, time.Minute, now); !got.Equal(now.Add(time.Minute)) {
		t.Errorf("after downtime: got %s", got)
	}
}

func TestDecide(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	s := &Scheduler{Now: func() time.Time { return now }}
	every := 60
	oneOff := func(attempts int) models.ScheduledJob {
		return models.ScheduledJob{ID: 1, RunAt: now.Add(-time.Minute), Attempts: attempts, MaxAttempts: 3}
	}
	repeating := func(attempts int) models.ScheduledJob {
		j := oneOff(attempts)
		j.RepeatSeconds = &every
		return j
	}
	boom := errors.New("boom")

	cases := []struct {
		name     string
		job      models.ScheduledJob
		err      error
		status   string
		runAt    time.Time
		attempts int
		outcome  outcome
	}{
		{"one-off succeeds", oneOff(1), nil, models.JobSucceeded, now.Add(-time.Minute), 1, outcomeSucceeded},
		{"one-off retries", oneOff(1), boom, models.JobPending, now.Add(30 * time.Second), 1, outcomeRetrying},
		{"second retry waits longer", oneOff(2), boom, models.JobPending, now.Add(time.Minute), 2, outcomeRetrying},
		{"out of attempts", oneOff(3), boom, models.JobFailed, now.Add(-time.Minute), 3, outcomeFailed},
		{"permanent fails at once", oneOff(1), Permanent(boom), models.JobFailed, now.Add(-time.Minute), 1, outcomeFailed},
		{"repeating succeeds", repeating(1), nil, models.JobPending, now.Add(time.Minute), 0, outcomeSucceeded},
		{"repeating retry capped at interval", repeating(2), boom, models.JobPending, now.Add(time.Minute), 2, outcomeRetrying},
		{"repeating out of attempts waits for next run", repeating(3), boom, models.JobPending, now.Add(time.Minute), 0, outcomeFailed},
	}
	for _, c := range cases {
		o, got := s.decide(c.job, c.err)
		if o.Status != c.status || !o.RunAt.Equal(c.runAt) || o.Attempts != c.attempts || got != c.outcome {
			t.Errorf("%s: got status=%s run_at=%s attempts=%d outcome=%d", c.name, o.Status, o.RunAt, o.Attempts, got)
		}
		if (c.err == nil) != (o.LastError == nil) {
			t.Errorf("%s: last error %v", c.name, o.LastError)
		}
	}
}

func TestDecodeBadPayloadIsPermanent(t *testing.T) {
	var v struct{ N int }
	err := Job{Payload: []byte(`{"N":"x"}`)}.Decode(&v)
	var perm permanentError
	if !errors.As(err, &perm) {
		t.Errorf("got %v, want a permanent error", err)
	}
}
