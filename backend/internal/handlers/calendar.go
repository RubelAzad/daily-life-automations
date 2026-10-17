package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/services"
)

type CalendarHandler struct {
	Calendar services.CalendarService
}

// List returns occurrences in ?from=&to= (RFC 3339, at most 366 days).
func (h CalendarHandler) List(c *gin.Context) {
	occs, err := h.Calendar.List(c.Request.Context(), middleware.CurrentUser(c).ID, c.Query("from"), c.Query("to"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": occs})
}

func (h CalendarHandler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	event, err := h.Calendar.Get(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": event})
}

// Create and Update return the saved event plus "conflicts": occurrences of
// other busy events it overlaps. Conflicts never block the save.
func (h CalendarHandler) Create(c *gin.Context) {
	var in services.CreateEventInput
	if !bindJSON(c, &in) {
		return
	}
	event, conflicts, err := h.Calendar.Create(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": event, "conflicts": conflicts})
}

func (h CalendarHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.UpdateEventInput
	if !bindJSON(c, &in) {
		return
	}
	event, conflicts, err := h.Calendar.Update(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": event, "conflicts": conflicts})
}

func (h CalendarHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.Calendar.Delete(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
