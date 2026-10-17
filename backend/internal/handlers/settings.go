package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/services"
)

type SettingsHandler struct {
	Settings services.SettingsService
}

func (h SettingsHandler) Get(c *gin.Context) {
	s, err := h.Settings.Get(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": s})
}

func (h SettingsHandler) Update(c *gin.Context) {
	var in services.UpdateSettingsInput
	if !bindJSON(c, &in) {
		return
	}
	s, err := h.Settings.Update(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": s})
}
