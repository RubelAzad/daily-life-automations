package services

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/storage"
)

const (
	maxNoteBody     = 100_000
	maxTagsPerNote  = 20
	maxTagLength    = 50
	maxFilesPerNote = 50
)

// ErrFileTooLarge is returned when an upload exceeds MaxUploadBytes.
var ErrFileTooLarge = errors.New("file too large")

type NoteService struct {
	Notes          repository.NoteRepository
	Files          repository.FileRepository
	Projects       repository.ProjectRepository
	Reminders      repository.ReminderRepository
	Store          storage.Store
	MaxUploadBytes int64
	// Index keeps the search index current; nil turns indexing off.
	Index NoteIndexer
}

// NoteIndexer is the part of SearchService that notes need.
type NoteIndexer interface {
	IndexNote(ctx context.Context, userID, noteID uint64) error
	IndexFileNow(ctx context.Context, f models.File) error
}

// indexNote updates the note in the search index straight away, so a note
// can be found (and asked about) right after saving. A failure leaves the
// note marked as changed, and the worker indexes it later.
func (s NoteService) indexNote(ctx context.Context, userID, id uint64) {
	if s.Index == nil {
		return
	}
	if err := s.Index.IndexNote(ctx, userID, id); err != nil {
		log.Printf("search: note %d: %v (the worker will retry)", id, err)
	}
}

// indexFileSoon extracts an upload's text in the background; the upload
// response doesn't wait for it. The worker picks up anything this misses.
func (s NoteService) indexFileSoon(f models.File) {
	if s.Index == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := s.Index.IndexFileNow(ctx, f); err != nil {
			log.Printf("search: file %d: %v (the worker will retry)", f.ID, err)
		}
	}()
}

type ListNotesInput struct {
	Query        string
	Kind         string
	TagID        string
	ProjectID    string
	Pinned       string // "true"/"false"
	Archived     string // "true" shows the archive instead
	ExpiringDays string // notes expiring within N days (including already expired)
	Sort         string
	Page         string
	PerPage      string
}

type CreateNoteInput struct {
	Title     string   `json:"title"`
	Body      *string  `json:"body"`
	Kind      string   `json:"kind"`
	URL       *string  `json:"url"`
	Pinned    bool     `json:"pinned"`
	ExpiresOn *string  `json:"expires_on"`
	ProjectID *uint64  `json:"project_id"`
	Tags      []string `json:"tags"`
}

type UpdateNoteInput struct {
	Title     Optional[string]   `json:"title"`
	Body      Optional[string]   `json:"body"`
	Kind      Optional[string]   `json:"kind"`
	URL       Optional[string]   `json:"url"`
	Pinned    Optional[bool]     `json:"pinned"`
	Archived  Optional[bool]     `json:"archived"`
	ExpiresOn Optional[string]   `json:"expires_on"`
	ProjectID Optional[uint64]   `json:"project_id"`
	Tags      Optional[[]string] `json:"tags"` // replaces the set; [] or null clears
}

type UpdateTagInput struct {
	Name  Optional[string] `json:"name"`
	Color Optional[string] `json:"color"`
}

func (s NoteService) List(ctx context.Context, userID uint64, in ListNotesInput) ([]models.Note, PageMeta, error) {
	var c checker
	f := repository.NoteFilter{
		Query:     strings.TrimSpace(in.Query),
		Kind:      in.Kind,
		TagID:     parseIDParam(&c, "tag_id", in.TagID),
		ProjectID: parseIDParam(&c, "project_id", in.ProjectID),
		Archived:  in.Archived == "true",
		Sort:      orDefault(in.Sort, repository.DefaultNoteSort),
	}
	if f.Kind != "" {
		c.oneOf("kind", f.Kind, models.NoteKinds)
	}
	switch in.Pinned {
	case "":
	case "true", "false":
		p := in.Pinned == "true"
		f.Pinned = &p
	default:
		c.fail("pinned", "must be true or false")
	}
	if in.ExpiringDays != "" {
		days, err := strconv.Atoi(in.ExpiringDays)
		if err != nil || days < 0 || days > 3650 {
			c.fail("expiring_within_days", "must be between 0 and 3650")
		} else {
			loc, err := userLocation(ctx, s.Notes.DB, userID)
			if err != nil {
				return nil, PageMeta{}, err
			}
			d := localToday(time.Now(), loc)
			d.Time = d.Time.AddDate(0, 0, days)
			f.ExpiringBefore = &d
			if in.Sort == "" {
				f.Sort = "expires_on"
			}
		}
	}
	if !repository.IsValidNoteSort(f.Sort) {
		c.fail("sort", "must be one of: -updated_at, title, expires_on")
	}
	meta := PageMeta{Page: positiveInt(&c, "page", in.Page, 1), PerPage: positiveInt(&c, "per_page", in.PerPage, defaultPerPage)}
	if meta.PerPage > maxPerPage {
		c.fail("per_page", "must be at most "+strconv.Itoa(maxPerPage))
	}
	if err := c.err(); err != nil {
		return nil, meta, err
	}
	f.Limit, f.Offset = meta.PerPage, (meta.Page-1)*meta.PerPage
	notes, total, err := s.Notes.List(ctx, userID, f)
	meta.Total = total
	return notes, meta, err
}

