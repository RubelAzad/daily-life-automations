package services

import (
	"context"
	"regexp"
	"strconv"
	"time"
	// Bundles the IANA time zone database, so time zones work on machines
	// without one (Windows, minimal containers).
	_ "time/tzdata"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

type SettingsService struct {
	Settings repository.SettingsRepository
}

type UpdateSettingsInput struct {
	Timezone          Optional[string] `json:"timezone"`
	WeekStartsOn      Optional[int]    `json:"week_starts_on"`
	QuietHoursEnabled Optional[bool]   `json:"quiet_hours_enabled"`
	QuietHoursStart   Optional[string] `json:"quiet_hours_start"`
	QuietHoursEnd     Optional[string] `json:"quiet_hours_end"`
	AIApprovalMode    Optional[string] `json:"ai_approval_mode"`

	MorningBriefingEnabled Optional[bool]   `json:"morning_briefing_enabled"`
	MorningBriefingTime    Optional[string] `json:"morning_briefing_time"`
	EveningSummaryEnabled  Optional[bool]   `json:"evening_summary_enabled"`
	EveningSummaryTime     Optional[string] `json:"evening_summary_time"`
}

var clockPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func (s SettingsService) Get(ctx context.Context, userID uint64) (models.UserSettings, error) {
	return s.Settings.Get(ctx, userID)
}

// CreateDefaults saves the defaults for a new account, using the time zone
// the browser reported when it is a valid one.
func (s SettingsService) CreateDefaults(ctx context.Context, user models.User, opts RegisterOptions) error {
	settings := models.DefaultSettings(user.ID)
	if _, ok := loadZone(opts.Timezone); ok {
		settings.Timezone = opts.Timezone
	}
	return s.Settings.Save(ctx, settings)
}

// Update changes the given fields. When the time zone changes, pending event
// and note reminders are recomputed, because their fire times depend on it.
func (s SettingsService) Update(ctx context.Context, userID uint64, in UpdateSettingsInput) (models.UserSettings, error) {
	settings, err := s.Settings.Get(ctx, userID)
	if err != nil {
		return settings, err
	}
	previousZone := settings.Timezone

	var c checker
	if in.Timezone.Set {
		if _, ok := loadZone(in.Timezone.Value); ok && !in.Timezone.Null {
			settings.Timezone = in.Timezone.Value
		} else {
			c.fail("timezone", "must be an IANA time zone name, e.g. Asia/Dhaka")
		}
	}
	if in.WeekStartsOn.Set {
		switch v := in.WeekStartsOn.Value; {
		case in.WeekStartsOn.Null || (v != 0 && v != 1 && v != 6):
			c.fail("week_starts_on", "must be 0 (Sunday), 1 (Monday) or 6 (Saturday)")
		default:
			settings.WeekStartsOn = v
		}
	}
	if in.QuietHoursEnabled.Set {
		if in.QuietHoursEnabled.Null {
			c.fail("quiet_hours_enabled", "must be true or false")
		}
		settings.QuietHoursEnabled = in.QuietHoursEnabled.Value
	}
	if in.QuietHoursStart.Set {
		settings.QuietHoursStart = c.clock("quiet_hours_start", in.QuietHoursStart)
	}
	if in.QuietHoursEnd.Set {
		settings.QuietHoursEnd = c.clock("quiet_hours_end", in.QuietHoursEnd)
	}
	if settings.QuietHoursStart == settings.QuietHoursEnd {
		c.fail("quiet_hours_end", "must differ from quiet_hours_start")
	}
	if in.AIApprovalMode.Set {
		settings.AIApprovalMode = in.AIApprovalMode.Value
		c.oneOf("ai_approval_mode", in.AIApprovalMode.Value, models.ApprovalModes)
	}
	for _, t := range []struct {
		field string
		in    Optional[bool]
		dst   *bool
	}{
		{"morning_briefing_enabled", in.MorningBriefingEnabled, &settings.MorningBriefingEnabled},
		{"evening_summary_enabled", in.EveningSummaryEnabled, &settings.EveningSummaryEnabled},
	} {
		if t.in.Set {
			if t.in.Null {
				c.fail(t.field, "must be true or false")
			}
			*t.dst = t.in.Value
		}
	}
	if in.MorningBriefingTime.Set {
		settings.MorningBriefingTime = c.clock("morning_briefing_time", in.MorningBriefingTime)
	}
	if in.EveningSummaryTime.Set {
		settings.EveningSummaryTime = c.clock("evening_summary_time", in.EveningSummaryTime)
	}
	if err := c.err(); err != nil {
		return models.UserSettings{}, err
	}

	err = repository.InTx(ctx, s.Settings.DB, func(db repository.DBTX) error {
		if err := (repository.SettingsRepository{DB: db}).Save(ctx, settings); err != nil {
			return err
		}
		if settings.Timezone == previousZone {
			return nil
		}
		return rescheduleForTimezone(ctx, db, userID, settings.Location(), time.Now().UTC())
	})
	if err != nil {
		return models.UserSettings{}, err
	}
	return s.Settings.Get(ctx, userID)
}

func (c *checker) clock(field string, v Optional[string]) string {
	if v.Null || !clockPattern.MatchString(v.Value) {
		c.fail(field, "must be a time as HH:MM, e.g. 22:00")
	}
	return v.Value
}

// loadZone accepts IANA names only; "Local" would mean the server's zone.
func loadZone(name string) (*time.Location, bool) {
	if name == "" || name == "Local" || len(name) > 64 {
		return nil, false
	}
	loc, err := time.LoadLocation(name)
	return loc, err == nil
}

// userLocation is the user's time zone for date calculations.
func userLocation(ctx context.Context, db repository.DBTX, userID uint64) (*time.Location, error) {
	s, err := repository.SettingsRepository{DB: db}.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.Location(), nil
}

// rescheduleForTimezone recomputes pending reminders whose fire time depends
// on the time zone: note expiry (09:00 local) and repeating events (same
// local time each occurrence). Already-sent reminders are left alone so a
// time-zone change never re-sends them.
func rescheduleForTimezone(ctx context.Context, db repository.DBTX, userID uint64, loc *time.Location, now time.Time) error {
	reminders := repository.ReminderRepository{DB: db}
	rems, err := reminders.ListScheduledRelativeForUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, r := range rems {
		switch {
		case r.NoteID != nil:
			note, err := repository.NoteRepository{DB: db}.Get(ctx, userID, *r.NoteID)
			if err != nil {
				return err
			}
			r.RemindAt, r.OccursAt = NoteReminderTime(note.ExpiresOn, *r.OffsetMinutes, now, loc)
		case r.EventID != nil:
			event, err := repository.CalendarEventRepository{DB: db}.Get(ctx, userID, *r.EventID)
			if err != nil {
				return err
			}
			r.RemindAt, r.OccursAt = NextEventReminder(event, *r.OffsetMinutes, now, loc)
		}
		if err := reminders.Update(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// QuietHoursEnd reports whether now falls in the user's quiet hours and, if
// so, when they end. A window whose start is after its end (22:00–07:00)
// crosses midnight.
func QuietHoursEnd(s models.UserSettings, now time.Time) (time.Time, bool) {
	if !s.QuietHoursEnabled {
		return time.Time{}, false
	}
	start, okStart := clockMinutes(s.QuietHoursStart)
	end, okEnd := clockMinutes(s.QuietHoursEnd)
	if !okStart || !okEnd || start == end {
		return time.Time{}, false
	}
	local := now.In(s.Location())
	minute := local.Hour()*60 + local.Minute()
	var quiet bool
	dayOffset := 0
	if start < end {
		quiet = minute >= start && minute < end
	} else {
		quiet = minute >= start || minute < end
		if minute >= start {
			dayOffset = 1 // ends tomorrow morning
		}
	}
	if !quiet {
		return time.Time{}, false
	}
	y, m, d := local.Date()
	return time.Date(y, m, d+dayOffset, end/60, end%60, 0, 0, local.Location()).UTC(), true
}

func clockMinutes(v string) (int, bool) {
	if !clockPattern.MatchString(v) {
		return 0, false
	}
	h, _ := strconv.Atoi(v[:2])
	m, _ := strconv.Atoi(v[3:])
	return h*60 + m, true
}

// localToday is the user's current calendar date.
func localToday(now time.Time, loc *time.Location) models.Date {
	y, m, d := now.In(loc).Date()
	return models.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}
