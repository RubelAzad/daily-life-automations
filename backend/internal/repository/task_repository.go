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
	DB *sql.DB
}

type TaskFilter struct {
	Statuses []string
	Priority string
	ListID   *uint64
	Query    string
	DueFrom  *time.Time
	DueTo    *time.Time
	Sort     string
	Limit    int
	Offset   int
}

// taskOrders is the allowlist of sort keys; user input never reaches SQL.
var taskOrders = map[string]string{
	"due_at":      "due_at IS NULL, due_at ASC, id DESC",
	"-due_at":     "due_at IS NULL, due_at DESC, id DESC",
	"created_at":  "created_at ASC, id ASC",
	"-created_at": "created_at DESC, id DESC",
	"priority":    "FIELD(priority, 'urgent', 'high', 'medium', 'low'), due_at IS NULL, due_at ASC, id DESC",
}

const DefaultTaskSort = "due_at"

func IsValidTaskSort(s string) bool {
	_, ok := taskOrders[s]
	return ok
}

const taskColumns = `id, user_id, list_id, title, description, status, priority, due_at, completed_at, created_at, updated_at`

func (r TaskRepository) List(ctx context.Context, userID uint64, f TaskFilter) ([]models.Task, int, error) {
	where := []string{"user_id = ?"}
	args := []any{userID}

	if len(f.Statuses) > 0 {
		where = append(where, "status IN ("+placeholders(len(f.Statuses))+")")
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.Priority != "" {
		where = append(where, "priority = ?")
		args = append(args, f.Priority)
	}
	if f.ListID != nil {
		where = append(where, "list_id = ?")
		args = append(args, *f.ListID)
	}
	if f.Query != "" {
		pattern := "%" + escapeLike(f.Query) + "%"
		where = append(where, "(title LIKE ? OR description LIKE ?)")
		args = append(args, pattern, pattern)
	}
	if f.DueFrom != nil {
		where = append(where, "due_at >= ?")
		args = append(args, f.DueFrom.UTC())
	}
	if f.DueTo != nil {
		where = append(where, "due_at < ?")
		args = append(args, f.DueTo.UTC())
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order, ok := taskOrders[f.Sort]
	if !ok {
		order = taskOrders[DefaultTaskSort]
	}
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE `+whereSQL+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
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
	return tasks, total, rows.Err()
}

func (r TaskRepository) Get(ctx context.Context, userID, id uint64) (models.Task, error) {
	t, err := scanTask(r.DB.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (r TaskRepository) Create(ctx context.Context, t models.Task) (models.Task, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO tasks (user_id, list_id, title, description, status, priority, due_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.UserID, t.ListID, t.Title, t.Description, t.Status, t.Priority, utcPtr(t.DueAt), utcPtr(t.CompletedAt))
	if err != nil {
		return models.Task{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Task{}, err
	}
	return r.Get(ctx, t.UserID, uint64(id))
}

// Update writes every mutable column of t, scoped to its owner.
func (r TaskRepository) Update(ctx context.Context, t models.Task) (models.Task, error) {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE tasks
		    SET list_id = ?, title = ?, description = ?, status = ?, priority = ?, due_at = ?, completed_at = ?
		  WHERE id = ? AND user_id = ?`,
		t.ListID, t.Title, t.Description, t.Status, t.Priority, utcPtr(t.DueAt), utcPtr(t.CompletedAt), t.ID, t.UserID)
	if err != nil {
		return models.Task{}, err
	}
	return r.Get(ctx, t.UserID, t.ID)
}

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

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (models.Task, error) {
	var t models.Task
	err := row.Scan(&t.ID, &t.UserID, &t.ListID, &t.Title, &t.Description, &t.Status, &t.Priority,
		&t.DueAt, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt)
	return t, err
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
