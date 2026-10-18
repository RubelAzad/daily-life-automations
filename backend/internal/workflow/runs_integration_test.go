//go:build integration

package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/config"
	"lifeos-ai/backend/internal/database"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
)

// brokenEngine is the fixture's engine with a notify tool whose database is
// closed, so notify fails with an internal (retryable) error.
func (f fixture) brokenEngine(t *testing.T) Engine {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	closed, err := database.Open(cfg.DB.DSN())
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	e := f.engine
	e.Executor.Tools = ai.AutomationTools(ai.Deps{Tasks: f.tasks, Notes: f.notes, Notifications: repository.NotificationRepository{DB: closed}})
	return e
}

func TestRunHistory(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	a := f.create(t, `{"name":"Log","trigger":{"type":"task.created"},"actions":[{"type":"notify","input":{"title":"New: {{task.title}}"}},{"type":"create_note","input":{"title":"About {{task.title}}"}}]}`)
	if _, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: "Buy tickets"}); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	runs, meta, err := f.engine.ListRuns(f.ctx, f.user, ListRunsInput{AutomationID: fmt.Sprint(a.ID)})
	if err != nil || meta.Total != 1 {
		t.Fatalf("runs: %d %v", meta.Total, err)
	}
	r := runs[0]
	if r.Status != models.RunSucceeded || r.TriggerSource != "event" || r.TriggerSummary == nil || *r.TriggerSummary != "Buy tickets" ||
		len(r.Steps) != 2 || r.Steps[0].ActionID == nil || r.Steps[1].Status != models.AIStatusSucceeded || r.DurationMS == nil || r.AutomationName != "Log" {
		t.Errorf("run: %+v", r)
	}
	if _, err := f.engine.GetRun(f.ctx, f.user+1_000_000, r.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("another user's run: %v", err)
	}
	if _, _, err := f.engine.ListRuns(f.ctx, f.user, ListRunsInput{Status: "exploded"}); err == nil {
		t.Error("bad status filter accepted")
	}

	// History outlives the automation.
	if err := f.service.Delete(f.ctx, f.user, a.ID); err != nil {
		t.Fatal(err)
	}
	kept, err := f.engine.GetRun(f.ctx, f.user, r.ID)
	if err != nil || kept.AutomationID != nil || kept.AutomationName != "Log" {
		t.Errorf("after delete: %+v %v", kept, err)
	}
}

