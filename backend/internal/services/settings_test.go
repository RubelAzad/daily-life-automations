package services

import (
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestQuietHoursEnd(t *testing.T) {
	s := models.DefaultSettings(1)
	s.Timezone, s.QuietHoursEnabled = "Asia/Dhaka", true // UTC+6, 22:00–07:00

	cases := []struct {
		now     string
		quiet   bool
		endsAtZ string
	}{
		{"2026-10-17T17:30:00Z", true, "2026-10-18T01:00:00Z"}, // 23:30 → 07:00 next day
		{"2026-10-17T00:30:00Z", true, "2026-10-17T01:00:00Z"}, // 06:30 → 07:00 same day
		{"2026-10-17T16:00:00Z", true, "2026-10-18T01:00:00Z"}, // 22:00 exactly
		{"2026-10-17T01:00:00Z", false, ""},                    // 07:00 exactly: over
		{"2026-10-17T03:00:00Z", false, ""},                    // 09:00
		{"2026-10-17T15:59:00Z", false, ""},                    // 21:59
	}
	for _, c := range cases {
		end, quiet := QuietHoursEnd(s, at(c.now))
		if quiet != c.quiet || (quiet && !end.Equal(at(c.endsAtZ))) {
			t.Errorf("%s: got quiet=%v end=%v, want quiet=%v end=%s", c.now, quiet, end, c.quiet, c.endsAtZ)
		}
	}

	// A daytime window (13:00–15:00) doesn't cross midnight.
	s.QuietHoursStart, s.QuietHoursEnd = "13:00", "15:00"
	if end, quiet := QuietHoursEnd(s, at("2026-10-17T08:00:00Z")); !quiet || !end.Equal(at("2026-10-17T09:00:00Z")) {
		t.Errorf("daytime window: got %v %v", quiet, end)
	}
	if _, quiet := QuietHoursEnd(s, at("2026-10-17T17:30:00Z")); quiet {
		t.Error("23:30 is outside 13:00–15:00")
	}

	s.QuietHoursEnabled = false
	if _, quiet := QuietHoursEnd(s, at("2026-10-17T17:30:00Z")); quiet {
		t.Error("disabled quiet hours must never hold")
	}
}

func TestLoadZone(t *testing.T) {
	for _, ok := range []string{"UTC", "Asia/Dhaka", "America/New_York", "Europe/London"} {
		if _, valid := loadZone(ok); !valid {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "Local", "../../etc/passwd", "Mars/Phobos", "asia/dhaka_x"} {
		if _, valid := loadZone(bad); valid {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestLocalToday(t *testing.T) {
	// 19:00 UTC on the 17th is 01:00 on the 18th in Dhaka.
	if got := localToday(at("2026-10-17T19:00:00Z"), zone(t, "Asia/Dhaka")).String(); got != "2026-10-18" {
		t.Errorf("got %s", got)
	}
	if got := localToday(at("2026-10-17T19:00:00Z"), time.UTC).String(); got != "2026-10-17" {
		t.Errorf("got %s", got)
	}
}

func TestNextOccurrenceKeepsLocalTimeAcrossDST(t *testing.T) {
	ny := zone(t, "America/New_York")
	// Daily at 09:00 New York time. DST starts on 8 March 2026, so 09:00 moves
	// from 14:00 UTC to 13:00 UTC.
	daily := models.Recurrence{Frequency: "daily", Interval: 1}
	anchor := at("2026-03-07T14:00:00Z")
	got, ok := nextOccurrence(daily, anchor, anchor, anchor, ny)
	if !ok || !got.Equal(at("2026-03-08T13:00:00Z")) {
		t.Errorf("New York: got %v", got)
	}
	// In UTC the same series stays at 14:00 UTC.
	if got, _ := nextOccurrence(daily, anchor, anchor, anchor, time.UTC); !got.Equal(at("2026-03-08T14:00:00Z")) {
		t.Errorf("UTC: got %v", got)
	}
	if got.Location() != time.UTC {
		t.Errorf("result should be UTC, got %v", got.Location())
	}
}

func TestNextOccurrenceMonthlyUsesLocalDate(t *testing.T) {
	// Monthly on the 1st at 01:00 Dhaka, which is 19:00 UTC on the last day of
	// the previous month. Counting in UTC would drift to "the 30th/31st".
	dhaka := zone(t, "Asia/Dhaka")
	monthly := models.Recurrence{Frequency: "monthly", Interval: 1}
	anchor := at("2026-09-30T19:00:00Z") // 1 Oct 01:00 Dhaka
	got, _ := nextOccurrence(monthly, anchor, anchor, anchor, dhaka)
	if !got.Equal(at("2026-10-31T19:00:00Z")) { // 1 Nov 01:00 Dhaka
		t.Errorf("got %v", got)
	}
}

func TestExpandEventFollowsUserTimeZone(t *testing.T) {
	ny := zone(t, "America/New_York")
	// Weekly Monday 09:00 New York; DST ends on 1 November 2026.
	e := models.CalendarEvent{
		ID: 1, StartsAt: at("2026-10-26T13:00:00Z"), EndsAt: at("2026-10-26T14:00:00Z"), Busy: true,
		Recurrence: &models.Recurrence{Frequency: "weekly", Interval: 1},
	}
	got := starts(expandEvent(e, at("2026-10-26T00:00:00Z"), at("2026-11-03T00:00:00Z"), ny))
	want := []string{"2026-10-26T13:00:00Z", "2026-11-02T14:00:00Z"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}

	// All-day events are calendar dates and ignore the time zone.
	allDay := models.CalendarEvent{
		ID: 2, StartsAt: at("2026-10-20T00:00:00Z"), EndsAt: at("2026-10-21T00:00:00Z"), AllDay: true,
		Recurrence: &models.Recurrence{Frequency: "daily", Interval: 1},
	}
	got = starts(expandEvent(allDay, at("2026-10-20T00:00:00Z"), at("2026-10-22T00:00:00Z"), zone(t, "Asia/Dhaka")))
	if len(got) != 2 || got[1] != "2026-10-21T00:00:00Z" {
		t.Errorf("all-day: got %v", got)
	}
}

func TestNoteExpiryInstant(t *testing.T) {
	d := models.Date{Time: at("2026-12-01T00:00:00Z")}
	if got := NoteExpiryInstant(d, zone(t, "Asia/Dhaka")); !got.Equal(at("2026-12-01T03:00:00Z")) {
		t.Errorf("Dhaka: got %v", got) // 09:00 Dhaka
	}
	if got := NoteExpiryInstant(d, time.UTC); !got.Equal(at("2026-12-01T09:00:00Z")) {
		t.Errorf("UTC: got %v", got)
	}
}
