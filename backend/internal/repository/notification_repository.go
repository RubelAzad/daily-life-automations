package repository

import (
	"context"

	"lifeos-ai/backend/internal/models"
)

type NotificationRepository struct {
	DB DBTX
}

// Insert stores n unless a notification with the same idempotency key
// already exists. Returns whether a new row was created.
func (r NotificationRepository) Insert(ctx context.Context, n models.Notification) (bool, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT IGNORE INTO notifications (user_id, kind, title, body, link, priority, reminder_id, occurs_at, idempotency_key)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.UserID, n.Kind, n.Title, n.Body, n.Link, n.Priority, n.ReminderID, utcPtr(n.OccursAt), n.IdempotencyKey)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected == 1, err
}

func (r NotificationRepository) List(ctx context.Context, userID uint64, unreadOnly bool, limit, offset int) ([]models.Notification, int, error) {
	where := `user_id = ?`
	if unreadOnly {
		where += ` AND read_at IS NULL`
	}
	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE `+where, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.DB.QueryContext(ctx,
		`SELECT id, user_id, kind, title, body, link, priority, reminder_id, occurs_at, read_at, created_at
		   FROM notifications WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Notification{}
	for rows.Next() {
		var n models.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.Priority, &n.ReminderID,
			&n.OccursAt, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	return out, total, rows.Err()
}

func (r NotificationRepository) UnreadCount(ctx context.Context, userID uint64) (int, error) {
	var n int
	err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

func (r NotificationRepository) MarkRead(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx,
		`UPDATE notifications SET read_at = COALESCE(read_at, UTC_TIMESTAMP()) WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	// RowsAffected is 0 both for "not found" and "already read"; tell them apart.
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE id = ? AND user_id = ?`, id, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func (r NotificationRepository) MarkAllRead(ctx context.Context, userID uint64) (int64, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE notifications SET read_at = UTC_TIMESTAMP() WHERE user_id = ? AND read_at IS NULL`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
