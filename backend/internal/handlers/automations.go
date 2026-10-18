package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/workflow"
)

type AutomationHandler struct {
	Automations workflow.Service
	Engine      workflow.Engine
}

// Catalog lists the triggers, condition fields and operators, and actions,
// so clients can build the editor.
func (h AutomationHandler) Catalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": workflow.CatalogData()})
}

// Templates lists the built-in workflows and which ones the user added.
func (h AutomationHandler) Templates(c *gin.Context) {
	list, err := h.Automations.ListTemplates(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// AddTemplate creates an automation from a built-in workflow.
func (h AutomationHandler) AddTemplate(c *gin.Context) {
	a, err := h.Automations.AddTemplate(c.Request.Context(), middleware.CurrentUser(c).ID, c.Param("key"))
	if errors.Is(err, workflow.ErrTemplateAdded) {
		respondError(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": a})
}

func (h AutomationHandler) List(c *gin.Context) {
	list, err := h.Automations.List(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h AutomationHandler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	a, err := h.Automations.Get(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h AutomationHandler) Create(c *gin.Context) {
	var in workflow.Input
	if !bindJSON(c, &in) {
		return
	}
	a, err := h.Automations.Create(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": a})
}

func (h AutomationHandler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in workflow.Input
	if !bindJSON(c, &in) {
		return
	}
	a, err := h.Automations.Update(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h AutomationHandler) Delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.Automations.Delete(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Run runs a schedule or manual automation now and returns what it did.
func (h AutomationHandler) Run(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	res, err := h.Engine.RunNow(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if errors.Is(err, workflow.ErrCantRunNow) {
		respondError(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// ListRuns lists the user's automation runs, newest first.
func (h AutomationHandler) ListRuns(c *gin.Context) {
	runs, meta, err := h.Engine.ListRuns(c.Request.Context(), middleware.CurrentUser(c).ID, workflow.ListRunsInput{
		AutomationID: c.Query("automation_id"), Status: c.Query("status"), Page: c.Query("page"), PerPage: c.Query("per_page"),
	})
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": runs, "meta": meta})
}

func (h AutomationHandler) GetRun(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	run, err := h.Engine.GetRun(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": run})
}

// RetryRun retries a failed run now, from the step that failed.
func (h AutomationHandler) RetryRun(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	run, err := h.Engine.Retry(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if errors.Is(err, workflow.ErrNotRetryable) || errors.Is(err, workflow.ErrAutomationGone) {
		respondError(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": run})
}
