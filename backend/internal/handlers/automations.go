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
