package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"lifeos-ai/backend/internal/models"
)

type WorkflowRunRepository struct {
	DB DBTX
}

const workflowRunColumns = `id, user_id, automation_id, automation_name, trigger_type, trigger_source, trigger_summary, trigger_data, depth,
	run_key, status, error, steps, attempt, next_retry_at, started_at, finished_at, duration_ms`

// Create inserts a run. If a run with the same key exists (a run job retried
// after a crash), it returns that one instead and created is false.
func (r WorkflowRunRepository) Create(ctx context.Context, run models.WorkflowRun) (models.WorkflowRun, bool, error) {
	steps, err := json.Marshal(run.Steps)
	if err != nil {
		return run, false, err
	}
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO workflow_runs (user_id, automation_id, automation_name, trigger_type, trigger_source, trigger_summary, trigger_data, depth,
		                            run_key, status, error, steps, attempt, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		run.UserID, run.AutomationID, run.AutomationName, run.TriggerType, run.TriggerSource, run.TriggerSummary, jsonArg(run.TriggerData), run.Depth,
		run.RunKey, run.Status, run.Error, string(steps), run.Attempt, run.StartedAt.UTC())
	if err != nil {
		return run, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return run, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return run, false, err
	}
	saved, err := r.Get(ctx, run.UserID, uint64(id))
	return saved, n == 1, err
}

// Save stores a run's progress.
func (r WorkflowRunRepository) Save(ctx context.Context, run models.WorkflowRun) error {
	steps, err := json.Marshal(run.Steps)
	if err != nil {
		return err
	}
	_, err = r.DB.ExecContext(ctx,
		`UPDATE workflow_runs SET status = ?, error = ?, steps = ?, attempt = ?, next_retry_at = ?, finished_at = ?, duration_ms = ?
		  WHERE id = ? AND user_id = ?`,
		run.Status, run.Error, string(steps), run.Attempt, utcPtr(run.NextRetryAt), utcPtr(run.FinishedAt), run.DurationMS, run.ID, run.UserID)
	return err
}

func (r WorkflowRunRepository) Get(ctx context.Context, userID, id uint64) (models.WorkflowRun, error) {
	run, err := scanWorkflowRun(r.DB.QueryRowContext(ctx, `SELECT `+workflowRunColumns+` FROM workflow_runs WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return run, ErrNotFound
	}
	return run, err
}

// GetForUpdate locks the run until the transaction ends, so a manual and an
// automatic retry can't both start.
func (r WorkflowRunRepository) GetForUpdate(ctx context.Context, userID, id uint64) (models.WorkflowRun, error) {
	run, err := scanWorkflowRun(r.DB.QueryRowContext(ctx, `SELECT `+workflowRunColumns+` FROM workflow_runs WHERE id = ? AND user_id = ? FOR UPDATE`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return run, ErrNotFound
	}
	return run, err
}

type WorkflowRunFilter struct {
	AutomationID *uint64
	Statuses     []string
	Limit        int
	Offset       int
}

// List returns the user's runs, newest first, and the total.
func (r WorkflowRunRepository) List(ctx context.Context, userID uint64, f WorkflowRunFilter) ([]models.WorkflowRun, int, error) {
	where, args := "user_id = ?", []any{userID}
	if f.AutomationID != nil {
		where += " AND automation_id = ?"
		args = append(args, *f.AutomationID)
	}
	if len(f.Statuses) > 0 {
		where += " AND status IN (" + placeholders(len(f.Statuses)) + ")"
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_runs WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT `+workflowRunColumns+` FROM workflow_runs WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.WorkflowRun{}
	for rows.Next() {
		run, err := scanWorkflowRun(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, run)
	}
	return out, total, rows.Err()
}

// PruneBefore deletes finished runs that started before t, up to limit.
func (r WorkflowRunRepository) PruneBefore(ctx context.Context, t time.Time, limit int) (int64, error) {
	res, err := r.DB.ExecContext(ctx,
		`DELETE FROM workflow_runs WHERE started_at < ? AND status NOT IN ('running', 'retrying', 'waiting_approval') LIMIT ?`, t.UTC(), limit)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanWorkflowRun(row rowScanner) (models.WorkflowRun, error) {
	var (
		run         models.WorkflowRun
		data, steps []byte
	)
	err := row.Scan(&run.ID, &run.UserID, &run.AutomationID, &run.AutomationName, &run.TriggerType, &run.TriggerSource, &run.TriggerSummary, &data, &run.Depth,
		&run.RunKey, &run.Status, &run.Error, &steps, &run.Attempt, &run.NextRetryAt, &run.StartedAt, &run.FinishedAt, &run.DurationMS)
	if err != nil {
		return run, err
	}
	run.TriggerData = rawJSON(data)
	return run, json.Unmarshal(steps, &run.Steps)
}
