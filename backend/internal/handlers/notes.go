package handlers

import (
	"errors"
	"mime"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/services"
)

type NoteHandler struct {
	Notes services.NoteService
}

func (h NoteHandler) List(c *gin.Context) {
	notes, meta, err := h.Notes.List(c.Request.Context(), middleware.CurrentUser(c).ID, services.ListNotesInput{
		Query:        c.Query("q"),
		Kind:         c.Query("kind"),
		TagID:        c.Query("tag_id"),
		ProjectID:    c.Query("project_id"),
		Pinned:       c.Query("pinned"),
		Archived:     c.Query("archived"),
		ExpiringDays: c.Query("expiring_within_days"),
		Sort:         c.Query("sort"),
		Page:         c.Query("page"),
		PerPage:      c.Query("per_page"),
	})
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": notes, "meta": meta})
}

func (h NoteHandler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	note, err := h.Notes.Get(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": note})
}

func (h NoteHandler) Create(c *gin.Context) {
	var in services.CreateNoteInput
	if !bindJSON(c, &in) {
		return
	}
	note, err := h.Notes.Create(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": note})
}

func (h NoteHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.UpdateNoteInput
	if !bindJSON(c, &in) {
		return
	}
	note, err := h.Notes.Update(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": note})
}

func (h NoteHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.Notes.Delete(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Upload accepts multipart/form-data with one "file" field.
func (h NoteHandler) Upload(c *gin.Context) {
	noteID, ok := pathID(c)
	if !ok {
		return
	}
	limit := h.Notes.MaxUploadBytes
	// Cap the whole request (file plus multipart overhead) before parsing.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit+1<<20)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			respondTooLarge(c, limit)
			return
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation failed", "fields": gin.H{"file": "send the file as multipart/form-data field \"file\""}})
		return
	}
	defer file.Close()

	f, err := h.Notes.Upload(c.Request.Context(), middleware.CurrentUser(c).ID, noteID, header.Filename, file)
	if errors.Is(err, services.ErrFileTooLarge) {
		respondTooLarge(c, limit)
		return
	}
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": f})
}

func respondTooLarge(c *gin.Context, limit int64) {
	c.JSON(http.StatusRequestEntityTooLarge, gin.H{
		"error": "file too large", "fields": gin.H{"file": "must be at most " + strconv.FormatInt(limit>>20, 10) + " MB"},
	})
}

// Download streams a file to its owner. Only images and PDFs are shown
// inline; everything else is forced to download, and the sandbox CSP plus
// nosniff stop the browser from executing anything it serves.
func (h NoteHandler) Download(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	f, rc, err := h.Notes.Open(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	defer rc.Close()

	disposition := "attachment"
	if services.IsInlineContentType(f.ContentType) && c.Query("download") != "1" {
		disposition = "inline"
	}
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": f.OriginalName}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	c.Header("Cache-Control", "private, no-store")
	c.DataFromReader(http.StatusOK, f.SizeBytes, f.ContentType, rc, nil)
}

func (h NoteHandler) DeleteFile(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.Notes.DeleteFile(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h NoteHandler) ListTags(c *gin.Context) {
	tags, err := h.Notes.ListTags(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tags})
}

func (h NoteHandler) UpdateTag(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.UpdateTagInput
	if !bindJSON(c, &in) {
		return
	}
	tag, err := h.Notes.UpdateTag(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tag})
}

func (h NoteHandler) DeleteTag(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.Notes.DeleteTag(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
