package models

import "time"

const (
	ReminderScheduled = "scheduled"
	ReminderSent      = "sent"
	ReminderCancelled = "cancelled"
)

var ReminderStatuses = []string{ReminderScheduled, ReminderSent, ReminderCancelled}

type Reminder struct {
	ID      uint64  `json:"id"`
	UserID  uint64  `json:"-"`
	TaskID  *uint64 `json:"task_id"`
	EventID *uint64 `json:"event_id"`
	NoteID  *uint64 `json:"note_id"` // fires before the note's expires_on
	Title   *string `json:"title"`   // standalone reminders only
	Note    *string `json:"note"`
	// OffsetMinutes is how long before the task's due time or the event's
	// start to fire. nil means remind_at is a fixed time.
	OffsetMinutes *int `json:"offset_minutes"`
	// RemindAt is the next time to fire; nil while inactive.
	RemindAt *time.Time `json:"remind_at"`
	// OccursAt is the due time/occurrence start the next firing refers to.
	OccursAt        *time.Time `json:"occurs_at"`
	FollowUpMinutes *int       `json:"follow_up_minutes"`
	MaxFollowUps    int        `json:"max_follow_ups"`
	FollowUpsSent   int        `json:"follow_ups_sent"`
	Status          string     `json:"status"`
	LastSentAt      *time.Time `json:"last_sent_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	// Derived.
	Kind         string `json:"kind"`          // "standalone", "task", "event" or "note"
	SubjectTitle string `json:"subject_title"` // the reminder, task or event title
}

func (r *Reminder) SetKind() {
	switch {
	case r.TaskID != nil:
		r.Kind = "task"
	case r.EventID != nil:
		r.Kind = "event"
	case r.NoteID != nil:
		r.Kind = "note"
	default:
		r.Kind = "standalone"
	}
}

type Notification struct {
	ID         uint64     `json:"id"`
	UserID     uint64     `json:"-"`
	Kind       string     `json:"kind"`
	Title      string     `json:"title"`
	Body       *string    `json:"body"`
	Link       *string    `json:"link"`
	Priority   string     `json:"priority"`
	ReminderID *uint64    `json:"reminder_id"`
	OccursAt   *time.Time `json:"occurs_at"`
	ReadAt     *time.Time `json:"read_at"`
	CreatedAt  time.Time  `json:"created_at"`
	// IdempotencyKey uniquely identifies one delivery; never exposed.
	IdempotencyKey string `json:"-"`
}
