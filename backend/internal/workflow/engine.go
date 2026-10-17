package workflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/events"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/scheduler"
)

// Job kinds.
const (
	JobRun         = "automation.run"
	JobScanOverdue = "automation.scan_overdue"
)

const (
	// lateLimit: a scheduled run more than this late (no worker was
	// running) is skipped rather than run at an odd time.
	lateLimit = 3 * time.Hour
	// maxActionsPerHour pauses an automation that runs away, e.g. one that
	// fires on every task and creates tasks.
	maxActionsPerHour = 60
	// overdueWindow: tasks that went overdue within this long are checked,
	// so a worker that was down for a while still catches them.
	overdueWindow = 24 * time.Hour
)

// Engine runs automations. The worker registers its jobs; the API uses it
// for "Run now".
type Engine struct {
	DB       *sql.DB
	Executor ai.Executor // with ai.AutomationTools
	Settings repository.SettingsRepository
	Events   events.Bus
	Log      *log.Logger
	Now      func() time.Time // overridable for tests
}

// Register adds the engine's jobs to the worker's scheduler.
func (e Engine) Register(s *scheduler.Scheduler, every time.Duration) {
	s.Handle(events.JobKind, e.dispatch)
	s.Handle(JobRun, e.runJob)
	s.Every(scheduler.Repeating{Kind: JobScanOverdue, Every: every, Handler: e.scanOverdue})
}

