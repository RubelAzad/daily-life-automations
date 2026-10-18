package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"lifeos-ai/backend/internal/events"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

// Automation triggers the finance module emits.
const (
	TriggerBillDueSoon    = "bill.due_soon"
	TriggerBudgetExceeded = "budget.exceeded"
)

const (
	maxRemindDays = 14
	// billTaskHour is the local time a "Pay …" task is due on the due date.
	billTaskHour = 9
)

// ErrBillNotPending: the task's bill payment was already recorded (or the
// task isn't the bill's current one).
var ErrBillNotPending = errors.New("this bill payment was already recorded")

// FinanceService manages expenses, income, budgets and recurring bills.
// LifeOS only records money; it never makes payments.
type FinanceService struct {
	Finance  repository.FinanceRepository
	Settings repository.SettingsRepository
	Tasks    TaskService // bill tasks
	// Events publishes budget.exceeded and bill.due_soon; nil turns them off.
	Events EventPublisher
	Now    func() time.Time // overridable for tests
}

func (s FinanceService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// today is the user's local date.
func (s FinanceService) today(ctx context.Context, userID uint64) (time.Time, models.UserSettings, error) {
	settings, err := s.Settings.Get(ctx, userID)
	if err != nil {
		return time.Time{}, settings, err
	}
	l := s.now().In(settings.Location())
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC), settings, nil
}

// FormatMoney writes an amount for people, e.g. "1,250.00 BDT".
func FormatMoney(m models.Money, currency string) string {
	s := m.String()
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String() + "." + frac + " " + currency
	if neg {
		out = "-" + out
	}
	return out
}

// monthBounds returns the first and last day of the month "YYYY-MM", or of
// the month containing today when raw is empty.
func monthBounds(c *checker, field, raw string, today time.Time) (models.Date, models.Date) {
	start := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	if raw != "" {
		t, err := time.Parse("2006-01", raw)
		if err != nil {
			c.fail(field, "must be a month like 2026-10")
		} else {
			start = t
		}
	}
	return models.Date{Time: start}, models.Date{Time: start.AddDate(0, 1, -1)}
}

func (c *checker) amount(field string, m *models.Money) models.Money {
	if m == nil {
		c.fail(field, "is required")
		return 0
	}
	if *m <= 0 || *m > models.MaxMoney {
		c.fail(field, "must be more than 0")
	}
	return *m
}

func (c *checker) category(field, v string) string {
	return c.requiredText(field, v, 40)
}

// page reads page/per_page query values.
func page(c *checker, rawPage, rawPerPage string) PageMeta {
	meta := PageMeta{Page: positiveInt(c, "page", rawPage, 1), PerPage: positiveInt(c, "per_page", rawPerPage, defaultPerPage)}
	if meta.PerPage > maxPerPage {
		c.fail("per_page", "must be at most "+strconv.Itoa(maxPerPage))
	}
	return meta
}

// ---- Expenses ---------------------------------------------------------------

type ListExpensesInput struct {
	Month    string // YYYY-MM; or From/To
	From, To string // YYYY-MM-DD
	Category string
	Query    string
	BillID   string
	Page     string
	PerPage  string
}

// ListMeta is a page of results with the total amount of all of them.
type ListMeta struct {
	PageMeta
	Sum models.Money `json:"sum"`
}

func (s FinanceService) ListExpenses(ctx context.Context, userID uint64, in ListExpensesInput) ([]models.Expense, ListMeta, error) {
	var c checker
	f := repository.ExpenseFilter{Category: strings.TrimSpace(in.Category), Query: strings.TrimSpace(in.Query)}
	if err := s.period(ctx, &c, userID, in.Month, in.From, in.To, &f.From, &f.To); err != nil {
		return nil, ListMeta{}, err
	}
	if in.BillID != "" {
		id, err := strconv.ParseUint(in.BillID, 10, 64)
		if err != nil {
			c.fail("bill_id", "must be an ID")
		}
		f.BillID = &id
	}
	meta := page(&c, in.Page, in.PerPage)
	if err := c.err(); err != nil {
		return nil, ListMeta{PageMeta: meta}, err
	}
	f.Limit, f.Offset = meta.PerPage, (meta.Page-1)*meta.PerPage
	list, total, sum, err := s.Finance.ListExpenses(ctx, userID, f)
	meta.Total = total
	return list, ListMeta{PageMeta: meta, Sum: sum}, err
}

