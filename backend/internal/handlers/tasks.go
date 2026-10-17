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
		Status:      c.Query("status"),
		Priority:    c.Query("priority"),
		ListID:      c.Query("list_id"),
		ParentID:    c.Query("parent_id"),
		ProjectID:   c.Query("project_id"),
		MilestoneID: c.Query("milestone_id"),
		Query:       c.Query("q"),
		DueFrom:     c.Query("due_from"),
		DueTo:       c.Query("due_to"),
		Sort:        c.Query("sort"),
		Page:        c.Query("page"),
		PerPage:     c.Query("per_page"),
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

type addDependencyRequest struct {
	DependsOnID uint64 `json:"depends_on_id" binding:"required"`
}

func (h TaskHandler) AddDependency(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req addDependencyRequest
	if !bindJSON(c, &req) {
		return
	}
	task, err := h.Tasks.AddDependency(c.Request.Context(), middleware.CurrentUser(c).ID, id, req.DependsOnID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": task})
}

func (h TaskHandler) RemoveDependency(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	dependsOnID, ok := pathParamID(c, "depends_on_id")
	if !ok {
		return
	}
	task, err := h.Tasks.RemoveDependency(c.Request.Context(), middleware.CurrentUser(c).ID, id, dependsOnID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": task})
}

// CarryForward moves unfinished tasks to today or tomorrow.
func (h TaskHandler) CarryForward(c *gin.Context) {
	var in services.CarryForwardInput
	if !bindJSON(c, &in) {
		return
	}
	tasks, err := h.Tasks.CarryForward(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tasks, "meta": gin.H{"moved": len(tasks)}})
}
