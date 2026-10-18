//go:build integration

package workflow

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
)

func money(t *testing.T, s string) *models.Money {
	t.Helper()
	m, err := models.ParseMoney(s)
	if err != nil {
		t.Fatal(err)
	}
	return &m
}

func (f fixture) localToday() time.Time {
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	l := f.clock.Now().In(dhaka)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
}

func (f fixture) expense(t *testing.T, amount, category, on string) models.Expense {
	t.Helper()
	e, err := f.finance.CreateExpense(f.ctx, f.user, services.ExpenseInput{Amount: money(t, amount), Category: category, SpentOn: on})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func isValidation(err error, field string) bool {
	var ve *services.ValidationError
	return errors.As(err, &ve) && ve.Fields[field] != ""
}

func TestExpensesIncomeAndSummary(t *testing.T) {
	f := newFixture(t, models.ApprovalAlways)
	today := f.localToday().Format(models.DateLayout)
	month := f.localToday().Format("2006-01")

	if _, err := f.finance.CreateExpense(f.ctx, f.user, services.ExpenseInput{Amount: money(t, "0"), Category: "Food"}); !isValidation(err, "amount") {
		t.Errorf("zero amount: %v", err)
	}
	if _, err := f.finance.CreateExpense(f.ctx, f.user, services.ExpenseInput{Amount: money(t, "5")}); !isValidation(err, "category") {
		t.Errorf("no category: %v", err)
	}
	coffee := f.expense(t, "4.50", "Dining", today)
	if coffee.Amount != 450 || coffee.SpentOn.Format(models.DateLayout) != today {
		t.Errorf("expense: %+v", coffee)
	}
	f.expense(t, "120.25", "Groceries", today)
	f.expense(t, "30", "Groceries", f.localToday().AddDate(0, -1, 0).Format(models.DateLayout))

	list, meta, err := f.finance.ListExpenses(f.ctx, f.user, services.ListExpensesInput{Month: month})
	if err != nil || meta.Total != 2 || meta.Sum != 12475 || len(list) != 2 {
		t.Errorf("this month: %d %d %v", meta.Total, meta.Sum, err)
	}
	if _, meta, _ := f.finance.ListExpenses(f.ctx, f.user, services.ListExpensesInput{Category: "Groceries"}); meta.Total != 2 || meta.Sum != 15025 {
		t.Errorf("groceries: %+v", meta)
	}
	if _, _, err := f.finance.ListExpenses(f.ctx, f.user, services.ListExpensesInput{Month: "October"}); !isValidation(err, "month") {
		t.Errorf("bad month: %v", err)
	}

	upd, err := f.finance.UpdateExpense(f.ctx, f.user, coffee.ID, services.UpdateExpenseInput{Amount: services.Optional[models.Money]{Set: true, Value: 500}})
	if err != nil || upd.Amount != 500 {
		t.Errorf("update: %+v %v", upd, err)
	}
	if _, err := f.finance.UpdateExpense(f.ctx, f.user+1_000_000, coffee.ID, services.UpdateExpenseInput{}); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("another user's expense: %v", err)
	}

	if _, err := f.finance.CreateIncome(f.ctx, f.user, services.IncomeInput{Amount: money(t, "1000"), Source: "Salary"}); err != nil {
		t.Fatal(err)
	}
	s, err := f.finance.Summary(f.ctx, f.user, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Month != month || s.Income != 100000 || s.Expenses != 12525 || s.Net != 100000-12525 || s.LastMonth != 3000 ||
		len(s.ByCategory) != 2 || s.ByCategory[0].Category != "Groceries" || s.Currency != "USD" {
		t.Errorf("summary: %+v", s)
	}
	cats, _ := f.finance.Categories(f.ctx, f.user)
	if len(cats) < 3 || cats[0] != "Groceries" {
		t.Errorf("categories: %v", cats)
	}
	if err := f.finance.DeleteExpense(f.ctx, f.user, coffee.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.finance.DeleteExpense(f.ctx, f.user, coffee.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("deleted twice: %v", err)
	}
}

func TestBudgetAlerts(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	today := f.localToday().Format(models.DateLayout)
	cat := "Groceries"
	limit := services.Optional[models.Money]{Set: true, Value: 10000}
	b, err := f.finance.CreateBudget(f.ctx, f.user, services.BudgetInput{Category: services.Optional[string]{Set: true, Value: cat}, MonthlyLimit: limit})
	if err != nil || b.AlertPercent != 80 {
		t.Fatalf("budget: %+v %v", b, err)
	}
	if _, err := f.finance.CreateBudget(f.ctx, f.user, services.BudgetInput{Category: services.Optional[string]{Set: true, Value: cat}, MonthlyLimit: limit}); !isValidation(err, "category") {
		t.Errorf("second Groceries budget: %v", err)
	}
	overall, err := f.finance.CreateBudget(f.ctx, f.user, services.BudgetInput{MonthlyLimit: services.Optional[models.Money]{Set: true, Value: 50000}})
	if err != nil || overall.Category != nil {
		t.Fatalf("overall: %+v %v", overall, err)
	}
	f.create(t, `{"name":"Overspent","trigger":{"type":"budget.exceeded"},"actions":[{"type":"notify","input":{"title":"Over: {{budget.category}} ({{budget.spent}})"}}]}`)

	f.expense(t, "30", cat, f.localToday().AddDate(0, -1, 0).Format(models.DateLayout)) // last month: no alert
	f.expense(t, "50", cat, today)
	if n := f.count(t, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND kind = 'finance'`); n != 0 {
		t.Errorf("alerts at 50%%: %d", n)
	}
	f.expense(t, "35", cat, today)
	if f.notifications(t, "Groceries budget: 80% used") != 1 {
		t.Error("no 80% alert")
	}
	f.expense(t, "20", cat, today)
	f.expense(t, "5", cat, today) // still over: nothing new
	f.drain(t)
	if f.notifications(t, "Groceries budget exceeded") != 1 || f.notifications(t, "Over: Groceries (105.00 USD)") != 1 {
		t.Errorf("exceeded: alert %d, automation %d", f.notifications(t, "Groceries budget exceeded"), f.notifications(t, "Over: Groceries (105.00 USD)"))
	}
	if f.notifications(t, "Overall budget: 80% used") != 0 {
		t.Error("overall budget alerted too early")
	}
	list, _ := f.finance.ListBudgets(f.ctx, f.user, "")
	if len(list) != 2 || list[0].Category != nil || list[1].Spent != 11000 || list[0].Spent != 11000 {
		t.Errorf("budgets with spending: %+v", list)
	}
}

func TestBillPaidByHand(t *testing.T) {
	f := newFixture(t, models.ApprovalAlways)
	b, err := f.finance.CreateBill(f.ctx, f.user, services.BillInput{
		Name: services.Optional[string]{Set: true, Value: "Rent"}, Amount: services.Optional[models.Money]{Set: true, Value: 2500000},
		Category: services.Optional[string]{Set: true, Value: "Rent"}, NextDueOn: services.Optional[string]{Set: true, Value: "2026-01-31"},
	})
	if err != nil || b.DueDay == nil || *b.DueDay != 31 || b.Frequency != models.BillMonthly {
		t.Fatalf("bill: %+v %v", b, err)
	}
	if _, err := f.finance.CreateBill(f.ctx, f.user, services.BillInput{Name: services.Optional[string]{Set: true, Value: "x"}}); !isValidation(err, "amount") {
		t.Errorf("missing fields: %v", err)
	}
	paid, err := f.finance.PayBill(f.ctx, f.user, b.ID, services.PayBillInput{})
	if err != nil {
		t.Fatal(err)
	}
	if paid.Bill.NextDueOn.Format(models.DateLayout) != "2026-02-28" || paid.Expense.Amount != 2500000 || paid.Expense.BillID == nil || paid.Bill.LastPaidOn == nil {
		t.Errorf("paid: %+v", paid)
	}
	paid, _ = f.finance.PayBill(f.ctx, f.user, b.ID, services.PayBillInput{Amount: money(t, "24000")})
	if paid.Bill.NextDueOn.Format(models.DateLayout) != "2026-03-31" || paid.Expense.Amount != 2400000 {
		t.Errorf("second payment, a different amount: %+v", paid)
	}
	if _, meta, _ := f.finance.ListExpenses(f.ctx, f.user, services.ListExpensesInput{BillID: itoa(b.ID)}); meta.Total != 2 {
		t.Errorf("bill's payments: %d", meta.Total)
	}
}

// Workflow C: a bill's reminder day → notification and "Pay …" task;
// completing the task records the payment and moves the due date.
func TestBillWorkflow(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	if _, err := f.service.AddTemplate(f.ctx, f.user, "bill_reminders"); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.service.ListTemplates(f.ctx, f.user); list[3].Key != "bill_payments" || list[3].AutomationID == nil {
		t.Fatalf("companion not added: %+v", list)
	}
	due := f.localToday().AddDate(0, 0, 2)
	b, err := f.finance.CreateBill(f.ctx, f.user, services.BillInput{
		Name: services.Optional[string]{Set: true, Value: "Internet"}, Amount: services.Optional[models.Money]{Set: true, Value: 120000},
		Category:  services.Optional[string]{Set: true, Value: "Internet & phone"},
		NextDueOn: services.Optional[string]{Set: true, Value: due.Format(models.DateLayout)}, RemindDaysBefore: services.Optional[int]{Set: true, Value: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	later, _ := f.finance.CreateBill(f.ctx, f.user, services.BillInput{
		Name: services.Optional[string]{Set: true, Value: "Insurance"}, Amount: services.Optional[models.Money]{Set: true, Value: 5000},
		NextDueOn: services.Optional[string]{Set: true, Value: f.localToday().AddDate(0, 0, 10).Format(models.DateLayout)},
	})

	for i := 0; i < 2; i++ { // a second scan changes nothing
		if _, err := f.finance.ScanBills(f.ctx); err != nil {
			t.Fatal(err)
		}
		f.drain(t)
	}
	title := "Internet is due " + due.Format("Mon 2 Jan")
	if n := f.notifications(t, title); n != 1 {
		t.Errorf("reminders %q: %d", title, n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND body = 'Expected amount: 1,200.00 USD.'`); n != 1 {
		t.Errorf("amount in the reminder: %d", n)
	}
	if f.tasksTitled(t, "Pay Insurance") != 0 {
		t.Error("a bill not yet in its reminder window got a task")
	}
	b, _ = f.finance.GetBill(f.ctx, f.user, b.ID)
	if b.PendingTaskID == nil || f.tasksTitled(t, "Pay Internet") != 1 {
		t.Fatalf("pay task: %+v (tasks %d)", b, f.tasksTitled(t, "Pay Internet"))
	}
	task, _ := f.tasks.Get(f.ctx, f.user, *b.PendingTaskID)
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	if task.DueAt == nil || task.DueAt.In(dhaka).Format("2006-01-02 15:04") != due.Format("2006-01-02")+" 09:00" || task.Priority != "high" {
		t.Errorf("task: %+v", task)
	}

	// Completing the task records the payment.
	if _, err := f.tasks.Update(f.ctx, f.user, task.ID, services.UpdateTaskInput{Status: services.Optional[string]{Set: true, Value: models.TaskCompleted}}); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	wantNext := b.NextDue(due).Format(models.DateLayout)
	b, _ = f.finance.GetBill(f.ctx, f.user, b.ID)
	if b.PendingTaskID != nil || b.NextDueOn.Format(models.DateLayout) != wantNext {
		t.Errorf("bill after paying: %+v", b)
	}
	expenses, meta, _ := f.finance.ListExpenses(f.ctx, f.user, services.ListExpensesInput{BillID: itoa(b.ID)})
	if meta.Total != 1 || expenses[0].Amount != 120000 || expenses[0].Category != "Internet & phone" || !strings.HasPrefix(*expenses[0].Description, "Internet (due ") {
		t.Errorf("expense: %+v", expenses)
	}

	// Paid from the bill instead: its open task is completed, and that
	// completion doesn't record a second payment.
	bt, err := f.finance.CreateBillTask(f.ctx, f.user, later.ID)
	if err != nil || !bt.Created {
		t.Fatalf("task: %+v %v", bt, err)
	}
	if again, _ := f.finance.CreateBillTask(f.ctx, f.user, later.ID); again.Created || again.Task.ID != bt.Task.ID {
		t.Errorf("second task for the same due date: %+v", again)
	}
	if _, err := f.finance.PayBill(f.ctx, f.user, later.ID, services.PayBillInput{}); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if got, _ := f.tasks.Get(f.ctx, f.user, bt.Task.ID); got.Status != models.TaskCompleted {
		t.Errorf("task not completed: %s", got.Status)
	}
	if _, meta, _ := f.finance.ListExpenses(f.ctx, f.user, services.ListExpensesInput{BillID: itoa(later.ID)}); meta.Total != 1 {
		t.Errorf("payments recorded: %d, want 1", meta.Total)
	}
	runs, _, _ := f.engine.ListRuns(f.ctx, f.user, ListRunsInput{Status: "failed"})
	if len(runs) != 0 {
		b, _ := json.Marshal(runs)
		t.Errorf("failed runs: %s", b)
	}
}
