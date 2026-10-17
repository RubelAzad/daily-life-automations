package services

import (
	"context"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

// eventReminderLookahead bounds the search for the next occurrence; it covers
// yearly series.
const eventReminderLookahead = 400

// TaskReminderTime returns when an offset-based task reminder fires and the
// due time it refers to. A task without an upcoming due time gives an
// inactive reminder (remindAt nil). If the offset point has already passed
// but the task isn't due yet, it fires right away.
func TaskReminderTime(due *time.Time, offsetMinutes int, now time.Time) (remindAt, occursAt *time.Time) {
	if due == nil || !due.After(now) {
		return nil, due
	}
	at := due.Add(-time.Duration(offsetMinutes) * time.Minute)
	return &at, due
}

// NextEventReminder returns the next firing for an offset-based event
// reminder: the first occurrence starting more than offset after now. nil
// when the event has no such occurrence (it's over). loc is the user's time
// zone, which repeating events follow.
func NextEventReminder(e models.CalendarEvent, offsetMinutes int, now time.Time, loc *time.Location) (remindAt, occursAt *time.Time) {
	offset := time.Duration(offsetMinutes) * time.Minute
	threshold := now.Add(offset)
	var start *time.Time
	if e.Recurrence == nil {
		if e.StartsAt.After(threshold) {
			start = &e.StartsAt
		}
	} else {
		for _, o := range expandEvent(e, threshold, threshold.AddDate(0, 0, eventReminderLookahead), loc) {
			if o.StartsAt.After(threshold) {
				s := o.StartsAt
				start = &s
				break
			}
		}
	}
	if start == nil {
		return nil, nil
	}
	at := start.Add(-offset)
	return &at, start
}

// noteExpiryHour is the local time of day an expiry date "happens".
const noteExpiryHour = 9

// NoteExpiryInstant is the moment a note's expiry date is reached: 09:00 on
// that date in loc, the user's time zone.
func NoteExpiryInstant(d models.Date, loc *time.Location) time.Time {
	y, m, day := d.Time.UTC().Date()
	return time.Date(y, m, day, noteExpiryHour, 0, 0, 0, loc).UTC()
}

// NoteReminderTime returns when an expiry reminder fires and the expiry
// moment it refers to; inactive when there is no upcoming expiry.
func NoteReminderTime(expiresOn *models.Date, offsetMinutes int, now time.Time, loc *time.Location) (remindAt, occursAt *time.Time) {
	if expiresOn == nil {
		return nil, nil
	}
	expiry := NoteExpiryInstant(*expiresOn, loc)
	return TaskReminderTime(&expiry, offsetMinutes, now)
}

func rescheduleNoteReminders(ctx context.Context, reminders repository.ReminderRepository, n models.Note, now time.Time, loc *time.Location) error {
	rems, err := reminders.ListRelativeForNote(ctx, n.ID)
	if err != nil {
		return err
	}
	for _, r := range rems {
		r.RemindAt, r.OccursAt = NoteReminderTime(n.ExpiresOn, *r.OffsetMinutes, now, loc)
		r.Status = models.ReminderScheduled
		if err := reminders.Update(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// rescheduleTaskReminders moves a task's offset-based reminders after its
// due time changed, re-arming any that already fired.
func rescheduleTaskReminders(ctx context.Context, reminders repository.ReminderRepository, t models.Task, now time.Time) error {
	rems, err := reminders.ListRelativeForTask(ctx, t.ID)
	if err != nil {
		return err
	}
	for _, r := range rems {
		r.RemindAt, r.OccursAt = TaskReminderTime(t.DueAt, *r.OffsetMinutes, now)
		r.Status, r.FollowUpsSent = models.ReminderScheduled, 0
		if err := reminders.Update(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// rescheduleEventReminders moves an event's offset-based reminders after its
// time or recurrence changed.
func rescheduleEventReminders(ctx context.Context, reminders repository.ReminderRepository, e models.CalendarEvent, now time.Time, loc *time.Location) error {
	rems, err := reminders.ListRelativeForEvent(ctx, e.ID)
	if err != nil {
		return err
	}
	for _, r := range rems {
		r.RemindAt, r.OccursAt = NextEventReminder(e, *r.OffsetMinutes, now, loc)
		r.Status = models.ReminderScheduled
		if err := reminders.Update(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// copyTaskReminders gives the next occurrence of a recurring task the same
// offset-based reminders. Fixed-time reminders belong to one occurrence and
// are not copied.
func copyTaskReminders(ctx context.Context, reminders repository.ReminderRepository, from uint64, to models.Task, now time.Time) error {
	rems, err := reminders.ListRelativeForTask(ctx, from)
	if err != nil {
		return err
	}
	for _, r := range rems {
		r.TaskID = &to.ID
		r.RemindAt, r.OccursAt = TaskReminderTime(to.DueAt, *r.OffsetMinutes, now)
		r.Status, r.FollowUpsSent, r.LastSentAt = models.ReminderScheduled, 0, nil
		if _, err := reminders.Create(ctx, r); err != nil {
			return err
		}
	}
	return nil
}
