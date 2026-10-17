package services

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"lifeos-ai/backend/internal/extract"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/storage"
)

const (
	chunkChars   = 1500
	chunkOverlap = 150
	// extractTimeout bounds one file's extraction.
	extractTimeout = 2 * time.Minute
	// staleClaim: a file "processing" longer than this was abandoned (crash).
	staleClaim     = 10 * time.Minute
	maxSearchTerms = 10
	maxSearchLimit = 20
	snippetChars   = 240
	excerptChars   = 800
)

// SearchService keeps the full-text index of notes and their files, and
// answers searches over it.
type SearchService struct {
	Search repository.SearchRepository
	Notes  repository.NoteRepository
	Files  repository.FileRepository
	Store  storage.Store
	Log    *log.Logger
}

// SearchResult is the best-matching passage of one note or file.
type SearchResult struct {
	NoteID    uint64  `json:"note_id"`
	NoteTitle string  `json:"note_title"`
	NoteKind  string  `json:"note_kind"`
	FileID    *uint64 `json:"file_id"`
	FileName  *string `json:"file_name"`
	Snippet   string  `json:"snippet"` // short, around the first matching word
	Excerpt   string  `json:"-"`       // longer passage, for the assistant
	Score     float64 `json:"score"`
}

// Query searches the user's notes and attachment text. Results are grouped
// to the best passage per note or file.
func (s SearchService) Query(ctx context.Context, userID uint64, q string, limit int) ([]SearchResult, error) {
	q = strings.TrimSpace(q)
	switch {
	case q == "":
		return nil, &ValidationError{Fields: map[string]string{"q": "is required"}}
	case utf8.RuneCountInString(q) > 200:
		return nil, &ValidationError{Fields: map[string]string{"q": "must be at most 200 characters"}}
	}
	if limit <= 0 || limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	terms := searchTerms(q)
	var hits []repository.SearchHit
	var err error
	if len(terms) > 0 {
		hits, err = s.Search.Search(ctx, userID, booleanQuery(terms), 100)
	} else {
		hits, err = s.Search.SearchLike(ctx, userID, q, 100)
		terms = []string{strings.ToLower(q)}
	}
	if err != nil {
		return nil, err
	}

	out := []SearchResult{}
	seen := map[[2]uint64]bool{}
	for _, h := range hits {
		key := [2]uint64{h.NoteID, 0}
		if h.FileID != nil {
			key[1] = *h.FileID
		}
		if seen[key] {
			continue // a better passage of this note/file is already in
		}
		seen[key] = true
		out = append(out, SearchResult{
			NoteID: h.NoteID, NoteTitle: h.NoteTitle, NoteKind: h.NoteKind, FileID: h.FileID, FileName: h.FileName, Score: h.Score,
			Snippet: around(h.Content, terms, snippetChars), Excerpt: around(h.Content, terms, excerptChars),
		})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// searchTerms keeps words the full-text index can match: letters and
// digits only (so no boolean-mode operators get through), at least 3
// characters (InnoDB's minimum token size), common words dropped.
func searchTerms(q string) []string {
	words := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) })
	var out []string
	seen := map[string]bool{}
	for _, w := range words {
		if utf8.RuneCountInString(w) < 3 || stopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) == maxSearchTerms {
			break
		}
	}
	return out
}

// booleanQuery matches any term, also as a prefix ("insur*" finds
// "insurance"); documents with more matching terms rank higher.
func booleanQuery(terms []string) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = t + "*"
	}
	return strings.Join(parts, " ")
}

var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "was": true, "were": true, "what": true, "where": true, "when": true,
	"which": true, "who": true, "whom": true, "why": true, "how": true, "with": true, "from": true, "that": true, "this": true,
	"these": true, "those": true, "have": true, "has": true, "had": true, "does": true, "did": true, "can": true, "could": true,
	"would": true, "should": true, "about": true, "into": true, "your": true, "you": true, "mine": true, "our": true, "its": true,
	"find": true, "show": true, "tell": true, "any": true, "all": true, "some": true, "there": true, "their": true, "they": true,
	"them": true, "then": true, "than": true, "not": true, "but": true, "out": true, "get": true, "got": true, "please": true,
}

