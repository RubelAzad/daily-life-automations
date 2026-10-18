package workflow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestNextRun(t *testing.T) {
	dhaka := mustLoc(t, "Asia/Dhaka") // UTC+6, no DST
	ny := mustLoc(t, "America/New_York")
	at := func(loc *time.Location, s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		name  string
		s     models.Schedule
		loc   *time.Location
		after time.Time
		want  string // local "2006-01-02 15:04", or "" for never
	}{
		{"daily later today", models.Schedule{Frequency: "daily", Time: "07:30"}, dhaka, at(dhaka, "2026-10-17 06:00"), "2026-10-17 07:30"},
		{"daily already passed", models.Schedule{Frequency: "daily", Time: "07:30"}, dhaka, at(dhaka, "2026-10-17 07:30"), "2026-10-18 07:30"},
		{"weekdays from Saturday", models.Schedule{Frequency: "weekly", Time: "09:00", Weekdays: []int{1, 2, 3, 4, 5}}, dhaka, at(dhaka, "2026-10-17 10:00"), "2026-10-19 09:00"},
		{"Sunday only", models.Schedule{Frequency: "weekly", Time: "20:00", Weekdays: []int{7}}, dhaka, at(dhaka, "2026-10-18 20:01"), "2026-10-25 20:00"},
		{"monthly 31st in February", models.Schedule{Frequency: "monthly", Time: "08:00", MonthDay: 31}, dhaka, at(dhaka, "2027-02-01 00:00"), "2027-02-28 08:00"},
		{"monthly next month", models.Schedule{Frequency: "monthly", Time: "08:00", MonthDay: 15}, dhaka, at(dhaka, "2026-10-15 09:00"), "2026-11-15 08:00"},
		{"once in the future", models.Schedule{Frequency: "once", Time: "12:00", Date: "2026-12-01"}, dhaka, at(dhaka, "2026-10-17 00:00"), "2026-12-01 12:00"},
		{"once in the past", models.Schedule{Frequency: "once", Time: "12:00", Date: "2026-10-01"}, dhaka, at(dhaka, "2026-10-17 00:00"), ""},
		// Wall-clock time is kept across the DST change (Nov 1, 2026 in New York).
		{"daily across DST end", models.Schedule{Frequency: "daily", Time: "07:00"}, ny, at(ny, "2026-10-31 08:00"), "2026-11-01 07:00"},
	}
	for _, c := range cases {
		got := NextRun(c.s, c.loc, c.after)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: got %s, want never", c.name, got)
		case c.want != "" && (got == nil || got.In(c.loc).Format("2006-01-02 15:04") != c.want):
			t.Errorf("%s: got %v, want %s", c.name, got, c.want)
		}
	}
	// Before and after the DST change, 07:00 local is a different UTC time.
	a := NextRun(models.Schedule{Frequency: "daily", Time: "07:00"}, ny, at(ny, "2026-10-31 06:00"))
	b := NextRun(models.Schedule{Frequency: "daily", Time: "07:00"}, ny, at(ny, "2026-11-01 06:00"))
	if a.Hour() != 11 || b.Hour() != 12 {
		t.Errorf("DST: got %s and %s UTC", a, b)
	}
}

func TestValidateSchedule(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	bad := []models.Schedule{
		{Frequency: "hourly", Time: "07:00"},
		{Frequency: "daily", Time: "7:00"},
		{Frequency: "daily", Time: "25:00"},
		{Frequency: "weekly", Time: "07:00"},
		{Frequency: "weekly", Time: "07:00", Weekdays: []int{0}},
		{Frequency: "monthly", Time: "07:00", MonthDay: 32},
		{Frequency: "once", Time: "07:00", Date: "2026-10-01"},
		{Frequency: "once", Time: "07:00", Date: "tomorrow"},
	}
	for _, s := range bad {
		if errs := validateSchedule(&s, now, time.UTC); len(errs) == 0 {
			t.Errorf("%+v: expected an error", s)
		}
	}
	good := models.Schedule{Frequency: "weekly", Time: "07:00", Weekdays: []int{5, 1, 5}}
	if errs := validateSchedule(&good, now, time.UTC); len(errs) != 0 {
		t.Errorf("valid schedule: %v", errs)
	}
	normalizeSchedule(&good)
	if len(good.Weekdays) != 2 || good.Weekdays[0] != 1 {
		t.Errorf("weekdays not normalized: %v", good.Weekdays)
	}
}

