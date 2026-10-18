package models

import (
	"encoding/json"
	"time"
)

// Automation runs actions when its trigger fires and its conditions hold.
type Automation struct {
	ID     uint64 `json:"id"`
	UserID uint64 `json:"-"`
	// TemplateKey names the built-in workflow it was added from, if any.
	TemplateKey *string     `json:"template_key"`
	Name        string      `json:"name"`
	Enabled     bool        `json:"enabled"`
	Trigger     Trigger     `json:"trigger"`
	Conditions  []Condition `json:"conditions"`
	Actions     []Action    `json:"actions"`
	NextRunAt   *time.Time  `json:"next_run_at"`
	LastRunAt   *time.Time  `json:"last_run_at"`
	LastStatus  *string     `json:"last_status"`
	LastError   *string     `json:"last_error"`
	RunCount    int         `json:"run_count"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// Trigger types.
const (
	TriggerSchedule      = "schedule"
	TriggerManual        = "manual"
	TriggerTaskCreated   = "task.created"
	TriggerTaskCompleted = "task.completed"
	TriggerTaskOverdue   = "task.overdue"
	TriggerNoteCreated   = "note.created"
	// TriggerWorkflowFailed fires when another automation's run fails for good.
	TriggerWorkflowFailed = "workflow.failed"
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

// Run statuses beyond the outcomes above.
const (
	RunRunning  = "running"
	RunRetrying = "retrying" // failed; an automatic retry is scheduled
)

// WorkflowRun is one run of an automation.
type WorkflowRun struct {
	ID             uint64          `json:"id"`
	UserID         uint64          `json:"-"`
	AutomationID   *uint64         `json:"automation_id"`
	AutomationName string          `json:"automation_name"`
	TriggerType    string          `json:"trigger_type"`
	TriggerSource  string          `json:"trigger_source"` // schedule | event | manual
	TriggerSummary *string         `json:"trigger_summary"`
	TriggerData    json.RawMessage `json:"-"`
	Depth          int             `json:"-"`
	RunKey         string          `json:"-"`
	Status         string          `json:"status"`
	Error          *string         `json:"error"`
	Steps          []RunStep       `json:"steps"`
	Attempt        int             `json:"attempt"`
	NextRetryAt    *time.Time      `json:"next_retry_at"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     *time.Time      `json:"finished_at"`
	DurationMS     *int            `json:"duration_ms"`
}

// RunStep is the outcome of one action in a run.
type RunStep struct {
	Index    int     `json:"index"`
	Type     string  `json:"type"`
	Status   string  `json:"status"` // an AI action status, or "not_run"
	ActionID *uint64 `json:"action_id,omitempty"`
	Summary  string  `json:"summary,omitempty"`
	Error    string  `json:"error,omitempty"`
	Attempt  int     `json:"attempt"`
}

// StepNotRun marks a step a run never reached.
const StepNotRun = "not_run"
