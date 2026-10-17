package repository

import (
	"context"
	"database/sql"
	"errors"

	"lifeos-ai/backend/internal/models"
)

type TaskListRepository struct {
	DB *sql.DB
}

const taskListSelect = `
	SELECT l.id, l.user_id, l.name, l.color, l.position, l.created_at, l.updated_at,
	       COUNT(t.id) AS open_task_count
	  FROM task_lists l
	  LEFT JOIN tasks t ON t.list_id = l.id AND t.status IN ('pending', 'in_progress')
	 WHERE l.user_id = ?`

func (r TaskListRepository) List(ctx context.Context, userID uint64) ([]models.TaskList, error) {
	rows, err := r.DB.QueryContext(ctx, taskListSelect+` GROUP BY l.id ORDER BY l.position, l.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lists := []models.TaskList{}
	for rows.Next() {
		l, err := scanTaskList(rows)
		if err != nil {
			return nil, err
		}
		lists = append(lists, l)
	}
	return lists, rows.Err()
}

func (r TaskListRepository) Get(ctx context.Context, userID, id uint64) (models.TaskList, error) {
	l, err := scanTaskList(r.DB.QueryRowContext(ctx, taskListSelect+` AND l.id = ? GROUP BY l.id`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

// Create appends the list after the user's existing lists.
func (r TaskListRepository) Create(ctx context.Context, userID uint64, name string, color *string) (models.TaskList, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO task_lists (user_id, name, color, position)
		 SELECT ?, ?, ?, COALESCE(MAX(position), 0) + 1 FROM task_lists WHERE user_id = ?`,
		userID, name, color, userID)
	if err != nil {
		if isDuplicate(err) {
			return models.TaskList{}, ErrDuplicate
		}
		return models.TaskList{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.TaskList{}, err
	}
	return r.Get(ctx, userID, uint64(id))
}

func (r TaskListRepository) Update(ctx context.Context, l models.TaskList) (models.TaskList, error) {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE task_lists SET name = ?, color = ?, position = ? WHERE id = ? AND user_id = ?`,
		l.Name, l.Color, l.Position, l.ID, l.UserID)
	if err != nil {
		if isDuplicate(err) {
			return models.TaskList{}, ErrDuplicate
		}
		return models.TaskList{}, err
	}
	return r.Get(ctx, l.UserID, l.ID)
}

// Delete removes the list; its tasks stay and lose their list (ON DELETE SET NULL).
func (r TaskListRepository) Delete(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM task_lists WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanTaskList(row rowScanner) (models.TaskList, error) {
	var l models.TaskList
	err := row.Scan(&l.ID, &l.UserID, &l.Name, &l.Color, &l.Position, &l.CreatedAt, &l.UpdatedAt, &l.OpenTaskCount)
	return l, err
}
