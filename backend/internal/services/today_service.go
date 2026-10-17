package services

import (
	"context"
	"sort"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

const (
	todayTaskLimit     = 100
	todayDoneLimit     = 50
	todayReminderLimit = 50
	todayNoteLimit     = 10
	// todayExpiringDays is how far ahead "expiring soon" looks.
	todayExpiringDays = 30
)

// TodayService assembles the dashboard for the user's current day.
type TodayService struct {
	Tasks     repository.TaskRepository
	Calendar  CalendarService
	Reminders repository.ReminderRepository
	Notes     repository.NoteRepository
	Now       func() time.Time // overridable for tests
}

type TodaySummary struct {
	Overdue        int `json:"overdue"`
	DueToday       int `json:"due_today"`
	CompletedToday int `json:"completed_today"`
	Events         int `json:"events"`
	Conflicts      int `json:"conflicts"` // events today that overlap another
}

// Today is the user's day: the bounds are local midnight to midnight in the
// user's time zone, as UTC instants.
type Today struct {
	Date           models.Date              `json:"date"`
	Timezone       string                   `json:"timezone"`
	StartsAt       time.Time                `json:"starts_at"`
	EndsAt         time.Time                `json:"ends_at"`
	Summary        TodaySummary             `json:"summary"`
	Overdue        []models.Task            `json:"overdue"`         // open, due before today (oldest first)
	DueToday       []models.Task            `json:"due_today"`       // open, due today
	CompletedToday []models.Task            `json:"completed_today"` // completed today, any due date
	Events         []models.EventOccurrence `json:"events"`
	Reminders      []models.Reminder        `json:"reminders"`      // scheduled to fire before the day ends
	ExpiringNotes  []models.Note            `json:"expiring_notes"` // expired or expiring within 30 days
}

// DayBounds returns local midnight of now's date in loc and the next
// midnight. Using the calendar (not 24 h) keeps daylight-saving days right.
func DayBounds(now time.Time, loc *time.Location) (start, end time.Time) {
	y, m, d := now.In(loc).Date()
	start = time.Date(y, m, d, 0, 0, 0, 0, loc)
	return start.UTC(), start.AddDate(0, 0, 1).UTC()
}

func (s TodayService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s TodayService) Get(ctx context.Context, userID uint64) (Today, error) {
	settings, err := repository.SettingsRepository{DB: s.Tasks.DB}.Get(ctx, userID)
	if err != nil {
		return Today{}, err
	}
	loc, now := settings.Location(), s.now()
	start, end := DayBounds(now, loc)
	date := localToday(now, loc)
	out := Today{Date: date, Timezone: settings.Timezone, StartsAt: start, EndsAt: end}

	open := []string{models.TaskPending, models.TaskInProgress}
	if out.Overdue, out.Summary.Overdue, err = s.Tasks.List(ctx, userID, repository.TaskFilter{
		Statuses: open, IncludeSubtasks: true, DueTo: &start, Sort: "due_at", Limit: todayTaskLimit,
	}); err != nil {
		return Today{}, err
	}
	if out.DueToday, out.Summary.DueToday, err = s.Tasks.List(ctx, userID, repository.TaskFilter{
		Statuses: open, IncludeSubtasks: true, DueFrom: &start, DueTo: &end, Sort: "due_at", Limit: todayTaskLimit,
	}); err != nil {
		return Today{}, err
	}
	if out.CompletedToday, out.Summary.CompletedToday, err = s.Tasks.List(ctx, userID, repository.TaskFilter{
		Statuses: []string{models.TaskCompleted}, IncludeSubtasks: true, CompletedFrom: &start, CompletedTo: &end,
		Sort: "-created_at", Limit: todayDoneLimit,
	}); err != nil {
		return Today{}, err
	}

	occs, err := s.Calendar.occurrences(ctx, userID, start, end)
	if err != nil {
		return Today{}, err
	}
	out.Events = []models.EventOccurrence{}
	for _, o := range occs {
		onToday := o.StartsAt.Before(end) && o.EndsAt.After(start)
		if o.AllDay { // calendar dates: compare with the local date
			onToday = !o.StartDate.After(date.Time) && !o.EndDate.Before(date.Time)
		}
		if onToday {
			out.Events = append(out.Events, o)
			if len(o.ConflictsWith) > 0 {
				out.Summary.Conflicts++
			}
		}
	}
	out.Summary.Events = len(out.Events)
	// All-day events first: they're stored at UTC midnight, which can sort
	// after morning events in the user's zone.
	sort.SliceStable(out.Events, func(a, b int) bool { return out.Events[a].AllDay && !out.Events[b].AllDay })

	rems, _, err := s.Reminders.List(ctx, userID, repository.ReminderFilter{
		Statuses: []string{models.ReminderScheduled}, Limit: todayReminderLimit,
	})
	if err != nil {
		return Today{}, err
	}
	out.Reminders = []models.Reminder{}
	for _, r := range rems { // ordered by remind_at, inactive ones last
		if r.RemindAt == nil || !r.RemindAt.Before(end) {
			break
		}
		out.Reminders = append(out.Reminders, r)
	}

	expiring := models.Date{Time: date.Time.AddDate(0, 0, todayExpiringDays)}
	if out.ExpiringNotes, _, err = s.Notes.List(ctx, userID, repository.NoteFilter{
		ExpiringBefore: &expiring, Sort: "expires_on", Limit: todayNoteLimit,
	}); err != nil {
		return Today{}, err
	}
	return out, nil
}
