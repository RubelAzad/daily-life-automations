package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"lifeos-ai/backend/internal/models"
)

type BriefingRepository struct {
	DB DBTX
}

const briefingColumns = `id, user_id, kind, briefing_date, trigger_source, status, content, error, model, created_at, updated_at`

// Start records a briefing that is being generated. For scheduled ones it
// returns created=false if that day's scheduled briefing already exists, so
// two workers can't both generate it.
func (r BriefingRepository) Start(ctx context.Context, userID uint64, kind string, date models.Date, scheduled bool) (id uint64, created bool, err error) {
	trigger, slot := "manual", any(nil)
	if scheduled {
		trigger, slot = "scheduled", 1
	}
	res, err := r.DB.ExecContext(ctx,
		`INSERT IGNORE INTO ai_briefings (user_id, kind, briefing_date, trigger_source, scheduled_slot, status)
		 VALUES (?, ?, ?, ?, ?, 'generating')`, userID, kind, date, trigger, slot)
	if err != nil {
		return 0, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, false, nil
	}
	lastID, err := res.LastInsertId()
	return uint64(lastID), true, err
}

// Finish stores the outcome: content when ready, an error when failed.
func (r BriefingRepository) Finish(ctx context.Context, id uint64, status string, content json.RawMessage, errMsg, model *string) error {
	var c any
	if len(content) > 0 {
		c = string(content)
	}
	_, err := r.DB.ExecContext(ctx,
		`UPDATE ai_briefings SET status = ?, content = ?, error = ?, model = ? WHERE id = ?`, status, c, errMsg, model, id)
	return err
}

func (r BriefingRepository) Get(ctx context.Context, userID, id uint64) (models.Briefing, error) {
	b, err := scanBriefing(r.DB.QueryRowContext(ctx, `SELECT `+briefingColumns+` FROM ai_briefings WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// Latest returns the newest briefing of a kind for a date, or ErrNotFound.
func (r BriefingRepository) Latest(ctx context.Context, userID uint64, kind string, date models.Date) (models.Briefing, error) {
	b, err := scanBriefing(r.DB.QueryRowContext(ctx,
		`SELECT `+briefingColumns+` FROM ai_briefings WHERE user_id = ? AND kind = ? AND briefing_date = ? ORDER BY id DESC LIMIT 1`,
		userID, kind, date))
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// SetContent replaces a briefing's content, e.g. when a task in today's
// plan is moved.
func (r BriefingRepository) SetContent(ctx context.Context, userID, id uint64, content json.RawMessage) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE ai_briefings SET content = ? WHERE id = ? AND user_id = ?`, string(content), id, userID)
	return err
}

// List returns briefings newest first.
func (r BriefingRepository) List(ctx context.Context, userID uint64, limit, offset int) ([]models.Briefing, int, error) {
	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_briefings WHERE user_id = ?`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+briefingColumns+` FROM ai_briefings WHERE user_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Briefing{}
	for rows.Next() {
		b, err := scanBriefing(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, b)
	}
	return out, total, rows.Err()
}

func scanBriefing(row rowScanner) (models.Briefing, error) {
	var (
		b       models.Briefing
		content []byte
	)
	err := row.Scan(&b.ID, &b.UserID, &b.Kind, &b.Date, &b.Trigger, &b.Status, &content, &b.Error, &b.Model, &b.CreatedAt, &b.UpdatedAt)
	b.Content = normalizeBriefing(b.Kind, rawJSON(content))
	return b, err
}

// normalizeBriefing re-encodes content through its struct: MySQL's JSON type
// reorders keys, and this keeps the documented field order.
func normalizeBriefing(kind string, raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	var v any
	switch kind {
	case models.BriefingMorning:
		v = &models.MorningPlan{}
	case models.BriefingEvening:
		v = &models.EveningSummary{}
	default:
		return raw
	}
	if json.Unmarshal(raw, v) != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}
