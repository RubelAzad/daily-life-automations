package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"lifeos-ai/backend/internal/events"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

const (
	defaultPerPage = 20
	maxPerPage     = 100
	maxSubtasks    = 100
)

type TaskService struct {
	Tasks    repository.TaskRepository
	Lists    repository.TaskListRepository
	Projects repository.ProjectRepository
	// Events publishes task.created and task.completed; nil turns them off.
	Events EventPublisher
}

// EventPublisher queues an automation event on db, the transaction making
// the change, so the event exists exactly when the change does.
type EventPublisher interface {
	Publish(ctx context.Context, db repository.DBTX, ev events.Event) error
}

func publish(ctx context.Context, p EventPublisher, db repository.DBTX, ev events.Event) error {
	if p == nil {
		return nil
	}
	return p.Publish(ctx, db, ev)
}

// ListTasksInput holds raw query-string values; the service validates them.
type ListTasksInput struct {
	Status      string // comma-separated, or "open" for pending+in_progress
	Priority    string
	ListID      string
	ProjectID   string
	MilestoneID string // an ID, or "none" for tasks without a milestone
	ParentID    string // set to list a task's subtasks
	Query       string
	DueFrom     string
	DueTo       string
	Sort        string
	Page        string
	PerPage     string
}

type PageMeta struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

type RecurrenceInput struct {
	Frequency string  `json:"frequency"`
	Interval  int     `json:"interval"`
	Until     *string `json:"until"`
}

type CreateTaskInput struct {
	Title       string           `json:"title"`
	Description *string          `json:"description"`
	Status      string           `json:"status"`
	Priority    string           `json:"priority"`
	DueAt       *string          `json:"due_at"`
	ListID      *uint64          `json:"list_id"`
	ProjectID   *uint64          `json:"project_id"`
	MilestoneID *uint64          `json:"milestone_id"` // implies its project when project_id is omitted
	ParentID    *uint64          `json:"parent_id"`
	Recurrence  *RecurrenceInput `json:"recurrence"`
}

// UpdateTaskInput has no parent_id: a task cannot be moved between parents.
type UpdateTaskInput struct {
	Title       Optional[string]          `json:"title"`
	Description Optional[string]          `json:"description"`
	Status      Optional[string]          `json:"status"`
	Priority    Optional[string]          `json:"priority"`
	DueAt       Optional[string]          `json:"due_at"`
	ListID      Optional[uint64]          `json:"list_id"`
	ProjectID   Optional[uint64]          `json:"project_id"`   // changing it clears a milestone from the old project
	MilestoneID Optional[uint64]          `json:"milestone_id"` // implies its project when project_id is omitted
	Recurrence  Optional[RecurrenceInput] `json:"recurrence"`
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
	f.ListID = parseIDParam(&c, "list_id", in.ListID)
	f.ProjectID = parseIDParam(&c, "project_id", in.ProjectID)
	if in.MilestoneID == "none" {
		f.NoMilestone = true
	} else {
		f.MilestoneID = parseIDParam(&c, "milestone_id", in.MilestoneID)
	}
	f.ParentID = parseIDParam(&c, "parent_id", in.ParentID)
	if in.DueFrom != "" {
		f.DueFrom = c.parseTime("due_from", in.DueFrom)
	}
	if in.DueTo != "" {
		f.DueTo = c.parseTime("due_to", in.DueTo)
	}
	switch {
	case f.Sort == "" && f.ParentID != nil:
		f.Sort = "created_at" // subtasks read as a checklist, in the order added
	case f.Sort == "":
		f.Sort = repository.DefaultTaskSort
	case !repository.IsValidTaskSort(f.Sort):
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
		ProjectID:   in.ProjectID,
		MilestoneID: in.MilestoneID,
		ParentID:    in.ParentID,
	}
	if t.Description != nil {
		c.maxLen("description", *t.Description, 10000)
	}
	c.oneOf("status", t.Status, models.TaskStatuses)
	c.oneOf("priority", t.Priority, models.TaskPriorities)
	if in.DueAt != nil && *in.DueAt != "" {
		t.DueAt = c.parseTime("due_at", *in.DueAt)
	}
	if in.Recurrence != nil {
		t.Recurrence = c.recurrence(*in.Recurrence)
	}

	if t.ParentID != nil {
		if err := s.checkParent(ctx, &c, userID, *t.ParentID); err != nil {
			return models.Task{}, err
		}
		if t.ListID != nil {
			c.fail("list_id", "subtasks use their parent's list")
		}
		if t.Recurrence != nil {
			c.fail("recurrence", "subtasks cannot repeat; make the parent task recurring")
		}
	} else if err := s.checkListOwnership(ctx, &c, userID, t.ListID); err != nil {
		return models.Task{}, err
	}
	if err := s.resolveProject(ctx, &c, userID, &t, in.ProjectID != nil); err != nil {
		return models.Task{}, err
	}
	checkRecurrenceDates(&c, &t)
	if t.Status == models.TaskCompleted && t.Recurrence != nil {
		c.fail("status", "a recurring task must start open")
	}
	if err := c.err(); err != nil {
		return models.Task{}, err
	}

	t.RecurrenceAnchorAt = recurrenceAnchor(t)
	setCompletedAt(&t, "")
	var created models.Task
	err := repository.InTx(ctx, s.Tasks.DB, func(db repository.DBTX) error {
		var err error
		if created, err = (repository.TaskRepository{DB: db}).Create(ctx, t); err != nil {
			return err
		}
		return publish(ctx, s.Events, db, events.Event{
			Type: models.TriggerTaskCreated, UserID: userID, Key: fmt.Sprintf("task.created:%d", created.ID), Data: events.TaskData(ctx, db, created),
		})
	})
	return created, err
}

