package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"lifeos-ai/backend/internal/models"
)

type TaskRepository struct {
	DB DBTX
}

// InTx runs fn with a repository bound to a single transaction.
func (r TaskRepository) InTx(ctx context.Context, fn func(TaskRepository) error) error {
	return inTx(ctx, r.DB, func(db DBTX) error { return fn(TaskRepository{DB: db}) })
}

type TaskFilter struct {
	Statuses    []string
	Priority    string
	ListID      *uint64
	ProjectID   *uint64
	MilestoneID *uint64
	NoMilestone bool    // tasks with no milestone (e.g. a project's "other tasks")
	ParentID    *uint64 // nil = top-level tasks only
	// IncludeSubtasks ignores ParentID and returns tasks at every level.
	IncludeSubtasks bool
	Query           string
	DueFrom         *time.Time
	DueTo           *time.Time
	CompletedFrom   *time.Time
	CompletedTo     *time.Time
	Sort            string
	Limit           int
	Offset          int
}

// taskOrders is the allowlist of sort keys; user input never reaches SQL.
var taskOrders = map[string]string{
	"due_at":      "t.due_at IS NULL, t.due_at ASC, t.id DESC",
	"-due_at":     "t.due_at IS NULL, t.due_at DESC, t.id DESC",
	"created_at":  "t.created_at ASC, t.id ASC",
	"-created_at": "t.created_at DESC, t.id DESC",
	"priority":    "FIELD(t.priority, 'urgent', 'high', 'medium', 'low'), t.due_at IS NULL, t.due_at ASC, t.id DESC",
}

const DefaultTaskSort = "due_at"

func IsValidTaskSort(s string) bool {
	_, ok := taskOrders[s]
	return ok
}

const taskSelect = `
	SELECT t.id, t.user_id, t.list_id, t.project_id, t.milestone_id, t.parent_id, t.title, t.description, t.status, t.priority,
	       t.due_at, t.carry_count, t.completed_at, t.recurrence, t.recurrence_interval, t.recurrence_until,
	       t.recurrence_anchor_at, t.next_task_id, t.created_at, t.updated_at,
	       (SELECT COUNT(*) FROM tasks s WHERE s.parent_id = t.id),
	       (SELECT COUNT(*) FROM tasks s WHERE s.parent_id = t.id AND s.status = 'completed')
	  FROM tasks t`

