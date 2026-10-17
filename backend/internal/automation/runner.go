package automation

import (
	"context"
	"log"
	"time"

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/services"
)

// Job is background work done on every tick.
type Job interface {
	Tick(ctx context.Context)
}

// Run ticks every job each interval until ctx is cancelled. A job that fails
// logs it and is retried on the next tick.
func Run(ctx context.Context, interval time.Duration, jobs ...Job) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for _, j := range jobs {
			j.Tick(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// BriefingWorker makes the scheduled morning and evening briefings.
type BriefingWorker struct {
	Briefer ai.Briefer
	Log     *log.Logger
}

func (w BriefingWorker) Tick(ctx context.Context) {
	stats, err := w.Briefer.RunScheduled(ctx)
	switch {
	case err != nil:
		w.Log.Printf("briefings: pass failed: %v", err)
	case stats != (ai.BriefingStats{}):
		w.Log.Printf("briefings: generated=%d failed=%d", stats.Generated, stats.Failed)
	}
}

// SearchIndexWorker extracts text from new uploads and re-indexes changed
// notes, as a backup for the indexing the API does straight away.
type SearchIndexWorker struct {
	Search services.SearchService
	Log    *log.Logger
}

func (w SearchIndexWorker) Tick(ctx context.Context) {
	files, notes, err := w.Search.IndexPending(ctx, 20)
	switch {
	case err != nil:
		w.Log.Printf("search: pass failed: %v", err)
	case files+notes > 0:
		w.Log.Printf("search: indexed files=%d notes=%d", files, notes)
	}
}