func TestResolveTime(t *testing.T) {
	dhaka := mustLoc(t, "Asia/Dhaka")
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC) // 16:00 in Dhaka
	cases := map[string]string{
		"+30m":                      "2026-10-17T16:30:00+06:00",
		"+2h":                       "2026-10-17T18:00:00+06:00",
		"+1d":                       "2026-10-18T16:00:00+06:00",
		"+1d 09:00":                 "2026-10-18T09:00:00+06:00",
		"today 17:00":               "2026-10-17T17:00:00+06:00",
		"Tomorrow 9:05":             "2026-10-18T09:05:00+06:00",
		"2026-12-01T08:00:00+06:00": "2026-12-01T08:00:00+06:00",
	}
	for in, want := range cases {
		got, err := resolveTime(in, now, dhaka)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"tomorrow", "+0d", "+400d", "+2h 09:00", "next week", "+1d 25:00", ""} {
		if _, err := resolveTime(in, now, dhaka); err == nil {
			t.Errorf("%q: expected an error", in)
		}
	}
}

func TestResolveInput(t *testing.T) {
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	data := map[string]any{"task": map[string]any{"id": float64(42), "title": "Pay rent", "priority": "high", "due_at": "2026-10-17T12:00:00Z"}}
	v := placeholderValues("Rent follow-up", data, now, time.UTC)

	out, err := resolveInput(json.RawMessage(`{"task_id":"{{task.id}}","title":"Follow up: {{ task.title }} ({{task.priority}}, due {{task.due}}) via {{automation.name}}","due_at":"+1d 09:00"}`), v, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["task_id"] != float64(42) {
		t.Errorf("task_id should be the number 42, got %#v", got["task_id"])
	}
	if got["title"] != "Follow up: Pay rent (high, due Sat 17 Oct 12:00) via Rent follow-up" {
		t.Errorf("title: %q", got["title"])
	}
	if got["due_at"] != "2026-10-18T09:00:00Z" {
		t.Errorf("due_at: %q", got["due_at"])
	}

	if _, err := resolveInput(json.RawMessage(`{"title":"{{note.title}}"}`), v, now, time.UTC); err == nil || !strings.Contains(err.Error(), "note.title") {
		t.Errorf("unknown placeholder: %v", err)
	}
	if _, err := resolveInput(json.RawMessage(`[1]`), v, now, time.UTC); err == nil {
		t.Error("non-object input must fail")
	}
}

func TestConditions(t *testing.T) {
	spec, _ := triggerSpec(models.TriggerTaskCreated)
	errs, conds := validateConditions(spec, []models.Condition{
		{Field: "task.priority", Op: "in", Value: []any{"high", "urgent"}},
		{Field: "task.title", Op: "contains", Value: " RENT "},
		{Field: "task.due_at", Op: "is_set"},
	})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	v := values{"task.priority": "urgent", "task.title": "Pay rent today", "task.due_at": "2026-10-17T12:00:00Z"}
	if !matches(conds, v) {
		t.Error("should match")
	}
	v["task.priority"] = "low"
	if matches(conds, v) {
		t.Error("low priority should not match")
	}

	noteSpec, _ := triggerSpec(models.TriggerNoteCreated)
	_, tagConds := validateConditions(noteSpec, []models.Condition{{Field: "note.tags", Op: "contains", Value: "Bills"}})
	if !matches(tagConds, values{"note.tags": []any{"bills", "home"}}) || matches(tagConds, values{"note.tags": []any{}}) {
		t.Error("tag contains")
	}

	bad := []models.Condition{
		{Field: "task.colour", Op: "equals", Value: "red"},
		{Field: "task.priority", Op: "contains", Value: "hi"},
		{Field: "task.priority", Op: "equals", Value: "critical"},
		{Field: "task.title", Op: "equals", Value: ""},
		{Field: "task.priority", Op: "in", Value: []any{}},
	}
	for _, c := range bad {
		if errs, _ := validateConditions(spec, []models.Condition{c}); len(errs) == 0 {
			t.Errorf("%+v: expected an error", c)
		}
	}
	scheduleSpec, _ := triggerSpec(models.TriggerSchedule)
	if errs, _ := validateConditions(scheduleSpec, []models.Condition{{Field: "task.title", Op: "is_set"}}); len(errs) == 0 {
		t.Error("schedule triggers take no conditions")
	}
}

func TestShortKey(t *testing.T) {
	long := strings.Repeat("x", 300)
	if k := shortKey(long); len(k) > 191 || k == shortKey(long+"y") {
		t.Errorf("shortKey: %d bytes", len(k))
	}
	if shortKey("abc") != "abc" {
		t.Error("short keys are kept")
	}
}
