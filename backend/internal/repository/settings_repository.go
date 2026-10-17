package repository

import (
	"context"
	"database/sql"
	"errors"

	"lifeos-ai/backend/internal/models"
)

type SettingsRepository struct {
	DB DBTX
}

// Get returns the user's settings, or the defaults if none are saved yet.
func (r SettingsRepository) Get(ctx context.Context, userID uint64) (models.UserSettings, error) {
	s := models.DefaultSettings(userID)
	err := r.DB.QueryRowContext(ctx,
		`SELECT timezone, week_starts_on, quiet_hours_enabled, quiet_hours_start, quiet_hours_end, ai_approval_mode, updated_at
		   FROM user_settings WHERE user_id = ?`, userID).
		Scan(&s.Timezone, &s.WeekStartsOn, &s.QuietHoursEnabled, &s.QuietHoursStart, &s.QuietHoursEnd, &s.AIApprovalMode, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	return s, err
}

// Save inserts or replaces the user's settings row.
func (r SettingsRepository) Save(ctx context.Context, s models.UserSettings) error {
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO user_settings (user_id, timezone, week_starts_on, quiet_hours_enabled, quiet_hours_start, quiet_hours_end, ai_approval_mode)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE timezone = VALUES(timezone), week_starts_on = VALUES(week_starts_on),
		   quiet_hours_enabled = VALUES(quiet_hours_enabled), quiet_hours_start = VALUES(quiet_hours_start),
		   quiet_hours_end = VALUES(quiet_hours_end), ai_approval_mode = VALUES(ai_approval_mode)`,
		s.UserID, s.Timezone, s.WeekStartsOn, s.QuietHoursEnabled, s.QuietHoursStart, s.QuietHoursEnd, s.AIApprovalMode)
	return err
}