// period fills from/to from a month or explicit dates.
func (s FinanceService) period(ctx context.Context, c *checker, userID uint64, month, from, to string, dFrom, dTo **models.Date) error {
	if month != "" {
		today, _, err := s.today(ctx, userID)
		if err != nil {
			return err
		}
		a, b := monthBounds(c, "month", month, today)
		*dFrom, *dTo = &a, &b
		return nil
	}
	*dFrom, *dTo = c.parseDate("from", from), c.parseDate("to", to)
	return nil
}

type ExpenseInput struct {
	Amount      *models.Money `json:"amount"`
	Category    string        `json:"category"`
	Description *string       `json:"description"`
	SpentOn     string        `json:"spent_on"` // default today
}

func (s FinanceService) CreateExpense(ctx context.Context, userID uint64, in ExpenseInput) (models.Expense, error) {
	today, settings, err := s.today(ctx, userID)
	if err != nil {
		return models.Expense{}, err
	}
	var c checker
	e := models.Expense{UserID: userID, Amount: c.amount("amount", in.Amount), Category: c.category("category", in.Category), Description: optionalText(in.Description)}
	if e.Description != nil {
		c.maxLen("description", *e.Description, 255)
	}
	e.SpentOn = models.Date{Time: today}
	if d := c.parseDate("spent_on", in.SpentOn); d != nil {
		e.SpentOn = *d
	}
	if err := c.err(); err != nil {
		return e, err
	}
	err = repository.InTx(ctx, s.Finance.DB, func(db repository.DBTX) error {
		id, err := (repository.FinanceRepository{DB: db}).CreateExpense(ctx, e)
		if err != nil {
			return err
		}
		e.ID = id
		return s.checkBudgets(ctx, db, userID, e.Category, e.SpentOn, today, settings.Currency)
	})
	if err != nil {
		return e, err
	}
	return s.Finance.GetExpense(ctx, userID, e.ID)
}

type UpdateExpenseInput struct {
	Amount      Optional[models.Money] `json:"amount"`
	Category    Optional[string]       `json:"category"`
	Description Optional[string]       `json:"description"`
	SpentOn     Optional[string]       `json:"spent_on"`
}

func (s FinanceService) UpdateExpense(ctx context.Context, userID, id uint64, in UpdateExpenseInput) (models.Expense, error) {
	e, err := s.Finance.GetExpense(ctx, userID, id)
	if err != nil {
		return e, err
	}
	today, settings, err := s.today(ctx, userID)
	if err != nil {
		return e, err
	}
	var c checker
	if in.Amount.Set {
		e.Amount = c.amount("amount", in.Amount.Ptr())
	}
	if in.Category.Set {
		e.Category = c.category("category", in.Category.Value)
	}
	if in.Description.Set {
		e.Description = optionalText(in.Description.Ptr())
		if e.Description != nil {
			c.maxLen("description", *e.Description, 255)
		}
	}
	if in.SpentOn.Set {
		if d := c.parseDate("spent_on", in.SpentOn.Value); d != nil {
			e.SpentOn = *d
		} else {
			c.fail("spent_on", "is required")
		}
	}
	if err := c.err(); err != nil {
		return e, err
	}
	err = repository.InTx(ctx, s.Finance.DB, func(db repository.DBTX) error {
		if err := (repository.FinanceRepository{DB: db}).UpdateExpense(ctx, e); err != nil {
			return err
		}
		return s.checkBudgets(ctx, db, userID, e.Category, e.SpentOn, today, settings.Currency)
	})
	if err != nil {
		return e, err
	}
	return s.Finance.GetExpense(ctx, userID, id)
}

func (s FinanceService) DeleteExpense(ctx context.Context, userID, id uint64) error {
	return s.Finance.DeleteExpense(ctx, userID, id)
}

