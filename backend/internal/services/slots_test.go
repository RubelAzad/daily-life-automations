package services

import (
	"testing"
	"time"
)

func TestFindSlot(t *testing.T) {
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	at := func(day, hhmm string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, dhaka)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	base := SlotRequest{Length: 30 * time.Minute, Loc: dhaka, QuietStart: "22:00", QuietEnd: "07:00"}
	cases := []struct {
		name      string
		mod       func(r *SlotRequest)
		want      time.Time
		wantFound bool
	}{
		{"rounds up to the quarter hour", func(r *SlotRequest) { r.NotBefore = at("2026-10-19", "10:07") }, at("2026-10-19", "10:15"), true},
		{"on the grid stays", func(r *SlotRequest) { r.NotBefore = at("2026-10-19", "10:30") }, at("2026-10-19", "10:30"), true},
		{"skips busy events", func(r *SlotRequest) {
			r.NotBefore = at("2026-10-19", "10:00")
			r.Busy = []Interval{{at("2026-10-19", "10:00"), at("2026-10-19", "11:00")}, {at("2026-10-19", "11:10"), at("2026-10-19", "12:00")}}
		}, at("2026-10-19", "12:00"), true},
		{"a gap too short is skipped", func(r *SlotRequest) {
			r.NotBefore = at("2026-10-19", "10:00")
			r.Busy = []Interval{{at("2026-10-19", "09:00"), at("2026-10-19", "10:15")}, {at("2026-10-19", "10:30"), at("2026-10-19", "11:00")}}
		}, at("2026-10-19", "11:00"), true},
		{"evening runs into quiet hours: next morning", func(r *SlotRequest) { r.NotBefore = at("2026-10-19", "21:45") }, at("2026-10-20", "07:00"), true},
		{"inside last night's quiet hours", func(r *SlotRequest) { r.NotBefore = at("2026-10-19", "03:00") }, at("2026-10-19", "07:00"), true},
		{"daytime quiet hours", func(r *SlotRequest) {
			r.QuietStart, r.QuietEnd = "13:00", "14:00"
			r.NotBefore = at("2026-10-19", "12:45")
		}, at("2026-10-19", "14:00"), true},
		{"no quiet hours", func(r *SlotRequest) {
			r.QuietStart, r.QuietEnd = "00:00", "00:00"
			r.NotBefore = at("2026-10-19", "23:00")
		}, at("2026-10-19", "23:00"), true},
		{"nothing before the horizon", func(r *SlotRequest) {
			r.NotBefore = at("2026-10-19", "10:00")
			r.Busy = []Interval{{at("2026-10-19", "10:00"), at("2026-10-19", "20:00")}}
			r.Horizon = at("2026-10-19", "20:00")
		}, time.Time{}, false},
	}
	for _, c := range cases {
		r := base
		r.Busy = nil
		c.mod(&r)
		if r.Horizon.IsZero() {
			r.Horizon = r.NotBefore.Add(7 * 24 * time.Hour)
		}
		got, ok := FindSlot(r)
		if ok != c.wantFound || (ok && (!got.Start.Equal(c.want) || got.End.Sub(got.Start) != r.Length)) {
			t.Errorf("%s: got %v %v, want %v %v", c.name, got.Start.In(dhaka), ok, c.want, c.wantFound)
		}
	}
}
