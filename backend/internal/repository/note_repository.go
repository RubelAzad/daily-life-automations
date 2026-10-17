package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"lifeos-ai/backend/internal/models"
)

type NoteRepository struct {
	DB DBTX
}

type NoteFilter struct {
	Query          string
	Kind           string
	TagID          *uint64
	ProjectID      *uint64
	Pinned         *bool
	Archived       bool
	ExpiringBefore *models.Date // expires_on on or before this date
	Sort           string
	Limit          int
	Offset         int
}

var noteOrders = map[string]string{
	"-updated_at": "n.pinned DESC, n.updated_at DESC, n.id DESC",
	"title":       "n.title ASC, n.id ASC",
	"expires_on":  "n.expires_on IS NULL, n.expires_on ASC, n.id ASC",
}

const DefaultNoteSort = "-updated_at"

func IsValidNoteSort(s string) bool {
	_, ok := noteOrders[s]
	return ok
}

const noteSelect = `
	SELECT n.id, n.user_id, n.project_id, n.title, n.body, n.kind, n.url, n.pinned, n.archived, n.expires_on,
	       n.created_at, n.updated_at,
	       (SELECT COUNT(*) FROM files f WHERE f.note_id = n.id)
	  FROM notes n`

func (r NoteRepository) List(ctx context.Context, userID uint64, f NoteFilter) ([]models.Note, int, error) {
	where := []string{"n.user_id = ?", "n.archived = ?"}
	args := []any{userID, f.Archived}
	if f.Query != "" {
		p := "%" + escapeLike(f.Query) + "%"
		// Searches the note itself, its attachments' names and its tags.
		where = append(where, `(n.title LIKE ? OR n.body LIKE ? OR n.url LIKE ?
			OR EXISTS (SELECT 1 FROM files f WHERE f.note_id = n.id AND f.original_name LIKE ?)
			OR EXISTS (SELECT 1 FROM note_tags nt JOIN tags tg ON tg.id = nt.tag_id WHERE nt.note_id = n.id AND tg.name LIKE ?))`)
		args = append(args, p, p, p, p, p)
	}
	if f.Kind != "" {
		where = append(where, "n.kind = ?")
		args = append(args, f.Kind)
	}
	if f.TagID != nil {
		where = append(where, "EXISTS (SELECT 1 FROM note_tags nt WHERE nt.note_id = n.id AND nt.tag_id = ?)")
		args = append(args, *f.TagID)
	}
	if f.ProjectID != nil {
		where = append(where, "n.project_id = ?")
		args = append(args, *f.ProjectID)
	}
	if f.Pinned != nil {
		where = append(where, "n.pinned = ?")
		args = append(args, *f.Pinned)
	}
	if f.ExpiringBefore != nil {
		where = append(where, "n.expires_on IS NOT NULL AND n.expires_on <= ?")
		args = append(args, *f.ExpiringBefore)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes n WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order, ok := noteOrders[f.Sort]
	if !ok {
		order = noteOrders[DefaultNoteSort]
	}
	rows, err := r.DB.QueryContext(ctx, noteSelect+` WHERE `+whereSQL+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	notes := []models.Note{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, 0, err
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return notes, total, r.attachTags(ctx, notes)
}

func (r NoteRepository) Get(ctx context.Context, userID, id uint64) (models.Note, error) {
	n, err := scanNote(r.DB.QueryRowContext(ctx, noteSelect+` WHERE n.id = ? AND n.user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	if err != nil {
		return n, err
	}
	one := []models.Note{n}
	err = r.attachTags(ctx, one)
	return one[0], err
}

func (r NoteRepository) Create(ctx context.Context, n models.Note) (uint64, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO notes (user_id, project_id, title, body, kind, url, pinned, archived, expires_on)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.UserID, n.ProjectID, n.Title, n.Body, n.Kind, n.URL, n.Pinned, n.Archived, n.ExpiresOn)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return uint64(id), err
}

func (r NoteRepository) Update(ctx context.Context, n models.Note) error {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE notes SET project_id = ?, title = ?, body = ?, kind = ?, url = ?, pinned = ?, archived = ?, expires_on = ?
		  WHERE id = ? AND user_id = ?`,
		n.ProjectID, n.Title, n.Body, n.Kind, n.URL, n.Pinned, n.Archived, n.ExpiresOn, n.ID, n.UserID)
	return err
}

// Delete removes the note; its files, tag links and reminders cascade. The
// caller removes file contents from the store.
func (r NoteRepository) Delete(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM notes WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTags replaces the note's tags with names, creating missing tags. Names
// match existing tags case-insensitively. Run inside a transaction.
func (r NoteRepository) SetTags(ctx context.Context, userID, noteID uint64, names []string) error {
	if _, err := r.DB.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id = ?`, noteID); err != nil {
		return err
	}
	for _, name := range names {
		// LAST_INSERT_ID(id) makes LastInsertId return the existing row's id
		// when the tag already exists.
		res, err := r.DB.ExecContext(ctx,
			`INSERT INTO tags (user_id, name) VALUES (?, ?) ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`, userID, name)
		if err != nil {
			return err
		}
		tagID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := r.DB.ExecContext(ctx, `INSERT IGNORE INTO note_tags (note_id, tag_id) VALUES (?, ?)`, noteID, tagID); err != nil {
			return err
		}
	}
	return nil
}

func (r NoteRepository) attachTags(ctx context.Context, notes []models.Note) error {
	if len(notes) == 0 {
		return nil
	}
	index := make(map[uint64]int, len(notes))
	args := make([]any, len(notes))
	for i := range notes {
		notes[i].Tags = []models.Tag{}
		index[notes[i].ID] = i
		args[i] = notes[i].ID
	}
	rows, err := r.DB.QueryContext(ctx,
		`SELECT nt.note_id, tg.id, tg.name, tg.color
		   FROM note_tags nt JOIN tags tg ON tg.id = nt.tag_id
		  WHERE nt.note_id IN (`+placeholders(len(notes))+`)
		  ORDER BY tg.name`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var noteID uint64
		var t models.Tag
		if err := rows.Scan(&noteID, &t.ID, &t.Name, &t.Color); err != nil {
			return err
		}
		n := &notes[index[noteID]]
		n.Tags = append(n.Tags, t)
	}
	return rows.Err()
}

// ---- Tags ------------------------------------------------------------------

const tagSelect = `
	SELECT tg.id, tg.user_id, tg.name, tg.color,
	       (SELECT COUNT(*) FROM note_tags nt JOIN notes n ON n.id = nt.note_id WHERE nt.tag_id = tg.id AND n.archived = FALSE)
	  FROM tags tg`

func (r NoteRepository) ListTags(ctx context.Context, userID uint64) ([]models.Tag, error) {
	rows, err := r.DB.QueryContext(ctx, tagSelect+` WHERE tg.user_id = ? ORDER BY tg.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []models.Tag{}
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func (r NoteRepository) GetTag(ctx context.Context, userID, id uint64) (models.Tag, error) {
	t, err := scanTag(r.DB.QueryRowContext(ctx, tagSelect+` WHERE tg.id = ? AND tg.user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (r NoteRepository) UpdateTag(ctx context.Context, t models.Tag) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE tags SET name = ?, color = ? WHERE id = ? AND user_id = ?`, t.Name, t.Color, t.ID, t.UserID)
	if isDuplicate(err) {
		return ErrDuplicate
	}
	return err
}

func (r NoteRepository) DeleteTag(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM tags WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanNote(row rowScanner) (models.Note, error) {
	var n models.Note
	err := row.Scan(&n.ID, &n.UserID, &n.ProjectID, &n.Title, &n.Body, &n.Kind, &n.URL, &n.Pinned, &n.Archived,
		&n.ExpiresOn, &n.CreatedAt, &n.UpdatedAt, &n.FileCount)
	return n, err
}

func scanTag(row rowScanner) (models.Tag, error) {
	var t models.Tag
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.Color, &t.NoteCount)
	return t, err
}
