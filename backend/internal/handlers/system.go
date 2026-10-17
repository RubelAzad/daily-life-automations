package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/scheduler"
)

type SystemHandler struct {
	DB *sql.DB
}

// Status says whether the background worker is running, so the app can warn
// when reminders and briefings aren't being delivered.
func (h SystemHandler) Status(c *gin.Context) {
	st, err := scheduler.Status(c.Request.Context(), h.DB, time.Now())
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"worker": st}})
}
