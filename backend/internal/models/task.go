package models

import "time"

const (
	TaskPending    = "pending"
	TaskInProgress = "in_progress"
	TaskCompleted  = "completed"
	TaskCancelled  = "cancelled"
)

var (
	TaskStatuses          = []string{TaskPending, TaskInProgress, TaskCompleted, TaskCancelled}
	TaskPriorities        = []string{"low", "medium", "high", "urgent"}
	RecurrenceFrequencies = []string{"daily", "weekly", "monthly", "yearly"}
)

// IsClosedStatus reports whether a task no longer needs work.
func IsClosedStatus(status string) bool {
	return status == TaskCompleted || status == TaskCancelled
}

type Recurrence struct {
	Frequency string     `json:"frequency"`
	Interval  int        `json:"interval"`
	Until     *time.Time `json:"until"`
}

// TaskRef is a short reference to another task, e.g. a dependency.
type TaskRef struct {
	ID     uint64 `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type Task struct {
	ID          uint64     `json:"id"`
	UserID      uint64     `json:"-"`
	ListID      *uint64    `json:"list_id"`
	ProjectID   *uint64    `json:"project_id"`
	MilestoneID *uint64    `json:"milestone_id"`
	ParentID    *uint64    `json:"parent_id"`
	Title       string     `json:"title"`
	Description *string    `json:"description"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`
	DueAt       *time.Time `json:"due_at"`
	// CarryCount is how many times the task was carried forward unfinished.
	CarryCount  int         `json:"carry_count"`
	CompletedAt *time.Time  `json:"completed_at"`
	Recurrence  *Recurrence `json:"recurrence"`
	// RecurrenceAnchorAt is the first due date of the series; occurrences are
	// computed from it so monthly dates don't drift (31st → 28th → 28th…).
	RecurrenceAnchorAt *time.Time `json:"-"`
	NextTaskID         *uint64    `json:"next_task_id"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`

	// Derived, read-only fields.
	SubtaskCount      int       `json:"subtask_count"`
	SubtasksCompleted int       `json:"subtasks_completed"`
	Dependencies      []TaskRef `json:"dependencies"`
	Blocked           bool      `json:"blocked"`
}

type TaskList struct {
	ID            uint64    `json:"id"`
	UserID        uint64    `json:"-"`
	Name          string    `json:"name"`
	Color         *string   `json:"color"`
	Position      int       `json:"position"`
	OpenTaskCount int       `json:"open_task_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
