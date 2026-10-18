package services

import (
	"sort"
	"time"
)

// Interval is a span of time [Start, End).
type Interval struct {
	Start time.Time
	End   time.Time
}

// SlotRequest asks for the first free stretch of time.
type SlotRequest struct {
	Length    time.Duration
	NotBefore time.Time // the search starts here, rounded up to the grid
	Horizon   time.Time // no slot may end after this
	Loc       *time.Location
	// QuietStart/QuietEnd ("HH:MM", local) are never used, e.g. 22:00–07:00.
	// Equal values mean no quiet hours.
	QuietStart, QuietEnd string
	Busy                 []Interval
}

// slotGrid: slots start on the quarter hour.
const slotGrid = 15 * time.Minute

// FindSlot returns the first slot of r.Length that starts on the grid at or
// after r.NotBefore, avoids r.Busy and quiet hours, and ends by r.Horizon.
func FindSlot(r SlotRequest) (Interval, bool) {
	loc := r.Loc
	if loc == nil {
		loc = time.UTC
	}
	blocked := append(quietIntervals(r.NotBefore, r.Horizon, loc, r.QuietStart, r.QuietEnd), r.Busy...)
	sort.Slice(blocked, func(i, j int) bool { return blocked[i].Start.Before(blocked[j].Start) })

	start := roundUp(r.NotBefore, slotGrid)
	for !start.Add(r.Length).After(r.Horizon) {
		end := start.Add(r.Length)
		clash := false
		for _, b := range blocked {
			if b.Start.Before(end) && b.End.After(start) {
				start, clash = roundUp(b.End, slotGrid), true
				break
			}
		}
		if !clash {
			return Interval{Start: start, End: end}, true
		}
	}
	return Interval{}, false
}

// quietIntervals lists the quiet hours of each local day touching
// [from, to], including the night that started the day before.
func quietIntervals(from, to time.Time, loc *time.Location, startHHMM, endHHMM string) []Interval {
	qs, ok1 := parseClock(startHHMM)
	qe, ok2 := parseClock(endHHMM)
	if !ok1 || !ok2 || qs == qe {
		return nil
	}
	var out []Interval
	f := from.In(loc)
	day := time.Date(f.Year(), f.Month(), f.Day()-1, 0, 0, 0, 0, loc)
	for !day.After(to) {
		s := time.Date(day.Year(), day.Month(), day.Day(), 0, qs, 0, 0, loc)
		e := time.Date(day.Year(), day.Month(), day.Day(), 0, qe, 0, 0, loc)
		if qe < qs {
			e = time.Date(day.Year(), day.Month(), day.Day()+1, 0, qe, 0, 0, loc)
		}
		out = append(out, Interval{Start: s, End: e})
		day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc)
	}
	return out
}

// parseClock turns "HH:MM" into minutes after midnight.
func parseClock(s string) (int, bool) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

// roundUp rounds t up to a multiple of d. Every time zone's offset is a
// whole quarter hour, so the grid is the same in local time.
func roundUp(t time.Time, d time.Duration) time.Time {
	r := t.Truncate(d)
	if r.Before(t) {
		r = r.Add(d)
	}
	return r
}
