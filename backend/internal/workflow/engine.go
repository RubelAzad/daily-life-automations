package workflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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
	JobRetry       = "automation.retry"
	JobScanOverdue = "automation.scan_overdue"
)

const (
	// lateLimit: a scheduled run more than this late (no worker was
	// running) is skipped rather than run at an odd time.
	lateLimit = 3 * time.Hour
	// maxActionsPerHour pauses an automation that runs away, e.g. one that
	// fires on every task and creates tasks.
	maxActionsPerHour = 60
	// maxRunAttempts: a run that fails unexpectedly is tried this many
	// times in all; failures caused by its input aren't retried.
	maxRunAttempts = 3
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
	s.Handle(JobRetry, e.retryJob)
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
	userID, t := *job.UserID, trigger{runKey: job.Key}
	if t.runKey == "" {
		t.runKey = fmt.Sprintf("automation-job:%d", job.ID)
	}

	var a models.Automation
	switch {
	case p.FireAt != nil:
		var due bool
		var err error
		if a, due, err = e.advanceSchedule(ctx, userID, p.AutomationID, *p.FireAt); err != nil || !due {
			return err
		}
		t.source, t.fireAt = "schedule", p.FireAt
		if late := e.now().Sub(*p.FireAt); late > lateLimit {
			t.skipped = "Skipped: the worker wasn't running at the scheduled time."
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
		t.source, t.data, t.depth = "event", p.Event.Data, p.Event.Depth
	default:
		return scheduler.Permanent(errors.New("run job has neither a time nor an event"))
	}
	_, err := e.start(ctx, a, t)
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

// trigger is what started a run.
type trigger struct {
	source  string // schedule | event | manual
	runKey  string
	data    map[string]any
	depth   int
	fireAt  *time.Time
	skipped string // set: record the run as skipped with this reason
}

// RunNow runs a schedule or manual automation at once, for the Run button.
// It doesn't move the schedule.
func (e Engine) RunNow(ctx context.Context, userID, id uint64) (models.WorkflowRun, error) {
	a, err := repository.AutomationRepository{DB: e.DB}.Get(ctx, userID, id)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	if spec, _ := triggerSpec(a.Trigger.Type); !spec.RunNow {
		return models.WorkflowRun{}, ErrCantRunNow
	}
	return e.start(ctx, a, trigger{source: "manual", runKey: fmt.Sprintf("automation:%d:manual:%d", a.ID, e.now().UnixNano())})
}

// Errors for runs that can't be started or retried.
var (
	ErrCantRunNow     = errors.New("this automation runs when its event happens; it can't be run by hand")
	ErrNotRetryable   = errors.New("only a failed run can be retried")
	ErrAutomationGone = errors.New("the automation was deleted, so this run can't be retried")
)

// start checks the conditions and, if they hold, records a run and runs it.
// A run whose conditions don't hold isn't recorded: nothing happened. If the
// run already exists (its job is retried after a crash), it is resumed.
func (e Engine) start(ctx context.Context, a models.Automation, t trigger) (models.WorkflowRun, error) {
	settings, err := e.Settings.Get(ctx, a.UserID)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	now, loc := e.now(), settings.Location()
	if spec, ok := triggerSpec(a.Trigger.Type); ok && len(a.Conditions) > 0 {
		_, conds := validateConditions(spec, a.Conditions)
		if !matches(conds, placeholderValues(a.Name, t.data, now, loc)) {
			return models.WorkflowRun{Status: models.RunSkipped, Steps: []models.RunStep{}}, nil
		}
	}

	run := models.WorkflowRun{
		UserID: a.UserID, AutomationID: &a.ID, AutomationName: a.Name, TriggerType: a.Trigger.Type, TriggerSource: t.source,
		TriggerSummary: summarize(t, loc), Depth: t.depth, RunKey: shortKey(t.runKey), Status: models.RunRunning,
		Steps: notRun(a.Actions), Attempt: 1, StartedAt: now,
	}
	if t.data != nil {
		if run.TriggerData, err = json.Marshal(t.data); err != nil {
			return run, err
		}
	}
	run, created, err := repository.WorkflowRunRepository{DB: e.DB}.Create(ctx, run)
	if err != nil {
		return run, err
	}
	if !created && run.Status != models.RunRunning {
		return run, nil // finished before its job could be marked done
	}

	if t.skipped != "" {
		return e.finish(ctx, a, run, run.StartedAt, models.RunSkipped, &t.skipped, created)
	}
	if created {
		used, err := e.Executor.Actions.CountForAutomationSince(ctx, a.ID, now.Add(-time.Hour))
		if err != nil {
			return run, err
		}
		if used+len(a.Actions) > maxActionsPerHour {
			msg := fmt.Sprintf("Paused: more than %d actions in the last hour. Check its trigger and conditions.", maxActionsPerHour)
			return e.finish(ctx, a, run, run.StartedAt, models.RunFailed, &msg, true)
		}
	}
	return e.resume(ctx, a, run)
}

// resume runs the steps not yet taken: all of them on the first attempt;
// on a retry, the failed step and those after it. Steps follow the
// automation's current actions, keeping the outcome of any step already
// taken. Action failures end the run; only internal errors are returned
// (the job is then retried, and steps already recorded under this attempt
// are skipped thanks to their idempotency keys).
func (e Engine) resume(ctx context.Context, a models.Automation, run models.WorkflowRun) (models.WorkflowRun, error) {
	settings, err := e.Settings.Get(ctx, a.UserID)
	if err != nil {
		return run, err
	}
	var data map[string]any
	if len(run.TriggerData) > 0 {
		if err := json.Unmarshal(run.TriggerData, &data); err != nil {
			return run, err
		}
	}
	now, loc := e.now(), settings.Location()
	vals := placeholderValues(a.Name, data, now, loc)

	steps := notRun(a.Actions)
	for i := range steps {
		if i < len(run.Steps) && run.Steps[i].Type == steps[i].Type && taken(run.Steps[i].Status) {
			steps[i] = run.Steps[i]
		}
	}
	run.Steps, run.Error = steps, nil

	ctx = events.WithCause(ctx, events.Cause{AutomationID: a.ID, Depth: run.Depth})
	origin := ai.Origin{Source: "automation", AutomationID: &a.ID, RunID: &run.ID}
	var failure *string
	retryable, waiting := false, false
	for i := range run.Steps {
		step := &run.Steps[i]
		if taken(step.Status) {
			waiting = waiting || step.Status == models.AIStatusPendingApproval
			continue
		}
		label := fmt.Sprintf("Step %d (%s)", i+1, step.Type)
		step.Attempt, step.Error, step.Summary, step.ActionID = run.Attempt, "", "", nil
		input, err := resolveInput(a.Actions[i].Input, vals, now, loc)
		if err != nil {
			step.Status, step.Error = models.AIStatusFailed, err.Error()
			failure = strPtr(label + ": " + err.Error())
			break
		}
		origin.Key = shortKey(fmt.Sprintf("%s:%d:a%d", run.RunKey, i, run.Attempt))
		r, err := e.Executor.Propose(ctx, a.UserID, origin, step.Type, input)
		if err != nil {
			return run, err
		}
		step.ActionID, step.Status, step.Summary, step.Error = &r.ActionID, r.Status, r.Summary, r.Error
		if r.Status == models.AIStatusPendingApproval {
			waiting = true
			continue
		}
		if r.Status != models.AIStatusSucceeded {
			failure, retryable = strPtr(label+": "+r.Error), r.Retryable
			break
		}
	}

	newRun := run.Attempt == 1
	switch {
	case failure != nil && retryable && run.Attempt < maxRunAttempts && a.Enabled:
		next := e.now().Add(retryDelay(run.Attempt))
		run.NextRetryAt = &next
		if _, _, err := scheduler.Enqueue(ctx, e.DB, scheduler.NewJob{
			UserID: &run.UserID, Kind: JobRetry, RunAt: next, Payload: retryPayload{RunID: run.ID},
			Key: fmt.Sprintf("workflow-run:%d:retry:%d", run.ID, run.Attempt+1),
		}); err != nil {
			return run, err
		}
		return e.finish(ctx, a, run, now, models.RunRetrying, failure, newRun)
	case failure != nil:
		return e.finish(ctx, a, run, now, models.RunFailed, failure, newRun)
	case waiting:
		return e.finish(ctx, a, run, now, models.RunWaitingApproval, nil, newRun)
	default:
		return e.finish(ctx, a, run, now, models.RunSucceeded, nil, newRun)
	}
}

// finish records how a run (attempt) ended, taking since as the attempt's
// start for its duration, and tells the user what they
// need to know: approvals to give, and failures that won't be retried.
func (e Engine) finish(ctx context.Context, a models.Automation, run models.WorkflowRun, since time.Time, status string, errMsg *string, newRun bool) (models.WorkflowRun, error) {
	now := e.now()
	run.Status, run.Error = status, errMsg
	if status != models.RunRetrying {
		run.NextRetryAt = nil
	}
	ms := int(now.Sub(since) / time.Millisecond)
	run.FinishedAt, run.DurationMS = &now, &ms
	if err := (repository.WorkflowRunRepository{DB: e.DB}).Save(ctx, run); err != nil {
		return run, err
	}
	if err := (repository.AutomationRepository{DB: e.DB}).RecordRun(ctx, a.ID, now, status, errMsg, newRun); err != nil {
		return run, err
	}

	var n *models.Notification
	switch status {
	case models.RunWaitingApproval:
		n = &models.Notification{
			Title: fmt.Sprintf("“%s” is waiting for your approval", a.Name), Body: strPtr("Review what it wants to do in Approvals."),
			Link: strPtr("/approvals"), IdempotencyKey: fmt.Sprintf("workflow-run:%d:approval:%d", run.ID, run.Attempt),
		}
	case models.RunFailed:
		n = &models.Notification{
			Title: fmt.Sprintf("“%s” failed", a.Name), Body: errMsg, Priority: "high",
			Link: strPtr(fmt.Sprintf("/automations?run=%d", run.ID)), IdempotencyKey: fmt.Sprintf("workflow-run:%d:failed:%d", run.ID, run.Attempt),
		}
		// workflow.failed lets other automations react. It carries this
		// run's cause, so an automation never reacts to its own failure.
		ev := events.Event{
			Type: models.TriggerWorkflowFailed, UserID: run.UserID, Key: fmt.Sprintf("workflow.failed:%d:%d", run.ID, run.Attempt),
			Data: map[string]any{"run": map[string]any{"id": run.ID, "automation": a.Name, "error": deref(errMsg)}},
		}
		if err := e.Events.Publish(events.WithCause(ctx, events.Cause{AutomationID: a.ID, Depth: run.Depth}), e.DB, ev); err != nil {
			return run, err
		}
	}
	if n != nil {
		n.UserID, n.Kind = run.UserID, "automation"
		if n.Priority == "" {
			n.Priority = "normal"
		}
		if _, err := (repository.NotificationRepository{DB: e.DB}).Insert(ctx, *n); err != nil {
			return run, err
		}
	}
	return run, nil
}

// retryJob runs an automatic retry of a run that failed unexpectedly.
func (e Engine) retryJob(ctx context.Context, job scheduler.Job) error {
	var p retryPayload
	if err := job.Decode(&p); err != nil {
		return err
	}
	if job.UserID == nil {
		return scheduler.Permanent(errors.New("retry job without a user"))
	}
	run, a, err := e.beginRetry(ctx, *job.UserID, p.RunID, false)
	switch {
	case errors.Is(err, ErrNotRetryable), errors.Is(err, repository.ErrNotFound):
		return nil // retried by hand already, or deleted
	case errors.Is(err, ErrAutomationGone):
		return e.failOrphan(ctx, run, "The automation was deleted before the retry.")
	case err != nil:
		return err
	}
	if !a.Enabled {
		_, err := e.finish(ctx, a, run, e.now(), models.RunFailed, strPtr("Not retried: the automation was turned off."), false)
		return err
	}
	_, err = e.resume(ctx, a, run)
	return err
}

// Retry retries a failed run now, from the step that failed, with the
// automation's current actions. A scheduled automatic retry is dropped.
func (e Engine) Retry(ctx context.Context, userID, runID uint64) (models.WorkflowRun, error) {
	run, a, err := e.beginRetry(ctx, userID, runID, true)
	if err != nil {
		return run, err
	}
	return e.resume(ctx, a, run)
}

// beginRetry moves a run on to its next attempt, under a lock so a manual
// and an automatic retry can't both start it. Automatic retries need the
// run to be retrying; manual ones accept failed runs too.
func (e Engine) beginRetry(ctx context.Context, userID, runID uint64, manual bool) (run models.WorkflowRun, a models.Automation, err error) {
	err = repository.InTx(ctx, e.DB, func(db repository.DBTX) error {
		runs := repository.WorkflowRunRepository{DB: db}
		var err error
		if run, err = runs.GetForUpdate(ctx, userID, runID); err != nil {
			return err
		}
		if run.Status != models.RunRetrying && !(manual && run.Status == models.RunFailed) {
			return ErrNotRetryable
		}
		if run.AutomationID == nil {
			return ErrAutomationGone
		}
		if a, err = (repository.AutomationRepository{DB: db}).Get(ctx, userID, *run.AutomationID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrAutomationGone
			}
			return err
		}
		run.Attempt++
		run.Status, run.NextRetryAt, run.FinishedAt = models.RunRunning, nil, nil
		return runs.Save(ctx, run)
	})
	return run, a, err
}

// failOrphan fails a run whose automation is gone.
func (e Engine) failOrphan(ctx context.Context, run models.WorkflowRun, msg string) error {
	now := e.now()
	run.Status, run.Error, run.NextRetryAt, run.FinishedAt = models.RunFailed, &msg, nil, &now
	return repository.WorkflowRunRepository{DB: e.DB}.Save(ctx, run)
}

// Refresh brings steps waiting for approval up to date with the user's
// decision, and the run's status with them. Runs are read through it, and
// the approvals inbox calls it (through RefreshRun) after each decision.
func (e Engine) Refresh(ctx context.Context, run models.WorkflowRun) (models.WorkflowRun, error) {
	if run.Status != models.RunWaitingApproval {
		return run, nil
	}
	// Most reads find nothing decided: check before taking the lock.
	if _, changed, err := e.applyDecisions(ctx, run); err != nil || !changed {
		return run, err
	}
	err := repository.InTx(ctx, e.DB, func(db repository.DBTX) error {
		locked, err := repository.WorkflowRunRepository{DB: db}.GetForUpdate(ctx, run.UserID, run.ID)
		if err != nil {
			return err
		}
		if locked.Status != models.RunWaitingApproval {
			run = locked
			return nil
		}
		updated, changed, err := e.applyDecisions(ctx, locked)
		if err != nil || !changed {
			run = locked
			return err
		}
		run = updated
		if err := (repository.WorkflowRunRepository{DB: db}).Save(ctx, run); err != nil {
			return err
		}
		if run.AutomationID != nil && run.Status != models.RunWaitingApproval {
			return repository.AutomationRepository{DB: db}.SetStatusIfLatest(ctx, *run.AutomationID, run.ID, run.Status, run.Error)
		}
		return nil
	})
	return run, err
}

// RefreshRun refreshes one of the user's runs by ID.
func (e Engine) RefreshRun(ctx context.Context, userID, runID uint64) (models.WorkflowRun, error) {
	run, err := repository.WorkflowRunRepository{DB: e.DB}.Get(ctx, userID, runID)
	if err != nil {
		return run, err
	}
	return e.Refresh(ctx, run)
}

// applyDecisions copies the outcome of decided approvals into the run's
// steps. Once none is left waiting, the run succeeds, or fails if a step
// was declined, expired or failed.
func (e Engine) applyDecisions(ctx context.Context, run models.WorkflowRun) (models.WorkflowRun, bool, error) {
	run.Steps = append([]models.RunStep(nil), run.Steps...)
	pending, changed := false, false
	var failure *string
	for i := range run.Steps {
		step := &run.Steps[i]
		if step.Status != models.AIStatusPendingApproval || step.ActionID == nil {
			continue
		}
		act, err := e.Executor.Actions.Get(ctx, run.UserID, *step.ActionID)
		if err != nil {
			return run, false, err
		}
		switch act.Status {
		case models.AIStatusPendingApproval, models.AIStatusRunning:
			pending = true
			continue
		case models.AIStatusSucceeded:
		default: // rejected, expired or failed
			if failure == nil {
				failure = strPtr(fmt.Sprintf("Step %d (%s): %s", i+1, step.Type, decision(act)))
			}
		}
		step.Status, step.Error, changed = act.Status, deref(act.Error), true
	}
	if changed && !pending {
		run.Status, run.Error = models.RunSucceeded, nil
		if failure != nil {
			run.Status, run.Error = models.RunFailed, failure
		}
	}
	return run, changed, nil
}

func decision(a models.AIAction) string {
	switch a.Status {
	case models.AIStatusRejected:
		return "you declined it"
	case models.AIStatusExpired:
		return "the approval request expired"
	}
	if a.Error != nil {
		return *a.Error
	}
	return a.Status
}

// taken reports a step that has been done, or is waiting for approval: a
// retry leaves it alone.
func taken(status string) bool {
	return status == models.AIStatusSucceeded || status == models.AIStatusPendingApproval
}

func notRun(actions []models.Action) []models.RunStep {
	steps := make([]models.RunStep, len(actions))
	for i, a := range actions {
		steps[i] = models.RunStep{Index: i, Type: a.Type, Status: models.StepNotRun}
	}
	return steps
}

// summarize says what triggered a run, e.g. the task's title.
func summarize(t trigger, loc *time.Location) *string {
	switch {
	case t.fireAt != nil:
		return strPtr("Scheduled for " + t.fireAt.In(loc).Format("Mon 2 Jan 15:04"))
	case t.source == "manual":
		return strPtr("Run by hand")
	}
	for _, subject := range []string{"task", "note", "run", "bill", "budget"} {
		if d, ok := t.data[subject].(map[string]any); ok {
			for _, k := range []string{"title", "automation", "name", "category"} {
				if s, ok := d[k].(string); ok && s != "" {
					return strPtr(truncateRunes(s, 255))
				}
			}
		}
	}
	return nil
}

// retryDelay is the wait before attempt n+1: 1 minute, then 5.
func retryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return time.Minute
	}
	return 5 * time.Minute
}

type retryPayload struct {
	RunID uint64 `json:"run_id"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
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