// Get returns the note with its tags and files.
func (s NoteService) Get(ctx context.Context, userID, id uint64) (models.Note, error) {
	n, err := s.Notes.Get(ctx, userID, id)
	if err != nil {
		return n, err
	}
	files, err := s.Files.ListForNote(ctx, userID, id)
	n.Files = &files
	return n, err
}

func (s NoteService) Create(ctx context.Context, userID uint64, in CreateNoteInput) (models.Note, error) {
	var c checker
	n := models.Note{
		UserID:    userID,
		Title:     c.requiredText("title", in.Title, 255),
		Body:      optionalText(in.Body),
		Kind:      orDefault(in.Kind, "note"),
		URL:       optionalText(in.URL),
		Pinned:    in.Pinned,
		ProjectID: in.ProjectID,
	}
	if in.ExpiresOn != nil {
		n.ExpiresOn = c.parseDate("expires_on", *in.ExpiresOn)
	}
	tags := cleanTags(&c, in.Tags)
	if err := s.validate(ctx, &c, &n); err != nil {
		return models.Note{}, err
	}
	if err := c.err(); err != nil {
		return models.Note{}, err
	}

	var id uint64
	err := repository.InTx(ctx, s.Notes.DB, func(db repository.DBTX) error {
		notes := repository.NoteRepository{DB: db}
		var err error
		if id, err = notes.Create(ctx, n); err != nil {
			return err
		}
		return notes.SetTags(ctx, userID, id, tags)
	})
	if err != nil {
		return models.Note{}, err
	}
	s.indexNote(ctx, userID, id)
	return s.Get(ctx, userID, id)
}

func (s NoteService) Update(ctx context.Context, userID, id uint64, in UpdateNoteInput) (models.Note, error) {
	n, err := s.Notes.Get(ctx, userID, id)
	if err != nil {
		return n, err
	}
	var c checker
	if in.Title.Set {
		n.Title = c.requiredText("title", in.Title.Value, 255)
	}
	if in.Body.Set {
		n.Body = optionalText(in.Body.Ptr())
	}
	if in.Kind.Set {
		n.Kind = in.Kind.Value
	}
	if in.URL.Set {
		n.URL = optionalText(in.URL.Ptr())
	}
	if in.Pinned.Set {
		n.Pinned = in.Pinned.Value
	}
	if in.Archived.Set {
		n.Archived = in.Archived.Value
	}
	if in.ExpiresOn.Set {
		n.ExpiresOn = c.parseDate("expires_on", in.ExpiresOn.Value)
	}
	if in.ProjectID.Set {
		n.ProjectID = in.ProjectID.Ptr()
	}
	var tags []string
	if in.Tags.Set {
		tags = cleanTags(&c, in.Tags.Value)
	}
	if err := s.validate(ctx, &c, &n); err != nil {
		return models.Note{}, err
	}
	if err := c.err(); err != nil {
		return models.Note{}, err
	}

	err = repository.InTx(ctx, s.Notes.DB, func(db repository.DBTX) error {
		notes := repository.NoteRepository{DB: db}
		if err := notes.Update(ctx, n); err != nil {
			return err
		}
		if in.Tags.Set {
			if err := notes.SetTags(ctx, userID, id, tags); err != nil {
				return err
			}
		}
		if in.ExpiresOn.Set {
			loc, err := userLocation(ctx, db, userID)
			if err != nil {
				return err
			}
			return rescheduleNoteReminders(ctx, repository.ReminderRepository{DB: db}, n, time.Now().UTC(), loc)
		}
		return nil
	})
	if err != nil {
		return models.Note{}, err
	}
	s.indexNote(ctx, userID, id)
	return s.Get(ctx, userID, id)
}

