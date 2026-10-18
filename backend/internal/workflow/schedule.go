package workflow

import (
	"fmt"
	"slices"
	"time"

	"lifeos-ai/backend/internal/models"
)

// validateSchedule checks a schedule and returns field errors keyed under
// trigger.schedule.
func validateSchedule(s *models.Schedule, now time.Time, loc *time.Location) map[string]string {
	errs := map[string]string{}
	if s == nil {
		errs["trigger.schedule"] = "is required for a schedule trigger"
		return errs
	}
	if _, _, ok := parseClock(s.Time); !ok {
		errs["trigger.schedule.time"] = "must be HH:MM"
	}
	switch s.Frequency {
	case "daily":
	case "weekly":
		if len(s.Weekdays) == 0 {
			errs["trigger.schedule.weekdays"] = "choose at least one day"
		}
		for _, d := range s.Weekdays {
			if d < 1 || d > 7 {
				errs["trigger.schedule.weekdays"] = "days are 1 (Monday) to 7 (Sunday)"
			}
		}
	case "monthly":
		if s.MonthDay < 1 || s.MonthDay > 31 {
			errs["trigger.schedule.month_day"] = "must be 1–31"
		}
	case "once":
		if _, err := time.ParseInLocation("2006-01-02", s.Date, loc); err != nil {
			errs["trigger.schedule.date"] = "must be YYYY-MM-DD"
		} else if len(errs) == 0 {
			if next := NextRun(*s, loc, now); next == nil {
				errs["trigger.schedule.date"] = "must be in the future"
			}
		}
	default:
		errs["trigger.schedule.frequency"] = "must be daily, weekly, monthly or once"
	}
	return errs
}

// normalizeSchedule drops the fields a frequency doesn't use, and sorts
// and de-duplicates weekdays.
func normalizeSchedule(s *models.Schedule) {
	switch s.Frequency {
	case "weekly":
		slices.Sort(s.Weekdays)
		s.Weekdays = slices.Compact(s.Weekdays)
		s.MonthDay, s.Date = 0, ""
	case "monthly":
		s.Weekdays, s.Date = nil, ""
	case "once":
		s.Weekdays, s.MonthDay = nil, 0
	default:
		s.Weekdays, s.MonthDay, s.Date = nil, 0, ""
	}
}

// NextRun is the first time strictly after `after` that the schedule fires,
// or nil if it never will (a one-time schedule in the past). Times are wall
// clock times in loc, so a 07:00 schedule stays at 07:00 across daylight
// saving changes; a time that doesn't exist that day (in the spring-forward
// gap) moves forward by the gap.
func NextRun(s models.Schedule, loc *time.Location, after time.Time) *time.Time {
	hh, mm, ok := parseClock(s.Time)
	if !ok {
		return nil
	}
	local := after.In(loc)
	at := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, hh, mm, 0, 0, loc) }
	found := func(t time.Time) *time.Time { u := t.UTC(); return &u }

	switch s.Frequency {
	case "once":
		d, err := time.ParseInLocation("2006-01-02", s.Date, loc)
		if err != nil {
			return nil
		}
		if t := at(d.Year(), d.Month(), d.Day()); t.After(after) {
			return found(t)
		}
		return nil
	case "daily", "weekly":
		for i := 0; i <= 7; i++ {
			day := local.AddDate(0, 0, i)
			t := at(day.Year(), day.Month(), day.Day())
			if !t.After(after) {
				continue
			}
			if s.Frequency == "daily" || slices.Contains(s.Weekdays, isoWeekday(t)) {
				return found(t)
			}
		}
	case "monthly":
		first := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
		for i := 0; i <= 12; i++ {
			m := first.AddDate(0, i, 0)
			t := at(m.Year(), m.Month(), min(s.MonthDay, daysIn(m.Year(), m.Month())))
			if t.After(after) {
				return found(t)
			}
		}
	}
	return nil
}

// Describe is a short human summary, e.g. "Weekdays at 07:30".
func Describe(s models.Schedule) string {
	switch s.Frequency {
	case "daily":
		return "Every day at " + s.Time
	case "weekly":
		names := []string{"", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
		if slices.Equal(s.Weekdays, []int{1, 2, 3, 4, 5}) {
			return "Weekdays at " + s.Time
		}
		out := ""
		for i, d := range s.Weekdays {
			if d >= 1 && d <= 7 {
				if i > 0 {
					out += ", "
				}
				out += names[d]
			}
		}
		return out + " at " + s.Time
	case "monthly":
		return fmt.Sprintf("Monthly on day %d at %s", s.MonthDay, s.Time)
	case "once":
		return "Once on " + s.Date + " at " + s.Time
	}
	return ""
}

func parseClock(s string) (h, m int, ok bool) {
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != 5 {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}

func isoWeekday(t time.Time) int {
	if d := int(t.Weekday()); d != 0 {
		return d
	}
	return 7
}

func daysIn(y int, m time.Month) int { return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day() }
