package services

import (
	"context"
	"strconv"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

const (
	maxRangeDays       = 366
	maxTimedEventDays  = 31
	maxAllDayEventDays = 366
	maxBufferMinutes   = 24 * 60
	// conflictHorizonDays bounds how far ahead a recurring event's conflicts
	// are reported when it is saved.
	conflictHorizonDays = 90
	maxReportedConflict = 20
	// rangePadding widens queries so all-day events (stored at UTC midnight)
	// and prep buffers near the edges are found for any viewer time zone.
	rangePadding = 24 * time.Hour
)

type CalendarService struct {
	Events    repository.CalendarEventRepository
	Reminders repository.ReminderRepository
}

// CreateEventInput: timed events use starts_at/ends_at (RFC 3339); all-day
// events use start_date/end_date (YYYY-MM-DD, end inclusive, defaults to
// start_date).
type CreateEventInput struct {
	Title               string           `json:"title"`
	Description         *string          `json:"description"`
	Location            *string          `json:"location"`
	AllDay              bool             `json:"all_day"`
	StartsAt            *string          `json:"starts_at"`
	EndsAt              *string          `json:"ends_at"`
	StartDate           *string          `json:"start_date"`
	EndDate             *string          `json:"end_date"`
	BufferBeforeMinutes int              `json:"buffer_before_minutes"`
	Busy                *bool            `json:"busy"`
	Color               *string          `json:"color"`
	Recurrence          *RecurrenceInput `json:"recurrence"`
}

type UpdateEventInput struct {
	Title               Optional[string]          `json:"title"`
	Description         Optional[string]          `json:"description"`
	Location            Optional[string]          `json:"location"`
	AllDay              Optional[bool]            `json:"all_day"`
	StartsAt            Optional[string]          `json:"starts_at"`
	EndsAt              Optional[string]          `json:"ends_at"`
	StartDate           Optional[string]          `json:"start_date"`
	EndDate             Optional[string]          `json:"end_date"`
	BufferBeforeMinutes Optional[int]             `json:"buffer_before_minutes"`
	Busy                Optional[bool]            `json:"busy"`
	Color               Optional[string]          `json:"color"`
	Recurrence          Optional[RecurrenceInput] `json:"recurrence"`
}

// eventTimes holds the schedule fields while a create/update is validated.
type eventTimes struct {
	allDay             bool
	startsAt, endsAt   *time.Time
	startDate, endDate *models.Date
}

// List returns every occurrence overlapping [from, to), with conflicts marked.
// All-day occurrences within a day of the range are included too; clients
// place them by start_date/end_date.
func (s CalendarService) List(ctx context.Context, userID uint64, fromRaw, toRaw string) ([]models.EventOccurrence, error) {
	var c checker
	var from, to *time.Time
	if fromRaw == "" {
		c.fail("from", "is required")
	} else {
		from = c.parseTime("from", fromRaw)
	}
	if toRaw == "" {
		c.fail("to", "is required")
	} else {
		to = c.parseTime("to", toRaw)
	}
	if from != nil && to != nil {
		if !to.After(*from) {
			c.fail("to", "must be after from")
		} else if to.Sub(*from) > maxRangeDays*24*time.Hour {
			c.fail("to", "range must be at most 366 days")
		}
	}
	if err := c.err(); err != nil {
		return nil, err
	}

	occs, err := s.occurrences(ctx, userID, *from, *to)
	if err != nil {
		return nil, err
	}
	out := make([]models.EventOccurrence, 0, len(occs))
	for _, o := range occs {
		if o.AllDay || (o.StartsAt.Before(*to) && o.EndsAt.After(*from)) {
			out = append(out, o)
		}
	}
	return out, nil
}

func (s CalendarService) Get(ctx context.Context, userID, id uint64) (models.CalendarEvent, error) {
	return s.Events.Get(ctx, userID, id)
}

// Create saves the event and returns the existing occurrences it overlaps.
// Conflicts are warnings: the event is saved regardless.
func (s CalendarService) Create(ctx context.Context, userID uint64, in CreateEventInput) (models.CalendarEvent, []models.EventOccurrence, error) {
	var c checker
	e := models.CalendarEvent{
		UserID:              userID,
		Title:               c.requiredText("title", in.Title, 255),
		Description:         optionalText(in.Description),
		Location:            optionalText(in.Location),
		BufferBeforeMinutes: in.BufferBeforeMinutes,
		Busy:                in.Busy == nil || *in.Busy,
		Color:               c.color("color", in.Color),
	}
	t := eventTimes{allDay: in.AllDay}
	if in.StartsAt != nil {
		t.startsAt = c.parseTime("starts_at", *in.StartsAt)
	}
	if in.EndsAt != nil {
		t.endsAt = c.parseTime("ends_at", *in.EndsAt)
	}
	if in.StartDate != nil {
		t.startDate = c.parseDate("start_date", *in.StartDate)
	}
	if in.EndDate != nil {
		t.endDate = c.parseDate("end_date", *in.EndDate)
	}
	if in.Recurrence != nil {
		e.Recurrence = c.recurrence(*in.Recurrence)
	}
	validateEvent(&c, &e, t)
	if err := c.err(); err != nil {
		return models.CalendarEvent{}, nil, err
	}

	saved, err := s.Events.Create(ctx, e)
	if err != nil {
		return saved, nil, err
	}
	conflicts, err := s.conflictsFor(ctx, saved)
	return saved, conflicts, err
}

func (s CalendarService) Update(ctx context.Context, userID, id uint64, in UpdateEventInput) (models.CalendarEvent, []models.EventOccurrence, error) {
	e, err := s.Events.Get(ctx, userID, id)
	if err != nil {
		return e, nil, err
	}
	loc, err := userLocation(ctx, s.Events.DB, userID)
	if err != nil {
		return e, nil, err
	}

	// Start from the stored schedule in the shape matching its current kind.
	t := eventTimes{allDay: e.AllDay}
	if e.AllDay {
		t.startDate, t.endDate = e.StartDate, e.EndDate
	} else {
		t.startsAt, t.endsAt = &e.StartsAt, &e.EndsAt
	}

	var c checker
	if in.Title.Set {
		e.Title = c.requiredText("title", in.Title.Value, 255)
	}
	if in.Description.Set {
		e.Description = optionalText(in.Description.Ptr())
	}
	if in.Location.Set {
		e.Location = optionalText(in.Location.Ptr())
	}
	if in.AllDay.Set {
		if in.AllDay.Null {
			c.fail("all_day", "must be true or false")
		}
		if in.AllDay.Value != t.allDay {
			// Switching kind: derive the other representation from the old one
			// unless the client sends it.
			if in.AllDay.Value {
				start := localToday(e.StartsAt, loc) // the event's date in the user's time zone
				t.startDate, t.endDate = &start, nil
			} else {
				t.startsAt, t.endsAt = nil, nil
			}
		}
		t.allDay = in.AllDay.Value
	}
	if in.StartsAt.Set {
		t.startsAt = nil
		if !in.StartsAt.Null {
			t.startsAt = c.parseTime("starts_at", in.StartsAt.Value)
		}
	}
	if in.EndsAt.Set {
		t.endsAt = nil
		if !in.EndsAt.Null {
			t.endsAt = c.parseTime("ends_at", in.EndsAt.Value)
		}
	}
	if in.StartDate.Set {
		t.startDate = c.parseDate("start_date", in.StartDate.Value)
	}
	if in.EndDate.Set {
		t.endDate = c.parseDate("end_date", in.EndDate.Value)
	}
	if in.BufferBeforeMinutes.Set {
		e.BufferBeforeMinutes = in.BufferBeforeMinutes.Value
	}
	if in.Busy.Set {
		if in.Busy.Null {
			c.fail("busy", "must be true or false")
		}
		e.Busy = in.Busy.Value
	}
	if in.Color.Set {
		e.Color = c.color("color", in.Color.Ptr())
	}
	if in.Recurrence.Set {
		e.Recurrence = nil
		if !in.Recurrence.Null {
			e.Recurrence = c.recurrence(in.Recurrence.Value)
		}
	}
	validateEvent(&c, &e, t)
	if err := c.err(); err != nil {
		return models.CalendarEvent{}, nil, err
	}

	saved, err := s.Events.Update(ctx, e)
	if err != nil {
		return saved, nil, err
	}
	// Reminders follow the event when its time or recurrence moves.
	if err := rescheduleEventReminders(ctx, s.Reminders, saved, time.Now().UTC(), loc); err != nil {
		return saved, nil, err
	}
	conflicts, err := s.conflictsFor(ctx, saved)
	return saved, conflicts, err
}

func (s CalendarService) Delete(ctx context.Context, userID, id uint64) error {
	return s.Events.Delete(ctx, userID, id)
}

// validateEvent checks text limits and the schedule, then writes the final
// StartsAt/EndsAt/AllDay onto e.
func validateEvent(c *checker, e *models.CalendarEvent, t eventTimes) {
	if e.Description != nil {
		c.maxLen("description", *e.Description, 10000)
	}
	if e.Location != nil {
		c.maxLen("location", *e.Location, 255)
	}
	if e.BufferBeforeMinutes < 0 || e.BufferBeforeMinutes > maxBufferMinutes {
		c.fail("buffer_before_minutes", "must be between 0 and 1440")
	}

	e.AllDay = t.allDay
	if t.allDay {
		if t.startDate == nil {
			c.fail("start_date", "is required for all-day events")
			return
		}
		end := t.startDate
		if t.endDate != nil {
			end = t.endDate
		}
		if end.Before(t.startDate.Time) {
			c.fail("end_date", "must be on or after start_date")
			return
		}
		e.StartsAt = t.startDate.Time.UTC()
		e.EndsAt = end.Time.UTC().AddDate(0, 0, 1) // exclusive end: midnight after the last day
		if e.EndsAt.Sub(e.StartsAt) > maxAllDayEventDays*24*time.Hour {
			c.fail("end_date", "an all-day event can span at most 366 days")
		}
	} else {
		if t.startsAt == nil {
			c.fail("starts_at", "is required")
		}
		if t.endsAt == nil {
			c.fail("ends_at", "is required")
		}
		if t.startsAt == nil || t.endsAt == nil {
			return
		}
		if !t.endsAt.After(*t.startsAt) {
			c.fail("ends_at", "must be after starts_at")
			return
		}
		if t.endsAt.Sub(*t.startsAt) > maxTimedEventDays*24*time.Hour {
			c.fail("ends_at", "a timed event can last at most 31 days")
		}
		e.StartsAt, e.EndsAt = *t.startsAt, *t.endsAt
	}

	if r := e.Recurrence; r != nil && r.Until != nil && r.Until.Before(e.StartsAt) {
		c.fail("recurrence.until", "must be on or after the event start")
	}
}

// occurrences expands every event near [from, to) and marks conflicts. The
// padded fetch ensures buffers and all-day events at the edges are considered.
func (s CalendarService) occurrences(ctx context.Context, userID uint64, from, to time.Time) ([]models.EventOccurrence, error) {
	paddedFrom, paddedTo := from.Add(-rangePadding), to.Add(rangePadding)
	events, err := s.Events.ListOverlapping(ctx, userID, paddedFrom, paddedTo)
	if err != nil {
		return nil, err
	}
	loc, err := userLocation(ctx, s.Events.DB, userID)
	if err != nil {
		return nil, err
	}
	occs := []models.EventOccurrence{}
	for _, e := range events {
		occs = append(occs, expandEvent(e, paddedFrom, paddedTo, loc)...)
	}
	markConflicts(occs)
	sortOccurrences(occs)
	return occs, nil
}

// conflictsFor returns occurrences of other events that overlap e, looking up
// to conflictHorizonDays ahead for recurring events.
func (s CalendarService) conflictsFor(ctx context.Context, e models.CalendarEvent) ([]models.EventOccurrence, error) {
	conflicts := []models.EventOccurrence{}
	if e.AllDay || !e.Busy {
		return conflicts, nil
	}
	horizon := e.EndsAt
	if e.Recurrence != nil {
		horizon = e.StartsAt.AddDate(0, 0, conflictHorizonDays)
		if e.Recurrence.Until != nil && e.Recurrence.Until.Before(horizon) {
			horizon = e.Recurrence.Until.Add(e.EndsAt.Sub(e.StartsAt))
		}
	}
	occs, err := s.occurrences(ctx, e.UserID, e.BlockedFrom(), horizon)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	for _, o := range occs {
		if o.ID == e.ID || !containsID(o.ConflictsWith, e.ID) {
			continue
		}
		key := o.StartsAt.Format(time.RFC3339) + "#" + strconv.FormatUint(o.ID, 10)
		if seen[key] {
			continue
		}
		seen[key] = true
		conflicts = append(conflicts, o)
		if len(conflicts) == maxReportedConflict {
			break
		}
	}
	return conflicts, nil
}

func containsID(ids []uint64, id uint64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
