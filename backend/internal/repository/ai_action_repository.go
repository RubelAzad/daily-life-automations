package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"lifeos-ai/backend/internal/models"
)

type AIActionRepository struct {
	DB DBTX
}

type AIActionFilter struct {
	Statuses       []string
	Kind           string
	ConversationID *uint64
	AutomationID   *uint64
	// CreatedAfter leaves out older entries (pending ones past their expiry).
	CreatedAfter *time.Time
	// Decided keeps only tool calls the user approved or rejected, or that
	// expired, newest decision first.
	Decided bool
	// OldestFirst orders by creation, oldest first.
	OldestFirst bool
	Limit       int
	Offset      int
}

const aiActionColumns = `id, user_id, kind, source, conversation_id, automation_id, workflow_run_id, idempotency_key, tool, risk, summary, input, pending_input, output, status, error,
	model, input_tokens, output_tokens, duration_ms, decided_at, executed_at, created_at, updated_at`

func (r AIActionRepository) Create(ctx context.Context, a models.AIAction) (uint64, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO ai_actions (user_id, kind, source, conversation_id, automation_id, workflow_run_id, idempotency_key, tool, risk, summary, input, pending_input, output, status, error,
		                         model, input_tokens, output_tokens, duration_ms, decided_at, executed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.UserID, a.Kind, a.Source, a.ConversationID, a.AutomationID, a.WorkflowRunID, a.IdempotencyKey, a.Tool, a.Risk, a.Summary, jsonArg(a.Input), jsonArg(a.PendingInput), jsonArg(a.Output),
		a.Status, a.Error, a.Model, a.InputTokens, a.OutputTokens, a.DurationMS, utcPtr(a.DecidedAt), utcPtr(a.ExecutedAt))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return uint64(id), err
}