// checkBudgets sends a one-time alert per budget and month when this
// month's spending reaches the budget's alert level, and again at 100%
// (which also publishes budget.exceeded). Spending in other months is left
// alone: an alert about last month helps nobody.
func (s FinanceService) checkBudgets(ctx context.Context, db repository.DBTX, userID uint64, category string, spentOn models.Date, today time.Time, currency string) error {
	if spentOn.Year() != today.Year() || spentOn.Month() != today.Month() {
		return nil
	}
	repo := repository.FinanceRepository{DB: db}
	budgets, err := repo.BudgetsFor(ctx, userID, category)
	if err != nil {
		return err
	}
	from := models.Date{Time: time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)}
	to := models.Date{Time: from.AddDate(0, 1, -1)}
	month := today.Format("2006-01")
	for _, b := range budgets {
		spent, err := repo.SpentInCategory(ctx, userID, b.Category, from, to)
		if err != nil {
			return err
		}
		percent := int(int64(spent) * 100 / int64(b.MonthlyLimit))
		name := budgetName(b)
		for _, level := range alertLevels(b.AlertPercent) {
			if percent < level {
				continue
			}
			title := fmt.Sprintf("%s budget: %d%% used", name, level)
			if level >= 100 {
				title = name + " budget exceeded"
			}
			body := fmt.Sprintf("Spent %s of %s this month.", FormatMoney(spent, currency), FormatMoney(b.MonthlyLimit, currency))
			link := "/finance"
			created, err := repository.NotificationRepository{DB: db}.Insert(ctx, models.Notification{
				UserID: userID, Kind: "finance", Title: title, Body: &body, Link: &link, Priority: map[bool]string{true: "high", false: "normal"}[level >= 100],
				IdempotencyKey: fmt.Sprintf("budget:%d:%s:%d", b.ID, month, level),
			})
			if err != nil {
				return err
			}
			if created && level >= 100 {
				err := publish(ctx, s.Events, db, events.Event{
					Type: TriggerBudgetExceeded, UserID: userID, OccurredAt: s.now(), Key: fmt.Sprintf("budget.exceeded:%d:%s", b.ID, month),
					Data: map[string]any{"budget": map[string]any{
						"id": b.ID, "category": name, "limit": FormatMoney(b.MonthlyLimit, currency), "spent": FormatMoney(spent, currency), "month": month,
					}},
				})
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func alertLevels(alertPercent int) []int {
	if alertPercent >= 100 || alertPercent <= 0 {
		return []int{100}
	}
	return []int{alertPercent, 100}
}

func budgetName(b models.Budget) string {
	if b.Category == nil {
		return "Overall"
	}
	return *b.Category
}

// ---- Income -----------------------------------------------------------------

type ListIncomesInput struct {
	Month, From, To string
	Page, PerPage   string
}

func (s FinanceService) ListIncomes(ctx context.Context, userID uint64, in ListIncomesInput) ([]models.Income, ListMeta, error) {
	var c checker
	var f repository.IncomeFilter
	if err := s.period(ctx, &c, userID, in.Month, in.From, in.To, &f.From, &f.To); err != nil {
		return nil, ListMeta{}, err
	}
	meta := page(&c, in.Page, in.PerPage)
	if err := c.err(); err != nil {
		return nil, ListMeta{PageMeta: meta}, err
	}
	f.Limit, f.Offset = meta.PerPage, (meta.Page-1)*meta.PerPage
	list, total, sum, err := s.Finance.ListIncomes(ctx, userID, f)
	meta.Total = total
	return list, ListMeta{PageMeta: meta, Sum: sum}, err
}

type IncomeInput struct {
	Amount      *models.Money `json:"amount"`
	Source      string        `json:"source"`
	Description *string       `json:"description"`
	ReceivedOn  string        `json:"received_on"` // default today
}

func (s FinanceService) CreateIncome(ctx context.Context, userID uint64, in IncomeInput) (models.Income, error) {
	today, _, err := s.today(ctx, userID)
	if err != nil {
		return models.Income{}, err
	}
	var c checker
	i := models.Income{UserID: userID, Amount: c.amount("amount", in.Amount), Source: c.requiredText("source", in.Source, 100), Description: optionalText(in.Description)}
	if i.Description != nil {
		c.maxLen("description", *i.Description, 255)
	}
	i.ReceivedOn = models.Date{Time: today}
	if d := c.parseDate("received_on", in.ReceivedOn); d != nil {
		i.ReceivedOn = *d
	}
	if err := c.err(); err != nil {
		return i, err
	}
	id, err := s.Finance.CreateIncome(ctx, i)
	if err != nil {
		return i, err
	}
	return s.Finance.GetIncome(ctx, userID, id)
}

type UpdateIncomeInput struct {
	Amount      Optional[models.Money] `json:"amount"`
	Source      Optional[string]       `json:"source"`
	Description Optional[string]       `json:"description"`
	ReceivedOn  Optional[string]       `json:"received_on"`
}

func (s FinanceService) UpdateIncome(ctx context.Context, userID, id uint64, in UpdateIncomeInput) (models.Income, error) {
	i, err := s.Finance.GetIncome(ctx, userID, id)
	if err != nil {
		return i, err
	}
	var c checker
	if in.Amount.Set {
		i.Amount = c.amount("amount", in.Amount.Ptr())
	}
	if in.Source.Set {
		i.Source = c.requiredText("source", in.Source.Value, 100)
	}
	if in.Description.Set {
		i.Description = optionalText(in.Description.Ptr())
		if i.Description != nil {
			c.maxLen("description", *i.Description, 255)
		}
	}
	if in.ReceivedOn.Set {
		if d := c.parseDate("received_on", in.ReceivedOn.Value); d != nil {
			i.ReceivedOn = *d
		} else {
			c.fail("received_on", "is required")
		}
	}
	if err := c.err(); err != nil {
		return i, err
	}
	if err := s.Finance.UpdateIncome(ctx, i); err != nil {
		return i, err
	}
	return s.Finance.GetIncome(ctx, userID, id)
}

func (s FinanceService) DeleteIncome(ctx context.Context, userID, id uint64) error {
	return s.Finance.DeleteIncome(ctx, userID, id)
}

// ---- Budgets ----------------------------------------------------------------

// ListBudgets returns the budgets with this month's spending.
func (s FinanceService) ListBudgets(ctx context.Context, userID uint64, month string) ([]models.Budget, error) {
	today, _, err := s.today(ctx, userID)
	if err != nil {
		return nil, err
	}
	var c checker
	from, to := monthBounds(&c, "month", month, today)
	if err := c.err(); err != nil {
		return nil, err
	}
	list, err := s.Finance.ListBudgets(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Spent, err = s.Finance.SpentInCategory(ctx, userID, list[i].Category, from, to); err != nil {
			return nil, err
		}
	}
	return list, nil
}

type BudgetInput struct {
	Category     Optional[string]       `json:"category"` // null or "" for all spending
	MonthlyLimit Optional[models.Money] `json:"monthly_limit"`
	AlertPercent Optional[int]          `json:"alert_percent"`
}

func (s FinanceService) CreateBudget(ctx context.Context, userID uint64, in BudgetInput) (models.Budget, error) {
	b := models.Budget{UserID: userID, AlertPercent: 80}
	if !in.MonthlyLimit.Set {
		return b, &ValidationError{Fields: map[string]string{"monthly_limit": "is required"}}
	}
	if err := applyBudget(&b, in); err != nil {
		return b, err
	}
	id, err := s.Finance.CreateBudget(ctx, b)
	if errors.Is(err, repository.ErrDuplicate) {
		return b, &ValidationError{Fields: map[string]string{"category": "already has a budget"}}
	}
	if err != nil {
		return b, err
	}
	return s.getBudget(ctx, userID, id)
}

func (s FinanceService) UpdateBudget(ctx context.Context, userID, id uint64, in BudgetInput) (models.Budget, error) {
	b, err := s.Finance.GetBudget(ctx, userID, id)
	if err != nil {
		return b, err
	}
	if err := applyBudget(&b, in); err != nil {
		return b, err
	}
	if err := s.Finance.UpdateBudget(ctx, b); errors.Is(err, repository.ErrDuplicate) {
		return b, &ValidationError{Fields: map[string]string{"category": "already has a budget"}}
	} else if err != nil {
		return b, err
	}
	return s.getBudget(ctx, userID, id)
}

func applyBudget(b *models.Budget, in BudgetInput) error {
	var c checker
	if in.Category.Set {
		b.Category = optionalText(in.Category.Ptr())
		if b.Category != nil {
			c.maxLen("category", *b.Category, 40)
		}
	}
	if in.MonthlyLimit.Set {
		b.MonthlyLimit = c.amount("monthly_limit", in.MonthlyLimit.Ptr())
	}
	if in.AlertPercent.Set {
		if in.AlertPercent.Null || in.AlertPercent.Value < 1 || in.AlertPercent.Value > 100 {
			c.fail("alert_percent", "must be 1–100")
		} else {
			b.AlertPercent = in.AlertPercent.Value
		}
	}
	return c.err()
}

func (s FinanceService) getBudget(ctx context.Context, userID, id uint64) (models.Budget, error) {
	list, err := s.ListBudgets(ctx, userID, "")
	if err != nil {
		return models.Budget{}, err
	}
	for _, b := range list {
		if b.ID == id {
			return b, nil
		}
	}
	return models.Budget{}, repository.ErrNotFound
}

func (s FinanceService) DeleteBudget(ctx context.Context, userID, id uint64) error {
	return s.Finance.DeleteBudget(ctx, userID, id)
}

// ---- Bills ------------------------------------------------------------------

func (s FinanceService) ListBills(ctx context.Context, userID uint64) ([]models.Bill, error) {
	return s.Finance.ListBills(ctx, userID)
}

func (s FinanceService) GetBill(ctx context.Context, userID, id uint64) (models.Bill, error) {
	return s.Finance.GetBill(ctx, userID, id)
}

type BillInput struct {
	Name             Optional[string]       `json:"name"`
	Amount           Optional[models.Money] `json:"amount"`
	Category         Optional[string]       `json:"category"`
	Frequency        Optional[string]       `json:"frequency"`
	NextDueOn        Optional[string]       `json:"next_due_on"`
	RemindDaysBefore Optional[int]          `json:"remind_days_before"`
	Active           Optional[bool]         `json:"active"`
	Notes            Optional[string]       `json:"notes"`
}

func (s FinanceService) CreateBill(ctx context.Context, userID uint64, in BillInput) (models.Bill, error) {
	missing := map[string]string{}
	for field, set := range map[string]bool{"name": in.Name.Set, "amount": in.Amount.Set, "next_due_on": in.NextDueOn.Set} {
		if !set {
			missing[field] = "is required"
		}
	}
	if len(missing) > 0 {
		return models.Bill{}, &ValidationError{Fields: missing}
	}
	b := models.Bill{UserID: userID, Category: "Utilities", Frequency: models.BillMonthly, RemindDaysBefore: 3, Active: true}
	if err := applyBill(&b, in, true); err != nil {
		return b, err
	}
	id, err := s.Finance.CreateBill(ctx, b)
	if err != nil {
		return b, err
	}
	return s.Finance.GetBill(ctx, userID, id)
}

func (s FinanceService) UpdateBill(ctx context.Context, userID, id uint64, in BillInput) (models.Bill, error) {
	b, err := s.Finance.GetBill(ctx, userID, id)
	if err != nil {
		return b, err
	}
	if err := applyBill(&b, in, false); err != nil {
		return b, err
	}
	if err := s.Finance.SaveBill(ctx, b); err != nil {
		return b, err
	}
	return s.Finance.GetBill(ctx, userID, id)
}

// applyBill validates and applies input. The day of month a monthly (or
// longer) bill is due is taken from next_due_on, so a bill due on the 31st
// stays on the last day of shorter months.
func applyBill(b *models.Bill, in BillInput, creating bool) error {
	var c checker
	if in.Name.Set {
		b.Name = c.requiredText("name", in.Name.Value, 100)
	}
	if in.Amount.Set {
		b.Amount = c.amount("amount", in.Amount.Ptr())
	}
	if in.Category.Set {
		b.Category = c.category("category", in.Category.Value)
	}
	if in.Frequency.Set {
		c.oneOf("frequency", in.Frequency.Value, models.BillFrequencies)
		b.Frequency = in.Frequency.Value
	}
	if in.NextDueOn.Set {
		if d := c.parseDate("next_due_on", in.NextDueOn.Value); d != nil {
			b.NextDueOn = *d
			day := d.Day()
			b.DueDay = &day
		} else {
			c.fail("next_due_on", "is required")
		}
	}
	if b.Frequency == models.BillWeekly {
		b.DueDay = nil
	} else if b.DueDay == nil && !b.NextDueOn.IsZero() {
		day := b.NextDueOn.Day()
		b.DueDay = &day
	}
	if in.RemindDaysBefore.Set {
		if in.RemindDaysBefore.Null || in.RemindDaysBefore.Value < 0 || in.RemindDaysBefore.Value > maxRemindDays {
			c.fail("remind_days_before", fmt.Sprintf("must be 0–%d", maxRemindDays))
		} else {
			b.RemindDaysBefore = in.RemindDaysBefore.Value
		}
	}
	if in.Active.Set {
		if in.Active.Null {
			c.fail("active", "must be true or false")
		}
		b.Active = in.Active.Value
	}
	if in.Notes.Set {
		b.Notes = optionalText(in.Notes.Ptr())
		if b.Notes != nil {
			c.maxLen("notes", *b.Notes, 1000)
		}
	}
	return c.err()
}

func (s FinanceService) DeleteBill(ctx context.Context, userID, id uint64) error {
	return s.Finance.DeleteBill(ctx, userID, id)
}

type PayBillInput struct {
	Amount *models.Money `json:"amount"`  // default the bill's amount
	PaidOn string        `json:"paid_on"` // default today
}

// PaidBill is the outcome of recording a bill payment.
type PaidBill struct {
	Bill    models.Bill    `json:"bill"`
	Expense models.Expense `json:"expense"`
}

// PayBill records a payment of the bill's current due date: an expense,
// and the next due date. Its "Pay …" task, if any, is completed (that
// completion doesn't record the payment again).
func (s FinanceService) PayBill(ctx context.Context, userID, id uint64, in PayBillInput) (PaidBill, error) {
	out, taskID, err := s.pay(ctx, userID, id, nil, in)
	if err != nil || taskID == nil {
		return out, err
	}
	if t, err := s.Tasks.Get(ctx, userID, *taskID); err == nil && t.Status != models.TaskCompleted && t.Status != models.TaskCancelled {
		if _, err := s.Tasks.Update(ctx, userID, *taskID, UpdateTaskInput{Status: Optional[string]{Set: true, Value: models.TaskCompleted}}); err != nil {
			return out, err
		}
	}
	return out, nil
}

// PayBillForTask records the payment when the bill's "Pay …" task was
// completed. ErrBillNotPending if this task's payment is already recorded.
func (s FinanceService) PayBillForTask(ctx context.Context, userID, billID, taskID uint64) (PaidBill, error) {
	out, _, err := s.pay(ctx, userID, billID, &taskID, PayBillInput{})
	return out, err
}

func (s FinanceService) pay(ctx context.Context, userID, id uint64, forTask *uint64, in PayBillInput) (PaidBill, *uint64, error) {
	today, settings, err := s.today(ctx, userID)
	if err != nil {
		return PaidBill{}, nil, err
	}
	var c checker
	paidOn := models.Date{Time: today}
	if d := c.parseDate("paid_on", in.PaidOn); d != nil {
		paidOn = *d
	}
	if in.Amount != nil {
		c.amount("amount", in.Amount)
	}
	if err := c.err(); err != nil {
		return PaidBill{}, nil, err
	}
	var out PaidBill
	var pendingTask *uint64
	err = repository.InTx(ctx, s.Finance.DB, func(db repository.DBTX) error {
		repo := repository.FinanceRepository{DB: db}
		b, err := repo.GetBillForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		if forTask != nil && (b.PendingTaskID == nil || *b.PendingTaskID != *forTask) {
			return ErrBillNotPending
		}
		amount := b.Amount
		if in.Amount != nil {
			amount = *in.Amount
		}
		desc := fmt.Sprintf("%s (due %s)", b.Name, b.NextDueOn.Format(models.DateLayout))
		e := models.Expense{UserID: userID, Amount: amount, Category: b.Category, Description: &desc, SpentOn: paidOn, BillID: &b.ID}
		if e.ID, err = repo.CreateExpense(ctx, e); err != nil {
			return err
		}
		pendingTask = b.PendingTaskID
		b.PendingTaskID, b.LastPaidOn = nil, &paidOn
		b.NextDueOn = models.Date{Time: b.NextDue(b.NextDueOn.Time)}
		if err := repo.SaveBill(ctx, b); err != nil {
			return err
		}
		if err := s.checkBudgets(ctx, db, userID, e.Category, e.SpentOn, today, settings.Currency); err != nil {
			return err
		}
		out.Bill, out.Expense = b, e
		return nil
	})
	if err != nil {
		return PaidBill{}, nil, err
	}
	if out.Bill, err = s.Finance.GetBill(ctx, userID, id); err != nil {
		return out, nil, err
	}
	out.Expense, err = s.Finance.GetExpense(ctx, userID, out.Expense.ID)
	return out, pendingTask, err
}

// BillTask is the "Pay …" task for a bill's current due date.
type BillTask struct {
	Bill    models.Bill `json:"bill"`
	Task    models.Task `json:"task"`
	Created bool        `json:"created"`
}

// CreateBillTask makes the "Pay …" task for the bill's current due date,
// due at 09:00 local on that day, unless it already has an open one.
// Completing it records the payment (with the "Record paid bills" workflow).
func (s FinanceService) CreateBillTask(ctx context.Context, userID, billID uint64) (BillTask, error) {
	b, err := s.Finance.GetBill(ctx, userID, billID)
	if err != nil {
		return BillTask{}, err
	}
	if b.PendingTaskID != nil {
		if t, err := s.Tasks.Get(ctx, userID, *b.PendingTaskID); err == nil && t.Status != models.TaskCompleted && t.Status != models.TaskCancelled {
			return BillTask{Bill: b, Task: t}, nil
		}
	}
	settings, err := s.Settings.Get(ctx, userID)
	if err != nil {
		return BillTask{}, err
	}
	loc := settings.Location()
	due := time.Date(b.NextDueOn.Year(), b.NextDueOn.Month(), b.NextDueOn.Day(), billTaskHour, 0, 0, 0, loc).Format(time.RFC3339)
	desc := fmt.Sprintf("Expected amount: %s. Completing this task records the payment.", FormatMoney(b.Amount, settings.Currency))
	t, err := s.Tasks.Create(ctx, userID, CreateTaskInput{Title: "Pay " + b.Name, Description: &desc, Priority: "high", DueAt: &due})
	if err != nil {
		return BillTask{}, err
	}
	linked := false
	err = repository.InTx(ctx, s.Finance.DB, func(db repository.DBTX) error {
		repo := repository.FinanceRepository{DB: db}
		locked, err := repo.GetBillForUpdate(ctx, userID, billID)
		if err != nil {
			return err
		}
		if locked.PendingTaskID != nil {
			if open, err := s.openTask(ctx, db, userID, *locked.PendingTaskID); err != nil || open {
				b = locked
				return err // another run linked a task meanwhile
			}
		}
		locked.PendingTaskID, linked = &t.ID, true
		b = locked
		return repo.SaveBill(ctx, locked)
	})
	if err != nil {
		return BillTask{}, err
	}
	if !linked {
		_ = s.Tasks.Delete(ctx, userID, t.ID)
		existing, err := s.Tasks.Get(ctx, userID, *b.PendingTaskID)
		return BillTask{Bill: b, Task: existing}, err
	}
	return BillTask{Bill: b, Task: t, Created: true}, nil
}

func (s FinanceService) openTask(ctx context.Context, db repository.DBTX, userID, taskID uint64) (bool, error) {
	t, err := repository.TaskRepository{DB: db}.Get(ctx, userID, taskID)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	return err == nil && t.Status != models.TaskCompleted && t.Status != models.TaskCancelled, err
}

// BillData is a bill's snapshot for automations.
func BillData(b models.Bill, currency string) map[string]any {
	return map[string]any{
		"id": b.ID, "name": b.Name, "amount": FormatMoney(b.Amount, currency), "category": b.Category,
		"due": b.NextDueOn.Format("Mon 2 Jan"), "due_on": b.NextDueOn.Format(models.DateLayout),
	}
}

// ScanBills publishes bill.due_soon for each active bill whose reminder
// day has come (remind_days_before its due date, in the user's time zone),
// once per bill and due date (repeats are dropped by the event's key).
// Bills already past due are included, so a worker that was down still
// reminds. It returns how many bills were due a reminder.
func (s FinanceService) ScanBills(ctx context.Context) (int, error) {
	if s.Events == nil {
		return 0, nil
	}
	bills, err := s.Finance.ListBillsDueBy(ctx, s.now().AddDate(0, 0, maxRemindDays+1))
	if err != nil {
		return 0, err
	}
	settings := map[uint64]models.UserSettings{}
	n := 0
	for _, b := range bills {
		st, ok := settings[b.UserID]
		if !ok {
			if st, err = s.Settings.Get(ctx, b.UserID); err != nil {
				return n, err
			}
			settings[b.UserID] = st
		}
		l := s.now().In(st.Location())
		today := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
		if today.Before(b.NextDueOn.AddDate(0, 0, -b.RemindDaysBefore)) {
			continue
		}
		data := BillData(b, st.Currency)
		data["days_left"] = int(b.NextDueOn.Sub(today).Hours() / 24)
		err := s.Events.Publish(ctx, s.Finance.DB, events.Event{
			Type: TriggerBillDueSoon, UserID: b.UserID, OccurredAt: s.now(),
			Key:  fmt.Sprintf("bill.due_soon:%d:%s", b.ID, b.NextDueOn.Format(models.DateLayout)),
			Data: map[string]any{"bill": data},
		})
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ---- Summary ----------------------------------------------------------------

// Summary is one month's money at a glance.
type Summary struct {
	Month      string                     `json:"month"` // YYYY-MM
	Currency   string                     `json:"currency"`
	Income     models.Money               `json:"income"`
	Expenses   models.Money               `json:"expenses"`
	Net        models.Money               `json:"net"`
	LastMonth  models.Money               `json:"last_month_expenses"`
	ByCategory []repository.CategoryTotal `json:"by_category"`
	ByDay      []repository.DayTotal      `json:"by_day"`
	Budgets    []models.Budget            `json:"budgets"`
	Bills      []models.Bill              `json:"upcoming_bills"` // active, due within 30 days or overdue
}

func (s FinanceService) Summary(ctx context.Context, userID uint64, month string) (Summary, error) {
	today, settings, err := s.today(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	var c checker
	from, to := monthBounds(&c, "month", month, today)
	if err := c.err(); err != nil {
		return Summary{}, err
	}
	out := Summary{Month: from.Format("2006-01"), Currency: settings.Currency}
	if out.ByCategory, err = s.Finance.ExpensesByCategory(ctx, userID, from, to); err != nil {
		return out, err
	}
	for _, ct := range out.ByCategory {
		out.Expenses += ct.Total
	}
	if out.ByDay, err = s.Finance.ExpensesByDay(ctx, userID, from, to); err != nil {
		return out, err
	}
	if _, _, out.Income, err = s.Finance.ListIncomes(ctx, userID, repository.IncomeFilter{From: &from, To: &to, Limit: 1}); err != nil {
		return out, err
	}
	out.Net = out.Income - out.Expenses
	lastFrom := models.Date{Time: from.AddDate(0, -1, 0)}
	lastTo := models.Date{Time: from.AddDate(0, 0, -1)}
	if out.LastMonth, err = s.Finance.SpentInCategory(ctx, userID, nil, lastFrom, lastTo); err != nil {
		return out, err
	}
	if out.Budgets, err = s.ListBudgets(ctx, userID, out.Month); err != nil {
		return out, err
	}
	bills, err := s.Finance.ListBills(ctx, userID)
	if err != nil {
		return out, err
	}
	out.Bills = []models.Bill{}
	horizon := today.AddDate(0, 0, 30)
	for _, b := range bills {
		if b.Active && !b.NextDueOn.After(horizon) {
			out.Bills = append(out.Bills, b)
		}
	}
	return out, nil
}

// Categories are the suggested categories plus the ones the user used.
func (s FinanceService) Categories(ctx context.Context, userID uint64) ([]string, error) {
	used, err := s.Finance.Categories(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := append([]string{}, used...)
	seen := map[string]bool{}
	for _, c := range used {
		seen[strings.ToLower(c)] = true
	}
	for _, c := range models.DefaultExpenseCategories {
		if !seen[strings.ToLower(c)] {
			out = append(out, c)
		}
	}
	return out, nil
}
