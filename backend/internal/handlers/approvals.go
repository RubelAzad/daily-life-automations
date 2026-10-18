package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/approvals"
	"lifeos-ai/backend/internal/middleware"
)

// ApprovalHandler serves the approvals inbox.
type ApprovalHandler struct {
	Approvals approvals.Service
}

func (h ApprovalHandler) List(c *gin.Context) {
	list, meta, err := h.Approvals.Pending(c.Request.Context(), middleware.CurrentUser(c).ID, pageInput(c))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "meta": meta})
}

func (h ApprovalHandler) History(c *gin.Context) {
	list, meta, err := h.Approvals.History(c.Request.Context(), middleware.CurrentUser(c).ID, pageInput(c))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "meta": meta})
}

func (h ApprovalHandler) Count(c *gin.Context) {
	n, err := h.Approvals.Count(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"pending": n}})
}

func (h ApprovalHandler) Approve(c *gin.Context) { h.decide(c, true) }

func (h ApprovalHandler) Reject(c *gin.Context) { h.decide(c, false) }

func (h ApprovalHandler) decide(c *gin.Context, approve bool) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	a, err := h.Approvals.Decide(c.Request.Context(), middleware.CurrentUser(c).ID, id, approve)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

type decideManyRequest struct {
	IDs      []uint64 `json:"ids" binding:"required"`
	Decision string   `json:"decision" binding:"required,oneof=approve reject"`
}

// DecideMany approves or rejects several actions; each one's outcome is in
// the results.
func (h ApprovalHandler) DecideMany(c *gin.Context) {
	var req decideManyRequest
	if !bindJSON(c, &req) {
		return
	}
	results, err := h.Approvals.DecideMany(c.Request.Context(), middleware.CurrentUser(c).ID, req.IDs, req.Decision == "approve")
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": results})
}

func pageInput(c *gin.Context) approvals.PageInput {
	return approvals.PageInput{Page: c.Query("page"), PerPage: c.Query("per_page")}
}
