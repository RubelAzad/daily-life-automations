package services

import (
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
)

func TestTaskReminderTime(t *testing.T) {
	now := at("2026-10-17T08:00:00Z")
	due := at("2026-10-17T12:00:00Z")

	r, o := TaskReminderTime(&due, 60, now)
	if r == nil || !r.Equal(at("2026-10-17T11:00:00Z")) || !o.Equal(due) {
		t.Errorf("1h before noon: got %v / %v", r, o)
	}
	// Offset point already passed but not yet due: fire immediately.
	r, _ = TaskReminderTime(&due, 600, now)
	if r == nil || !r.Before(now) {
		t.Errorf("past offset point should give a time <= now, got %v", r)
	}
	// Overdue or no due date: inactive.
	past := at("2026-10-17T07:00:00Z")
	if r, _ := TaskReminderTime(&past, 15, now); r != nil {
		t.Errorf("overdue task should be inactive, got %v", r)
	}
	if r, _ := TaskReminderTime(nil, 15, now); r != nil {
		t.Errorf("no due date should be inactive, got %v", r)
	}
}

func TestNextEventReminder(t *testing.T) {
	e := timedEvent(1, "2026-10-19T03:00:00Z", 30) // Monday 03:00 UTC
	e.Recurrence = &models.Recurrence{Frequency: "weekly", Interval: 1}

	// Well before the first occurrence.
	r, o := NextEventReminder(e, 15, at("2026-10-18T00:00:00Z"), time.UTC)
	if r == nil || !r.Equal(at("2026-10-19T02:45:00Z")) || !o.Equal(at("2026-10-19T03:00:00Z")) {
		t.Fatalf("first: got %v / %v", r, o)
	}
	// Worker firing slightly late at 02:45:20 → next week's occurrence.
	r, o = NextEventReminder(e, 15, at("2026-10-19T02:45:20Z"), time.UTC)
	if r == nil || !o.Equal(at("2026-10-26T03:00:00Z")) {
		t.Fatalf("after firing: got %v / %v", r, o)
	}
	// One-off event that already started: no reminder.
	one := timedEvent(2, "2026-10-19T03:00:00Z", 30)
	if r, _ := NextEventReminder(one, 15, at("2026-10-19T02:50:00Z"), time.UTC); r != nil {
		t.Errorf("past offset point of one-off event should be nil, got %v", r)
	}
	// Until reached.
	until := at("2026-10-20T00:00:00Z")
	e.Recurrence.Until = &until
	if r, _ := NextEventReminder(e, 15, at("2026-10-19T02:46:00Z"), time.UTC); r != nil {
		t.Errorf("ended series should be nil, got %v", r)
	}
}