// Update applies the patch. Completing a recurring task also creates its next
// occurrence (with fresh copies of its subtasks) in the same transaction.
func (s TaskService) Update(ctx context.Context, userID, id uint64, in UpdateTaskInput) (models.Task, error) {
	t, err := s.Tasks.Get(ctx, userID, id)
	if err != nil {
		return t, err
	}
	previousStatus := t.Status
	scheduleChanged := false

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
		scheduleChanged = true
	}
	if in.ListID.Set {
		t.ListID = in.ListID.Ptr()
		if t.ParentID != nil && t.ListID != nil {
			c.fail("list_id", "subtasks use their parent's list")
		} else if err := s.checkListOwnership(ctx, &c, userID, t.ListID); err != nil {
			return models.Task{}, err
		}
	}
	if in.ProjectID.Set || in.MilestoneID.Set {
		previousProject := t.ProjectID
		if in.ProjectID.Set {
			t.ProjectID = in.ProjectID.Ptr()
			if !in.MilestoneID.Set && !sameID(previousProject, t.ProjectID) {
				t.MilestoneID = nil // the old milestone belongs to the old project
			}
		}
		if in.MilestoneID.Set {
			t.MilestoneID = in.MilestoneID.Ptr()
		}
		if err := s.resolveProject(ctx, &c, userID, &t, in.ProjectID.Set); err != nil {
			return models.Task{}, err
		}
	}
	if in.Recurrence.Set {
		t.Recurrence = nil
		if !in.Recurrence.Null {
			t.Recurrence = c.recurrence(in.Recurrence.Value)
		}
		if t.ParentID != nil && t.Recurrence != nil {
			c.fail("recurrence", "subtasks cannot repeat; make the parent task recurring")
		}
		scheduleChanged = true
	}
	checkRecurrenceDates(&c, &t)

	completing := t.Status == models.TaskCompleted && previousStatus != models.TaskCompleted
	if completing && t.Blocked {
		c.fail("status", "cannot complete: waiting on "+openDependencyTitles(t))
	}
	if err := c.err(); err != nil {
		return models.Task{}, err
	}

	if scheduleChanged {
		t.RecurrenceAnchorAt = recurrenceAnchor(t)
	}
	setCompletedAt(&t, previousStatus)

	var updated models.Task
	now := time.Now().UTC()
	err = repository.InTx(ctx, s.Tasks.DB, func(db repository.DBTX) error {
		tasks, reminders := repository.TaskRepository{DB: db}, repository.ReminderRepository{DB: db}
		if completing && t.Recurrence != nil && t.NextTaskID == nil {
			nextID, err := createNextOccurrence(ctx, tasks, reminders, t, now)
			if err != nil {
				return err
			}
			t.NextTaskID = nextID
		}
		var err error
		if updated, err = tasks.Update(ctx, t); err != nil {
			return err
		}
		if in.DueAt.Set {
			if err := rescheduleTaskReminders(ctx, reminders, updated, now); err != nil {
				return err
			}
		}
		if completing {
			return publish(ctx, s.Events, db, events.Event{
				Type: models.TriggerTaskCompleted, UserID: userID, Key: fmt.Sprintf("task.completed:%d:%d", updated.ID, now.UnixMilli()),
				Data: events.TaskData(ctx, db, updated),
			})
		}
		return nil
	})
	return updated, err
}

