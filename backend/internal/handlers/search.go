package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/services"
)

type SearchHandler struct {
	Service services.SearchService
}

// Search looks through the user's notes and the text inside their files.
func (h SearchHandler) Search(c *gin.Context) {
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 20 {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation failed", "fields": gin.H{"limit": "must be between 1 and 20"}})
			return
		}
		limit = n
	}
	results, err := h.Service.Query(c.Request.Context(), middleware.CurrentUser(c).ID, c.Query("q"), limit)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": results})
}
