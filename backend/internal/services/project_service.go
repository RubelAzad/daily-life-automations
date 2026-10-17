package services

import (
	"context"
	"strconv"
	"strings"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

type ProjectService struct {
	Projects repository.ProjectRepository
}

type ListProjectsInput struct {
	Status  string // comma-separated; empty = everything except archived
	Page    string
	PerPage string
}

type CreateProjectInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	Color       *string `json:"color"`
	StartDate   *string `json:"start_date"`
	TargetDate  *string `json:"target_date"`
}

type UpdateProjectInput struct {
	Name        Optional[string] `json:"name"`
	Description Optional[string] `json:"description"`
	Status      Optional[string] `json:"status"`
	Color       Optional[string] `json:"color"`
	StartDate   Optional[string] `json:"start_date"`
	TargetDate  Optional[string] `json:"target_date"`
}

type CreateMilestoneInput struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	DueDate     *string `json:"due_date"`
}

type UpdateMilestoneInput struct {
	Title       Optional[string] `json:"title"`
	Description Optional[string] `json:"description"`
	DueDate     Optional[string] `json:"due_date"`
	Completed   Optional[bool]   `json:"completed"`
	Position    Optional[int]    `json:"position"`
}

func (s ProjectService) List(ctx context.Context, userID uint64, in ListProjectsInput) ([]models.Project, PageMeta, error) {
	var c checker
	var f repository.ProjectFilter
	if in.Status == "" {
		f.Statuses = []string{models.ProjectActive, models.ProjectOnHold, models.ProjectCompleted}
	} else {
		for _, st := range strings.Split(in.Status, ",") {
			c.oneOf("status", st, models.ProjectStatuses)
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
	f.Limit = meta.PerPage
	f.Offset = (meta.Page - 1) * meta.PerPage
	projects, total, err := s.Projects.List(ctx, userID, f)
	meta.Total = total
	return projects, meta, err
}

func (s ProjectService) Get(ctx context.Context, userID, id uint64) (models.Project, error) {
	return s.Projects.GetWithMilestones(ctx, userID, id)
}

func (s ProjectService) Create(ctx context.Context, userID uint64, in CreateProjectInput) (models.Project, error) {
	var c checker
	p := models.Project{
		UserID:      userID,
		Name:        c.requiredText("name", in.Name, 150),
		Description: optionalText(in.Description),
		Status:      orDefault(in.Status, models.ProjectActive),
		Color:       c.color("color", in.Color),
	}
	if p.Description != nil {
		c.maxLen("description", *p.Description, 10000)
	}
	c.oneOf("status", p.Status, models.ProjectStatuses)
	if in.StartDate != nil {
		p.StartDate = c.parseDate("start_date", *in.StartDate)
	}
	if in.TargetDate != nil {
		p.TargetDate = c.parseDate("target_date", *in.TargetDate)
	}
	checkDateRange(&c, p.StartDate, p.TargetDate)
	if err := c.err(); err != nil {
		return models.Project{}, err
	}
	p.CompletedAt = completionTime(p.Status == models.ProjectCompleted, nil)
	return s.Projects.Create(ctx, p)
}

func (s ProjectService) Update(ctx context.Context, userID, id uint64, in UpdateProjectInput) (models.Project, error) {
	p, err := s.Projects.Get(ctx, userID, id)
	if err != nil {
		return p, err
	}
	var c checker
	if in.Name.Set {
		p.Name = c.requiredText("name", in.Name.Value, 150)
	}
	if in.Description.Set {
		p.Description = optionalText(in.Description.Ptr())
		if p.Description != nil {
			c.maxLen("description", *p.Description, 10000)
		}
	}
	if in.Status.Set {
		p.Status = in.Status.Value
		c.oneOf("status", p.Status, models.ProjectStatuses)
	}
	if in.Color.Set {
		p.Color = c.color("color", in.Color.Ptr())
	}
	if in.StartDate.Set {
		p.StartDate = c.parseDate("start_date", in.StartDate.Value)
	}
	if in.TargetDate.Set {
		p.TargetDate = c.parseDate("target_date", in.TargetDate.Value)
	}
	checkDateRange(&c, p.StartDate, p.TargetDate)
	if err := c.err(); err != nil {
		return models.Project{}, err
	}
	p.CompletedAt = completionTime(p.Status == models.ProjectCompleted, p.CompletedAt)
	return s.Projects.Update(ctx, p)
}

func (s ProjectService) Delete(ctx context.Context, userID, id uint64) error {
	return s.Projects.Delete(ctx, userID, id)
}

func (s ProjectService) CreateMilestone(ctx context.Context, userID, projectID uint64, in CreateMilestoneInput) (models.Milestone, error) {
	if _, err := s.Projects.Get(ctx, userID, projectID); err != nil {
		return models.Milestone{}, err
	}
	var c checker
	m := models.Milestone{
		ProjectID:   projectID,
		Title:       c.requiredText("title", in.Title, 255),
		Description: optionalText(in.Description),
	}
	if m.Description != nil {
		c.maxLen("description", *m.Description, 10000)
	}
	if in.DueDate != nil {
		m.DueDate = c.parseDate("due_date", *in.DueDate)
	}
	if err := c.err(); err != nil {
		return models.Milestone{}, err
	}
	return s.Projects.CreateMilestone(ctx, userID, m)
}

func (s ProjectService) UpdateMilestone(ctx context.Context, userID, id uint64, in UpdateMilestoneInput) (models.Milestone, error) {
	m, err := s.Projects.GetMilestone(ctx, userID, id)
	if err != nil {
		return m, err
	}
	var c checker
	if in.Title.Set {
		m.Title = c.requiredText("title", in.Title.Value, 255)
	}
	if in.Description.Set {
		m.Description = optionalText(in.Description.Ptr())
		if m.Description != nil {
			c.maxLen("description", *m.Description, 10000)
		}
	}
	if in.DueDate.Set {
		m.DueDate = c.parseDate("due_date", in.DueDate.Value)
	}
	if in.Completed.Set {
		if in.Completed.Null {
			c.fail("completed", "must be true or false")
		}
		m.CompletedAt = completionTime(in.Completed.Value, m.CompletedAt)
	}
	if in.Position.Set {
		if in.Position.Null || in.Position.Value < 0 {
			c.fail("position", "must be zero or greater")
		}
		m.Position = in.Position.Value
	}
	if err := c.err(); err != nil {
		return models.Milestone{}, err
	}
	return s.Projects.UpdateMilestone(ctx, userID, m)
}

func (s ProjectService) DeleteMilestone(ctx context.Context, userID, id uint64) error {
	return s.Projects.DeleteMilestone(ctx, userID, id)
}

// completionTime keeps an existing completion time, stamps now when newly
// done, and clears it when no longer done.
func completionTime(done bool, current *time.Time) *time.Time {
	if !done {
		return nil
	}
	if current != nil {
		return current
	}
	now := time.Now().UTC()
	return &now
}

func checkDateRange(c *checker, start, target *models.Date) {
	if start != nil && target != nil && target.Before(start.Time) {
		c.fail("target_date", "must be on or after the start date")
	}
}
