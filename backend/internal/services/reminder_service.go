package services

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

const (
	maxOffsetMinutes   = 30 * 24 * 60
	minFollowUpMinutes = 5
	maxFollowUpMinutes = 7 * 24 * 60
	maxFollowUps       = 10
	defaultFollowUps   = 3
	maxSnoozeMinutes   = 7 * 24 * 60
)

type ReminderService struct {
	Reminders repository.ReminderRepository
	Tasks     repository.TaskRepository
	Events    repository.CalendarEventRepository
	Notes     repository.NoteRepository
	// Now is overridable for tests; defaults to time.Now.
	Now func() time.Time
}

func (s ReminderService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

type ListRemindersInput struct {
	Status  string // comma-separated; default "scheduled"
	TaskID  string
	EventID string
	NoteID  string
	Page    string
	PerPage string
}

// CreateReminderInput: standalone needs title + remind_at; a task reminder
// needs offset_minutes (before due) or remind_at; an event reminder needs
// offset_minutes (before each occurrence).
type CreateReminderInput struct {
	Title           *string `json:"title"`
	Note            *string `json:"note"`
	RemindAt        *string `json:"remind_at"`
	TaskID          *uint64 `json:"task_id"`
	EventID         *uint64 `json:"event_id"`
	NoteID          *uint64 `json:"note_id"` // offset_minutes before the note's expires_on
	OffsetMinutes   *int    `json:"offset_minutes"`
	FollowUpMinutes *int    `json:"follow_up_minutes"` // tasks only: repeat until done
	MaxFollowUps    *int    `json:"max_follow_ups"`
}

type UpdateReminderInput struct {
	Title           Optional[string] `json:"title"`
	Note            Optional[string] `json:"note"`
	RemindAt        Optional[string] `json:"remind_at"`
	OffsetMinutes   Optional[int]    `json:"offset_minutes"`
	FollowUpMinutes Optional[int]    `json:"follow_up_minutes"`
	MaxFollowUps    Optional[int]    `json:"max_follow_ups"`
}

func (s ReminderService) List(ctx context.Context, userID uint64, in ListRemindersInput) ([]models.Reminder, PageMeta, error) {
	var c checker
	f := repository.ReminderFilter{
		TaskID:  parseIDParam(&c, "task_id", in.TaskID),
		EventID: parseIDParam(&c, "event_id", in.EventID),
		NoteID:  parseIDParam(&c, "note_id", in.NoteID),
	}
	status := in.Status
	if status == "" {
		status = models.ReminderScheduled
	}
	if status != "all" {
		for _, st := range strings.Split(status, ",") {
			c.oneOf("status", st, append(models.ReminderStatuses, "all"))
			f.Statuses = append(f.Statuses, st)
		}
	}
	meta := PageMeta{Page: positiveInt(&c, "page", in.Page, 1), PerPage: positiveInt(&c, "per_page", in.PerPage, defaultPerPage)}
	if meta.PerPage > maxPerPage {
		c.fail("per_page", "must be at most "+strconv.Itoa(maxPerPage))
	}
	if err := c.err(); err != nil {
		return nil, meta, err
	}
	f.Limit, f.Offset = meta.PerPage, (meta.Page-1)*meta.PerPage
	rems, total, err := s.Reminders.List(ctx, userID, f)
	meta.Total = total
	return rems, meta, err
}

func (s ReminderService) Create(ctx context.Context, userID uint64, in CreateReminderInput) (models.Reminder, error) {
	var c checker
	r := models.Reminder{
		UserID:        userID,
		TaskID:        in.TaskID,
		EventID:       in.EventID,
		NoteID:        in.NoteID,
		Note:          optionalText(in.Note),
		OffsetMinutes: in.OffsetMinutes,
		Status:        models.ReminderScheduled,
	}
	if countSet(in.TaskID, in.EventID, in.NoteID) > 1 {
		c.fail("event_id", "a reminder belongs to one task, event or note, not several")
		return models.Reminder{}, c.err()
	}
	if in.Title != nil {
		title := c.requiredText("title", *in.Title, 255)
		r.Title = &title
	}
	var fixed *time.Time
	if in.RemindAt != nil && *in.RemindAt != "" {
		fixed = c.parseTime("remind_at", *in.RemindAt)
	}
	if in.FollowUpMinutes != nil {
		r.FollowUpMinutes = in.FollowUpMinutes
		r.MaxFollowUps = defaultFollowUps
	}
	if in.MaxFollowUps != nil {
		r.MaxFollowUps = *in.MaxFollowUps
	}
	if err := s.schedule(ctx, &c, &r, fixed); err != nil {
		return models.Reminder{}, err
	}
	if err := c.err(); err != nil {
		return models.Reminder{}, err
	}
	return s.Reminders.Create(ctx, r)
}

func (s ReminderService) Update(ctx context.Context, userID, id uint64, in UpdateReminderInput) (models.Reminder, error) {
	r, err := s.Reminders.Get(ctx, userID, id)
	if err != nil {
		return r, err
	}
	var c checker
	if in.Title.Set {
		if r.Kind != "standalone" {
			c.fail("title", "only standalone reminders have their own title")
		} else {
			title := c.requiredText("title", in.Title.Value, 255)
			r.Title = &title
		}
	}
	if in.Note.Set {
		r.Note = optionalText(in.Note.Ptr())
	}
	// Any schedule change re-arms the reminder from scratch.
	var fixed *time.Time
	if r.OffsetMinutes == nil {
		fixed = r.RemindAt
	}
	if in.RemindAt.Set {
		fixed = nil
		if !in.RemindAt.Null && in.RemindAt.Value != "" {
			fixed = c.parseTime("remind_at", in.RemindAt.Value)
		}
		r.OffsetMinutes = nil
	}
	if in.OffsetMinutes.Set {
		r.OffsetMinutes = in.OffsetMinutes.Ptr()
		if r.OffsetMinutes != nil {
			fixed = nil
		}
	}
	if in.FollowUpMinutes.Set {
		r.FollowUpMinutes = in.FollowUpMinutes.Ptr()
		if r.FollowUpMinutes == nil {
			r.MaxFollowUps = 0
		} else if r.MaxFollowUps == 0 {
			r.MaxFollowUps = defaultFollowUps
		}
	}
	if in.MaxFollowUps.Set {
		r.MaxFollowUps = in.MaxFollowUps.Value
	}
	r.Status, r.FollowUpsSent = models.ReminderScheduled, 0
	if err := s.schedule(ctx, &c, &r, fixed); err != nil {
		return models.Reminder{}, err
	}
	if err := c.err(); err != nil {
		return models.Reminder{}, err
	}
	if err := s.Reminders.Update(ctx, r); err != nil {
		return models.Reminder{}, err
	}
	return s.Reminders.Get(ctx, userID, id)
}

func (s ReminderService) Delete(ctx context.Context, userID, id uint64) error {
	return s.Reminders.Delete(ctx, userID, id)
}

// Snooze fires the reminder again after minutes, without using up follow-ups.
func (s ReminderService) Snooze(ctx context.Context, userID, id uint64, minutes int) (models.Reminder, error) {
	r, err := s.Reminders.Get(ctx, userID, id)
	if err != nil {
		return r, err
	}
	if minutes < 1 || minutes > maxSnoozeMinutes {
		return r, &ValidationError{Fields: map[string]string{"minutes": "must be between 1 and 10080"}}
	}
	at := s.now().Add(time.Duration(minutes) * time.Minute)
	r.RemindAt, r.Status = &at, models.ReminderScheduled
	if err := s.Reminders.Update(ctx, r); err != nil {
		return r, err
	}
	return s.Reminders.Get(ctx, userID, id)
}

// schedule validates the reminder's kind-specific fields and computes
// RemindAt/OccursAt. fixed is an explicit remind_at, if any.
func (s ReminderService) schedule(ctx context.Context, c *checker, r *models.Reminder, fixed *time.Time) error {
	now := s.now()
	if r.Note != nil {
		c.maxLen("note", *r.Note, 2000)
	}
	if r.OffsetMinutes != nil && (*r.OffsetMinutes < 0 || *r.OffsetMinutes > maxOffsetMinutes) {
		c.fail("offset_minutes", "must be between 0 and 43200 (30 days)")
		return nil
	}
	if r.FollowUpMinutes != nil {
		switch {
		case r.TaskID == nil:
			c.fail("follow_up_minutes", "follow-ups are only for task reminders")
		case *r.FollowUpMinutes < minFollowUpMinutes || *r.FollowUpMinutes > maxFollowUpMinutes:
			c.fail("follow_up_minutes", "must be between 5 and 10080")
		case r.MaxFollowUps < 1 || r.MaxFollowUps > maxFollowUps:
			c.fail("max_follow_ups", "must be between 1 and 10")
		}
	} else if r.MaxFollowUps != 0 {
		c.fail("max_follow_ups", "requires follow_up_minutes")
	}

	switch r.Kind = ""; {
	case r.TaskID != nil:
		r.Kind = "task"
		if r.Title != nil {
			c.fail("title", "only standalone reminders have their own title")
		}
		task, err := s.Tasks.Get(ctx, r.UserID, *r.TaskID)
		if errors.Is(err, repository.ErrNotFound) {
			c.fail("task_id", "task not found")
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case r.OffsetMinutes != nil:
			if task.DueAt == nil {
				c.fail("offset_minutes", "the task has no due date; set one or use remind_at")
				return nil
			}
			r.RemindAt, r.OccursAt = TaskReminderTime(task.DueAt, *r.OffsetMinutes, now)
			if r.RemindAt == nil {
				c.fail("offset_minutes", "the task is already past its due time")
			}
		case fixed != nil:
			r.RemindAt, r.OccursAt = fixed, task.DueAt
			requireFuture(c, "remind_at", fixed, now)
		default:
			c.fail("offset_minutes", "give offset_minutes (before the due time) or remind_at")
		}
	case r.EventID != nil:
		r.Kind = "event"
		if r.Title != nil {
			c.fail("title", "only standalone reminders have their own title")
		}
		if fixed != nil {
			c.fail("remind_at", "event reminders use offset_minutes before each occurrence")
			return nil
		}
		if r.OffsetMinutes == nil {
			c.fail("offset_minutes", "is required for event reminders")
			return nil
		}
		event, err := s.Events.Get(ctx, r.UserID, *r.EventID)
		if errors.Is(err, repository.ErrNotFound) {
			c.fail("event_id", "event not found")
			return nil
		}
		if err != nil {
			return err
		}
		loc, err := userLocation(ctx, s.Reminders.DB, r.UserID)
		if err != nil {
			return err
		}
		r.RemindAt, r.OccursAt = NextEventReminder(event, *r.OffsetMinutes, now, loc)
		if r.RemindAt == nil {
			c.fail("offset_minutes", "the event has no upcoming occurrence for this reminder")
		}
	case r.NoteID != nil:
		r.Kind = "note"
		if r.Title != nil {
			c.fail("title", "only standalone reminders have their own title")
		}
		if fixed != nil {
			c.fail("remind_at", "note reminders use offset_minutes before the expiry date")
			return nil
		}
		if r.OffsetMinutes == nil {
			c.fail("offset_minutes", "is required for note reminders")
			return nil
		}
		note, err := s.Notes.Get(ctx, r.UserID, *r.NoteID)
		if errors.Is(err, repository.ErrNotFound) {
			c.fail("note_id", "note not found")
			return nil
		}
		if err != nil {
			return err
		}
		if note.ExpiresOn == nil {
			c.fail("offset_minutes", "the note has no expiry date")
			return nil
		}
		loc, err := userLocation(ctx, s.Reminders.DB, r.UserID)
		if err != nil {
			return err
		}
		r.RemindAt, r.OccursAt = NoteReminderTime(note.ExpiresOn, *r.OffsetMinutes, now, loc)
		if r.RemindAt == nil {
			c.fail("offset_minutes", "the note has already expired")
		}
	default:
		r.Kind = "standalone"
		if r.Title == nil {
			c.fail("title", "is required")
		}
		if r.OffsetMinutes != nil {
			c.fail("offset_minutes", "standalone reminders use remind_at")
		}
		if fixed == nil {
			c.fail("remind_at", "is required")
			return nil
		}
		r.RemindAt, r.OccursAt = fixed, nil
		requireFuture(c, "remind_at", fixed, now)
	}
	return nil
}

func countSet(ids ...*uint64) int {
	n := 0
	for _, id := range ids {
		if id != nil {
			n++
		}
	}
	return n
}

func requireFuture(c *checker, field string, t *time.Time, now time.Time) {
	if t != nil && !t.After(now) {
		c.fail(field, "must be in the future")
	}
}

// ---- Notifications ---------------------------------------------------------

type NotificationService struct {
	Notifications repository.NotificationRepository
}

func (s NotificationService) List(ctx context.Context, userID uint64, unread, page, perPage string) ([]models.Notification, PageMeta, error) {
	var c checker
	meta := PageMeta{Page: positiveInt(&c, "page", page, 1), PerPage: positiveInt(&c, "per_page", perPage, defaultPerPage)}
	if meta.PerPage > maxPerPage {
		c.fail("per_page", "must be at most "+strconv.Itoa(maxPerPage))
	}
	if unread != "" && unread != "true" && unread != "false" {
		c.fail("unread", "must be true or false")
	}
	if err := c.err(); err != nil {
		return nil, meta, err
	}
	items, total, err := s.Notifications.List(ctx, userID, unread == "true", meta.PerPage, (meta.Page-1)*meta.PerPage)
	meta.Total = total
	return items, meta, err
}

func (s NotificationService) UnreadCount(ctx context.Context, userID uint64) (int, error) {
	return s.Notifications.UnreadCount(ctx, userID)
}

func (s NotificationService) MarkRead(ctx context.Context, userID, id uint64) error {
	return s.Notifications.MarkRead(ctx, userID, id)
}

func (s NotificationService) MarkAllRead(ctx context.Context, userID uint64) (int64, error) {
	return s.Notifications.MarkAllRead(ctx, userID)
}
