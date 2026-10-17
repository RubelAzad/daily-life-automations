package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"lifeos-ai/backend/internal/models"
)

type CalendarEventRepository struct {
	DB DBTX
}

const eventColumns = `id, user_id, title, description, location, starts_at, ends_at, all_day,
	buffer_before_minutes, busy, color, recurrence, recurrence_interval, recurrence_until, created_at, updated_at`

// ListOverlapping returns one-off events overlapping [from, to) and every
// recurring series that has started before to and not ended before from.
// The caller expands recurring series into occurrences.
func (r CalendarEventRepository) ListOverlapping(ctx context.Context, userID uint64, from, to time.Time) ([]models.CalendarEvent, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+eventColumns+` FROM calendar_events
		  WHERE user_id = ? AND starts_at < ?
		    AND ((recurrence IS NULL AND ends_at > ?)
		      OR (recurrence IS NOT NULL AND (recurrence_until IS NULL OR recurrence_until >= ?)))
		  ORDER BY starts_at, id`,
		userID, to.UTC(), from.UTC(), from.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []models.CalendarEvent{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (r CalendarEventRepository) Get(ctx context.Context, userID, id uint64) (models.CalendarEvent, error) {
	e, err := scanEvent(r.DB.QueryRowContext(ctx,
		`SELECT `+eventColumns+` FROM calendar_events WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

func (r CalendarEventRepository) Create(ctx context.Context, e models.CalendarEvent) (models.CalendarEvent, error) {
	freq, interval, until := recurrenceColumns(e.Recurrence)
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO calendar_events (user_id, title, description, location, starts_at, ends_at, all_day,
		                              buffer_before_minutes, busy, color, recurrence, recurrence_interval, recurrence_until)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.UserID, e.Title, e.Description, e.Location, e.StartsAt.UTC(), e.EndsAt.UTC(), e.AllDay,
		e.BufferBeforeMinutes, e.Busy, e.Color, freq, interval, until)
	if err != nil {
		return models.CalendarEvent{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.CalendarEvent{}, err
	}
	return r.Get(ctx, e.UserID, uint64(id))
}

func (r CalendarEventRepository) Update(ctx context.Context, e models.CalendarEvent) (models.CalendarEvent, error) {
	freq, interval, until := recurrenceColumns(e.Recurrence)
	_, err := r.DB.ExecContext(ctx,
		`UPDATE calendar_events
		    SET title = ?, description = ?, location = ?, starts_at = ?, ends_at = ?, all_day = ?,
		        buffer_before_minutes = ?, busy = ?, color = ?, recurrence = ?, recurrence_interval = ?, recurrence_until = ?
		  WHERE id = ? AND user_id = ?`,
		e.Title, e.Description, e.Location, e.StartsAt.UTC(), e.EndsAt.UTC(), e.AllDay,
		e.BufferBeforeMinutes, e.Busy, e.Color, freq, interval, until, e.ID, e.UserID)
	if err != nil {
		return models.CalendarEvent{}, err
	}
	return r.Get(ctx, e.UserID, e.ID)
}

func (r CalendarEventRepository) Delete(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM calendar_events WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanEvent(row rowScanner) (models.CalendarEvent, error) {
	var (
		e        models.CalendarEvent
		freq     sql.NullString
		interval int
		until    *time.Time
	)
	err := row.Scan(&e.ID, &e.UserID, &e.Title, &e.Description, &e.Location, &e.StartsAt, &e.EndsAt, &e.AllDay,
		&e.BufferBeforeMinutes, &e.Busy, &e.Color, &freq, &interval, &until, &e.CreatedAt, &e.UpdatedAt)
	if freq.Valid {
		e.Recurrence = &models.Recurrence{Frequency: freq.String, Interval: interval, Until: until}
	}
	e.SetDates()
	return e, err
}
