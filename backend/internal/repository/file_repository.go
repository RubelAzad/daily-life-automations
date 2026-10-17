package repository

import (
	"context"
	"database/sql"
	"errors"

	"lifeos-ai/backend/internal/models"
)

type FileRepository struct {
	DB DBTX
}

const fileColumns = `id, user_id, note_id, original_name, storage_key, content_type, size_bytes, sha256, created_at`

func (r FileRepository) Create(ctx context.Context, f models.File) (models.File, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO files (user_id, note_id, original_name, storage_key, content_type, size_bytes, sha256)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.UserID, f.NoteID, f.OriginalName, f.StorageKey, f.ContentType, f.SizeBytes, f.SHA256)
	if err != nil {
		return models.File{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.File{}, err
	}
	return r.Get(ctx, f.UserID, uint64(id))
}

func (r FileRepository) Get(ctx context.Context, userID, id uint64) (models.File, error) {
	f, err := scanFile(r.DB.QueryRowContext(ctx, `SELECT `+fileColumns+` FROM files WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

func (r FileRepository) ListForNote(ctx context.Context, userID, noteID uint64) ([]models.File, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+fileColumns+` FROM files WHERE note_id = ? AND user_id = ? ORDER BY created_at, id`, noteID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []models.File{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (r FileRepository) Delete(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM files WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanFile(row rowScanner) (models.File, error) {
	var f models.File
	err := row.Scan(&f.ID, &f.UserID, &f.NoteID, &f.OriginalName, &f.StorageKey, &f.ContentType, &f.SizeBytes, &f.SHA256, &f.CreatedAt)
	return f, err
}
