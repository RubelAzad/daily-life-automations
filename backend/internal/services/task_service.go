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
	defaultPerPage = 20
	maxPerPage     = 100
)

type TaskService struct {
	Tasks repository.TaskRepository
	Lists repository.TaskListRepository
}

// ListTasksInput holds raw query-string values; the service validates them.
type ListTasksInput struct {
	Status   string // comma-separated, or "open" for pending+in_progress
	Priority string
	ListID   string
	Query    string
	DueFrom  string
	DueTo    string
	Sort     string
	Page     string
	PerPage  string
}

type PageMeta struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

type CreateTaskInput struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	DueAt       *string `json:"due_at"`
	ListID      *uint64 `json:"list_id"`
}

type UpdateTaskInput struct {
	Title       Optional[string] `json:"title"`
	Description Optional[string] `json:"description"`
	Status      Optional[string] `json:"status"`
	Priority    Optional[string] `json:"priority"`
	DueAt       Optional[string] `json:"due_at"`
	ListID      Optional[uint64] `json:"list_id"`
}

func (s TaskService) List(ctx context.Context, userID uint64, in ListTasksInput) ([]models.Task, PageMeta, error) {
	var c checker
	f := repository.TaskFilter{Priority: in.Priority, Query: strings.TrimSpace(in.Query), Sort: in.Sort}

	switch in.Status {
	case "":
	case "open":
		f.Statuses = []string{models.TaskPending, models.TaskInProgress}
	default:
		for _, st := range strings.Split(in.Status, ",") {
			c.oneOf("status", st, models.TaskStatuses)
			f.Statuses = append(f.Statuses, st)
		}
	}
	if f.Priority != "" {
		c.oneOf("priority", f.Priority, models.TaskPriorities)
	}
	if in.ListID != "" {
		id, err := strconv.ParseUint(in.ListID, 10, 64)
		if err != nil {
			c.fail("list_id", "must be a list ID")
		}
		f.ListID = &id
	}
	if in.DueFrom != "" {
		f.DueFrom = c.parseTime("due_from", in.DueFrom)
	}
	if in.DueTo != "" {
		f.DueTo = c.parseTime("due_to", in.DueTo)
	}
	if f.Sort == "" {
		f.Sort = repository.DefaultTaskSort
	} else if !repository.IsValidTaskSort(f.Sort) {
		c.fail("sort", "must be one of: due_at, -due_at, created_at, -created_at, priority")
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
	tasks, total, err := s.Tasks.List(ctx, userID, f)
	meta.Total = total
	return tasks, meta, err
}

func (s TaskService) Get(ctx context.Context, userID, id uint64) (models.Task, error) {
	return s.Tasks.Get(ctx, userID, id)
}

func (s TaskService) Create(ctx context.Context, userID uint64, in CreateTaskInput) (models.Task, error) {
	var c checker
	t := models.Task{
		UserID:      userID,
		Title:       c.requiredText("title", in.Title, 255),
		Description: optionalText(in.Description),
		Status:      orDefault(in.Status, models.TaskPending),
		Priority:    orDefault(in.Priority, "medium"),
		ListID:      in.ListID,
	}
	if t.Description != nil {
		c.maxLen("description", *t.Description, 10000)
	}
	c.oneOf("status", t.Status, models.TaskStatuses)
	c.oneOf("priority", t.Priority, models.TaskPriorities)
	if in.DueAt != nil && *in.DueAt != "" {
		t.DueAt = c.parseTime("due_at", *in.DueAt)
	}
	if err := s.checkListOwnership(ctx, &c, userID, t.ListID); err != nil {
		return models.Task{}, err
	}
	if err := c.err(); err != nil {
		return models.Task{}, err
	}
	setCompletedAt(&t, "")
	return s.Tasks.Create(ctx, t)
}

func (s TaskService) Update(ctx context.Context, userID, id uint64, in UpdateTaskInput) (models.Task, error) {
	t, err := s.Tasks.Get(ctx, userID, id)
	if err != nil {
		return t, err
	}
	previousStatus := t.Status

	var c checker
	if in.Title.Set {
		t.Title = c.requiredText("title", in.Title.Value, 255)
	}
	if in.Description.Set {
		t.Description = optionalText(in.Description.Ptr())
		if t.Description != nil {
			c.maxLen("description", *t.Description, 10000)
		}
	}
	if in.Status.Set {
		t.Status = in.Status.Value
		c.oneOf("status", t.Status, models.TaskStatuses)
	}
	if in.Priority.Set {
		t.Priority = in.Priority.Value
		c.oneOf("priority", t.Priority, models.TaskPriorities)
	}
	if in.DueAt.Set {
		t.DueAt = nil
		if !in.DueAt.Null && in.DueAt.Value != "" {
			t.DueAt = c.parseTime("due_at", in.DueAt.Value)
		}
	}
	if in.ListID.Set {
		t.ListID = in.ListID.Ptr()
		if err := s.checkListOwnership(ctx, &c, userID, t.ListID); err != nil {
			return models.Task{}, err
		}
	}
	if err := c.err(); err != nil {
		return models.Task{}, err
	}
	setCompletedAt(&t, previousStatus)
	return s.Tasks.Update(ctx, t)
}

func (s TaskService) Delete(ctx context.Context, userID, id uint64) error {
	return s.Tasks.Delete(ctx, userID, id)
}

// checkListOwnership rejects list IDs that don't exist or belong to another
// user; the foreign key alone would accept another user's list.
func (s TaskService) checkListOwnership(ctx context.Context, c *checker, userID uint64, listID *uint64) error {
	if listID == nil {
		return nil
	}
	_, err := s.Lists.Get(ctx, userID, *listID)
	if errors.Is(err, repository.ErrNotFound) {
		c.fail("list_id", "list not found")
		return nil
	}
	return err
}

// setCompletedAt stamps completion time on entering "completed" and clears it
// on leaving.
func setCompletedAt(t *models.Task, previousStatus string) {
	switch {
	case t.Status == models.TaskCompleted && previousStatus != models.TaskCompleted:
		now := time.Now().UTC()
		t.CompletedAt = &now
	case t.Status != models.TaskCompleted:
		t.CompletedAt = nil
	}
}

func positiveInt(c *checker, field, raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		c.fail(field, "must be a positive integer")
		return fallback
	}
	return n
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
