package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"lifeos-ai/backend/internal/models"
)

type FileRepository struct {
	DB DBTX
}

const fileColumns = `id, user_id, note_id, original_name, storage_key, content_type, size_bytes, sha256,
	text_status, text_error, text_chars, created_at`

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
	err := row.Scan(&f.ID, &f.UserID, &f.NoteID, &f.OriginalName, &f.StorageKey, &f.ContentType, &f.SizeBytes, &f.SHA256,
		&f.TextStatus, &f.TextError, &f.TextChars, &f.CreatedAt)
	return f, err
}

// claimable is a file waiting for text extraction, or one whose extraction
// was abandoned (a crashed worker) before staleBefore.
const claimable = `(text_status = 'pending' OR (text_status = 'processing' AND text_claimed_at < ?))`

// PendingText returns up to limit files that need text extraction.
func (r FileRepository) PendingText(ctx context.Context, staleBefore time.Time, limit int) ([]models.File, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT `+fileColumns+` FROM files WHERE `+claimable+` ORDER BY id LIMIT ?`, staleBefore.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ClaimText marks a file as being extracted. ok is false if another process
// claimed it first, so each file is extracted once.
func (r FileRepository) ClaimText(ctx context.Context, id uint64, staleBefore, now time.Time) (bool, error) {
	res, err := r.DB.ExecContext(ctx,
		`UPDATE files SET text_status = 'processing', text_claimed_at = ? WHERE id = ? AND `+claimable, now.UTC(), id, staleBefore.UTC())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r FileRepository) FinishText(ctx context.Context, id uint64, status string, errMsg *string, chars *int) error {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE files SET text_status = ?, text_error = ?, text_chars = ?, text_claimed_at = NULL WHERE id = ?`, status, errMsg, chars, id)
	return err
}
