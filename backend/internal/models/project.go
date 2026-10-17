package models

import "time"

const (
	ProjectActive    = "active"
	ProjectOnHold    = "on_hold"
	ProjectCompleted = "completed"
	ProjectArchived  = "archived"
)

var ProjectStatuses = []string{ProjectActive, ProjectOnHold, ProjectCompleted, ProjectArchived}

// Progress summarizes the top-level tasks of a project or milestone.
// Cancelled tasks don't count towards the total.
type Progress struct {
	TotalTasks     int `json:"total_tasks"`
	CompletedTasks int `json:"completed_tasks"`
	OverdueTasks   int `json:"overdue_tasks"`
	Percent        int `json:"percent"`
}

// ComputePercent sets Percent from the counts (rounded down, 0 when empty).
func (p *Progress) ComputePercent() {
	if p.TotalTasks > 0 {
		p.Percent = p.CompletedTasks * 100 / p.TotalTasks
	}
}

type ProjectProgress struct {
	Progress
	Milestones          int `json:"milestones"`
	MilestonesCompleted int `json:"milestones_completed"`
}

type Project struct {
	ID          uint64          `json:"id"`
	UserID      uint64          `json:"-"`
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Status      string          `json:"status"`
	Color       *string         `json:"color"`
	StartDate   *Date           `json:"start_date"`
	TargetDate  *Date           `json:"target_date"`
	CompletedAt *time.Time      `json:"completed_at"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Progress    ProjectProgress `json:"progress"`
	// Milestones is only loaded for single-project responses: omitted from
	// lists (nil), always an array on a single project.
	Milestones *[]Milestone `json:"milestones,omitempty"`
}

type Milestone struct {
	ID          uint64     `json:"id"`
	ProjectID   uint64     `json:"project_id"`
	Title       string     `json:"title"`
	Description *string    `json:"description"`
	DueDate     *Date      `json:"due_date"`
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completed_at"`
	Position    int        `json:"position"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Progress    Progress   `json:"progress"`
}