func (s TaskService) Delete(ctx context.Context, userID, id uint64) error {
	return s.Tasks.Delete(ctx, userID, id)
}

// AddDependency makes taskID wait for dependsOnID. Both must belong to the
// user, and the link must not create a cycle.
func (s TaskService) AddDependency(ctx context.Context, userID, taskID, dependsOnID uint64) (models.Task, error) {
	if _, err := s.Tasks.Get(ctx, userID, taskID); err != nil {
		return models.Task{}, err
	}
	var c checker
	switch _, err := s.Tasks.Get(ctx, userID, dependsOnID); {
	case taskID == dependsOnID:
		c.fail("depends_on_id", "a task cannot depend on itself")
	case errors.Is(err, repository.ErrNotFound):
		c.fail("depends_on_id", "task not found")
	case err != nil:
		return models.Task{}, err
	default:
		graph, err := s.Tasks.DependencyGraph(ctx, userID)
		if err != nil {
			return models.Task{}, err
		}
		if dependsTransitively(graph, dependsOnID, taskID) {
			c.fail("depends_on_id", "would create a circular dependency")
		}
	}
	if err := c.err(); err != nil {
		return models.Task{}, err
	}
	if err := s.Tasks.AddDependency(ctx, taskID, dependsOnID); err != nil {
		return models.Task{}, err
	}
	return s.Tasks.Get(ctx, userID, taskID)
}

func (s TaskService) RemoveDependency(ctx context.Context, userID, taskID, dependsOnID uint64) (models.Task, error) {
	if err := s.Tasks.RemoveDependency(ctx, userID, taskID, dependsOnID); err != nil {
		return models.Task{}, err
	}
	return s.Tasks.Get(ctx, userID, taskID)
}

// ---- Helpers ---------------------------------------------------------------

// createNextOccurrence inserts the follow-up of a completed recurring task and
// copies its subtasks as open items. Returns nil when the series has ended.
func createNextOccurrence(ctx context.Context, tx repository.TaskRepository, reminders repository.ReminderRepository, t models.Task, now time.Time) (*uint64, error) {
	if t.DueAt == nil || t.RecurrenceAnchorAt == nil {
		return nil, nil
	}
	loc, err := userLocation(ctx, tx.DB, t.UserID)
	if err != nil {
		return nil, err
	}
	due, ok := nextOccurrence(*t.Recurrence, *t.RecurrenceAnchorAt, *t.DueAt, now, loc)
	if !ok {
		return nil, nil
	}
	next, err := tx.Create(ctx, models.Task{
		UserID:             t.UserID,
		ListID:             t.ListID,
		ProjectID:          t.ProjectID,
		MilestoneID:        t.MilestoneID,
		Title:              t.Title,
		Description:        t.Description,
		Status:             models.TaskPending,
		Priority:           t.Priority,
		DueAt:              &due,
		Recurrence:         t.Recurrence,
		RecurrenceAnchorAt: t.RecurrenceAnchorAt,
	})
	if err != nil {
		return nil, err
	}

	subtasks, _, err := tx.List(ctx, t.UserID, repository.TaskFilter{ParentID: &t.ID, Sort: "created_at", Limit: maxSubtasks})
	if err != nil {
		return nil, err
	}
	for _, st := range subtasks {
		if _, err := tx.Create(ctx, models.Task{
			UserID:      t.UserID,
			ParentID:    &next.ID,
			Title:       st.Title,
			Description: st.Description,
			Status:      models.TaskPending,
			Priority:    st.Priority,
		}); err != nil {
			return nil, err
		}
	}
	if err := copyTaskReminders(ctx, reminders, t.ID, next, now); err != nil {
		return nil, err
	}
	return &next.ID, nil
}

