package models

import (
	"encoding/json"
	"time"
)

const (
	BriefingMorning = "morning"
	BriefingEvening = "evening"

	BriefingGenerating = "generating"
	BriefingReady      = "ready"
	BriefingFailed     = "failed"
)

// Briefing is an AI-written morning plan or evening wrap-up for one day.
type Briefing struct {
	ID        uint64          `json:"id"`
	UserID    uint64          `json:"-"`
	Kind      string          `json:"kind"`
	Date      Date            `json:"date"`
	Trigger   string          `json:"trigger"` // manual | scheduled
	Status    string          `json:"status"`
	Content   json.RawMessage `json:"content"` // MorningPlan or EveningSummary
	Error     *string         `json:"error"`
	Model     *string         `json:"model"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// MorningPlan is the validated content of a morning briefing. Task and
// event IDs are checked against the user's data and titles are filled in
// by the server, never taken from the model.
type MorningPlan struct {
	Summary  string          `json:"summary"`
	Focus    []PlanFocus     `json:"focus"`
	Schedule []ScheduleBlock `json:"schedule"`
	Warnings []string        `json:"warnings"`
}

type PlanFocus struct {
	TaskID uint64 `json:"task_id"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// ScheduleBlock is a suggested slot, in local "HH:MM" time.
type ScheduleBlock struct {
	Start   string  `json:"start"`
	End     string  `json:"end"`
	Kind    string  `json:"kind"` // event | task | break
	Title   string  `json:"title"`
	TaskID  *uint64 `json:"task_id,omitempty"`
	EventID *uint64 `json:"event_id,omitempty"`
}

// EveningSummary is the validated content of an evening wrap-up.
type EveningSummary struct {
	Summary    string           `json:"summary"`
	Wins       []string         `json:"wins"`
	Unfinished []UnfinishedTask `json:"unfinished"`
	Tomorrow   string           `json:"tomorrow"`
}

type UnfinishedTask struct {
	TaskID     uint64 `json:"task_id"`
	Title      string `json:"title"`
	Suggestion string `json:"suggestion"` // tomorrow | later | drop
	Reason     string `json:"reason"`
}