// around returns about n characters of content centred on the first match.
func around(content string, terms []string, n int) string {
	runes := []rune(content)
	if len(runes) <= n {
		return strings.TrimSpace(content)
	}
	lower := []rune(strings.ToLower(content))
	pos := -1
	for _, t := range terms {
		if i := indexRunes(lower, []rune(t)); i >= 0 && (pos < 0 || i < pos) {
			pos = i
		}
	}
	start := 0
	if pos > n/3 {
		start = pos - n/3
	}
	end := start + n
	if end > len(runes) {
		end, start = len(runes), max(0, len(runes)-n)
	}
	out := strings.TrimSpace(string(runes[start:end]))
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

func indexRunes(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := range sub {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// ---- Indexing --------------------------------------------------------------

// IndexNote (re)indexes a note's own text: title, link and body.
func (s SearchService) IndexNote(ctx context.Context, userID, noteID uint64) error {
	n, err := s.Notes.Get(ctx, userID, noteID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil // deleted meanwhile; its chunks went with it
	}
	if err != nil {
		return err
	}
	var text strings.Builder
	text.WriteString(n.Title)
	if n.URL != nil {
		text.WriteString("\n" + *n.URL)
	}
	if n.Body != nil {
		text.WriteString("\n" + *n.Body)
	}
	chunks := extract.Chunks(text.String(), chunkChars, chunkOverlap)
	now := time.Now().UTC()
	return repository.InTx(ctx, s.Search.DB, func(db repository.DBTX) error {
		search := repository.SearchRepository{DB: db}
		if err := search.ReplaceChunks(ctx, userID, n.ID, nil, n.Title, chunks); err != nil {
			return err
		}
		return search.MarkNoteIndexed(ctx, n.ID, now)
	})
}

// IndexFileNow extracts and indexes one file if it is waiting for that and
// no other process has claimed it. Safe to call from several places.
func (s SearchService) IndexFileNow(ctx context.Context, f models.File) error {
	now := time.Now().UTC()
	ok, err := s.Files.ClaimText(ctx, f.ID, now.Add(-staleClaim), now)
	if err != nil || !ok {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, extractTimeout)
	defer cancel()

	status, errMsg, chars := models.TextReady, (*string)(nil), (*int)(nil)
	text, err := s.readText(ctx, f)
	switch {
	case errors.Is(err, extract.ErrUnsupported):
		status, errMsg = models.TextUnsupported, strPtr("This file type isn't searchable yet (images need text recognition).")
	case errors.Is(err, extract.ErrEncrypted):
		status, errMsg = models.TextFailed, strPtr("The file is password-protected.")
	case err != nil:
		status, errMsg = models.TextFailed, strPtr("Could not read the text in this file.")
		s.logf("search: file %d: %v", f.ID, err)
	default:
		n := utf8.RuneCountInString(text)
		chars = &n
		title := f.OriginalName
		err = repository.InTx(ctx, s.Search.DB, func(db repository.DBTX) error {
			return repository.SearchRepository{DB: db}.ReplaceChunks(ctx, f.UserID, f.NoteID, &f.ID, title, extract.Chunks(text, chunkChars, chunkOverlap))
		})
		if err != nil {
			// Leave it claimed; it is retried once the claim goes stale.
			return err
		}
	}
	return s.Files.FinishText(ctx, f.ID, status, errMsg, chars)
}

func (s SearchService) readText(ctx context.Context, f models.File) (string, error) {
	rc, err := s.Store.Open(f.StorageKey)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, 600<<20))
	if err != nil {
		return "", err
	}
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		t, err := extract.Text(f.OriginalName, data)
		done <- result{t, err}
	}()
	select {
	case r := <-done:
		return r.text, r.err
	case <-ctx.Done():
		// The parser can't be interrupted; it finishes in the background
		// and its result is dropped.
		return "", errors.New("text extraction took too long")
	}
}

// IndexPending processes files waiting for extraction and notes changed
// since they were last indexed. The worker calls it on every tick.
func (s SearchService) IndexPending(ctx context.Context, limit int) (files, notes int, err error) {
	pending, err := s.Files.PendingText(ctx, time.Now().UTC().Add(-staleClaim), limit)
	if err != nil {
		return 0, 0, err
	}
	for _, f := range pending {
		if err := s.IndexFileNow(ctx, f); err != nil {
			s.logf("search: file %d: %v", f.ID, err)
			continue
		}
		files++
	}
	stale, err := s.Search.NotesNeedingIndex(ctx, limit)
	if err != nil {
		return files, 0, err
	}
	for _, n := range stale {
		if err := s.IndexNote(ctx, n[0], n[1]); err != nil {
			s.logf("search: note %d: %v", n[1], err)
			continue
		}
		notes++
	}
	return files, notes, nil
}

func (s SearchService) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

func strPtr(s string) *string { return &s }
