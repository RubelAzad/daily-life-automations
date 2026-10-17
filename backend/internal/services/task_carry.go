package services

import (
	"context"
	"errors"
	"strconv"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

const (
	maxCarryTasks   = 200
	carryAllOverdue = 500
	carryToToday    = "today"
	carryToTomorrow = "tomorrow"
)

type CarryForwardInput struct {
	// TaskIDs to move; empty means every open task that is overdue.
	TaskIDs []uint64 `json:"task_ids"`
	// To is "today" (default) or "tomorrow".
	To string `json:"to"`
}

// CarryForward moves unfinished tasks to today or tomorrow, keeping each
// task's local time of day. When that time has already passed today, 23:59
// is used instead, so the task isn't overdue again straight away. Each move
// increments carry_count, and offset reminders follow the new due time.
func (s TaskService) CarryForward(ctx context.Context, userID uint64, in CarryForwardInput) ([]models.Task, error) {
	var c checker
	to := orDefault(in.To, carryToToday)
	c.oneOf("to", to, []string{carryToToday, carryToTomorrow})
	if len(in.TaskIDs) > maxCarryTasks {
		c.fail("task_ids", "at most "+strconv.Itoa(maxCarryTasks)+" tasks at a time")
	}
	if err := c.err(); err != nil {
		return nil, err
	}

	loc, err := userLocation(ctx, s.Tasks.DB, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	todayStart, tomorrowStart := DayBounds(now, loc)
	target := todayStart
	if to == carryToTomorrow {
		target = tomorrowStart
	}

	var tasks []models.Task
	if len(in.TaskIDs) == 0 {
		tasks, _, err = s.Tasks.List(ctx, userID, repository.TaskFilter{
			Statuses: []string{models.TaskPending, models.TaskInProgress}, IncludeSubtasks: true,
			DueTo: &todayStart, Sort: "due_at", Limit: carryAllOverdue,
		})
		if err != nil {
			return nil, err
		}
	} else {
		seen := map[uint64]bool{}
		for _, id := range in.TaskIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			ref := "task " + strconv.FormatUint(id, 10)
			t, err := s.Tasks.Get(ctx, userID, id)
			switch {
			case errors.Is(err, repository.ErrNotFound):
				c.fail("task_ids", ref+" not found")
				continue
			case err != nil:
				return nil, err
			}
			switch {
			case models.IsClosedStatus(t.Status):
				c.fail("task_ids", ref+" is already "+t.Status)
			case t.DueAt == nil:
				c.fail("task_ids", ref+" has no due date")
			case !t.DueAt.Before(target):
				c.fail("task_ids", ref+" is already due "+to+" or later")
			default:
				tasks = append(tasks, t)
			}
		}
		if err := c.err(); err != nil {
			return nil, err
		}
	}

	moved := make([]models.Task, 0, len(tasks))
	err = repository.InTx(ctx, s.Tasks.DB, func(db repository.DBTX) error {
		taskRepo, reminders := repository.TaskRepository{DB: db}, repository.ReminderRepository{DB: db}
		for _, t := range tasks {
			if err := taskRepo.CarryForward(ctx, userID, t.ID, carriedDue(*t.DueAt, target, now, loc)); err != nil {
				return err
			}
			updated, err := taskRepo.Get(ctx, userID, t.ID)
			if err != nil {
				return err
			}
			if err := rescheduleTaskReminders(ctx, reminders, updated, now); err != nil {
				return err
			}
			moved = append(moved, updated)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return moved, nil
}

// carriedDue puts due's local time of day on the day starting at dayStart.
// If that moment is not in the future, it uses 23:59 that day.
func carriedDue(due, dayStart, now time.Time, loc *time.Location) time.Time {
	local := due.In(loc)
	y, m, d := dayStart.In(loc).Date()
	next := time.Date(y, m, d, local.Hour(), local.Minute(), local.Second(), 0, loc)
	if !next.After(now) {
		next = time.Date(y, m, d, 23, 59, 0, 0, loc)
	}
	return next.UTC()
}