func (c *checker) recurrence(in RecurrenceInput) *models.Recurrence {
	r := &models.Recurrence{Frequency: in.Frequency, Interval: in.Interval}
	c.oneOf("recurrence.frequency", r.Frequency, models.RecurrenceFrequencies)
	if r.Interval == 0 {
		r.Interval = 1
	}
	if r.Interval < 1 || r.Interval > 365 {
		c.fail("recurrence.interval", "must be between 1 and 365")
	}
	if in.Until != nil && *in.Until != "" {
		r.Until = c.parseTime("recurrence.until", *in.Until)
	}
	return r
}

func checkRecurrenceDates(c *checker, t *models.Task) {
	if t.Recurrence == nil {
		return
	}
	if t.DueAt == nil {
		c.fail("recurrence", "requires a due date")
		return
	}
	if t.Recurrence.Until != nil && t.Recurrence.Until.Before(*t.DueAt) {
		c.fail("recurrence.until", "must be on or after the due date")
	}
}

// recurrenceAnchor is the date a series is computed from: the due date at the
// moment the schedule was (re)defined.
func recurrenceAnchor(t models.Task) *time.Time {
	if t.Recurrence == nil {
		return nil
	}
	return t.DueAt
}

// dependsTransitively reports whether from depends on target directly or
// through other tasks.
func dependsTransitively(graph map[uint64][]uint64, from, target uint64) bool {
	seen := map[uint64]bool{}
	stack := []uint64{from}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if id == target {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, graph[id]...)
	}
	return false
}

func openDependencyTitles(t models.Task) string {
	var titles []string
	for _, d := range t.Dependencies {
		if !models.IsClosedStatus(d.Status) {
			titles = append(titles, `"`+d.Title+`"`)
		}
	}
	return strings.Join(titles, ", ")
}

func (s TaskService) checkParent(ctx context.Context, c *checker, userID, parentID uint64) error {
	parent, err := s.Tasks.Get(ctx, userID, parentID)
	if errors.Is(err, repository.ErrNotFound) {
		c.fail("parent_id", "task not found")
		return nil
	}
	if err != nil {
		return err
	}
	if parent.ParentID != nil {
		c.fail("parent_id", "subtasks cannot have their own subtasks")
	} else if parent.SubtaskCount >= maxSubtasks {
		c.fail("parent_id", "a task can have at most "+strconv.Itoa(maxSubtasks)+" subtasks")
	}
	return nil
}

// resolveProject validates t's project and milestone. A milestone fills in its
// own project when none was given explicitly; an explicit project must match
// the milestone's. Subtasks inherit their parent's project and can't set one.
func (s TaskService) resolveProject(ctx context.Context, c *checker, userID uint64, t *models.Task, projectExplicit bool) error {
	if t.ParentID != nil {
		if t.ProjectID != nil || t.MilestoneID != nil {
			c.fail("project_id", "subtasks use their parent's project")
		}
		return nil
	}
	if t.MilestoneID != nil {
		m, err := s.Projects.GetMilestone(ctx, userID, *t.MilestoneID)
		if errors.Is(err, repository.ErrNotFound) {
			c.fail("milestone_id", "milestone not found")
			return nil
		}
		if err != nil {
			return err
		}
		if projectExplicit && !sameID(t.ProjectID, &m.ProjectID) {
			c.fail("milestone_id", "belongs to a different project")
			return nil
		}
		t.ProjectID = &m.ProjectID
		return nil
	}
	if t.ProjectID != nil {
		_, err := s.Projects.Get(ctx, userID, *t.ProjectID)
		if errors.Is(err, repository.ErrNotFound) {
			c.fail("project_id", "project not found")
			return nil
		}
		return err
	}
	return nil
}

func sameID(a, b *uint64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
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

func parseIDParam(c *checker, field, raw string) *uint64 {
	if raw == "" {
		return nil
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		c.fail(field, "must be an ID")
		return nil
	}
	return &id
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