// GetByKey finds the action recorded under an idempotency key.
func (r AIActionRepository) GetByKey(ctx context.Context, userID uint64, key string) (models.AIAction, error) {
	a, err := scanAIAction(r.DB.QueryRowContext(ctx,
		`SELECT `+aiActionColumns+` FROM ai_actions WHERE idempotency_key = ? AND user_id = ?`, key, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// CountForAutomationSince counts the tool calls an automation made since t,
// for its rate limit.
func (r AIActionRepository) CountForAutomationSince(ctx context.Context, automationID uint64, t time.Time) (int, error) {
	var n int
	err := r.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ai_actions WHERE automation_id = ? AND created_at >= ?`, automationID, t.UTC()).Scan(&n)
	return n, err
}

func (r AIActionRepository) Get(ctx context.Context, userID, id uint64) (models.AIAction, error) {
	a, err := scanAIAction(r.DB.QueryRowContext(ctx,
		`SELECT `+aiActionColumns+` FROM ai_actions WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// List returns the newest entries first.
func (r AIActionRepository) List(ctx context.Context, userID uint64, f AIActionFilter) ([]models.AIAction, int, error) {
	where := []string{"user_id = ?"}
	args := []any{userID}
	if len(f.Statuses) > 0 {
		where = append(where, "status IN ("+placeholders(len(f.Statuses))+")")
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, f.Kind)
	}
	if f.ConversationID != nil {
		where = append(where, "conversation_id = ?")
		args = append(args, *f.ConversationID)
	}
	if f.AutomationID != nil {
		where = append(where, "automation_id = ?")
		args = append(args, *f.AutomationID)
	}
	if f.CreatedAfter != nil {
		where = append(where, "created_at >= ?")
		args = append(args, f.CreatedAfter.UTC())
	}
	order := "id DESC"
	switch {
	case f.Decided:
		where = append(where, "kind = 'tool' AND decided_at IS NOT NULL")
		order = "decided_at DESC, id DESC"
	case f.OldestFirst:
		order = "id"
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_actions WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+aiActionColumns+` FROM ai_actions WHERE `+whereSQL+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.AIAction{}
	for rows.Next() {
		a, err := scanAIAction(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// CountPending counts a user's tool calls waiting for approval that were
// proposed at or after notBefore (older ones have expired).
func (r AIActionRepository) CountPending(ctx context.Context, userID uint64, notBefore time.Time) (int, error) {
	var n int
	err := r.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ai_actions WHERE user_id = ? AND status = 'pending_approval' AND created_at >= ?`,
		userID, notBefore.UTC()).Scan(&n)
	return n, err
}

// ListStale returns up to limit actions, of any user, still waiting for
// approval though proposed before cutoff: they have expired.
func (r AIActionRepository) ListStale(ctx context.Context, cutoff time.Time, limit int) ([]models.AIAction, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+aiActionColumns+` FROM ai_actions WHERE status = 'pending_approval' AND created_at < ? ORDER BY created_at LIMIT ?`,
		cutoff.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.AIAction{}
	for rows.Next() {
		a, err := scanAIAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ClaimPending atomically moves a pending action created at or after
// notBefore to "running", so it runs at most once even if approved twice at
// the same moment. ok is false if it was not pending, or had expired.
func (r AIActionRepository) ClaimPending(ctx context.Context, userID, id uint64, notBefore, now time.Time) (bool, error) {
	res, err := r.DB.ExecContext(ctx,
		`UPDATE ai_actions SET status = 'running', decided_at = ?
		  WHERE id = ? AND user_id = ? AND status = 'pending_approval' AND created_at >= ?`,
		now.UTC(), id, userID, notBefore.UTC())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Decide gives a pending action its final status without running it
// (rejected or expired) and drops its stored input.
func (r AIActionRepository) Decide(ctx context.Context, userID, id uint64, status string, now time.Time) (bool, error) {
	res, err := r.DB.ExecContext(ctx,
		`UPDATE ai_actions SET status = ?, decided_at = ?, pending_input = NULL
		  WHERE id = ? AND user_id = ? AND status = 'pending_approval'`,
		status, now.UTC(), id, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ListDecidedSince returns a conversation's tool actions that the user
// approved, rejected or that expired after since, oldest first.
func (r AIActionRepository) ListDecidedSince(ctx context.Context, userID, conversationID uint64, since time.Time) ([]models.AIAction, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+aiActionColumns+` FROM ai_actions
		  WHERE user_id = ? AND conversation_id = ? AND kind = 'tool' AND decided_at > ?
		  ORDER BY decided_at, id LIMIT 50`,
		userID, conversationID, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.AIAction{}
	for rows.Next() {
		a, err := scanAIAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Finish records the outcome of an approved action and drops its stored input.
func (r AIActionRepository) Finish(ctx context.Context, a models.AIAction) error {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE ai_actions SET status = ?, output = ?, error = ?, duration_ms = ?, executed_at = ?, pending_input = NULL
		  WHERE id = ? AND user_id = ?`,
		a.Status, jsonArg(a.Output), a.Error, a.DurationMS, utcPtr(a.ExecutedAt), a.ID, a.UserID)
	return err
}

func scanAIAction(row rowScanner) (models.AIAction, error) {
	var (
		a                      models.AIAction
		input, pending, output []byte
	)
	err := row.Scan(&a.ID, &a.UserID, &a.Kind, &a.Source, &a.ConversationID, &a.AutomationID, &a.WorkflowRunID, &a.IdempotencyKey, &a.Tool, &a.Risk, &a.Summary, &input, &pending, &output, &a.Status, &a.Error,
		&a.Model, &a.InputTokens, &a.OutputTokens, &a.DurationMS, &a.DecidedAt, &a.ExecutedAt, &a.CreatedAt, &a.UpdatedAt)
	a.Input, a.PendingInput, a.Output = rawJSON(input), rawJSON(pending), rawJSON(output)
	return a, err
}

func jsonArg(v json.RawMessage) any {
	if len(v) == 0 {
		return nil
	}
	return string(v)
}

// rawJSON compacts a JSON column (MySQL returns it re-formatted with spaces).
func rawJSON(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if json.Compact(&buf, b) != nil {
		return json.RawMessage(b)
	}
	return json.RawMessage(buf.Bytes())
}
