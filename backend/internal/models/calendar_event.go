package models

import "time"

type CalendarEvent struct {
	ID          uint64  `json:"id"`
	UserID      uint64  `json:"-"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Location    *string `json:"location"`
	// StartsAt/EndsAt are UTC instants. For all-day events they are UTC
	// midnight of the first day and of the day after the last day.
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	AllDay   bool      `json:"all_day"`
	// StartDate/EndDate (inclusive) are set only for all-day events.
	StartDate *Date `json:"start_date"`
	EndDate   *Date `json:"end_date"`
	// BufferBeforeMinutes is prep or travel time that also blocks the
	// calendar before the event starts.
	BufferBeforeMinutes int         `json:"buffer_before_minutes"`
	Busy                bool        `json:"busy"`
	Color               *string     `json:"color"`
	Recurrence          *Recurrence `json:"recurrence"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
}

// SetDates fills StartDate/EndDate from StartsAt/EndsAt for all-day events.
func (e *CalendarEvent) SetDates() {
	if !e.AllDay {
		e.StartDate, e.EndDate = nil, nil
		return
	}
	start := Date{Time: e.StartsAt.UTC().Truncate(24 * time.Hour)}
	end := Date{Time: e.EndsAt.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)}
	e.StartDate, e.EndDate = &start, &end
}

// BlockedFrom is when the event starts occupying time, including prep/travel.
func (e CalendarEvent) BlockedFrom() time.Time {
	return e.StartsAt.Add(-time.Duration(e.BufferBeforeMinutes) * time.Minute)
}

// EventOccurrence is one concrete instance of an event in a time range. For a
// recurring event, ID is the series and StartsAt/EndsAt are this instance.
type EventOccurrence struct {
	CalendarEvent
	// ConflictsWith lists IDs of busy timed events overlapping this one
	// (including their prep/travel buffers).
	ConflictsWith []uint64 `json:"conflicts_with"`
}
