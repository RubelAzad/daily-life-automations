package ai

import (
	"context"
	"errors"
	"strconv"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/services"
)

// Finance tools only record money; nothing here pays, transfers or buys.

type expenseToolInput struct {
	Amount      *models.Money `json:"amount"`
	Category    string        `json:"category"`
	Description *string       `json:"description"`
	SpentOn     string        `json:"spent_on"`
}

func recordExpenseTool(d Deps) Tool {
	return define("record_expense",
		"Record money the user spent. amount has at most two decimals, in the user's currency; spent_on is YYYY-MM-DD (default today). "+
			"Use a short category such as Groceries, Dining, Transport, Rent, Utilities, Health, Shopping.",
		RiskLow, `{"type":"object","properties":{
			"amount":{"type":"number","exclusiveMinimum":0},
			"category":{"type":"string","minLength":1,"maxLength":40},
			"description":{"type":"string","maxLength":255},
			"spent_on":{"type":"string","format":"date"}
		},"required":["amount","category"],"additionalProperties":false}`,
		func(ctx context.Context, userID uint64, in expenseToolInput) string {
			amount := "?"
			if in.Amount != nil {
				amount = services.FormatMoney(*in.Amount, currency(ctx, d, userID))
			}
			s := "Record " + amount + " spent on " + quote(in.Category)
			if in.Description != nil && *in.Description != "" {
				s += " (" + truncateRunes(*in.Description, 60) + ")"
			}
			return s
		},
		func(ctx context.Context, userID uint64, in expenseToolInput) (any, error) {
			e, err := d.Finance.CreateExpense(ctx, userID, services.ExpenseInput{Amount: in.Amount, Category: in.Category, Description: in.Description, SpentOn: in.SpentOn})
			if err != nil {
				return nil, err
			}
			return map[string]any{"id": e.ID, "amount": e.Amount, "category": e.Category, "spent_on": e.SpentOn}, nil
		})
}

type financeSummaryInput struct {
	Month string `json:"month"`
}

func financeSummaryTool(d Deps) Tool {
	return define("get_finance_summary",
		"Get a month's money: income, spending by category, budgets with what's spent, and bills due in the next 30 days. month is YYYY-MM (default this month).",
		RiskRead, `{"type":"object","properties":{"month":{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}$"}},"additionalProperties":false}`,
		func(_ context.Context, _ uint64, in financeSummaryInput) string {
			if in.Month == "" {
				return "Look up this month's finances"
			}
			return "Look up finances for " + in.Month
		},
		func(ctx context.Context, userID uint64, in financeSummaryInput) (any, error) {
			s, err := d.Finance.Summary(ctx, userID, in.Month)
			if err != nil {
				return nil, err
			}
			bills := make([]map[string]any, len(s.Bills))
			for i, b := range s.Bills {
				bills[i] = map[string]any{"id": b.ID, "name": b.Name, "amount": b.Amount, "due_on": b.NextDueOn}
			}
			budgets := make([]map[string]any, len(s.Budgets))
			for i, b := range s.Budgets {
				name := "Overall"
				if b.Category != nil {
					name = *b.Category
				}
				budgets[i] = map[string]any{"category": name, "limit": b.MonthlyLimit, "spent": b.Spent}
			}
			return map[string]any{
				"month": s.Month, "currency": s.Currency, "income": s.Income, "expenses": s.Expenses, "net": s.Net,
				"last_month_expenses": s.LastMonth, "by_category": s.ByCategory, "budgets": budgets, "upcoming_bills": bills,
			}, nil
		})
}

type billIDInput struct {
	BillID uint64 `json:"bill_id"`
}

// createBillTaskTool adds the "Pay …" task for a bill's current due date.
func createBillTaskTool(d Deps) Tool {
	return define("create_bill_task",
		"Add a \"Pay <bill>\" task for a recurring bill's current due date (due 09:00 that day). Does nothing if it already has an open one. Completing the task records the payment.",
		RiskLow, `{"type":"object","properties":{"bill_id":{"type":"integer","minimum":1}},"required":["bill_id"],"additionalProperties":false}`,
		func(ctx context.Context, userID uint64, in billIDInput) string {
			return "Add a task to pay " + billLabel(ctx, d, userID, in.BillID)
		},
		func(ctx context.Context, userID uint64, in billIDInput) (any, error) {
			if in.BillID == 0 {
				return nil, &InputError{Message: "bill_id is required"}
			}
			bt, err := d.Finance.CreateBillTask(ctx, userID, in.BillID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"task": compactTask(bt.Task), "created": bt.Created}, nil
		})
}

type payBillInput struct {
	BillID uint64        `json:"bill_id"`
	TaskID *uint64       `json:"task_id"`
	Amount *models.Money `json:"amount"`
}

// payBillTool records that a bill was paid (the user paid it themselves):
// an expense, and the bill moves to its next due date. With task_id it
// records the payment for that completed "Pay …" task, once.
func payBillTool(d Deps) Tool {
	return define("pay_bill",
		"Record that the user paid a recurring bill: adds the expense and moves the bill to its next due date. LifeOS never pays anything itself. "+
			"task_id: the bill's completed \"Pay …\" task, so the payment is recorded once. amount defaults to the bill's amount.",
		RiskLow, `{"type":"object","properties":{
			"bill_id":{"type":"integer","minimum":1},
			"task_id":{"type":"integer","minimum":1},
			"amount":{"type":"number","exclusiveMinimum":0}
		},"required":["bill_id"],"additionalProperties":false}`,
		func(ctx context.Context, userID uint64, in payBillInput) string {
			return "Record payment of " + billLabel(ctx, d, userID, in.BillID)
		},
		func(ctx context.Context, userID uint64, in payBillInput) (any, error) {
			if in.BillID == 0 {
				return nil, &InputError{Message: "bill_id is required"}
			}
			var paid services.PaidBill
			var err error
			if in.TaskID != nil {
				paid, err = d.Finance.PayBillForTask(ctx, userID, in.BillID, *in.TaskID)
				if errors.Is(err, services.ErrBillNotPending) {
					return map[string]any{"already_recorded": true}, nil
				}
			} else {
				paid, err = d.Finance.PayBill(ctx, userID, in.BillID, services.PayBillInput{Amount: in.Amount})
			}
			if err != nil {
				return nil, err
			}
			return map[string]any{"expense_id": paid.Expense.ID, "amount": paid.Expense.Amount, "next_due_on": paid.Bill.NextDueOn}, nil
		})
}

func billLabel(ctx context.Context, d Deps, userID, id uint64) string {
	if b, err := d.Finance.GetBill(ctx, userID, id); err == nil {
		return quote(b.Name) + " (" + services.FormatMoney(b.Amount, currency(ctx, d, userID)) + ")"
	}
	return "bill #" + strconv.FormatUint(id, 10)
}

func currency(ctx context.Context, d Deps, userID uint64) string {
	if s, err := d.Settings.Get(ctx, userID); err == nil {
		return s.Currency
	}
	return ""
}
