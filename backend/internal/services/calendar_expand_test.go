package services

import (
	"slices"
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
)

func timedEvent(id uint64, start string, minutes int) models.CalendarEvent {
	s := at(start)
	return models.CalendarEvent{ID: id, StartsAt: s, EndsAt: s.Add(time.Duration(minutes) * time.Minute), Busy: true}
}

func starts(occs []models.EventOccurrence) []string {
	var out []string
	for _, o := range occs {
		out = append(out, o.StartsAt.Format(time.RFC3339))
	}
	return out
}

func TestExpandOneOff(t *testing.T) {
	e := timedEvent(1, "2026-10-20T09:00:00Z", 60)
	if got := expandEvent(e, at("2026-10-20T09:30:00Z"), at("2026-10-21T00:00:00Z")); len(got) != 1 {
		t.Errorf("event running at window start should be included, got %d", len(got))
	}
	if got := expandEvent(e, at("2026-10-20T10:00:00Z"), at("2026-10-21T00:00:00Z")); len(got) != 0 {
		t.Errorf("event ending exactly at window start must be excluded, got %d", len(got))
	}
}

func TestExpandWeeklySkipsAheadAndRespectsUntil(t *testing.T) {
	e := timedEvent(1, "2020-01-06T09:00:00Z", 60) // a Monday, years ago
	until := at("2026-10-26T09:00:00Z")
	e.Recurrence = &models.Recurrence{Frequency: "weekly", Interval: 1, Until: &until}
	got := starts(expandEvent(e, at("2026-10-12T00:00:00Z"), at("2026-11-09T00:00:00Z")))
	want := []string{"2026-10-12T09:00:00Z", "2026-10-19T09:00:00Z", "2026-10-26T09:00:00Z"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandDailyIncludesOccurrenceStillRunning(t *testing.T) {
	e := timedEvent(1, "2026-10-01T23:00:00Z", 120) // 23:00–01:00 every day
	e.Recurrence = &models.Recurrence{Frequency: "daily", Interval: 1}
	got := starts(expandEvent(e, at("2026-10-10T00:00:00Z"), at("2026-10-10T12:00:00Z")))
	want := []string{"2026-10-09T23:00:00Z"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandMonthlyOn31st(t *testing.T) {
	e := timedEvent(1, "2026-01-31T10:00:00Z", 30)
	e.Recurrence = &models.Recurrence{Frequency: "monthly", Interval: 1}
	got := starts(expandEvent(e, at("2026-02-01T00:00:00Z"), at("2026-05-01T00:00:00Z")))
	want := []string{"2026-02-28T10:00:00Z", "2026-03-31T10:00:00Z", "2026-04-30T10:00:00Z"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMarkConflicts(t *testing.T) {
	a := timedEvent(1, "2026-10-20T09:00:00Z", 60) // 09:00–10:00
	b := timedEvent(2, "2026-10-20T09:30:00Z", 60) // overlaps a
	c := timedEvent(3, "2026-10-20T10:30:00Z", 30) // 10:30–11:00, touches b's end: no conflict
	d := timedEvent(4, "2026-10-20T11:15:00Z", 30) // 30-min travel buffer → blocked from 10:45, overlaps c
	d.BufferBeforeMinutes = 30
	free := timedEvent(5, "2026-10-20T09:00:00Z", 60)
	free.Busy = false
	allDay := models.CalendarEvent{ID: 6, StartsAt: at("2026-10-20T00:00:00Z"), EndsAt: at("2026-10-21T00:00:00Z"), AllDay: true, Busy: true}

	occs := []models.EventOccurrence{}
	for _, e := range []models.CalendarEvent{a, b, c, d, free, allDay} {
		occs = append(occs, models.EventOccurrence{CalendarEvent: e, ConflictsWith: []uint64{}})
	}
	markConflicts(occs)

	want := map[uint64][]uint64{1: {2}, 2: {1}, 3: {4}, 4: {3}, 5: {}, 6: {}}
	for _, o := range occs {
		got := slices.Clone(o.ConflictsWith)
		slices.Sort(got)
		if !slices.Equal(got, want[o.ID]) {
			t.Errorf("event %d conflicts = %v, want %v", o.ID, got, want[o.ID])
		}
	}
}

func TestMarkConflictsIgnoresSameSeries(t *testing.T) {
	e := timedEvent(1, "2026-10-20T09:00:00Z", 26*60) // longer than its daily period
	e.Recurrence = &models.Recurrence{Frequency: "daily", Interval: 1}
	occs := expandEvent(e, at("2026-10-20T00:00:00Z"), at("2026-10-23T00:00:00Z"))
	markConflicts(occs)
	for _, o := range occs {
		if len(o.ConflictsWith) != 0 {
			t.Fatalf("occurrences of one series must not conflict: %v", o.ConflictsWith)
		}
	}
}
