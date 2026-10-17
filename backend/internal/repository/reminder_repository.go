package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"lifeos-ai/backend/internal/models"
)

type ReminderRepository struct {
	DB DBTX
}

type ReminderFilter struct {
	Statuses []string
	TaskID   *uint64
	EventID  *uint64
	NoteID   *uint64
	Limit    int
	Offset   int
}

const reminderSelect = `
	SELECT r.id, r.user_id, r.task_id, r.event_id, r.note_id, r.title, r.note, r.offset_minutes, r.remind_at, r.occurs_at,
	       r.follow_up_minutes, r.max_follow_ups, r.follow_ups_sent, r.status, r.last_sent_at, r.created_at, r.updated_at,
	       COALESCE(r.title, t.title, e.title, nt.title, '')
	  FROM reminders r
	  LEFT JOIN tasks t ON t.id = r.task_id
	  LEFT JOIN calendar_events e ON e.id = r.event_id
	  LEFT JOIN notes nt ON nt.id = r.note_id`

// List orders by next fire time; inactive reminders (no remind_at) last.
func (r ReminderRepository) List(ctx context.Context, userID uint64, f ReminderFilter) ([]models.Reminder, int, error) {
	where := []string{"r.user_id = ?"}
	args := []any{userID}
	if len(f.Statuses) > 0 {
		where = append(where, "r.status IN ("+placeholders(len(f.Statuses))+")")
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.TaskID != nil {
		where = append(where, "r.task_id = ?")
		args = append(args, *f.TaskID)
	}
	if f.EventID != nil {
		where = append(where, "r.event_id = ?")
		args = append(args, *f.EventID)
	}
	if f.NoteID != nil {
		where = append(where, "r.note_id = ?")
		args = append(args, *f.NoteID)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reminders r WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.DB.QueryContext(ctx,
		reminderSelect+` WHERE `+whereSQL+` ORDER BY r.remind_at IS NULL, r.remind_at, r.id LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	reminders := []models.Reminder{}
	for rows.Next() {
		rem, err := scanReminder(rows)
		if err != nil {
			return nil, 0, err
		}
		reminders = append(reminders, rem)
	}
	return reminders, total, rows.Err()
}

func (r ReminderRepository) Get(ctx context.Context, userID, id uint64) (models.Reminder, error) {
	rem, err := scanReminder(r.DB.QueryRowContext(ctx, reminderSelect+` WHERE r.id = ? AND r.user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return rem, ErrNotFound
	}
	return rem, err
}

// ListRelativeForTask / ListRelativeForEvent return the offset-based reminders
// that must move when the task's due time or the event's schedule changes.
// Callers must have verified ownership of the task/event.
func (r ReminderRepository) ListRelativeForTask(ctx context.Context, taskID uint64) ([]models.Reminder, error) {
	return r.listWhere(ctx, `r.task_id = ? AND r.offset_minutes IS NOT NULL AND r.status <> 'cancelled'`, taskID)
}

func (r ReminderRepository) ListRelativeForEvent(ctx context.Context, eventID uint64) ([]models.Reminder, error) {
	return r.listWhere(ctx, `r.event_id = ? AND r.offset_minutes IS NOT NULL AND r.status <> 'cancelled'`, eventID)
}

func (r ReminderRepository) ListRelativeForNote(ctx context.Context, noteID uint64) ([]models.Reminder, error) {
	return r.listWhere(ctx, `r.note_id = ? AND r.offset_minutes IS NOT NULL AND r.status <> 'cancelled'`, noteID)
}

func (r ReminderRepository) listWhere(ctx context.Context, cond string, args ...any) ([]models.Reminder, error) {
	rows, err := r.DB.QueryContext(ctx, reminderSelect+` WHERE `+cond+` ORDER BY r.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Reminder{}
	for rows.Next() {
		rem, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rem)
	}
	return out, rows.Err()
}

func (r ReminderRepository) Create(ctx context.Context, rem models.Reminder) (models.Reminder, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO reminders (user_id, task_id, event_id, note_id, title, note, offset_minutes, remind_at, occurs_at,
		                        follow_up_minutes, max_follow_ups, follow_ups_sent, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rem.UserID, rem.TaskID, rem.EventID, rem.NoteID, rem.Title, rem.Note, rem.OffsetMinutes, utcPtr(rem.RemindAt), utcPtr(rem.OccursAt),
		rem.FollowUpMinutes, rem.MaxFollowUps, rem.FollowUpsSent, rem.Status)
	if err != nil {
		return models.Reminder{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Reminder{}, err
	}
	return r.Get(ctx, rem.UserID, uint64(id))
}

// Update writes every mutable column; task_id/event_id are immutable.
func (r ReminderRepository) Update(ctx context.Context, rem models.Reminder) error {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE reminders
		    SET title = ?, note = ?, offset_minutes = ?, remind_at = ?, occurs_at = ?, follow_up_minutes = ?,
		        max_follow_ups = ?, follow_ups_sent = ?, status = ?, last_sent_at = ?
		  WHERE id = ? AND user_id = ?`,
		rem.Title, rem.Note, rem.OffsetMinutes, utcPtr(rem.RemindAt), utcPtr(rem.OccursAt), rem.FollowUpMinutes,
		rem.MaxFollowUps, rem.FollowUpsSent, rem.Status, utcPtr(rem.LastSentAt), rem.ID, rem.UserID)
	return err
}

func (r ReminderRepository) Delete(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM reminders WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DueIDs returns up to limit reminders ready to fire at now, oldest first.
func (r ReminderRepository) DueIDs(ctx context.Context, now time.Time, limit int) ([]uint64, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT id FROM reminders WHERE status = 'scheduled' AND remind_at <= ? ORDER BY remind_at, id LIMIT ?`,
		now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uint64
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// LockDue locks one reminder for processing if it is still due. Must run in a
// transaction. SKIP LOCKED lets concurrent workers pass over rows another
// worker holds; ErrNotFound means "already handled or taken".
func (r ReminderRepository) LockDue(ctx context.Context, id uint64, now time.Time) (models.Reminder, error) {
	rem, err := scanReminder(r.DB.QueryRowContext(ctx,
		reminderSelect+` WHERE r.id = ? AND r.status = 'scheduled' AND r.remind_at <= ? FOR UPDATE OF r SKIP LOCKED`,
		id, now.UTC()))
	if errors.Is(err, sql.ErrNoRows) {
		return rem, ErrNotFound
	}
	return rem, err
}

func scanReminder(row rowScanner) (models.Reminder, error) {
	var rem models.Reminder
	err := row.Scan(&rem.ID, &rem.UserID, &rem.TaskID, &rem.EventID, &rem.NoteID, &rem.Title, &rem.Note, &rem.OffsetMinutes,
		&rem.RemindAt, &rem.OccursAt, &rem.FollowUpMinutes, &rem.MaxFollowUps, &rem.FollowUpsSent, &rem.Status,
		&rem.LastSentAt, &rem.CreatedAt, &rem.UpdatedAt, &rem.SubjectTitle)
	rem.SetKind()
	return rem, err
}

// ListScheduledRelativeForUser returns the user's pending offset-based event
// and note reminders, whose fire times depend on the user's time zone.
func (r ReminderRepository) ListScheduledRelativeForUser(ctx context.Context, userID uint64) ([]models.Reminder, error) {
	return r.listWhere(ctx, `r.user_id = ? AND r.offset_minutes IS NOT NULL AND r.status = 'scheduled'
		AND (r.event_id IS NOT NULL OR r.note_id IS NOT NULL)`, userID)
}
