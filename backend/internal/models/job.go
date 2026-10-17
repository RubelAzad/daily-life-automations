package models

import (
	"encoding/json"
	"time"
)

// ScheduledJob is a unit of background work in the durable scheduler.
type ScheduledJob struct {
	ID             uint64          `json:"id"`
	UserID         *uint64         `json:"-"`
	Kind           string          `json:"kind"`
	Payload        json.RawMessage `json:"-"`
	IdempotencyKey *string         `json:"-"`
	Status         string          `json:"status"`
	RunAt          time.Time       `json:"run_at"`
	RepeatSeconds  *int            `json:"repeat_seconds"`
	Attempts       int             `json:"attempts"`
	MaxAttempts    int             `json:"max_attempts"`
	LockedBy       *string         `json:"-"`
	LockedUntil    *time.Time      `json:"-"`
	LastStatus     *string         `json:"last_status"`
	LastError      *string         `json:"-"`
	LastStartedAt  *time.Time      `json:"last_started_at"`
	LastFinishedAt *time.Time      `json:"last_finished_at"`
	CreatedAt      time.Time       `json:"created_at"`
}

// Scheduled job statuses.
const (
	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
	JobCancelled = "cancelled"
)
