package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/services"
)

type ProjectHandler struct {
	Projects services.ProjectService
}

func (h ProjectHandler) List(c *gin.Context) {
	projects, meta, err := h.Projects.List(c.Request.Context(), middleware.CurrentUser(c).ID, services.ListProjectsInput{
		Status:  c.Query("status"),
		Page:    c.Query("page"),
		PerPage: c.Query("per_page"),
	})
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": projects, "meta": meta})
}

func (h ProjectHandler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	project, err := h.Projects.Get(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": project})
}

func (h ProjectHandler) Create(c *gin.Context) {
	var in services.CreateProjectInput
	if !bindJSON(c, &in) {
		return
	}
	project, err := h.Projects.Create(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": project})
}

func (h ProjectHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.UpdateProjectInput
	if !bindJSON(c, &in) {
		return
	}
	project, err := h.Projects.Update(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": project})
}

func (h ProjectHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.Projects.Delete(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h ProjectHandler) CreateMilestone(c *gin.Context) {
	projectID, ok := pathID(c)
	if !ok {
		return
	}
	var in services.CreateMilestoneInput
	if !bindJSON(c, &in) {
		return
	}
	milestone, err := h.Projects.CreateMilestone(c.Request.Context(), middleware.CurrentUser(c).ID, projectID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": milestone})
}

func (h ProjectHandler) UpdateMilestone(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.UpdateMilestoneInput
	if !bindJSON(c, &in) {
		return
	}
	milestone, err := h.Projects.UpdateMilestone(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": milestone})
}

func (h ProjectHandler) DeleteMilestone(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.Projects.DeleteMilestone(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
