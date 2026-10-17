// Package automation holds background work that runs in the worker process,
// separate from the API.
package automation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
)

const defaultBatchSize = 200

// ReminderWorker turns due reminders into in-app notifications.
//
// Delivery is safe to retry and to run in several processes at once: each
// reminder is locked with FOR UPDATE SKIP LOCKED inside its own transaction,
// and each notification carries an idempotency key ("reminder:<id>:<unix
// fire time>") so it is inserted at most once.
type ReminderWorker struct {
	DB        *sql.DB
	Log       *log.Logger
	BatchSize int
	Now       func() time.Time // overridable for tests
}

type RunStats struct {
	Delivered int // notifications created
	Skipped   int // reminders cancelled because their task is already done
	Errors    int
}

// Run processes due reminders every interval until ctx is cancelled. A failed
// pass is logged and retried on the next tick.
func (w ReminderWorker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		w.logPass(w.RunOnce(ctx))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w ReminderWorker) logPass(stats RunStats, err error) {
	if err != nil {
		w.Log.Printf("reminders: pass failed: %v", err)
		return
	}
	if stats != (RunStats{}) {
		w.Log.Printf("reminders: delivered=%d skipped=%d errors=%d", stats.Delivered, stats.Skipped, stats.Errors)
	}
}

// RunOnce processes every reminder due now, in batches.
func (w ReminderWorker) RunOnce(ctx context.Context) (RunStats, error) {
	var stats RunStats
	batch := w.BatchSize
	if batch <= 0 {
		batch = defaultBatchSize
	}
	now := w.now()
	seen := map[uint64]bool{}
	for {
		ids, err := repository.ReminderRepository{DB: w.DB}.DueIDs(ctx, now, batch)
		if err != nil {
			return stats, err
		}
		progressed := false
		for _, id := range ids {
			if seen[id] {
				continue // failed earlier in this pass, or rescheduled into the past
			}
			seen[id] = true
			progressed = true
			if err := w.process(ctx, id, now, &stats); err != nil {
				stats.Errors++
				w.Log.Printf("reminders: reminder %d: %v", id, err)
			}
		}
		if !progressed || len(ids) < batch {
			return stats, nil
		}
	}
}

func (w ReminderWorker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

// process fires one reminder in its own transaction.
func (w ReminderWorker) process(ctx context.Context, id uint64, now time.Time, stats *RunStats) error {
	return repository.InTx(ctx, w.DB, func(tx repository.DBTX) error {
		reminders := repository.ReminderRepository{DB: tx}
		r, err := reminders.LockDue(ctx, id, now)
		if errors.Is(err, repository.ErrNotFound) {
			return nil // handled by another worker, or no longer due
		}
		if err != nil {
			return err
		}

		n, err := w.buildNotification(ctx, tx, &r, now)
		if err != nil {
			return err
		}
		if n == nil {
			stats.Skipped++
		} else {
			created, err := repository.NotificationRepository{DB: tx}.Insert(ctx, *n)
			if err != nil {
				return err
			}
			if created {
				stats.Delivered++
			}
			r.LastSentAt = &now
		}
		return reminders.Update(ctx, r)
	})
}

// buildNotification decides what to send for r and advances r to its next
// state (next follow-up, next event occurrence, or done). It returns nil when
// nothing should be sent.
func (w ReminderWorker) buildNotification(ctx context.Context, tx repository.DBTX, r *models.Reminder, now time.Time) (*models.Notification, error) {
	n := &models.Notification{
		UserID:         r.UserID,
		Kind:           "reminder",
		Priority:       "normal",
		ReminderID:     &r.ID,
		OccursAt:       r.OccursAt,
		IdempotencyKey: fmt.Sprintf("reminder:%d:%d", r.ID, r.RemindAt.Unix()),
	}

	switch r.Kind {
	case "task":
		task, err := repository.TaskRepository{DB: tx}.Get(ctx, r.UserID, *r.TaskID)
		if err != nil {
			return nil, err
		}
		if models.IsClosedStatus(task.Status) {
			r.Status = models.ReminderCancelled // nothing left to remind about
			return nil, nil
		}
		n.Title = task.Title
		n.Link = strPtr("/tasks")
		n.OccursAt = task.DueAt
		if task.Priority == "urgent" || task.Priority == "high" {
			n.Priority = "high"
		}
		switch {
		case r.FollowUpsSent > 0:
			n.Body = withNote(fmt.Sprintf("Follow-up %d: still not done", r.FollowUpsSent), r.Note)
		case r.OffsetMinutes != nil:
			n.Body = withNote("Task due soon", r.Note)
		default:
			n.Body = withNote("Task reminder", r.Note)
		}
		// Follow-ups keep nagging until the task is done or the limit is hit.
		if r.FollowUpMinutes != nil && r.FollowUpsSent < r.MaxFollowUps {
			next := now.Add(time.Duration(*r.FollowUpMinutes) * time.Minute)
			r.RemindAt, r.FollowUpsSent = &next, r.FollowUpsSent+1
		} else {
			r.Status = models.ReminderSent
		}

	case "event":
		event, err := repository.CalendarEventRepository{DB: tx}.Get(ctx, r.UserID, *r.EventID)
		if err != nil {
			return nil, err
		}
		n.Title = event.Title
		n.Link = strPtr("/calendar")
		body := "Event starts soon"
		if event.Location != nil {
			body += " · " + *event.Location
		}
		n.Body = withNote(body, r.Note)
		// Repeating events: arm the reminder for the next occurrence.
		r.RemindAt, r.OccursAt = services.NextEventReminder(event, *r.OffsetMinutes, now)
		if r.RemindAt == nil {
			r.Status = models.ReminderSent
		}

	case "note":
		note, err := repository.NoteRepository{DB: tx}.Get(ctx, r.UserID, *r.NoteID)
		if err != nil {
			return nil, err
		}
		if note.Archived || note.ExpiresOn == nil {
			r.Status = models.ReminderCancelled // archived or no longer expiring
			return nil, nil
		}
		n.Title = note.Title
		n.Link = strPtr(fmt.Sprintf("/notes?id=%d", note.ID))
		n.Body = withNote("Expires on "+note.ExpiresOn.String(), r.Note) // a calendar date: no time zone needed
		r.Status = models.ReminderSent

	default:
		n.Title = r.SubjectTitle
		n.Link = strPtr("/reminders")
		n.Body = r.Note
		r.Status = models.ReminderSent
	}
	return n, nil
}

func withNote(body string, note *string) *string {
	if note != nil {
		body += " — " + *note
	}
	return &body
}

func strPtr(s string) *string { return &s }
