package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/middleware"
)

type AIHandler struct {
	AI      ai.Service
	Briefer ai.Briefer
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
		Status: c.Query("status"), Kind: c.Query("kind"), ConversationID: c.Query("conversation_id"), AutomationID: c.Query("automation_id"),
		Page: c.Query("page"), PerPage: c.Query("per_page"),
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

func respondAIError(c *gin.Context, err error) {
	var apiErr *ai.APIError
	switch {
	case errors.Is(err, ai.ErrNotConfigured):
		respondError(c, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, ai.ErrRateLimited), errors.Is(err, ai.ErrChatRateLimited), errors.Is(err, ai.ErrBriefingRateLimited):
		respondError(c, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, ai.ErrNotPending), errors.Is(err, ai.ErrApprovalExpired), errors.Is(err, ai.ErrChatBusy):
		respondError(c, http.StatusConflict, err.Error())
	case errors.As(err, &apiErr):
		// The provider's message (e.g. "invalid x-api-key") helps fix setup and
		// never contains the key.
		respondError(c, http.StatusBadGateway, "AI provider error ("+apiErr.Type+"): "+apiErr.Message)
	default:
		respondServiceError(c, err)
	}
}

func (h AIHandler) ListConversations(c *gin.Context) {
	page, perPage, ok := pageParams(c)
	if !ok {
		return
	}
	list, meta, err := h.AI.ListConversations(c.Request.Context(), middleware.CurrentUser(c).ID, page, perPage)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "meta": meta})
}

func (h AIHandler) CreateConversation(c *gin.Context) {
	var in struct {
		Title string `json:"title"`
	}
	if c.Request.ContentLength != 0 && !bindJSON(c, &in) {
		return
	}
	conv, err := h.AI.CreateConversation(c.Request.Context(), middleware.CurrentUser(c).ID, in.Title)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": conv})
}

func (h AIHandler) GetConversation(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	conv, err := h.AI.GetConversation(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": conv})
}

// DeleteConversation removes the chat history. Audit log entries are kept.
func (h AIHandler) DeleteConversation(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.AI.DeleteConversation(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// SendMessage runs one chat turn; it can take a while (several model calls).
func (h AIHandler) SendMessage(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if !bindJSON(c, &in) {
		return
	}
	res, err := h.AI.SendMessage(c.Request.Context(), middleware.CurrentUser(c).ID, id, in.Text)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// pageParams reads ?page and ?per_page (default 1 and 20, per_page at most 100).
func pageParams(c *gin.Context) (page, perPage int, ok bool) {
	page, perPage = 1, 20
	fields := gin.H{}
	if raw := c.Query("page"); raw != "" {
		if n, err := strconv.Atoi(raw); err != nil || n < 1 {
			fields["page"] = "must be a positive integer"
		} else {
			page = n
		}
	}
	if raw := c.Query("per_page"); raw != "" {
		if n, err := strconv.Atoi(raw); err != nil || n < 1 || n > 100 {
			fields["per_page"] = "must be between 1 and 100"
		} else {
			perPage = n
		}
	}
	if len(fields) > 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation failed", "fields": fields})
		return 0, 0, false
	}
	return page, perPage, true
}

// TodayBriefings returns the latest morning and evening briefing for today.
func (h AIHandler) TodayBriefings(c *gin.Context) {
	res, err := h.Briefer.Today(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h AIHandler) ListBriefings(c *gin.Context) {
	page, perPage, ok := pageParams(c)
	if !ok {
		return
	}
	list, meta, err := h.Briefer.List(c.Request.Context(), middleware.CurrentUser(c).ID, page, perPage)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "meta": meta})
}

// CreateBriefing writes a briefing now. It only advises; nothing is changed.
func (h AIHandler) CreateBriefing(c *gin.Context) {
	var in struct {
		Kind string `json:"kind"`
	}
	if !bindJSON(c, &in) {
		return
	}
	b, err := h.Briefer.Generate(c.Request.Context(), middleware.CurrentUser(c).ID, in.Kind, false)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": b})
}
