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
		`SELECT `+settingsColumns+` FROM user_settings WHERE user_id = ?`, userID).
		Scan(settingsDest(&s)...)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	return s, err
}

// Save inserts or replaces the user's settings row.
func (r SettingsRepository) Save(ctx context.Context, s models.UserSettings) error {
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO user_settings (user_id, timezone, week_starts_on, currency, quiet_hours_enabled, quiet_hours_start, quiet_hours_end, ai_approval_mode,
		                            morning_briefing_enabled, morning_briefing_time, evening_summary_enabled, evening_summary_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE timezone = VALUES(timezone), week_starts_on = VALUES(week_starts_on), currency = VALUES(currency),
		   quiet_hours_enabled = VALUES(quiet_hours_enabled), quiet_hours_start = VALUES(quiet_hours_start),
		   quiet_hours_end = VALUES(quiet_hours_end), ai_approval_mode = VALUES(ai_approval_mode),
		   morning_briefing_enabled = VALUES(morning_briefing_enabled), morning_briefing_time = VALUES(morning_briefing_time),
		   evening_summary_enabled = VALUES(evening_summary_enabled), evening_summary_time = VALUES(evening_summary_time)`,
		s.UserID, s.Timezone, s.WeekStartsOn, s.Currency, s.QuietHoursEnabled, s.QuietHoursStart, s.QuietHoursEnd, s.AIApprovalMode,
		s.MorningBriefingEnabled, s.MorningBriefingTime, s.EveningSummaryEnabled, s.EveningSummaryTime)
	return err
}

const settingsColumns = `user_id, timezone, week_starts_on, currency, quiet_hours_enabled, quiet_hours_start, quiet_hours_end, ai_approval_mode,
	morning_briefing_enabled, morning_briefing_time, evening_summary_enabled, evening_summary_time, updated_at`

func settingsDest(s *models.UserSettings) []any {
	return []any{&s.UserID, &s.Timezone, &s.WeekStartsOn, &s.Currency, &s.QuietHoursEnabled, &s.QuietHoursStart, &s.QuietHoursEnd, &s.AIApprovalMode,
		&s.MorningBriefingEnabled, &s.MorningBriefingTime, &s.EveningSummaryEnabled, &s.EveningSummaryTime, &s.UpdatedAt}
}

// ListBriefingEnabled returns settings of users with an automatic briefing
// turned on.
func (r SettingsRepository) ListBriefingEnabled(ctx context.Context) ([]models.UserSettings, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+settingsColumns+` FROM user_settings WHERE morning_briefing_enabled = 1 OR evening_summary_enabled = 1 ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.UserSettings
	for rows.Next() {
		var s models.UserSettings
		if err := rows.Scan(settingsDest(&s)...); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
