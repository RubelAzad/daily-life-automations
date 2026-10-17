package models

import (
	"encoding/json"
	"time"
)

// Automation runs actions when its trigger fires and its conditions hold.
type Automation struct {
	ID         uint64      `json:"id"`
	UserID     uint64      `json:"-"`
	Name       string      `json:"name"`
	Enabled    bool        `json:"enabled"`
	Trigger    Trigger     `json:"trigger"`
	Conditions []Condition `json:"conditions"`
	Actions    []Action    `json:"actions"`
	NextRunAt  *time.Time  `json:"next_run_at"`
	LastRunAt  *time.Time  `json:"last_run_at"`
	LastStatus *string     `json:"last_status"`
	LastError  *string     `json:"last_error"`
	RunCount   int         `json:"run_count"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// Trigger types.
const (
	TriggerSchedule      = "schedule"
	TriggerManual        = "manual"
	TriggerTaskCreated   = "task.created"
	TriggerTaskCompleted = "task.completed"
	TriggerTaskOverdue   = "task.overdue"
	TriggerNoteCreated   = "note.created"
)

// Trigger is what starts an automation.
type Trigger struct {
	Type     string    `json:"type"`
	Schedule *Schedule `json:"schedule,omitempty"`
}

// Schedule is a time in the user's time zone: every day, on some weekdays,
// on a day of the month (the last day in shorter months), or once.
type Schedule struct {
	Frequency string `json:"frequency"`           // daily | weekly | monthly | once
	Time      string `json:"time"`                // HH:MM, local
	Weekdays  []int  `json:"weekdays,omitempty"`  // weekly: 1 = Monday … 7 = Sunday
	MonthDay  int    `json:"month_day,omitempty"` // monthly: 1–31
	Date      string `json:"date,omitempty"`      // once: YYYY-MM-DD
}

// Condition compares a field of the event's subject, e.g. task.priority.
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`
}

// Action is one tool call. Strings in Input may contain placeholders such
// as {{task.title}}, and time fields may be relative ("+1d", "tomorrow 09:00").
type Action struct {
	Type  string          `json:"type"`
	Input json.RawMessage `json:"input"`
}

// Automation run outcomes (last_status).
const (
	RunSucceeded       = "succeeded"
	RunWaitingApproval = "waiting_approval"
	RunFailed          = "failed"
	RunSkipped         = "skipped"
)
