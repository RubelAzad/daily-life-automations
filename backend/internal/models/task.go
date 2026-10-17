package models

import "time"

const (
	TaskPending    = "pending"
	TaskInProgress = "in_progress"
	TaskCompleted  = "completed"
	TaskCancelled  = "cancelled"
)

var (
	TaskStatuses   = []string{TaskPending, TaskInProgress, TaskCompleted, TaskCancelled}
	TaskPriorities = []string{"low", "medium", "high", "urgent"}
)

type Task struct {
	ID          uint64     `json:"id"`
	UserID      uint64     `json:"-"`
	ListID      *uint64    `json:"list_id"`
	Title       string     `json:"title"`
	Description *string    `json:"description"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`
	DueAt       *time.Time `json:"due_at"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
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
