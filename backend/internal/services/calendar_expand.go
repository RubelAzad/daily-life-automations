package services

import (
	"sort"
	"time"

	"lifeos-ai/backend/internal/models"
)

// expandEvent returns the occurrences of e that overlap [from, to). One-off
// events yield at most one occurrence. Recurring events are computed from the
// series start, so monthly dates don't drift. Timed events repeat at the same
// local time in loc (the user's time zone); all-day events are calendar dates
// stored at UTC midnight and repeat in UTC.
func expandEvent(e models.CalendarEvent, from, to time.Time, loc *time.Location) []models.EventOccurrence {
	duration := e.EndsAt.Sub(e.StartsAt)
	if e.Recurrence == nil {
		if e.StartsAt.Before(to) && e.EndsAt.After(from) {
			return []models.EventOccurrence{occurrenceAt(e, e.StartsAt, duration)}
		}
		return nil
	}

	var out []models.EventOccurrence
	r := e.Recurrence
	if e.AllDay {
		loc = time.UTC
	}
	anchor := e.StartsAt.In(loc)
	first := firstCandidateStep(e, from)
	for k := first; k < first+maxOccurrenceSteps; k++ {
		start := occurrence(anchor, r.Frequency, k*r.Interval).UTC()
		if !start.Before(to) || (r.Until != nil && start.After(*r.Until)) {
			break
		}
		if start.Add(duration).After(from) {
			out = append(out, occurrenceAt(e, start, duration))
		}
	}
	return out
}

// firstCandidateStep skips whole periods before from for fixed-length
// frequencies, so a daily event started years ago doesn't iterate every day.
// It starts one step early to catch an occurrence that began before from but
// is still running.
func firstCandidateStep(e models.CalendarEvent, from time.Time) int {
	var periodDays int
	switch e.Recurrence.Frequency {
	case "daily":
		periodDays = e.Recurrence.Interval
	case "weekly":
		periodDays = 7 * e.Recurrence.Interval
	default:
		return 0 // months/years vary in length; at most 12 steps per year anyway
	}
	if !from.After(e.StartsAt) {
		return 0
	}
	steps := int(from.Sub(e.StartsAt).Hours()/24) / periodDays
	if steps > 0 {
		steps--
	}
	return steps
}

func occurrenceAt(e models.CalendarEvent, start time.Time, duration time.Duration) models.EventOccurrence {
	e.StartsAt = start
	e.EndsAt = start.Add(duration)
	e.SetDates()
	return models.EventOccurrence{CalendarEvent: e, ConflictsWith: []uint64{}}
}

// markConflicts records, on each busy timed occurrence, the other events that
// overlap it. An event's prep/travel buffer counts as blocked time. Touching
// events (one ends exactly when the next is blocked) don't conflict, and
// occurrences of the same series never conflict with each other.
func markConflicts(occs []models.EventOccurrence) {
	var idx []int
	for i, o := range occs {
		if o.Busy && !o.AllDay {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(a, b int) bool { return occs[idx[a]].BlockedFrom().Before(occs[idx[b]].BlockedFrom()) })

	for a := 0; a < len(idx); a++ {
		x := &occs[idx[a]]
		for b := a + 1; b < len(idx); b++ {
			y := &occs[idx[b]]
			// Sorted by blocked start: once y starts after x ends, so do all later ones.
			if !y.BlockedFrom().Before(x.EndsAt) {
				break
			}
			if x.ID == y.ID {
				continue
			}
			x.ConflictsWith = appendUnique(x.ConflictsWith, y.ID)
			y.ConflictsWith = appendUnique(y.ConflictsWith, x.ID)
		}
	}
}

func appendUnique(ids []uint64, id uint64) []uint64 {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

// sortOccurrences orders by start; all-day events first on the same instant.
func sortOccurrences(occs []models.EventOccurrence) {
	sort.SliceStable(occs, func(a, b int) bool {
		if !occs[a].StartsAt.Equal(occs[b].StartsAt) {
			return occs[a].StartsAt.Before(occs[b].StartsAt)
		}
		return occs[a].AllDay && !occs[b].AllDay
	})
}
