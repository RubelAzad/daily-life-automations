package models

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Money is an amount in cents (minor units). In JSON it is a number with
// at most two decimals, e.g. 1250.5; it is parsed from the text, never
// through a float, so it is exact.
type Money int64

// MaxMoney keeps amounts sane: 100 billion.
const MaxMoney Money = 100_000_000_000_00

func (m Money) MarshalJSON() ([]byte, error) { return []byte(m.String()), nil }

// String is the amount with two decimals, e.g. "-12.50".
func (m Money) String() string {
	sign, v := "", int64(m)
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

func (m *Money) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`) // "12.50" is accepted too
	v, err := ParseMoney(s)
	if err != nil {
		// A type error names the field, so the API answers 422 with it.
		return &json.UnmarshalTypeError{Value: "an amount with more than two decimals", Type: reflect.TypeOf(m).Elem()}
	}
	*m = v
	return nil
}

// ParseMoney reads "12", "12.5" or "12.50" (a leading minus is allowed;
// callers check the sign).
func ParseMoney(s string) (Money, error) {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" || len(whole) > 12 || (hasFrac && (frac == "" || len(frac) > 2)) || !digits(whole) || !digits(frac) {
		return 0, fmt.Errorf("must be an amount with at most two decimals")
	}
	w, _ := strconv.ParseInt(whole, 10, 64)
	f := int64(0)
	if frac != "" {
		f, _ = strconv.ParseInt(frac, 10, 64)
		if len(frac) == 1 {
			f *= 10
		}
	}
	v := Money(w*100 + f)
	if neg {
		v = -v
	}
	return v, nil
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Expense is money spent.
type Expense struct {
	ID          uint64    `json:"id"`
	UserID      uint64    `json:"-"`
	Amount      Money     `json:"amount"`
	Category    string    `json:"category"`
	Description *string   `json:"description"`
	SpentOn     Date      `json:"spent_on"`
	BillID      *uint64   `json:"bill_id"` // the recurring bill it paid
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Income is money received.
type Income struct {
	ID          uint64    `json:"id"`
	UserID      uint64    `json:"-"`
	Amount      Money     `json:"amount"`
	Source      string    `json:"source"`
	Description *string   `json:"description"`
	ReceivedOn  Date      `json:"received_on"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Budget is a monthly spending limit for a category, or for all spending
// when Category is nil.
type Budget struct {
	ID           uint64    `json:"id"`
	UserID       uint64    `json:"-"`
	Category     *string   `json:"category"`
	MonthlyLimit Money     `json:"monthly_limit"`
	AlertPercent int       `json:"alert_percent"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	// Spent is this month's spending against it (set on reads).
	Spent Money `json:"spent"`
}

// Bill frequencies.
const (
	BillWeekly    = "weekly"
	BillMonthly   = "monthly"
	BillQuarterly = "quarterly"
	BillYearly    = "yearly"
)

var BillFrequencies = []string{BillWeekly, BillMonthly, BillQuarterly, BillYearly}

// Bill is a recurring payment such as rent or electricity.
type Bill struct {
	ID               uint64  `json:"id"`
	UserID           uint64  `json:"-"`
	Name             string  `json:"name"`
	Amount           Money   `json:"amount"` // expected
	Category         string  `json:"category"`
	Frequency        string  `json:"frequency"`
	DueDay           *int    `json:"due_day"` // day of month for monthly and longer
	NextDueOn        Date    `json:"next_due_on"`
	RemindDaysBefore int     `json:"remind_days_before"`
	Active           bool    `json:"active"`
	Notes            *string `json:"notes"`
	// PendingTaskID is the "Pay …" task for the current due date, if any.
	PendingTaskID *uint64   `json:"pending_task_id"`
	LastPaidOn    *Date     `json:"last_paid_on"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NextDue is the due date after d for a bill paid on schedule.
func (b Bill) NextDue(d time.Time) time.Time {
	switch b.Frequency {
	case BillWeekly:
		return d.AddDate(0, 0, 7)
	case BillQuarterly:
		return addMonths(d, 3, b.DueDay)
	case BillYearly:
		return addMonths(d, 12, b.DueDay)
	default:
		return addMonths(d, 1, b.DueDay)
	}
}

// addMonths moves d n months on, to dueDay (or d's day) clamped to the
// month's last day: a bill due on the 31st is due on 30 April.
func addMonths(d time.Time, n int, dueDay *int) time.Time {
	day := d.Day()
	if dueDay != nil {
		day = *dueDay
	}
	first := time.Date(d.Year(), d.Month()+time.Month(n), 1, 0, 0, 0, 0, d.Location())
	last := first.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, d.Location())
}

// DefaultExpenseCategories are suggested in forms; any text is allowed.
var DefaultExpenseCategories = []string{
	"Groceries", "Dining", "Transport", "Rent", "Utilities", "Internet & phone", "Health", "Shopping",
	"Entertainment", "Education", "Travel", "Loan", "Insurance", "Other",
}