func (r TaskRepository) List(ctx context.Context, userID uint64, f TaskFilter) ([]models.Task, int, error) {
	where := []string{"t.user_id = ?"}
	args := []any{userID}

	switch {
	case f.IncludeSubtasks:
	case f.ParentID == nil:
		where = append(where, "t.parent_id IS NULL")
	default:
		where = append(where, "t.parent_id = ?")
		args = append(args, *f.ParentID)
	}
	if len(f.Statuses) > 0 {
		where = append(where, "t.status IN ("+placeholders(len(f.Statuses))+")")
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.Priority != "" {
		where = append(where, "t.priority = ?")
		args = append(args, f.Priority)
	}
	if f.ListID != nil {
		where = append(where, "t.list_id = ?")
		args = append(args, *f.ListID)
	}
	if f.ProjectID != nil {
		where = append(where, "t.project_id = ?")
		args = append(args, *f.ProjectID)
	}
	switch {
	case f.NoMilestone:
		where = append(where, "t.milestone_id IS NULL")
	case f.MilestoneID != nil:
		where = append(where, "t.milestone_id = ?")
		args = append(args, *f.MilestoneID)
	}
	if f.Query != "" {
		pattern := "%" + escapeLike(f.Query) + "%"
		where = append(where, "(t.title LIKE ? OR t.description LIKE ?)")
		args = append(args, pattern, pattern)
	}
	if f.DueFrom != nil {
		where = append(where, "t.due_at >= ?")
		args = append(args, f.DueFrom.UTC())
	}
	if f.DueTo != nil {
		where = append(where, "t.due_at < ?")
		args = append(args, f.DueTo.UTC())
	}
	if f.CompletedFrom != nil {
		where = append(where, "t.completed_at >= ?")
		args = append(args, f.CompletedFrom.UTC())
	}
	if f.CompletedTo != nil {
		where = append(where, "t.completed_at < ?")
		args = append(args, f.CompletedTo.UTC())
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks t WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order, ok := taskOrders[f.Sort]
	if !ok {
		order = taskOrders[DefaultTaskSort]
	}
	rows, err := r.DB.QueryContext(ctx,
		taskSelect+` WHERE `+whereSQL+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	tasks := make([]models.Task, 0, f.Limit)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := r.attachDependencies(ctx, tasks); err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}

func (r TaskRepository) Get(ctx context.Context, userID, id uint64) (models.Task, error) {
	t, err := scanTask(r.DB.QueryRowContext(ctx, taskSelect+` WHERE t.id = ? AND t.user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	one := []models.Task{t}
	err = r.attachDependencies(ctx, one)
	return one[0], err
}

func (r TaskRepository) Create(ctx context.Context, t models.Task) (models.Task, error) {
	freq, interval, until := recurrenceColumns(t.Recurrence)
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO tasks (user_id, list_id, project_id, milestone_id, parent_id, title, description, status, priority,
		                    due_at, completed_at, recurrence, recurrence_interval, recurrence_until, recurrence_anchor_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.UserID, t.ListID, t.ProjectID, t.MilestoneID, t.ParentID, t.Title, t.Description, t.Status, t.Priority, utcPtr(t.DueAt), utcPtr(t.CompletedAt),
		freq, interval, until, utcPtr(t.RecurrenceAnchorAt))
	if err != nil {
		return models.Task{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Task{}, err
	}
	return r.Get(ctx, t.UserID, uint64(id))
}

// Update writes every mutable column of t, scoped to its owner. parent_id is
// immutable.
func (r TaskRepository) Update(ctx context.Context, t models.Task) (models.Task, error) {
	freq, interval, until := recurrenceColumns(t.Recurrence)
	_, err := r.DB.ExecContext(ctx,
		`UPDATE tasks
		    SET list_id = ?, project_id = ?, milestone_id = ?, title = ?, description = ?, status = ?, priority = ?,
		        due_at = ?, completed_at = ?, recurrence = ?, recurrence_interval = ?, recurrence_until = ?,
		        recurrence_anchor_at = ?, next_task_id = ?
		  WHERE id = ? AND user_id = ?`,
		t.ListID, t.ProjectID, t.MilestoneID, t.Title, t.Description, t.Status, t.Priority, utcPtr(t.DueAt), utcPtr(t.CompletedAt),
		freq, interval, until, utcPtr(t.RecurrenceAnchorAt), t.NextTaskID, t.ID, t.UserID)
	if err != nil {
		return models.Task{}, err
	}
	return r.Get(ctx, t.UserID, t.ID)
}

// CarryForward moves an open task to a new due time and counts the move.
// Returns ErrNotFound if the task isn't the user's or is no longer open.
func (r TaskRepository) CarryForward(ctx context.Context, userID, id uint64, due time.Time) error {
	res, err := r.DB.ExecContext(ctx,
		`UPDATE tasks SET due_at = ?, carry_count = carry_count + 1
		  WHERE id = ? AND user_id = ? AND status IN ('pending', 'in_progress')`,
		due.UTC(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes the task; subtasks and dependency links cascade.
func (r TaskRepository) Delete(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM tasks WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- Dependencies ----------------------------------------------------------

// AddDependency records that taskID depends on dependsOnID. Callers must have
// verified both tasks belong to the same user.
func (r TaskRepository) AddDependency(ctx context.Context, taskID, dependsOnID uint64) error {
	_, err := r.DB.ExecContext(ctx,
		`INSERT IGNORE INTO task_dependencies (task_id, depends_on_id) VALUES (?, ?)`, taskID, dependsOnID)
	return err
}

func (r TaskRepository) RemoveDependency(ctx context.Context, userID, taskID, dependsOnID uint64) error {
	res, err := r.DB.ExecContext(ctx,
		`DELETE d FROM task_dependencies d
		   JOIN tasks t ON t.id = d.task_id
		  WHERE d.task_id = ? AND d.depends_on_id = ? AND t.user_id = ?`,
		taskID, dependsOnID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DependencyGraph returns the user's edges as task ID → IDs it depends on.
func (r TaskRepository) DependencyGraph(ctx context.Context, userID uint64) (map[uint64][]uint64, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT d.task_id, d.depends_on_id
		   FROM task_dependencies d
		   JOIN tasks t ON t.id = d.task_id
		  WHERE t.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	graph := map[uint64][]uint64{}
	for rows.Next() {
		var from, to uint64
		if err := rows.Scan(&from, &to); err != nil {
			return nil, err
		}
		graph[from] = append(graph[from], to)
	}
	return graph, rows.Err()
}

// attachDependencies loads each task's dependencies in one query and sets
// Blocked when any of them is still open.
func (r TaskRepository) attachDependencies(ctx context.Context, tasks []models.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	index := make(map[uint64]int, len(tasks))
	args := make([]any, len(tasks))
	for i := range tasks {
		tasks[i].Dependencies = []models.TaskRef{}
		index[tasks[i].ID] = i
		args[i] = tasks[i].ID
	}

	rows, err := r.DB.QueryContext(ctx,
		`SELECT d.task_id, dep.id, dep.title, dep.status
		   FROM task_dependencies d
		   JOIN tasks dep ON dep.id = d.depends_on_id
		  WHERE d.task_id IN (`+placeholders(len(tasks))+`)
		  ORDER BY dep.id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var taskID uint64
		var ref models.TaskRef
		if err := rows.Scan(&taskID, &ref.ID, &ref.Title, &ref.Status); err != nil {
			return err
		}
		t := &tasks[index[taskID]]
		t.Dependencies = append(t.Dependencies, ref)
		if !models.IsClosedStatus(ref.Status) {
			t.Blocked = true
		}
	}
	return rows.Err()
}

// ---- Helpers ---------------------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (models.Task, error) {
	var (
		t        models.Task
		freq     sql.NullString
		interval int
		until    *time.Time
	)
	err := row.Scan(&t.ID, &t.UserID, &t.ListID, &t.ProjectID, &t.MilestoneID, &t.ParentID, &t.Title, &t.Description, &t.Status, &t.Priority,
		&t.DueAt, &t.CarryCount, &t.CompletedAt, &freq, &interval, &until, &t.RecurrenceAnchorAt, &t.NextTaskID,
		&t.CreatedAt, &t.UpdatedAt, &t.SubtaskCount, &t.SubtasksCompleted)
	if freq.Valid {
		t.Recurrence = &models.Recurrence{Frequency: freq.String, Interval: interval, Until: until}
	}
	return t, err
}

func recurrenceColumns(r *models.Recurrence) (freq any, interval int, until any) {
	if r == nil {
		return nil, 1, nil
	}
	return r.Frequency, r.Interval, utcPtr(r.Until)
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func utcPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}
