package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
)

// Planner makes today's plan (the morning briefing). Briefer implements it.
type Planner interface {
	PlanToday(ctx context.Context, userID uint64, refresh bool) (models.Briefing, error)
}

const (
	// recoverHorizon is how far ahead recover_task looks for a free slot.
	recoverHorizon = 7 * 24 * time.Hour
	// taskBlock is the time an open task's due time is taken to occupy.
	taskBlock = 30 * time.Minute
)

type planDayInput struct {
	Refresh bool `json:"refresh"`
}

// planDayTool makes today's plan and sends the morning-briefing
// notification. It changes none of the user's data, so it never needs
// approval. Without refresh, a plan already made today is reused (no model
// call); with it, a new one is written.
func planDayTool(d Deps) Tool {
	return define("plan_day",
		"Plan the user's day (the morning briefing: focus tasks and a suggested schedule) and send it as a notification. refresh writes a new plan even if there is one for today.",
		RiskRead, `{"type":"object","properties":{"refresh":{"type":"boolean"}},"additionalProperties":false}`,
		func(_ context.Context, _ uint64, in planDayInput) string {
			if in.Refresh {
				return "Write a new plan for today and send it"
			}
			return "Plan today and send the morning briefing"
		},
		func(ctx context.Context, userID uint64, in planDayInput) (any, error) {
			if d.Planner == nil {
				return nil, &InputError{Message: "planning isn't available"}
			}
			br, err := d.Planner.PlanToday(ctx, userID, in.Refresh)
			switch {
			case errors.Is(err, ErrNotConfigured):
				return nil, &InputError{Message: "the AI assistant is off, so the day can't be planned: " + err.Error()}
			case errors.Is(err, ErrBriefingRateLimited):
				return nil, &InputError{Message: err.Error()}
			case err != nil:
				return nil, err
			}
			var plan models.MorningPlan
			_ = json.Unmarshal(br.Content, &plan)
			return map[string]any{"briefing_id": br.ID, "summary": plan.Summary, "focus": len(plan.Focus), "blocks": len(plan.Schedule)}, nil
		})
}

type recoverInput struct {
	TaskID  uint64  `json:"task_id"`
	Minutes *int    `json:"minutes"`
	At      *string `json:"at"`
}

// recoverTaskTool moves a missed task to the next free slot: after the
// user's busy events (with their prep time) and other tasks, outside quiet
// hours. Urgent, high and medium tasks get the first free slot from now;
// low-priority ones from tomorrow. The chosen time is recorded with the call
// (as "at"), so an approval moves the task to exactly the time shown. If a
// plan for today exists, it is updated.
func recoverTaskTool(d Deps) Tool {
	return Tool{
		Name: "recover_task",
		Description: "Find a new time for a missed task and move it there: the next free slot around the user's events and other tasks, outside quiet hours. " +
			"minutes is the time it needs (default 30). at, if given, is used instead of searching.",
		Risk: RiskLow,
		Schema: json.RawMessage(`{"type":"object","properties":{
			"task_id":{"type":"integer","minimum":1},
			"minutes":{"type":"integer","minimum":15,"maximum":240},
			"at":{"type":"string","format":"date-time"}
		},"required":["task_id"],"additionalProperties":false}`),
		prepare: func(ctx context.Context, userID uint64, raw json.RawMessage) (prepared, error) {
			var in recoverInput
			if err := decodeStrict(raw, &in); err != nil {
				return prepared{}, err
			}
			if in.Minutes != nil && (*in.Minutes < 15 || *in.Minutes > 240) {
				return prepared{}, &InputError{Message: "minutes must be 15–240"}
			}
			length := 30 * time.Minute
			if in.Minutes != nil {
				length = time.Duration(*in.Minutes) * time.Minute
			}
			settings, err := d.Settings.Get(ctx, userID)
			if err != nil {
				return prepared{}, err
			}
			loc := settings.Location()
			task, terr := d.Tasks.Get(ctx, userID, in.TaskID)
			if terr != nil {
				// Checked with sample values when an automation is saved: the
				// task is looked up again when it runs.
				return prepared{summary: "Find a new time for task #" + fmt.Sprint(in.TaskID), run: func(ctx context.Context) (any, error) { return nil, terr }}, nil
			}

			var slot services.Interval
			if in.At != nil {
				start, err := time.Parse(time.RFC3339, *in.At)
				if err != nil {
					return prepared{}, &InputError{Message: "at must be an RFC 3339 time"}
				}
				slot = services.Interval{Start: start, End: start.Add(length)}
			} else {
				found, ok, err := findSlotFor(ctx, d, userID, task, length, settings, time.Now().UTC())
				if err != nil {
					return prepared{}, err
				}
				if !ok {
					return prepared{summary: "Find a new time for " + quote(task.Title), run: func(context.Context) (any, error) {
						return nil, &InputError{Message: "no free time in the next 7 days"}
					}}, nil
				}
				slot = found
				at := slot.Start.In(loc).Format(time.RFC3339)
				in.At = &at
			}
			resolved, _ := json.Marshal(in)
			start, end := slot.Start.In(loc), slot.End.In(loc)
			return prepared{
				summary: fmt.Sprintf("Move %s to %s–%s", quote(task.Title), start.Format("Mon 2 Jan 15:04"), end.Format("15:04")),
				input:   resolved,
				run: func(ctx context.Context) (any, error) {
					return moveTask(ctx, d, userID, in.TaskID, slot, loc)
				},
			}, nil
		},
	}
}

