package repository

import (
	"context"
	"database/sql"
	"time"

	"lifeos-ai/backend/internal/models"
)

type SessionRepository struct {
	DB *sql.DB
}

func (r SessionRepository) Create(ctx context.Context, userID uint64, tokenHash, userAgent, ip string, expiresAt time.Time) error {
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO sessions (user_id, token_hash, user_agent, ip_address, expires_at) VALUES (?, ?, ?, ?, ?)`,
		userID, tokenHash, nullIfEmpty(userAgent), nullIfEmpty(ip), expiresAt.UTC())
	return err
}

// FindUserByTokenHash returns the owner of an unexpired session.
func (r SessionRepository) FindUserByTokenHash(ctx context.Context, tokenHash string) (models.User, error) {
	return scanUser(r.DB.QueryRowContext(ctx,
		`SELECT u.id, u.name, u.email, u.password_hash, u.created_at, u.updated_at
		   FROM sessions s
		   JOIN users u ON u.id = s.user_id
		  WHERE s.token_hash = ? AND s.expires_at > UTC_TIMESTAMP()`,
		tokenHash))
}

func (r SessionRepository) DeleteByTokenHash(ctx context.Context, tokenHash string) error {
	_, err := r.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
