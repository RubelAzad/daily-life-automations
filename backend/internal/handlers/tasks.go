package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/services"
)

type TaskHandler struct {
	Tasks services.TaskService
}

func (h TaskHandler) List(c *gin.Context) {
	tasks, meta, err := h.Tasks.List(c.Request.Context(), middleware.CurrentUser(c).ID, services.ListTasksInput{
		Status:   c.Query("status"),
		Priority: c.Query("priority"),
		ListID:   c.Query("list_id"),
		Query:    c.Query("q"),
		DueFrom:  c.Query("due_from"),
		DueTo:    c.Query("due_to"),
		Sort:     c.Query("sort"),
		Page:     c.Query("page"),
		PerPage:  c.Query("per_page"),
	})
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tasks, "meta": meta})
}

func (h TaskHandler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	task, err := h.Tasks.Get(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": task})
}

func (h TaskHandler) Create(c *gin.Context) {
	var in services.CreateTaskInput
	if !bindJSON(c, &in) {
		return
	}
	task, err := h.Tasks.Create(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": task})
}

func (h TaskHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.UpdateTaskInput
	if !bindJSON(c, &in) {
		return
	}
	task, err := h.Tasks.Update(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": task})
}

func (h TaskHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.Tasks.Delete(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
