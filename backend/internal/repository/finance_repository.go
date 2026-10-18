package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"lifeos-ai/backend/internal/models"
)

// FinanceRepository stores expenses, income, budgets and recurring bills.
type FinanceRepository struct {
	DB DBTX
}

// ---- Expenses ---------------------------------------------------------------

type ExpenseFilter struct {
	From, To *models.Date // inclusive
	Category string
	Query    string
	BillID   *uint64
	Limit    int
	Offset   int
}

const expenseColumns = `id, user_id, amount_cents, category, description, spent_on, bill_id, created_at, updated_at`

func scanExpense(row rowScanner) (models.Expense, error) {
	var e models.Expense
	err := row.Scan(&e.ID, &e.UserID, &e.Amount, &e.Category, &e.Description, &e.SpentOn, &e.BillID, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

func (r FinanceRepository) CreateExpense(ctx context.Context, e models.Expense) (uint64, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO expenses (user_id, amount_cents, category, description, spent_on, bill_id) VALUES (?, ?, ?, ?, ?, ?)`,
		e.UserID, e.Amount, e.Category, e.Description, e.SpentOn, e.BillID)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return uint64(id), err
}

func (r FinanceRepository) UpdateExpense(ctx context.Context, e models.Expense) error {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE expenses SET amount_cents = ?, category = ?, description = ?, spent_on = ? WHERE id = ? AND user_id = ?`,
		e.Amount, e.Category, e.Description, e.SpentOn, e.ID, e.UserID)
	return err
}

func (r FinanceRepository) GetExpense(ctx context.Context, userID, id uint64) (models.Expense, error) {
	e, err := scanExpense(r.DB.QueryRowContext(ctx, `SELECT `+expenseColumns+` FROM expenses WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

func (r FinanceRepository) DeleteExpense(ctx context.Context, userID, id uint64) error {
	return deleteOwned(ctx, r.DB, `DELETE FROM expenses WHERE id = ? AND user_id = ?`, id, userID)
}

// ListExpenses returns the newest first, and the total amount of all
// matching expenses.
func (r FinanceRepository) ListExpenses(ctx context.Context, userID uint64, f ExpenseFilter) ([]models.Expense, int, models.Money, error) {
	where := []string{"user_id = ?"}
	args := []any{userID}
	if f.From != nil {
		where, args = append(where, "spent_on >= ?"), append(args, *f.From)
	}
	if f.To != nil {
		where, args = append(where, "spent_on <= ?"), append(args, *f.To)
	}
	if f.Category != "" {
		where, args = append(where, "category = ?"), append(args, f.Category)
	}
	if f.Query != "" {
		p := "%" + escapeLike(f.Query) + "%"
		where, args = append(where, "(description LIKE ? OR category LIKE ?)"), append(args, p, p)
	}
	if f.BillID != nil {
		where, args = append(where, "bill_id = ?"), append(args, *f.BillID)
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	var sum models.Money
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(amount_cents), 0) FROM expenses WHERE `+whereSQL, args...).Scan(&total, &sum); err != nil {
		return nil, 0, 0, err
	}
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+expenseColumns+` FROM expenses WHERE `+whereSQL+` ORDER BY spent_on DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	out := []models.Expense{}
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, 0, 0, err
		}
		out = append(out, e)
	}
	return out, total, sum, rows.Err()
}

// CategoryTotal is spending in one category over a period.
type CategoryTotal struct {
	Category string       `json:"category"`
	Total    models.Money `json:"total"`
	Count    int          `json:"count"`
}

// ExpensesByCategory sums spending per category between from and to
// (inclusive), largest first.
func (r FinanceRepository) ExpensesByCategory(ctx context.Context, userID uint64, from, to models.Date) ([]CategoryTotal, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT category, SUM(amount_cents), COUNT(*) FROM expenses WHERE user_id = ? AND spent_on BETWEEN ? AND ?
		  GROUP BY category ORDER BY SUM(amount_cents) DESC, category`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CategoryTotal{}
	for rows.Next() {
		var c CategoryTotal
		if err := rows.Scan(&c.Category, &c.Total, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DayTotal is spending on one day.
type DayTotal struct {
	Date  models.Date  `json:"date"`
	Total models.Money `json:"total"`
}

func (r FinanceRepository) ExpensesByDay(ctx context.Context, userID uint64, from, to models.Date) ([]DayTotal, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT spent_on, SUM(amount_cents) FROM expenses WHERE user_id = ? AND spent_on BETWEEN ? AND ? GROUP BY spent_on ORDER BY spent_on`,
		userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DayTotal{}
	for rows.Next() {
		var d DayTotal
		if err := rows.Scan(&d.Date, &d.Total); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SpentInCategory sums spending between from and to; an empty category
// means all spending.
func (r FinanceRepository) SpentInCategory(ctx context.Context, userID uint64, category *string, from, to models.Date) (models.Money, error) {
	q := `SELECT COALESCE(SUM(amount_cents), 0) FROM expenses WHERE user_id = ? AND spent_on BETWEEN ? AND ?`
	args := []any{userID, from, to}
	if category != nil {
		q, args = q+` AND category = ?`, append(args, *category)
	}
	var m models.Money
	err := r.DB.QueryRowContext(ctx, q, args...).Scan(&m)
	return m, err
}

// Categories lists the categories the user has used, most used first.
func (r FinanceRepository) Categories(ctx context.Context, userID uint64) ([]string, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT category FROM (
		   SELECT category, COUNT(*) AS n FROM expenses WHERE user_id = ? GROUP BY category
		   UNION ALL SELECT category, 0 FROM recurring_bills WHERE user_id = ?
		   UNION ALL SELECT category, 0 FROM budgets WHERE user_id = ? AND category IS NOT NULL
		 ) c GROUP BY category ORDER BY SUM(n) DESC, category LIMIT 100`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- Income -----------------------------------------------------------------

type IncomeFilter struct {
	From, To *models.Date
	Limit    int
	Offset   int
}

const incomeColumns = `id, user_id, amount_cents, source, description, received_on, created_at, updated_at`

func scanIncome(row rowScanner) (models.Income, error) {
	var i models.Income
	err := row.Scan(&i.ID, &i.UserID, &i.Amount, &i.Source, &i.Description, &i.ReceivedOn, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

func (r FinanceRepository) CreateIncome(ctx context.Context, i models.Income) (uint64, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO incomes (user_id, amount_cents, source, description, received_on) VALUES (?, ?, ?, ?, ?)`,
		i.UserID, i.Amount, i.Source, i.Description, i.ReceivedOn)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return uint64(id), err
}

func (r FinanceRepository) UpdateIncome(ctx context.Context, i models.Income) error {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE incomes SET amount_cents = ?, source = ?, description = ?, received_on = ? WHERE id = ? AND user_id = ?`,
		i.Amount, i.Source, i.Description, i.ReceivedOn, i.ID, i.UserID)
	return err
}

func (r FinanceRepository) GetIncome(ctx context.Context, userID, id uint64) (models.Income, error) {
	i, err := scanIncome(r.DB.QueryRowContext(ctx, `SELECT `+incomeColumns+` FROM incomes WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return i, ErrNotFound
	}
	return i, err
}

func (r FinanceRepository) DeleteIncome(ctx context.Context, userID, id uint64) error {
	return deleteOwned(ctx, r.DB, `DELETE FROM incomes WHERE id = ? AND user_id = ?`, id, userID)
}

// ListIncomes returns the newest first, and the total of all matching.
func (r FinanceRepository) ListIncomes(ctx context.Context, userID uint64, f IncomeFilter) ([]models.Income, int, models.Money, error) {
	where := []string{"user_id = ?"}
	args := []any{userID}
	if f.From != nil {
		where, args = append(where, "received_on >= ?"), append(args, *f.From)
	}
	if f.To != nil {
		where, args = append(where, "received_on <= ?"), append(args, *f.To)
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	var sum models.Money
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(amount_cents), 0) FROM incomes WHERE `+whereSQL, args...).Scan(&total, &sum); err != nil {
		return nil, 0, 0, err
	}
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+incomeColumns+` FROM incomes WHERE `+whereSQL+` ORDER BY received_on DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	out := []models.Income{}
	for rows.Next() {
		i, err := scanIncome(rows)
		if err != nil {
			return nil, 0, 0, err
		}
		out = append(out, i)
	}
	return out, total, sum, rows.Err()
}

// ---- Budgets ----------------------------------------------------------------

const budgetColumns = `id, user_id, category, monthly_limit_cents, alert_percent, created_at, updated_at`

func scanBudget(row rowScanner) (models.Budget, error) {
	var b models.Budget
	err := row.Scan(&b.ID, &b.UserID, &b.Category, &b.MonthlyLimit, &b.AlertPercent, &b.CreatedAt, &b.UpdatedAt)
	return b, err
}

func (r FinanceRepository) CreateBudget(ctx context.Context, b models.Budget) (uint64, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO budgets (user_id, category, monthly_limit_cents, alert_percent) VALUES (?, ?, ?, ?)`,
		b.UserID, b.Category, b.MonthlyLimit, b.AlertPercent)
	if isDuplicate(err) {
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return uint64(id), err
}

func (r FinanceRepository) UpdateBudget(ctx context.Context, b models.Budget) error {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE budgets SET category = ?, monthly_limit_cents = ?, alert_percent = ? WHERE id = ? AND user_id = ?`,
		b.Category, b.MonthlyLimit, b.AlertPercent, b.ID, b.UserID)
	if isDuplicate(err) {
		return ErrDuplicate
	}
	return err
}

func (r FinanceRepository) GetBudget(ctx context.Context, userID, id uint64) (models.Budget, error) {
	b, err := scanBudget(r.DB.QueryRowContext(ctx, `SELECT `+budgetColumns+` FROM budgets WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

func (r FinanceRepository) DeleteBudget(ctx context.Context, userID, id uint64) error {
	return deleteOwned(ctx, r.DB, `DELETE FROM budgets WHERE id = ? AND user_id = ?`, id, userID)
}

// ListBudgets returns the overall budget first, then by category.
func (r FinanceRepository) ListBudgets(ctx context.Context, userID uint64) ([]models.Budget, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT `+budgetColumns+` FROM budgets WHERE user_id = ? ORDER BY category IS NOT NULL, category`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Budget{}
	for rows.Next() {
		b, err := scanBudget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// BudgetsFor returns the budgets an expense in category counts against:
// the category's own and the overall one.
func (r FinanceRepository) BudgetsFor(ctx context.Context, userID uint64, category string) ([]models.Budget, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+budgetColumns+` FROM budgets WHERE user_id = ? AND (category = ? OR category IS NULL)`, userID, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Budget{}
	for rows.Next() {
		b, err := scanBudget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---- Bills ------------------------------------------------------------------

const billColumns = `id, user_id, name, amount_cents, category, frequency, due_day, next_due_on, remind_days_before, active, notes,
	pending_task_id, last_paid_on, created_at, updated_at`

func scanBill(row rowScanner) (models.Bill, error) {
	var b models.Bill
	var lastPaid sql.NullTime
	err := row.Scan(&b.ID, &b.UserID, &b.Name, &b.Amount, &b.Category, &b.Frequency, &b.DueDay, &b.NextDueOn, &b.RemindDaysBefore, &b.Active, &b.Notes,
		&b.PendingTaskID, &lastPaid, &b.CreatedAt, &b.UpdatedAt)
	if lastPaid.Valid {
		b.LastPaidOn = &models.Date{Time: lastPaid.Time}
	}
	return b, err
}

func (r FinanceRepository) CreateBill(ctx context.Context, b models.Bill) (uint64, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO recurring_bills (user_id, name, amount_cents, category, frequency, due_day, next_due_on, remind_days_before, active, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.UserID, b.Name, b.Amount, b.Category, b.Frequency, b.DueDay, b.NextDueOn, b.RemindDaysBefore, b.Active, b.Notes)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return uint64(id), err
}

// SaveBill writes every field of a bill.
func (r FinanceRepository) SaveBill(ctx context.Context, b models.Bill) error {
	var lastPaid any
	if b.LastPaidOn != nil {
		lastPaid = *b.LastPaidOn
	}
	_, err := r.DB.ExecContext(ctx,
		`UPDATE recurring_bills SET name = ?, amount_cents = ?, category = ?, frequency = ?, due_day = ?, next_due_on = ?, remind_days_before = ?,
		        active = ?, notes = ?, pending_task_id = ?, last_paid_on = ?
		  WHERE id = ? AND user_id = ?`,
		b.Name, b.Amount, b.Category, b.Frequency, b.DueDay, b.NextDueOn, b.RemindDaysBefore, b.Active, b.Notes, b.PendingTaskID, lastPaid, b.ID, b.UserID)
	return err
}

func (r FinanceRepository) GetBill(ctx context.Context, userID, id uint64) (models.Bill, error) {
	return r.getBill(ctx, `SELECT `+billColumns+` FROM recurring_bills WHERE id = ? AND user_id = ?`, id, userID)
}

// GetBillForUpdate locks the bill row until the transaction ends.
func (r FinanceRepository) GetBillForUpdate(ctx context.Context, userID, id uint64) (models.Bill, error) {
	return r.getBill(ctx, `SELECT `+billColumns+` FROM recurring_bills WHERE id = ? AND user_id = ? FOR UPDATE`, id, userID)
}

// BillForTask returns the bill whose pending task this is.
func (r FinanceRepository) BillForTask(ctx context.Context, userID, taskID uint64) (models.Bill, error) {
	return r.getBill(ctx, `SELECT `+billColumns+` FROM recurring_bills WHERE pending_task_id = ? AND user_id = ?`, taskID, userID)
}

func (r FinanceRepository) getBill(ctx context.Context, q string, args ...any) (models.Bill, error) {
	b, err := scanBill(r.DB.QueryRowContext(ctx, q, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

func (r FinanceRepository) DeleteBill(ctx context.Context, userID, id uint64) error {
	return deleteOwned(ctx, r.DB, `DELETE FROM recurring_bills WHERE id = ? AND user_id = ?`, id, userID)
}

// ListBills returns the user's bills, soonest due first (inactive last).
func (r FinanceRepository) ListBills(ctx context.Context, userID uint64) ([]models.Bill, error) {
	return r.bills(ctx, `SELECT `+billColumns+` FROM recurring_bills WHERE user_id = ? ORDER BY active DESC, next_due_on, name`, userID)
}

// ListBillsDueBy returns active bills of any user due on or before d, for
// the reminder scan.
func (r FinanceRepository) ListBillsDueBy(ctx context.Context, d time.Time) ([]models.Bill, error) {
	return r.bills(ctx, `SELECT `+billColumns+` FROM recurring_bills WHERE active = 1 AND next_due_on <= ? ORDER BY next_due_on LIMIT 1000`, d.Format(models.DateLayout))
}

func (r FinanceRepository) bills(ctx context.Context, q string, args ...any) ([]models.Bill, error) {
	rows, err := r.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Bill{}
	for rows.Next() {
		b, err := scanBill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// deleteOwned runs a DELETE scoped to the user; ErrNotFound if nothing
// was deleted.
func deleteOwned(ctx context.Context, db DBTX, q string, args ...any) error {
	res, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}
