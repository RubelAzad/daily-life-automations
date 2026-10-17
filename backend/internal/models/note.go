package models

import "time"

var NoteKinds = []string{"note", "idea", "link", "document", "receipt"}

type Note struct {
	ID        uint64    `json:"id"`
	UserID    uint64    `json:"-"`
	ProjectID *uint64   `json:"project_id"`
	Title     string    `json:"title"`
	Body      *string   `json:"body"`
	Kind      string    `json:"kind"`
	URL       *string   `json:"url"`
	Pinned    bool      `json:"pinned"`
	Archived  bool      `json:"archived"`
	ExpiresOn *Date     `json:"expires_on"` // documents, memberships, warranties
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Derived.
	Tags      []Tag `json:"tags"`
	FileCount int   `json:"file_count"`
	// Files is only loaded for single-note responses.
	Files *[]File `json:"files,omitempty"`
}

type Tag struct {
	ID        uint64  `json:"id"`
	UserID    uint64  `json:"-"`
	Name      string  `json:"name"`
	Color     *string `json:"color"`
	NoteCount int     `json:"note_count"`
}

type File struct {
	ID           uint64 `json:"id"`
	UserID       uint64 `json:"-"`
	NoteID       uint64 `json:"note_id"`
	OriginalName string `json:"name"`
	StorageKey   string `json:"-"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	// TextStatus is whether the file's text is searchable: pending,
	// processing, ready, unsupported (e.g. images) or failed.
	TextStatus string    `json:"text_status"`
	TextError  *string   `json:"text_error"`
	TextChars  *int      `json:"text_chars"`
	CreatedAt  time.Time `json:"created_at"`
}

// File text statuses.
const (
	TextPending     = "pending"
	TextProcessing  = "processing"
	TextReady       = "ready"
	TextUnsupported = "unsupported"
	TextFailed      = "failed"
)