// Delete removes the note and its attachments' contents.
func (s NoteService) Delete(ctx context.Context, userID, id uint64) error {
	files, err := s.Files.ListForNote(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := s.Notes.Delete(ctx, userID, id); err != nil {
		return err
	}
	// Rows are gone (cascade); a leftover file on disk is harmless but logged.
	for _, f := range files {
		if err := s.Store.Delete(f.StorageKey); err != nil {
			log.Printf("notes: could not delete stored file %s: %v", f.StorageKey, err)
		}
	}
	return nil
}

func (s NoteService) validate(ctx context.Context, c *checker, n *models.Note) error {
	c.oneOf("kind", n.Kind, models.NoteKinds)
	if n.Body != nil {
		c.maxLen("body", *n.Body, maxNoteBody)
	}
	if n.URL != nil {
		if u, err := url.Parse(*n.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(*n.URL) > 2048 {
			c.fail("url", "must be an http(s) URL")
		}
	} else if n.Kind == "link" {
		c.fail("url", "is required for links")
	}
	if n.ProjectID != nil {
		_, err := s.Projects.Get(ctx, n.UserID, *n.ProjectID)
		if errors.Is(err, repository.ErrNotFound) {
			c.fail("project_id", "project not found")
		} else if err != nil {
			return err
		}
	}
	return nil
}

// cleanTags trims, de-duplicates (case-insensitively) and validates names.
func cleanTags(c *checker, names []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range names {
		name := strings.Join(strings.Fields(raw), " ")
		if name == "" {
			continue
		}
		if len([]rune(name)) > maxTagLength || strings.ContainsAny(name, ",#") {
			c.fail("tags", "each tag must be at most 50 characters, without commas or #")
			return nil
		}
		key := strings.ToLower(name)
		if !seen[key] {
			seen[key] = true
			out = append(out, name)
		}
	}
	if len(out) > maxTagsPerNote {
		c.fail("tags", "a note can have at most 20 tags")
	}
	return out
}

// ---- Files -----------------------------------------------------------------

// Upload stores r (at most MaxUploadBytes) as an attachment of the note.
func (s NoteService) Upload(ctx context.Context, userID, noteID uint64, name string, r io.Reader) (models.File, error) {
	note, err := s.Get(ctx, userID, noteID)
	if err != nil {
		return models.File{}, err
	}
	if len(*note.Files) >= maxFilesPerNote {
		return models.File{}, &ValidationError{Fields: map[string]string{"file": "a note can have at most 50 files"}}
	}

	name = cleanFileName(name)
	br := bufio.NewReaderSize(r, 512)
	head, _ := br.Peek(512) // fewer bytes is fine for small files
	contentType, problem := checkUpload(name, head)
	if problem != "" {
		return models.File{}, &ValidationError{Fields: map[string]string{"file": problem}}
	}

	key, err := storage.NewKey(userID)
	if err != nil {
		return models.File{}, err
	}
	// Read one byte past the limit so an oversized file is detected, not truncated.
	size, sha, err := s.Store.Save(key, io.LimitReader(br, s.MaxUploadBytes+1))
	if err != nil {
		return models.File{}, err
	}
	if size > s.MaxUploadBytes {
		_ = s.Store.Delete(key)
		return models.File{}, ErrFileTooLarge
	}

	f, err := s.Files.Create(ctx, models.File{
		UserID: userID, NoteID: noteID, OriginalName: name, StorageKey: key,
		ContentType: contentType, SizeBytes: size, SHA256: sha,
	})
	if err != nil {
		_ = s.Store.Delete(key) // don't leave an orphan behind
		return models.File{}, err
	}
	s.indexFileSoon(f)
	return f, nil
}

// Open returns the file's metadata and contents if it belongs to userID.
func (s NoteService) Open(ctx context.Context, userID, fileID uint64) (models.File, io.ReadCloser, error) {
	f, err := s.Files.Get(ctx, userID, fileID)
	if err != nil {
		return f, nil, err
	}
	rc, err := s.Store.Open(f.StorageKey)
	return f, rc, err
}

func (s NoteService) DeleteFile(ctx context.Context, userID, fileID uint64) error {
	f, err := s.Files.Get(ctx, userID, fileID)
	if err != nil {
		return err
	}
	if err := s.Files.Delete(ctx, userID, fileID); err != nil {
		return err
	}
	return s.Store.Delete(f.StorageKey)
}

// ---- Tags ------------------------------------------------------------------

func (s NoteService) ListTags(ctx context.Context, userID uint64) ([]models.Tag, error) {
	return s.Notes.ListTags(ctx, userID)
}

func (s NoteService) UpdateTag(ctx context.Context, userID, id uint64, in UpdateTagInput) (models.Tag, error) {
	t, err := s.Notes.GetTag(ctx, userID, id)
	if err != nil {
		return t, err
	}
	var c checker
	if in.Name.Set {
		names := cleanTags(&c, []string{in.Name.Value})
		if len(names) == 0 && c.err() == nil {
			c.fail("name", "is required")
		} else if len(names) == 1 {
			t.Name = names[0]
		}
	}
	if in.Color.Set {
		t.Color = c.color("color", in.Color.Ptr())
	}
	if err := c.err(); err != nil {
		return models.Tag{}, err
	}
	if err := s.Notes.UpdateTag(ctx, t); errors.Is(err, repository.ErrDuplicate) {
		return models.Tag{}, &ValidationError{Fields: map[string]string{"name": "a tag with this name already exists"}}
	} else if err != nil {
		return models.Tag{}, err
	}
	return s.Notes.GetTag(ctx, userID, id)
}

func (s NoteService) DeleteTag(ctx context.Context, userID, id uint64) error {
	return s.Notes.DeleteTag(ctx, userID, id)
}
