// Package events is the internal event bus. Services publish events such as
// task.created inside the transaction that made the change; each event is a
// job in the durable scheduler, so it is delivered once the change commits,
// and never for a change that rolled back. The worker hands events to the
// user's automations.
package events

import (
	"context"
	"fmt"
	"strings"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/scheduler"
)

// JobKind is the scheduler job that delivers an event.
const JobKind = "automation.event"

// MaxDepth limits chains: an automation's actions can trigger other
// automations, whose actions can trigger others, this many levels deep.
// Deeper events are dropped, so automations can't loop forever.
const MaxDepth = 3

// Event is something that happened to a user's data.
type Event struct {
	Type   string `json:"type"` // a trigger type, e.g. task.created
	UserID uint64 `json:"user_id"`
	// Key names the occurrence, e.g. "task.created:42"; the same key is
	// published once.
	Key        string    `json:"key"`
	OccurredAt time.Time `json:"occurred_at"`
	// Data is a snapshot of the subject when it happened, e.g. {"task": {...}}.
	Data map[string]any `json:"data"`
	// Depth is 0 for the user's own changes and n+1 for changes made by an
	// automation running at depth n; CausedBy is that automation.
	Depth    int     `json:"depth"`
	CausedBy *uint64 `json:"caused_by,omitempty"`
}

type causeKey struct{}

// Cause is the automation run whose actions are making changes.
type Cause struct {
	AutomationID uint64
	Depth        int // depth of the event that started the run
}

// WithCause marks changes made under ctx as caused by an automation run.
func WithCause(ctx context.Context, c Cause) context.Context {
	return context.WithValue(ctx, causeKey{}, c)
}

// Bus publishes events.
type Bus struct {
	Now func() time.Time // overridable for tests
}

// Publish queues the event on db, normally the transaction making the
// change. It does nothing if no enabled automation of the user listens for
// this type, or if the chain is too deep.
func (b Bus) Publish(ctx context.Context, db repository.DBTX, ev Event) error {
	if c, ok := ctx.Value(causeKey{}).(Cause); ok {
		ev.Depth, ev.CausedBy = c.Depth+1, &c.AutomationID
	}
	if ev.Depth > MaxDepth {
		return nil
	}
	listening, err := repository.AutomationRepository{DB: db}.HasEnabled(ctx, ev.UserID, ev.Type)
	if err != nil || !listening {
		return err
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = b.now()
	}
	key := "event:" + ev.Key
	if ev.CausedBy != nil {
		key += fmt.Sprintf(":by:%d", *ev.CausedBy)
	}
	_, _, err = scheduler.Enqueue(ctx, db, scheduler.NewJob{UserID: &ev.UserID, Kind: JobKind, Payload: ev, Key: truncate(key, 191), RunAt: ev.OccurredAt})
	return err
}

func (b Bus) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

// TaskData is the snapshot of a task that conditions and placeholders use.
// list and project are names, looked up on db (which may be a transaction).
func TaskData(ctx context.Context, db repository.DBTX, t models.Task) map[string]any {
	d := map[string]any{
		"id": t.ID, "title": t.Title, "priority": t.Priority, "status": t.Status,
		"description": deref(t.Description), "list": "", "project": "", "due_at": nil,
	}
	if t.DueAt != nil {
		d["due_at"] = t.DueAt.UTC().Format(time.RFC3339)
	}
	if t.ListID != nil {
		var name string
		if err := db.QueryRowContext(ctx, `SELECT name FROM task_lists WHERE id = ? AND user_id = ?`, *t.ListID, t.UserID).Scan(&name); err == nil {
			d["list"] = name
		}
	}
	if t.ProjectID != nil {
		var name string
		if err := db.QueryRowContext(ctx, `SELECT name FROM projects WHERE id = ? AND user_id = ?`, *t.ProjectID, t.UserID).Scan(&name); err == nil {
			d["project"] = name
		}
	}
	out := map[string]any{"task": d}
	// A bill's "Pay …" task carries its bill, so completing it can record
	// the payment.
	d["bill"] = ""
	var billID uint64
	var billName string
	if err := db.QueryRowContext(ctx, `SELECT id, name FROM recurring_bills WHERE pending_task_id = ? AND user_id = ?`, t.ID, t.UserID).Scan(&billID, &billName); err == nil {
		d["bill"] = billName
		out["bill"] = map[string]any{"id": billID, "name": billName}
	}
	return out
}

// NoteData is the snapshot of a note.
func NoteData(n models.Note) map[string]any {
	tags := make([]string, len(n.Tags))
	for i, t := range n.Tags {
		tags[i] = t.Name
	}
	d := map[string]any{"id": n.ID, "title": n.Title, "kind": n.Kind, "url": deref(n.URL), "tags": tags, "expires_on": nil}
	if n.ExpiresOn != nil {
		d["expires_on"] = n.ExpiresOn.String()
	}
	return map[string]any{"note": d}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