func TestAutomaticRetry(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	a := f.create(t, `{"name":"Flaky","trigger":{"type":"manual"},"actions":[{"type":"create_note","input":{"title":"First step"}},{"type":"notify","input":{"title":"Second step"}}]}`)
	run, err := f.brokenEngine(t).RunNow(f.ctx, f.user, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != models.RunRetrying || run.NextRetryAt == nil || !run.NextRetryAt.Equal(f.clock.Now().Add(time.Minute)) ||
		run.Steps[0].Status != models.AIStatusSucceeded || run.Steps[1].Status != models.AIStatusFailed || run.Steps[1].Error != "internal error" {
		t.Fatalf("after an internal error: %+v", run)
	}
	if n := f.notifications(t, "“Flaky” failed"); n != 0 {
		t.Error("no failure notice while a retry is coming")
	}

	f.drain(t) // not due yet
	f.clock.Add(61 * time.Second)
	f.drain(t) // the fixture's working engine retries
	got, _ := f.engine.GetRun(f.ctx, f.user, run.ID)
	if got.Status != models.RunSucceeded || got.Attempt != 2 || got.Steps[1].Attempt != 2 || got.Steps[0].Attempt != 1 {
		t.Errorf("after the retry: %+v", got)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM notes WHERE user_id = ? AND title = 'First step'`); n != 1 {
		t.Errorf("first step ran %d times", n)
	}
	if n := f.notifications(t, "Second step"); n != 1 {
		t.Errorf("second step: %d", n)
	}
	if auto := f.reload(t, a.ID); auto.RunCount != 1 || *auto.LastStatus != models.RunSucceeded {
		t.Errorf("a retry isn't a new run: %+v", auto)
	}
}

func TestRetriesGiveUp(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	broken := f.brokenEngine(t)
	f.sched.Handle(JobRetry, broken.retryJob) // retries fail too
	a := f.create(t, `{"name":"Doomed","trigger":{"type":"manual"},"actions":[{"type":"notify","input":{"title":"never"}}]}`)
	run, err := broken.RunNow(f.ctx, f.user, a.ID)
	if err != nil || run.Status != models.RunRetrying {
		t.Fatalf("%+v %v", run, err)
	}
	f.clock.Add(61 * time.Second)
	f.drain(t)
	got, _ := f.engine.GetRun(f.ctx, f.user, run.ID)
	if got.Status != models.RunRetrying || got.Attempt != 2 || got.NextRetryAt == nil || !got.NextRetryAt.Equal(f.clock.Now().Add(5*time.Minute)) {
		t.Fatalf("second attempt: %+v", got)
	}
	f.clock.Add(5*time.Minute + time.Second)
	f.drain(t)
	got, _ = f.engine.GetRun(f.ctx, f.user, run.ID)
	if got.Status != models.RunFailed || got.Attempt != maxRunAttempts || got.NextRetryAt != nil {
		t.Errorf("after the last attempt: %+v", got)
	}
	if n := f.notifications(t, "“Doomed” failed"); n != 1 {
		t.Errorf("failure notifications: %d", n)
	}
}

func TestFailureChainAndManualRetry(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	watcher := f.create(t, `{"name":"Watcher","trigger":{"type":"workflow.failed"},
		"conditions":[{"field":"run.automation","op":"equals","value":"Mover"}],
		"actions":[{"type":"notify","input":{"title":"Alert: {{run.automation}} broke"}}]}`)
	mover := f.create(t, `{"name":"Mover","trigger":{"type":"manual"},"actions":[
		{"type":"notify","input":{"title":"Moving now"}},
		{"type":"reschedule_task","input":{"task_id":99999999,"due_at":"+1d"}}]}`)

	run, err := f.engine.RunNow(f.ctx, f.user, mover.ID)
	if err != nil {
		t.Fatal(err)
	}
	// "not found" is the input's fault: no automatic retry.
	if run.Status != models.RunFailed || run.Error == nil || !strings.Contains(*run.Error, "Step 2 (reschedule_task): not found") {
		t.Fatalf("run: %+v", run)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM scheduled_jobs WHERE user_id = ? AND kind = 'automation.retry'`); n != 0 {
		t.Error("input errors must not be retried automatically")
	}
	if n := f.notifications(t, "“Mover” failed"); n != 1 {
		t.Errorf("failure notification: %d", n)
	}
	f.drain(t)
	if n := f.notifications(t, "Alert: Mover broke"); n != 1 {
		t.Errorf("workflow.failed automation: %d", n)
	}
	if runs, _, _ := f.engine.ListRuns(f.ctx, f.user, ListRunsInput{AutomationID: fmt.Sprint(watcher.ID)}); len(runs) != 1 || runs[0].TriggerSummary == nil || *runs[0].TriggerSummary != "Mover" {
		t.Errorf("watcher run: %+v", runs)
	}

	// Fix the step and retry by hand: it resumes at step 2.
	task, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: "Movable"})
	if err != nil {
		t.Fatal(err)
	}
	actions := []models.Action{
		{Type: "notify", Input: json.RawMessage(`{"title":"Moving now"}`)},
		{Type: "reschedule_task", Input: json.RawMessage(fmt.Sprintf(`{"task_id":%d,"due_at":"+1d"}`, task.ID))},
	}
	if _, err := f.service.Update(f.ctx, f.user, mover.ID, Input{Actions: &actions}); err != nil {
		t.Fatal(err)
	}
	retried, err := f.engine.Retry(f.ctx, f.user, run.ID)
	if err != nil || retried.Status != models.RunSucceeded || retried.Attempt != 2 {
		t.Fatalf("manual retry: %+v %v", retried, err)
	}
	if n := f.notifications(t, "Moving now"); n != 1 {
		t.Errorf("step 1 repeated: %d", n)
	}
	if _, err := f.engine.Retry(f.ctx, f.user, run.ID); !errors.Is(err, ErrNotRetryable) {
		t.Errorf("retrying a succeeded run: %v", err)
	}
	if _, err := f.engine.Retry(f.ctx, f.user+1_000_000, run.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("another user's run: %v", err)
	}
}

func TestApprovalOutcomeReachesRun(t *testing.T) {
	f := newFixture(t, models.ApprovalAlways)
	a := f.create(t, `{"name":"Two notes","trigger":{"type":"manual"},"actions":[{"type":"create_note","input":{"title":"Keep"}},{"type":"create_note","input":{"title":"Drop"}}]}`)
	run, err := f.engine.RunNow(f.ctx, f.user, a.ID)
	if err != nil || run.Status != models.RunWaitingApproval {
		t.Fatalf("%+v %v", run, err)
	}
	if _, err := f.engine.Executor.Approve(f.ctx, f.user, *run.Steps[0].ActionID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.engine.GetRun(f.ctx, f.user, run.ID); got.Status != models.RunWaitingApproval || got.Steps[0].Status != models.AIStatusSucceeded {
		t.Errorf("one approved, one pending: %+v", got)
	}
	if _, err := f.engine.Executor.Reject(f.ctx, f.user, *run.Steps[1].ActionID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.engine.GetRun(f.ctx, f.user, run.ID)
	if got.Status != models.RunFailed || got.Error == nil || !strings.Contains(*got.Error, "you declined it") {
		t.Errorf("after declining: %+v", got)
	}
}