// dispatch hands an event to each enabled automation with that trigger, as
// a run job of its own, so one failing automation doesn't hold up others.
func (e Engine) dispatch(ctx context.Context, job scheduler.Job) error {
	var ev events.Event
	if err := job.Decode(&ev); err != nil {
		return err
	}
	list, err := repository.AutomationRepository{DB: e.DB}.EnabledFor(ctx, ev.UserID, ev.Type)
	if err != nil {
		return err
	}
	for _, a := range list {
		if ev.CausedBy != nil && *ev.CausedBy == a.ID {
			continue // an automation never triggers itself
		}
		_, _, err := scheduler.Enqueue(ctx, e.DB, scheduler.NewJob{
			UserID: &ev.UserID, Kind: JobRun, RunAt: e.now(),
			Key:     shortKey(fmt.Sprintf("automation:%d:%s", a.ID, job.Key)),
			Payload: runPayload{AutomationID: a.ID, Event: &ev},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// runJob runs one automation for a schedule time or an event.
func (e Engine) runJob(ctx context.Context, job scheduler.Job) error {
	var p runPayload
	if err := job.Decode(&p); err != nil {
		return err
	}
	if job.UserID == nil {
		return scheduler.Permanent(errors.New("run job without a user"))
	}
	userID, runKey := *job.UserID, job.Key
	if runKey == "" {
		runKey = fmt.Sprintf("automation-job:%d", job.ID)
	}

	var a models.Automation
	var data map[string]any
	depth := 0
	switch {
	case p.FireAt != nil:
		var due bool
		var err error
		if a, due, err = e.advanceSchedule(ctx, userID, p.AutomationID, *p.FireAt); err != nil || !due {
			return err
		}
		if late := e.now().Sub(*p.FireAt); late > lateLimit {
			msg := fmt.Sprintf("Skipped the %s run: the worker wasn't running then.", p.FireAt.Format(time.RFC3339))
			return repository.AutomationRepository{DB: e.DB}.RecordRun(ctx, a.ID, e.now(), models.RunSkipped, &msg)
		}
	case p.Event != nil:
		var err error
		a, err = repository.AutomationRepository{DB: e.DB}.Get(ctx, userID, p.AutomationID)
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !a.Enabled || a.Trigger.Type != p.Event.Type {
			return nil // turned off or changed since the event
		}
		data, depth = p.Event.Data, p.Event.Depth
	default:
		return scheduler.Permanent(errors.New("run job has neither a time nor an event"))
	}
	_, err := e.execute(ctx, a, data, depth, runKey)
	return err
}

// advanceSchedule moves a schedule on to its next run and queues it. due is
// false if the automation is gone, off, or has moved on (it was edited
// after this run was queued).
func (e Engine) advanceSchedule(ctx context.Context, userID, id uint64, fireAt time.Time) (a models.Automation, due bool, err error) {
	err = repository.InTx(ctx, e.DB, func(db repository.DBTX) error {
		automations := repository.AutomationRepository{DB: db}
		var err error
		a, err = automations.GetForUpdate(ctx, userID, id)
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !a.Enabled || a.Trigger.Schedule == nil || a.NextRunAt == nil || !a.NextRunAt.Equal(fireAt) {
			return nil
		}
		settings, err := e.Settings.Get(ctx, userID)
		if err != nil {
			return err
		}
		// From the later of the planned time and now, so a late run doesn't
		// queue a burst of catch-up runs.
		after := fireAt
		if now := e.now(); now.After(after) {
			after = now
		}
		a.NextRunAt = NextRun(*a.Trigger.Schedule, settings.Location(), after)
		if err := automations.SetNextRun(ctx, a.ID, a.NextRunAt); err != nil {
			return err
		}
		due = true
		return scheduleNext(ctx, db, a)
	})
	return a, due, err
}

// RunResult is what a run did.
type RunResult struct {
	Status  string      `json:"status"` // succeeded | waiting_approval | failed | skipped
	Error   *string     `json:"error"`
	Actions []ai.Result `json:"actions"`
}

// RunNow runs a schedule or manual automation at once, for the Run button.
// It doesn't move the schedule.
func (e Engine) RunNow(ctx context.Context, userID, id uint64) (RunResult, error) {
	a, err := repository.AutomationRepository{DB: e.DB}.Get(ctx, userID, id)
	if err != nil {
		return RunResult{}, err
	}
	if spec, _ := triggerSpec(a.Trigger.Type); !spec.RunNow {
		return RunResult{}, ErrCantRunNow
	}
	return e.execute(ctx, a, nil, 0, fmt.Sprintf("automation:%d:manual:%d", a.ID, e.now().UnixNano()))
}

// ErrCantRunNow: event automations need an event to run.
var ErrCantRunNow = errors.New("this automation runs when its event happens; it can't be run by hand")

// execute checks the conditions and runs the actions in order. Action
// failures are recorded on the automation, not returned: only internal
// errors are, so the job is retried, and actions already taken are skipped
// thanks to their idempotency keys.
func (e Engine) execute(ctx context.Context, a models.Automation, data map[string]any, depth int, runKey string) (RunResult, error) {
	settings, err := e.Settings.Get(ctx, a.UserID)
	if err != nil {
		return RunResult{}, err
	}
	now, loc := e.now(), settings.Location()
	vals := placeholderValues(a.Name, data, now, loc)

	if spec, ok := triggerSpec(a.Trigger.Type); ok && len(a.Conditions) > 0 {
		_, conds := validateConditions(spec, a.Conditions)
		if !matches(conds, vals) {
			return RunResult{Status: models.RunSkipped, Actions: []ai.Result{}}, nil // not recorded: nothing happened
		}
	}

	res := RunResult{Status: models.RunSucceeded, Actions: []ai.Result{}}
	automations := repository.AutomationRepository{DB: e.DB}
	used, err := e.Executor.Actions.CountForAutomationSince(ctx, a.ID, now.Add(-time.Hour))
	if err != nil {
		return res, err
	}
	if used+len(a.Actions) > maxActionsPerHour {
		res.Status, res.Error = models.RunFailed, strPtr(fmt.Sprintf("Paused: more than %d actions in the last hour. Check its trigger and conditions.", maxActionsPerHour))
		return res, automations.RecordRun(ctx, a.ID, now, res.Status, res.Error)
	}

	ctx = events.WithCause(ctx, events.Cause{AutomationID: a.ID, Depth: depth})
	origin := ai.Origin{Source: "automation", AutomationID: &a.ID}
	for i, act := range a.Actions {
		step := fmt.Sprintf("Step %d (%s)", i+1, act.Type)
		input, err := resolveInput(act.Input, vals, now, loc)
		if err != nil {
			res.Status, res.Error = models.RunFailed, strPtr(step+": "+err.Error())
			break
		}
		origin.Key = shortKey(fmt.Sprintf("%s:%d", runKey, i))
		r, err := e.Executor.Propose(ctx, a.UserID, origin, act.Type, input)
		if err != nil {
			return res, err
		}
		res.Actions = append(res.Actions, r)
		if r.Status == models.AIStatusPendingApproval {
			res.Status = models.RunWaitingApproval
			continue
		}
		if r.Status != models.AIStatusSucceeded {
			res.Status, res.Error = models.RunFailed, strPtr(step+": "+r.Error)
			break
		}
	}

	if res.Status == models.RunWaitingApproval || (res.Status == models.RunFailed && hasPending(res.Actions)) {
		n := models.Notification{
			UserID: a.UserID, Kind: "automation", Priority: "normal", Link: strPtr("/ai/activity"),
			Title: fmt.Sprintf("“%s” is waiting for your approval", a.Name), Body: strPtr("Review what it wants to do on the AI activity page."),
			IdempotencyKey: shortKey("automation-approval:" + runKey),
		}
		if _, err := (repository.NotificationRepository{DB: e.DB}).Insert(ctx, n); err != nil {
			return res, err
		}
	}
	return res, automations.RecordRun(ctx, a.ID, now, res.Status, res.Error)
}

func hasPending(rs []ai.Result) bool {
	for _, r := range rs {
		if r.Status == models.AIStatusPendingApproval {
			return true
		}
	}
	return false
}

// scanOverdue publishes task.overdue for open tasks that passed their due
// time within the window, for users with an automation listening. Each
// task and due time is published once (the event key includes both).
func (e Engine) scanOverdue(ctx context.Context, _ scheduler.Job) error {
	now := e.now()
	rows, err := e.DB.QueryContext(ctx,
		`SELECT t.user_id, t.id FROM tasks t
		  WHERE t.status IN ('pending', 'in_progress') AND t.due_at <= ? AND t.due_at > ?
		    AND EXISTS (SELECT 1 FROM automations a WHERE a.user_id = t.user_id AND a.enabled = 1 AND a.trigger_type = 'task.overdue')
		  ORDER BY t.due_at LIMIT 500`, now, now.Add(-overdueWindow))
	if err != nil {
		return err
	}
	type ref struct{ user, id uint64 }
	var refs []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.user, &r.id); err != nil {
			rows.Close()
			return err
		}
		refs = append(refs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	tasks := repository.TaskRepository{DB: e.DB}
	for _, r := range refs {
		t, err := tasks.Get(ctx, r.user, r.id)
		if errors.Is(err, repository.ErrNotFound) || t.DueAt == nil {
			continue
		}
		if err != nil {
			return err
		}
		ev := events.Event{
			Type: models.TriggerTaskOverdue, UserID: r.user, OccurredAt: *t.DueAt,
			Key: fmt.Sprintf("task.overdue:%d:%d", t.ID, t.DueAt.Unix()), Data: events.TaskData(ctx, e.DB, t),
		}
		if err := e.Events.Publish(ctx, e.DB, ev); err != nil {
			return err
		}
	}
	return nil
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

// shortKey keeps idempotency keys within the 191-byte columns, hashing the
// tail of a long one.
func shortKey(k string) string {
	if len(k) <= 191 {
		return k
	}
	sum := sha256.Sum256([]byte(k))
	return k[:120] + ":" + hex.EncodeToString(sum[:])
}

func strPtr(s string) *string { return &s }
