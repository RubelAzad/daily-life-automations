package services

import (
	"time"

	"lifeos-ai/backend/internal/models"
)

// maxOccurrenceSteps bounds the search for the next occurrence (≈ 27 years of
// daily repeats) so a malformed series can never loop forever.
const maxOccurrenceSteps = 10000

// nextOccurrence returns the first occurrence of the series strictly after
// both the previous due date and now, so completing a long-overdue daily task
// schedules the next upcoming day instead of a backlog of missed ones.
// Occurrences are computed from anchor (the series' first due date) so
// monthly dates don't drift. ok is false when the series has ended.
//
// TODO(step 1.8): compute in the user's time zone instead of UTC.
func nextOccurrence(r models.Recurrence, anchor, previousDue, now time.Time) (next time.Time, ok bool) {
	after := previousDue
	if now.After(after) {
		after = now
	}
	for k := 1; k <= maxOccurrenceSteps; k++ {
		candidate := occurrence(anchor, r.Frequency, k*r.Interval)
		if !candidate.After(after) {
			continue
		}
		if r.Until != nil && candidate.After(*r.Until) {
			return time.Time{}, false
		}
		return candidate, true
	}
	return time.Time{}, false
}

// occurrence returns anchor advanced by n periods of frequency.
func occurrence(anchor time.Time, frequency string, n int) time.Time {
	switch frequency {
	case "daily":
		return anchor.AddDate(0, 0, n)
	case "weekly":
		return anchor.AddDate(0, 0, 7*n)
	case "monthly":
		return addMonthsClamped(anchor, n)
	case "yearly":
		return addMonthsClamped(anchor, 12*n)
	}
	return anchor
}

// addMonthsClamped adds n months, clamping the day to the target month's
// length (Jan 31 + 1 month = Feb 28/29), unlike time.AddDate which overflows
// into the next month (Mar 3).
func addMonthsClamped(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	firstOfTarget := time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, t.Location())
	lastDay := firstOfTarget.AddDate(0, 1, -1).Day()
	if d > lastDay {
		d = lastDay
	}
	return time.Date(firstOfTarget.Year(), firstOfTarget.Month(), d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}
