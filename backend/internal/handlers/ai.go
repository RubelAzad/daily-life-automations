package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/middleware"
)

type AIHandler struct {
	AI ai.Service
}

func (h AIHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": h.AI.Status()})
}

// Ping makes a tiny model call to check the API key and connection.
func (h AIHandler) Ping(c *gin.Context) {
	res, err := h.AI.Ping(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h AIHandler) ListActions(c *gin.Context) {
	actions, meta, err := h.AI.ListActions(c.Request.Context(), middleware.CurrentUser(c).ID, ai.ListActionsInput{
		Status: c.Query("status"), Kind: c.Query("kind"), Page: c.Query("page"), PerPage: c.Query("per_page"),
	})
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": actions, "meta": meta})
}

func (h AIHandler) GetAction(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	a, err := h.AI.GetAction(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

// Approve runs an action the assistant proposed. The result is in the
// returned action's status, output and error.
func (h AIHandler) Approve(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	a, err := h.AI.Approve(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h AIHandler) Reject(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	a, err := h.AI.Reject(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func respondAIError(c *gin.Context, err error) {
	var apiErr *ai.APIError
	switch {
	case errors.Is(err, ai.ErrNotConfigured):
		respondError(c, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, ai.ErrRateLimited):
		respondError(c, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, ai.ErrNotPending), errors.Is(err, ai.ErrApprovalExpired):
		respondError(c, http.StatusConflict, err.Error())
	case errors.As(err, &apiErr):
		// The provider's message (e.g. "invalid x-api-key") helps fix setup and
		// never contains the key.
		respondError(c, http.StatusBadGateway, "AI provider error ("+apiErr.Type+"): "+apiErr.Message)
	default:
		respondServiceError(c, err)
	}
}
