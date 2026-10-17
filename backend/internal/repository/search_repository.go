package repository

import (
	"context"
	"fmt"
	"time"
)

type SearchRepository struct {
	DB DBTX
}

// SearchHit is one matching chunk.
type SearchHit struct {
	NoteID     uint64
	NoteTitle  string
	NoteKind   string
	FileID     *uint64
	FileName   *string
	ChunkIndex int
	Content    string
	Score      float64
}

// ReplaceChunks swaps the indexed text of a note (fileID nil) or of one of
// its files. Run it in a transaction so searches never see a half-indexed
// document.
func (r SearchRepository) ReplaceChunks(ctx context.Context, userID, noteID uint64, fileID *uint64, title string, chunks []string) error {
	var err error
	if fileID == nil {
		_, err = r.DB.ExecContext(ctx, `DELETE FROM search_chunks WHERE note_id = ? AND file_id IS NULL`, noteID)
	} else {
		_, err = r.DB.ExecContext(ctx, `DELETE FROM search_chunks WHERE file_id = ?`, *fileID)
	}
	if err != nil {
		return err
	}
	for i, c := range chunks {
		if _, err := r.DB.ExecContext(ctx,
			`INSERT INTO search_chunks (user_id, note_id, file_id, chunk_index, title, content) VALUES (?, ?, ?, ?, ?, ?)`,
			userID, noteID, fileID, i, title, c); err != nil {
			return err
		}
	}
	return nil
}

func (r SearchRepository) MarkNoteIndexed(ctx context.Context, noteID uint64, at time.Time) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE notes SET indexed_at = ?, updated_at = updated_at WHERE id = ?`, at.UTC(), noteID)
	return err
}

// NotesNeedingIndex returns notes never indexed or changed since.
func (r SearchRepository) NotesNeedingIndex(ctx context.Context, limit int) ([][2]uint64, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT user_id, id FROM notes WHERE indexed_at IS NULL OR indexed_at < updated_at ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]uint64
	for rows.Next() {
		var u, id uint64
		if err := rows.Scan(&u, &id); err != nil {
			return nil, err
		}
		out = append(out, [2]uint64{u, id})
	}
	return out, rows.Err()
}

const searchSelect = `
	SELECT c.note_id, n.title, n.kind, c.file_id, f.original_name, c.chunk_index, c.content, %s AS score
	  FROM search_chunks c
	  JOIN notes n ON n.id = c.note_id
	  LEFT JOIN files f ON f.id = c.file_id
	 WHERE c.user_id = ? AND n.archived = 0 AND %s
	 ORDER BY score DESC, c.note_id DESC, c.chunk_index
	 LIMIT ?`

// Search finds the user's chunks matching a boolean-mode full-text query,
// best first. Archived notes are left out.
func (r SearchRepository) Search(ctx context.Context, userID uint64, booleanQuery string, limit int) ([]SearchHit, error) {
	match := `MATCH(c.title, c.content) AGAINST (? IN BOOLEAN MODE)`
	return r.query(ctx, sprintf(searchSelect, match, match), []any{booleanQuery, userID, booleanQuery, limit})
}

// SearchLike is the fallback for queries without searchable words (e.g. too
// short for the full-text index): a plain substring match.
func (r SearchRepository) SearchLike(ctx context.Context, userID uint64, text string, limit int) ([]SearchHit, error) {
	pattern := "%" + escapeLike(text) + "%"
	return r.query(ctx, sprintf(searchSelect, "1", "(c.title LIKE ? OR c.content LIKE ?)"), []any{userID, pattern, pattern, limit})
}

func (r SearchRepository) query(ctx context.Context, q string, args []any) ([]SearchHit, error) {
	rows, err := r.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.NoteID, &h.NoteTitle, &h.NoteKind, &h.FileID, &h.FileName, &h.ChunkIndex, &h.Content, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }
