package scheduler

import (
	"context"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

// WorkerStatus says whether a worker is processing jobs, judged by when the
// repeating system jobs last ran.
type WorkerStatus struct {
	Running    bool        `json:"running"`
	LastSeenAt *time.Time  `json:"last_seen_at"`
	Jobs       []JobStatus `json:"jobs"`
}

// JobStatus is one repeating system job. Errors are left out on purpose:
// they are for the worker's log, not for every signed-in user.
type JobStatus struct {
	Kind           string     `json:"kind"`
	EverySeconds   int        `json:"every_seconds"`
	Running        bool       `json:"running"`
	LastStatus     *string    `json:"last_status"` // succeeded | failed
	LastFinishedAt *time.Time `json:"last_finished_at"`
	NextRunAt      *time.Time `json:"next_run_at"`
}

// Status reports on the repeating system jobs. The worker counts as running
// if one of them is running now, or one started or finished within twice
// the shortest interval plus a minute.
func Status(ctx context.Context, db repository.DBTX, now time.Time) (WorkerStatus, error) {
	jobs, err := repository.JobRepository{DB: db}.Repeating(ctx)
	if err != nil {
		return WorkerStatus{}, err
	}
	now = now.UTC()
	out := WorkerStatus{Jobs: []JobStatus{}}
	var shortest time.Duration
	seen := func(t *time.Time) {
		if t != nil && (out.LastSeenAt == nil || t.After(*out.LastSeenAt)) {
			v := *t
			out.LastSeenAt = &v
		}
	}
	for _, j := range jobs {
		every := time.Duration(*j.RepeatSeconds) * time.Second
		if shortest == 0 || every < shortest {
			shortest = every
		}
		running := j.Status == models.JobRunning && j.LockedUntil != nil && j.LockedUntil.After(now)
		st := JobStatus{Kind: j.Kind, EverySeconds: *j.RepeatSeconds, Running: running, LastStatus: j.LastStatus, LastFinishedAt: j.LastFinishedAt}
		if j.Status == models.JobPending {
			next := j.RunAt
			st.NextRunAt = &next
		}
		out.Jobs = append(out.Jobs, st)
		if running {
			seen(&now)
		}
		seen(j.LastStartedAt)
		seen(j.LastFinishedAt)
	}
	out.Running = out.LastSeenAt != nil && now.Sub(*out.LastSeenAt) <= 2*shortest+time.Minute
	return out, nil
}