// findSlotFor finds the first free slot for a task.
func findSlotFor(ctx context.Context, d Deps, userID uint64, task models.Task, length time.Duration, settings models.UserSettings, now time.Time) (services.Interval, bool, error) {
	loc := settings.Location()
	notBefore := now
	if task.Priority == "low" {
		l := now.In(loc)
		notBefore = time.Date(l.Year(), l.Month(), l.Day()+1, 0, 0, 0, 0, loc)
	}
	horizon := now.Add(recoverHorizon)
	from, to := now.Format(time.RFC3339), horizon.Format(time.RFC3339)

	var busy []services.Interval
	events, err := d.Calendar.List(ctx, userID, from, to)
	if err != nil {
		return services.Interval{}, false, err
	}
	for _, e := range events {
		if e.AllDay || !e.Busy {
			continue
		}
		busy = append(busy, services.Interval{Start: e.StartsAt.Add(-time.Duration(e.BufferBeforeMinutes) * time.Minute), End: e.EndsAt})
	}
	tasks, _, err := d.Tasks.List(ctx, userID, services.ListTasksInput{Status: "open", DueFrom: from, DueTo: to, PerPage: "100"})
	if err != nil {
		return services.Interval{}, false, err
	}
	for _, t := range tasks {
		if t.ID != task.ID && t.DueAt != nil {
			busy = append(busy, services.Interval{Start: t.DueAt.Add(-taskBlock), End: *t.DueAt})
		}
	}
	slot, ok := services.FindSlot(services.SlotRequest{
		Length: length, NotBefore: notBefore, Horizon: horizon, Loc: loc,
		QuietStart: settings.QuietHoursStart, QuietEnd: settings.QuietHoursEnd, Busy: busy,
	})
	return slot, ok, nil
}

// moveTask sets the task's due time to the slot's end and puts the slot in
// today's plan, if there is one.
func moveTask(ctx context.Context, d Deps, userID, taskID uint64, slot services.Interval, loc *time.Location) (any, error) {
	if err := requireTaskID(taskID); err != nil {
		return nil, err
	}
	current, err := d.Tasks.Get(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if current.Status == models.TaskCompleted || current.Status == models.TaskCancelled {
		return nil, &InputError{Message: "the task is already " + current.Status}
	}
	if slot.End.Before(time.Now()) {
		return nil, &InputError{Message: "the proposed time (" + slot.Start.In(loc).Format("Mon 2 Jan 15:04") + ") has already passed"}
	}
	t, err := d.Tasks.Update(ctx, userID, taskID, services.UpdateTaskInput{DueAt: set(slot.End.In(loc).Format(time.RFC3339))})
	if err != nil {
		return nil, err
	}
	updated, err := updateTodayPlan(ctx, d, userID, t, slot, loc)
	if err != nil {
		return nil, err
	}
	return map[string]any{"task": compactTask(t), "slot_start": slot.Start.In(loc).Format(time.RFC3339), "plan_updated": updated}, nil
}

// updateTodayPlan moves the task's block in today's plan: it is removed,
// and added at the new slot if that is today.
func updateTodayPlan(ctx context.Context, d Deps, userID uint64, t models.Task, slot services.Interval, loc *time.Location) (bool, error) {
	if d.Briefings.DB == nil {
		return false, nil
	}
	today := localDate(time.Now().UTC(), loc)
	br, err := d.Briefings.Latest(ctx, userID, models.BriefingMorning, today)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil || br.Status != models.BriefingReady {
		return false, err
	}
	var plan models.MorningPlan
	if err := json.Unmarshal(br.Content, &plan); err != nil {
		return false, nil // not a plan we can edit
	}
	blocks := plan.Schedule[:0]
	changed := false
	for _, b := range plan.Schedule {
		if b.TaskID != nil && *b.TaskID == t.ID {
			changed = true
			continue
		}
		blocks = append(blocks, b)
	}
	start, end := slot.Start.In(loc), slot.End.In(loc)
	day := today.Format("2006-01-02")
	if start.Format("2006-01-02") == day && end.Add(-time.Minute).Format("2006-01-02") == day {
		id := t.ID
		blocks = append(blocks, models.ScheduleBlock{Start: start.Format("15:04"), End: end.Format("15:04"), Kind: "task", Title: t.Title, TaskID: &id})
		changed = true
	}
	if !changed {
		return false, nil
	}
	sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].Start < blocks[j].Start })
	plan.Schedule = blocks
	content, err := json.Marshal(plan)
	if err != nil {
		return false, err
	}
	return true, d.Briefings.SetContent(ctx, userID, br.ID, content)
}
