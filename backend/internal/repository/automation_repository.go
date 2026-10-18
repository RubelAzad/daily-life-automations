package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"lifeos-ai/backend/internal/models"
)

type AutomationRepository struct {
	DB DBTX
}

const automationColumns = `id, user_id, template_key, name, enabled, trigger_type, trigger_config, conditions, actions,
	next_run_at, last_run_at, last_status, last_error, run_count, created_at, updated_at`

func (r AutomationRepository) Create(ctx context.Context, a models.Automation) (uint64, error) {
	schedule, conditions, actions, err := automationJSON(a)
	if err != nil {
		return 0, err
	}
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO automations (user_id, template_key, name, enabled, trigger_type, trigger_config, conditions, actions, next_run_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.UserID, a.TemplateKey, a.Name, a.Enabled, a.Trigger.Type, schedule, conditions, actions, utcPtr(a.NextRunAt))
	if isDuplicate(err) {
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return uint64(id), err
}

// Update saves the user-editable fields and the next run.
func (r AutomationRepository) Update(ctx context.Context, a models.Automation) error {
	schedule, conditions, actions, err := automationJSON(a)
	if err != nil {
		return err
	}
	_, err = r.DB.ExecContext(ctx,
		`UPDATE automations SET name = ?, enabled = ?, trigger_type = ?, trigger_config = ?, conditions = ?, actions = ?, next_run_at = ?
		  WHERE id = ? AND user_id = ?`,
		a.Name, a.Enabled, a.Trigger.Type, schedule, conditions, actions, utcPtr(a.NextRunAt), a.ID, a.UserID)
	return err
}

func automationJSON(a models.Automation) (schedule, conditions, actions any, err error) {
	if a.Trigger.Schedule != nil {
		b, err := json.Marshal(a.Trigger.Schedule)
		if err != nil {
			return nil, nil, nil, err
		}
		schedule = string(b)
	}
	if a.Conditions == nil {
		a.Conditions = []models.Condition{}
	}
	c, err := json.Marshal(a.Conditions)
	if err != nil {
		return nil, nil, nil, err
	}
	ac, err := json.Marshal(a.Actions)
	if err != nil {
		return nil, nil, nil, err
	}
	return schedule, string(c), string(ac), nil
}

func (r AutomationRepository) Get(ctx context.Context, userID, id uint64) (models.Automation, error) {
	a, err := scanAutomation(r.DB.QueryRowContext(ctx, `SELECT `+automationColumns+` FROM automations WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// GetForUpdate locks the automation until the transaction ends, so two runs
// of one schedule can't both move it on.
func (r AutomationRepository) GetForUpdate(ctx context.Context, userID, id uint64) (models.Automation, error) {
	a, err := scanAutomation(r.DB.QueryRowContext(ctx, `SELECT `+automationColumns+` FROM automations WHERE id = ? AND user_id = ? FOR UPDATE`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (r AutomationRepository) List(ctx context.Context, userID uint64) ([]models.Automation, error) {
	return r.query(ctx, `SELECT `+automationColumns+` FROM automations WHERE user_id = ? ORDER BY name, id`, userID)
}

// EnabledFor returns the user's enabled automations with this trigger.
func (r AutomationRepository) EnabledFor(ctx context.Context, userID uint64, trigger string) ([]models.Automation, error) {
	return r.query(ctx, `SELECT `+automationColumns+` FROM automations WHERE user_id = ? AND enabled = 1 AND trigger_type = ? ORDER BY id`, userID, trigger)
}

// HasEnabled reports whether the user has an enabled automation with this
// trigger, so events nobody listens to aren't queued at all.
func (r AutomationRepository) HasEnabled(ctx context.Context, userID uint64, trigger string) (bool, error) {
	var one int
	err := r.DB.QueryRowContext(ctx,
		`SELECT 1 FROM automations WHERE user_id = ? AND enabled = 1 AND trigger_type = ? LIMIT 1`, userID, trigger).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (r AutomationRepository) Count(ctx context.Context, userID uint64) (int, error) {
	var n int
	err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automations WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

func (r AutomationRepository) Delete(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM automations WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetNextRun moves a schedule on (nil when a one-time schedule is done).
func (r AutomationRepository) SetNextRun(ctx context.Context, id uint64, next *time.Time) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE automations SET next_run_at = ?, updated_at = updated_at WHERE id = ?`, utcPtr(next), id)
	return err
}

// RecordRun stores the outcome of a run, or of a retry of one (newRun false:
// not counted again).
func (r AutomationRepository) RecordRun(ctx context.Context, id uint64, at time.Time, status string, errMsg *string, newRun bool) error {
	add := 0
	if newRun {
		add = 1
	}
	_, err := r.DB.ExecContext(ctx,
		`UPDATE automations SET last_run_at = ?, last_status = ?, last_error = ?, run_count = run_count + ?, updated_at = updated_at WHERE id = ?`,
		at.UTC(), status, errMsg, add, id)
	return err
}

// SetStatusIfLatest updates an automation's last status when runID is its
// latest run, e.g. once the steps it was waiting on are approved.
func (r AutomationRepository) SetStatusIfLatest(ctx context.Context, id, runID uint64, status string, errMsg *string) error {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE automations SET last_status = ?, last_error = ?, updated_at = updated_at
		  WHERE id = ? AND NOT EXISTS (SELECT 1 FROM workflow_runs WHERE automation_id = ? AND id > ?)`,
		status, errMsg, id, id, runID)
	return err
}

func (r AutomationRepository) query(ctx context.Context, q string, args ...any) ([]models.Automation, error) {
	rows, err := r.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Automation{}
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanAutomation(row rowScanner) (models.Automation, error) {
	var (
		a                             models.Automation
		schedule, conditions, actions []byte
	)
	err := row.Scan(&a.ID, &a.UserID, &a.TemplateKey, &a.Name, &a.Enabled, &a.Trigger.Type, &schedule, &conditions, &actions,
		&a.NextRunAt, &a.LastRunAt, &a.LastStatus, &a.LastError, &a.RunCount, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return a, err
	}
	if len(schedule) > 0 {
		a.Trigger.Schedule = &models.Schedule{}
		if err := json.Unmarshal(schedule, a.Trigger.Schedule); err != nil {
			return a, err
		}
	}
	if err := json.Unmarshal(conditions, &a.Conditions); err != nil {
		return a, err
	}
	return a, json.Unmarshal(actions, &a.Actions)
}
