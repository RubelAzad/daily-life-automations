package services

import (
	"testing"
	"time"
)

func TestDayBounds(t *testing.T) {
	// 20:00 UTC on 17 Oct is 02:00 on 18 Oct in Dhaka (UTC+6).
	start, end := DayBounds(at("2026-10-17T20:00:00Z"), zone(t, "Asia/Dhaka"))
	if !start.Equal(at("2026-10-17T18:00:00Z")) || !end.Equal(at("2026-10-18T18:00:00Z")) {
		t.Errorf("Dhaka: got %v – %v", start, end)
	}
	// The day DST starts in New York has 23 hours.
	start, end = DayBounds(at("2027-03-14T15:00:00Z"), zone(t, "America/New_York"))
	if !start.Equal(at("2027-03-14T05:00:00Z")) || !end.Equal(at("2027-03-15T04:00:00Z")) {
		t.Errorf("New York DST day: got %v – %v", start, end)
	}
	if end.Sub(start) != 23*time.Hour {
		t.Errorf("want 23 h, got %v", end.Sub(start))
	}
}

func TestCarriedDue(t *testing.T) {
	dhaka := zone(t, "Asia/Dhaka")
	now := at("2026-10-17T04:00:00Z") // 10:00 Dhaka
	today, tomorrow := DayBounds(now, dhaka)

	cases := []struct {
		name, due string
		day       time.Time
		want      string
	}{
		// Due 15:00 Dhaka two days ago → 15:00 today (still ahead).
		{"keeps a time still ahead", "2026-10-15T09:00:00Z", today, "2026-10-17T09:00:00Z"},
		// Due 08:00 Dhaka yesterday → 08:00 today has passed → 23:59 today.
		{"passed time becomes 23:59", "2026-10-16T02:00:00Z", today, "2026-10-17T17:59:00Z"},
		// To tomorrow keeps 08:00.
		{"tomorrow keeps the time", "2026-10-16T02:00:00Z", tomorrow, "2026-10-18T02:00:00Z"},
	}
	for _, c := range cases {
		if got := carriedDue(at(c.due), c.day, now, dhaka); !got.Equal(at(c.want)) {
			t.Errorf("%s: got %v, want %s", c.name, got, c.want)
		}
	}
}
