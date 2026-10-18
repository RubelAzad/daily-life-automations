package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/services"
)

// FinanceHandler serves expenses, income, budgets, bills and the summary.
type FinanceHandler struct {
	Finance services.FinanceService
}

func (h FinanceHandler) Summary(c *gin.Context) {
	s, err := h.Finance.Summary(c.Request.Context(), middleware.CurrentUser(c).ID, c.Query("month"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": s})
}

func (h FinanceHandler) Categories(c *gin.Context) {
	list, err := h.Finance.Categories(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// ---- Expenses ----

func (h FinanceHandler) ListExpenses(c *gin.Context) {
	list, meta, err := h.Finance.ListExpenses(c.Request.Context(), middleware.CurrentUser(c).ID, services.ListExpensesInput{
		Month: c.Query("month"), From: c.Query("from"), To: c.Query("to"), Category: c.Query("category"), Query: c.Query("q"),
		BillID: c.Query("bill_id"), Page: c.Query("page"), PerPage: c.Query("per_page"),
	})
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "meta": meta})
}

func (h FinanceHandler) CreateExpense(c *gin.Context) {
	var in services.ExpenseInput
	if !bindJSON(c, &in) {
		return
	}
	e, err := h.Finance.CreateExpense(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": e})
}

func (h FinanceHandler) GetExpense(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	e, err := h.Finance.Finance.GetExpense(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": e})
}

func (h FinanceHandler) UpdateExpense(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.UpdateExpenseInput
	if !bindJSON(c, &in) {
		return
	}
	e, err := h.Finance.UpdateExpense(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": e})
}

func (h FinanceHandler) DeleteExpense(c *gin.Context) {
	h.delete(c, h.Finance.DeleteExpense)
}

// ---- Income ----

func (h FinanceHandler) ListIncomes(c *gin.Context) {
	list, meta, err := h.Finance.ListIncomes(c.Request.Context(), middleware.CurrentUser(c).ID, services.ListIncomesInput{
		Month: c.Query("month"), From: c.Query("from"), To: c.Query("to"), Page: c.Query("page"), PerPage: c.Query("per_page"),
	})
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "meta": meta})
}

func (h FinanceHandler) CreateIncome(c *gin.Context) {
	var in services.IncomeInput
	if !bindJSON(c, &in) {
		return
	}
	i, err := h.Finance.CreateIncome(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": i})
}

func (h FinanceHandler) UpdateIncome(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.UpdateIncomeInput
	if !bindJSON(c, &in) {
		return
	}
	i, err := h.Finance.UpdateIncome(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": i})
}

func (h FinanceHandler) DeleteIncome(c *gin.Context) {
	h.delete(c, h.Finance.DeleteIncome)
}

// ---- Budgets ----

func (h FinanceHandler) ListBudgets(c *gin.Context) {
	list, err := h.Finance.ListBudgets(c.Request.Context(), middleware.CurrentUser(c).ID, c.Query("month"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h FinanceHandler) CreateBudget(c *gin.Context) {
	var in services.BudgetInput
	if !bindJSON(c, &in) {
		return
	}
	b, err := h.Finance.CreateBudget(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": b})
}

func (h FinanceHandler) UpdateBudget(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.BudgetInput
	if !bindJSON(c, &in) {
		return
	}
	b, err := h.Finance.UpdateBudget(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h FinanceHandler) DeleteBudget(c *gin.Context) {
	h.delete(c, h.Finance.DeleteBudget)
}

// ---- Bills ----

func (h FinanceHandler) ListBills(c *gin.Context) {
	list, err := h.Finance.ListBills(c.Request.Context(), middleware.CurrentUser(c).ID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h FinanceHandler) GetBill(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	b, err := h.Finance.GetBill(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h FinanceHandler) CreateBill(c *gin.Context) {
	var in services.BillInput
	if !bindJSON(c, &in) {
		return
	}
	b, err := h.Finance.CreateBill(c.Request.Context(), middleware.CurrentUser(c).ID, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": b})
}

func (h FinanceHandler) UpdateBill(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.BillInput
	if !bindJSON(c, &in) {
		return
	}
	b, err := h.Finance.UpdateBill(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h FinanceHandler) DeleteBill(c *gin.Context) {
	h.delete(c, h.Finance.DeleteBill)
}

// PayBill records that the user paid the bill: an expense, and the next due
// date. The body is optional.
func (h FinanceHandler) PayBill(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in services.PayBillInput
	if c.Request.ContentLength != 0 && !bindJSON(c, &in) {
		return
	}
	paid, err := h.Finance.PayBill(c.Request.Context(), middleware.CurrentUser(c).ID, id, in)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": paid})
}

// BillTask adds the bill's "Pay …" task (or returns the open one).
func (h FinanceHandler) BillTask(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	bt, err := h.Finance.CreateBillTask(c.Request.Context(), middleware.CurrentUser(c).ID, id)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	status := http.StatusOK
	if bt.Created {
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{"data": bt})
}

func (h FinanceHandler) delete(c *gin.Context, fn func(ctx context.Context, userID, id uint64) error) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := fn(c.Request.Context(), middleware.CurrentUser(c).ID, id); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
