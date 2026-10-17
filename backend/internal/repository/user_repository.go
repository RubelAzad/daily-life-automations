package repository

import (
	"context"
	"database/sql"
	"errors"

	"lifeos-ai/backend/internal/models"
)

type UserRepository struct {
	DB *sql.DB
}

const userColumns = `id, name, email, password_hash, created_at, updated_at`

func (r UserRepository) Create(ctx context.Context, name, email, passwordHash string) (models.User, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO users (name, email, password_hash) VALUES (?, ?, ?)`,
		name, email, passwordHash)
	if err != nil {
		if isDuplicate(err) {
			return models.User{}, ErrDuplicate
		}
		return models.User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.User{}, err
	}
	return r.FindByID(ctx, uint64(id))
}

func (r UserRepository) FindByID(ctx context.Context, id uint64) (models.User, error) {
	return scanUser(r.DB.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

func (r UserRepository) FindByEmail(ctx context.Context, email string) (models.User, error) {
	return scanUser(r.DB.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = ?`, email))
}

func scanUser(row *sql.Row) (models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}
