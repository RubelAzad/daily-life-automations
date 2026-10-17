package automation

import (
	"context"
	"database/sql"
	"log"
	"time"

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/scheduler"
	"lifeos-ai/backend/internal/services"
)

// Kinds of the repeating system jobs.
const (
	KindReminders = "reminders.deliver"
	KindBriefings = "briefings.run"
	KindSearch    = "search.index"
	KindPrune     = "scheduler.prune"
)

const (
	// Finished one-off jobs are kept this long, then deleted (and their
	// idempotency keys can be used again).
	keepDoneJobs   = 30 * 24 * time.Hour
	keepFailedJobs = 90 * 24 * time.Hour
)

// SystemJobs is the worker's recurring background work, run as repeating
// jobs in the durable scheduler. Each pass is safe to run in several
// workers, but the scheduler runs it in only one at a time.
type SystemJobs struct {
	DB        *sql.DB
	Reminders ReminderWorker
	Briefer   ai.Briefer
	Search    services.SearchService
	Log       *log.Logger
	// Interval is how often reminders, briefings and search indexing run.
	Interval time.Duration
	// Verbose logs every pass, even when nothing happened (worker -once).
	Verbose bool
}

// Register adds the system jobs to s.
func (j SystemJobs) Register(s *scheduler.Scheduler) {
	s.Every(scheduler.Repeating{Kind: KindReminders, Every: j.Interval, Handler: j.reminders})
	s.Every(scheduler.Repeating{Kind: KindBriefings, Every: j.Interval, Handler: j.briefings})
	s.Every(scheduler.Repeating{Kind: KindSearch, Every: j.Interval, Handler: j.search})
	s.Every(scheduler.Repeating{Kind: KindPrune, Every: 24 * time.Hour, Handler: j.prune})
}

// reminders turns due reminders into in-app notifications.
func (j SystemJobs) reminders(ctx context.Context, _ scheduler.Job) error {
	stats, err := j.Reminders.RunOnce(ctx)
	if err != nil {
		return err
	}
	if j.Verbose || stats != (RunStats{}) {
		j.Log.Printf("reminders: delivered=%d skipped=%d held=%d errors=%d", stats.Delivered, stats.Skipped, stats.Held, stats.Errors)
	}
	return nil
}

// briefings makes the scheduled morning and evening briefings.
func (j SystemJobs) briefings(ctx context.Context, _ scheduler.Job) error {
	stats, err := j.Briefer.RunScheduled(ctx)
	if err != nil {
		return err
	}
	if j.Verbose || stats != (ai.BriefingStats{}) {
		j.Log.Printf("briefings: generated=%d failed=%d", stats.Generated, stats.Failed)
	}
	return nil
}

// search extracts text from new uploads and re-indexes changed notes, as a
// backup for the indexing the API does straight away.
func (j SystemJobs) search(ctx context.Context, _ scheduler.Job) error {
	files, notes, err := j.Search.IndexPending(ctx, 20)
	if err != nil {
		return err
	}
	if j.Verbose || files+notes > 0 {
		j.Log.Printf("search: indexed files=%d notes=%d", files, notes)
	}
	return nil
}

// prune deletes old finished one-off jobs, in batches.
func (j SystemJobs) prune(ctx context.Context, _ scheduler.Job) error {
	now := time.Now().UTC()
	var total int64
	for {
		n, err := repository.JobRepository{DB: j.DB}.PruneFinished(ctx, now.Add(-keepDoneJobs), now.Add(-keepFailedJobs), 1000)
		if err != nil {
			return err
		}
		total += n
		if n < 1000 {
			break
		}
	}
	if j.Verbose || total > 0 {
		j.Log.Printf("scheduler: pruned %d finished jobs", total)
	}
	return nil
}
