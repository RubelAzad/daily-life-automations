package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/services"
)

type TodayHandler struct {
	Today services.TodayService
}

func (h TodayHandler) Get(c *gin.Context) {
	today, err := h.Today.Get(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": today})
}
