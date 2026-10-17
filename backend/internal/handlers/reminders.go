package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/services"
)

type ReminderHandler struct {
	Reminders     services.ReminderService
	Notifications services.NotificationService
}

func (h ReminderHandler) List(c *gin.Context) {
	rems, meta, err := h.Reminders.List(c.Request.Context(), middleware.CurrentUser(c).ID, services.ListRemindersInput{
		Status:  c.Query("status"),
		TaskID:  c.Query("task_id"),
		EventID: c.Query("event_id"),
		Page:    c.Query("page"),
		PerPage: c.Query("per_page"),
	})
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rems, "meta": meta})
}

func (h ReminderHandler) Create(c *gin.Context) {
	var in services.CreateReminderInput
	if !bindJSON(c, &in) {
		return
	}
	rem, err := h.Reminders.Create(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": rem})
}

func (h ReminderHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.UpdateReminderInput
	if !bindJSON(c, &in) {
		return
	}
	rem, err := h.Reminders.Update(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rem})
}

func (h ReminderHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.Reminders.Delete(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type snoozeRequest struct {
	Minutes int `json:"minutes" binding:"required"`
}

func (h ReminderHandler) Snooze(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req snoozeRequest
	if !bindJSON(c, &req) {
		return
	}
	rem, err := h.Reminders.Snooze(c.Request.Context(), middleware.CurrentUser(c).ID, id, req.Minutes)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rem})
}

// ---- Notifications ---------------------------------------------------------

func (h ReminderHandler) ListNotifications(c *gin.Context) {
	items, meta, err := h.Notifications.List(c.Request.Context(), middleware.CurrentUser(c).ID, c.Query("unread"), c.Query("page"), c.Query("per_page"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": meta})
}

func (h ReminderHandler) UnreadCount(c *gin.Context) {
	n, err := h.Notifications.UnreadCount(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"unread": n}})
}

func (h ReminderHandler) MarkRead(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.Notifications.MarkRead(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h ReminderHandler) MarkAllRead(c *gin.Context) {
	n, err := h.Notifications.MarkAllRead(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"marked": n}})
}
