//go:build integration

package workflow

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/scheduler"
	"lifeos-ai/backend/internal/services"
)

func TestTemplates(t *testing.T) {
	f := newFixture(t, models.ApprovalAlways)
	list, err := f.service.ListTemplates(f.ctx, f.user)
	if err != nil || len(list) != 2 || list[0].AutomationID != nil || list[1].Key != "missed_task_recovery" {
		t.Fatalf("templates: %+v %v", list, err)
	}
	plan, err := f.service.AddTemplate(f.ctx, f.user, "daily_planning")
	if err != nil {
		t.Fatal(err)
	}
	if plan.TemplateKey == nil || *plan.TemplateKey != "daily_planning" || plan.Trigger.Schedule == nil || plan.Trigger.Schedule.Time != "07:00" ||
		plan.NextRunAt == nil || len(plan.Actions) != 1 || plan.Actions[0].Type != "plan_day" || !plan.Enabled {
		t.Errorf("daily planning: %+v", plan)
	}
	if _, err := f.service.AddTemplate(f.ctx, f.user, "daily_planning"); !errors.Is(err, ErrTemplateAdded) {
		t.Errorf("added twice: %v", err)
	}
	if _, err := f.service.AddTemplate(f.ctx, f.user, "world_peace"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("unknown template: %v", err)
	}
	rec, err := f.service.AddTemplate(f.ctx, f.user, "missed_task_recovery")
	if err != nil || rec.Trigger.Type != models.TriggerTaskOverdue {
		t.Fatalf("recovery: %+v %v", rec, err)
	}
	list, _ = f.service.ListTemplates(f.ctx, f.user)
	if list[0].AutomationID == nil || *list[0].AutomationID != plan.ID || list[1].AutomationID == nil {
		t.Errorf("added: %+v", list)
	}
	// Deleted, it can be added again.
	if err := f.service.Delete(f.ctx, f.user, plan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AddTemplate(f.ctx, f.user, "daily_planning"); err != nil {
		t.Errorf("re-adding after delete: %v", err)
	}
}

// noQuietHours lets slots fall at any time of day, so the test doesn't
// depend on when it runs.
func (f fixture) noQuietHours(t *testing.T) {
	t.Helper()
	s, err := f.settings.Get(f.ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	s.QuietHoursStart, s.QuietHoursEnd = "00:00", "00:00"
	if err := f.settings.Save(f.ctx, s); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) task(t *testing.T, title, priority string, due time.Time) models.Task {
	t.Helper()
	d := due.UTC().Format(time.RFC3339)
	task, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: title, Priority: priority, DueAt: &d})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestMissedTaskRecovery(t *testing.T) {
	f := newFixture(t, models.ApprovalAlways)
	f.noQuietHours(t)
	if _, err := f.service.AddTemplate(f.ctx, f.user, "missed_task_recovery"); err != nil {
		t.Fatal(err)
	}
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	now := time.Now().UTC().Truncate(time.Second)
	missed := f.task(t, "Missed report", "high", now.Add(-10*time.Minute))
	// Busy until now+90m, then another task due 30 minutes later: the first
	// free slot starts after that.
	eventEnd := now.Add(90 * time.Minute)
	start, end := now.Add(-10*time.Minute).Format(time.RFC3339), eventEnd.Format(time.RFC3339)
	if _, _, err := f.calendar.Create(f.ctx, f.user, services.CreateEventInput{Title: "Workshop", StartsAt: &start, EndsAt: &end}); err != nil {
		t.Fatal(err)
	}
	f.task(t, "Other", "medium", eventEnd.Add(30*time.Minute))
	wantStart := eventEnd.Add(30 * time.Minute).Truncate(15 * time.Minute)
	if wantStart.Before(eventEnd.Add(30 * time.Minute)) {
		wantStart = wantStart.Add(15 * time.Minute)
	}

	// Today's plan has the missed task in it.
	today := now.In(dhaka)
	date := models.Date{Time: time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)}
	briefings := repository.BriefingRepository{DB: testDB}
	planID, _, err := briefings.Start(f.ctx, f.user, models.BriefingMorning, date, false)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := json.Marshal(models.MorningPlan{Summary: "A day", Schedule: []models.ScheduleBlock{
		{Start: "08:00", End: "08:30", Kind: "break", Title: "Coffee"},
		{Start: "09:00", End: "09:30", Kind: "task", Title: "Missed report", TaskID: &missed.ID},
	}})
	if err := briefings.Finish(f.ctx, planID, models.BriefingReady, content, nil, nil); err != nil {
		t.Fatal(err)
	}

	f.clock.Set(now)
	if err := f.engine.scanOverdue(f.ctx, scheduler.Job{}); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	runs, _, err := f.engine.ListRuns(f.ctx, f.user, ListRunsInput{})
	if err != nil || len(runs) != 1 || runs[0].Status != models.RunWaitingApproval {
		t.Fatalf("runs: %+v %v", runs, err)
	}
	action, err := f.engine.Executor.Actions.Get(f.ctx, f.user, *runs[0].Steps[0].ActionID)
	if err != nil {
		t.Fatal(err)
	}
	wantSummary := "Move “Missed report” to " + wantStart.In(dhaka).Format("Mon 2 Jan 15:04")
	if action.Summary == nil || !strings.HasPrefix(*action.Summary, wantSummary) || !strings.Contains(string(action.Input), `"at"`) {
		t.Errorf("proposal: %v %s, want %q", deref(action.Summary), action.Input, wantSummary)
	}

	approved, err := f.engine.Executor.Approve(f.ctx, f.user, action.ID)
	if err != nil || approved.Status != models.AIStatusSucceeded {
		t.Fatalf("approve: %+v %v", approved, err)
	}
	got, _ := f.tasks.Get(f.ctx, f.user, missed.ID)
	if got.DueAt == nil || !got.DueAt.Equal(wantStart.Add(30*time.Minute)) {
		t.Errorf("due %v, want %v", got.DueAt, wantStart.Add(30*time.Minute))
	}
	if run, _ := f.engine.GetRun(f.ctx, f.user, runs[0].ID); run.Status != models.RunSucceeded {
		t.Errorf("run: %+v", run)
	}

	br, _ := briefings.Get(f.ctx, f.user, planID)
	var plan models.MorningPlan
	_ = json.Unmarshal(br.Content, &plan)
	var blocks []models.ScheduleBlock
	for _, b := range plan.Schedule {
		if b.TaskID != nil && *b.TaskID == missed.ID {
			blocks = append(blocks, b)
		}
	}
	slotLocal := wantStart.In(dhaka)
	if slotLocal.Format("2006-01-02") == today.Format("2006-01-02") && slotLocal.Add(29*time.Minute).Format("2006-01-02") == today.Format("2006-01-02") {
		if len(blocks) != 1 || blocks[0].Start != slotLocal.Format("15:04") || plan.Schedule[0].Title != "Coffee" {
			t.Errorf("plan should show the new slot: %+v", plan.Schedule)
		}
	} else if len(blocks) != 0 {
		t.Errorf("slot isn't today: the task should leave today's plan: %+v", plan.Schedule)
	}
}

func TestRecoverTaskCases(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	f.noQuietHours(t)
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	exec := f.engine.Executor
	origin := ai.Origin{Source: "test"}
	now := time.Now()

	// Low priority: from tomorrow.
	low := f.task(t, "Someday", "low", now.Add(-time.Hour))
	r, err := exec.Propose(f.ctx, f.user, origin, "recover_task", json.RawMessage(`{"task_id":`+itoa(low.ID)+`}`))
	if err != nil || r.Status != models.AIStatusSucceeded {
		t.Fatalf("low: %+v %v", r, err)
	}
	got, _ := f.tasks.Get(f.ctx, f.user, low.ID)
	tomorrow := now.In(dhaka).AddDate(0, 0, 1).Format("2006-01-02")
	if got.DueAt == nil || got.DueAt.In(dhaka).Add(-30*time.Minute).Format("2006-01-02") != tomorrow {
		t.Errorf("low priority moved to %v, want a slot tomorrow (%s)", got.DueAt, tomorrow)
	}

	// A given time that has passed.
	task := f.task(t, "Late one", "medium", now.Add(-time.Hour))
	past := now.Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	r, _ = exec.Propose(f.ctx, f.user, origin, "recover_task", json.RawMessage(`{"task_id":`+itoa(task.ID)+`,"at":"`+past+`"}`))
	if r.Status != models.AIStatusFailed || !strings.Contains(r.Error, "already passed") {
		t.Errorf("past time: %+v", r)
	}

	// Completed while waiting for approval.
	s, _ := f.settings.Get(f.ctx, f.user)
	s.AIApprovalMode = models.ApprovalAlways
	if err := f.settings.Save(f.ctx, s); err != nil {
		t.Fatal(err)
	}
	r, _ = exec.Propose(f.ctx, f.user, origin, "recover_task", json.RawMessage(`{"task_id":`+itoa(task.ID)+`,"minutes":60}`))
	if r.Status != models.AIStatusPendingApproval {
		t.Fatalf("pending: %+v", r)
	}
	if _, err := f.tasks.Update(f.ctx, f.user, task.ID, services.UpdateTaskInput{Status: services.Optional[string]{Set: true, Value: models.TaskCompleted}}); err != nil {
		t.Fatal(err)
	}
	a, _ := exec.Approve(f.ctx, f.user, r.ActionID)
	if a.Status != models.AIStatusFailed || a.Error == nil || !strings.Contains(*a.Error, "already completed") {
		t.Errorf("approving after completion: %+v", a)
	}

	// A task that doesn't exist fails when it runs (not when it's checked).
	r, _ = exec.Propose(f.ctx, f.user, origin, "recover_task", json.RawMessage(`{"task_id":99999999999}`))
	if r.Status != models.AIStatusPendingApproval {
		t.Fatalf("unknown task proposal: %+v", r)
	}
	if a, _ := exec.Approve(f.ctx, f.user, r.ActionID); a.Status != models.AIStatusFailed || deref(a.Error) != "not found" {
		t.Errorf("unknown task: %+v", a)
	}
}

func TestDailyPlanning(t *testing.T) {
	f := newFixture(t, models.ApprovalAlways)
	a, err := f.service.AddTemplate(f.ctx, f.user, "daily_planning")
	if err != nil {
		t.Fatal(err)
	}
	f.task(t, "Write proposal", "high", time.Now().Add(3*time.Hour))

	planner := func(client ai.Client) Engine {
		actions := repository.AIActionRepository{DB: testDB}
		svc := ai.NewService(client, ai.Executor{Tools: ai.NewRegistry(), Actions: actions, Settings: f.settings},
			repository.AIConversationRepository{DB: testDB}, repository.UserRepository{DB: testDB})
		taskRepo, reminders, notes := repository.TaskRepository{DB: testDB}, repository.ReminderRepository{DB: testDB}, repository.NoteRepository{DB: testDB}
		briefer := ai.NewBriefer(svc, ai.Deps{
			Tasks: f.tasks, Calendar: f.calendar,
			Today: services.TodayService{Tasks: taskRepo, Calendar: f.calendar, Reminders: reminders, Notes: notes},
		}, repository.BriefingRepository{DB: testDB}, repository.NotificationRepository{DB: testDB})
		e := f.engine
		e.Executor.Tools = ai.AutomationTools(ai.Deps{Tasks: f.tasks, Calendar: f.calendar, Settings: f.settings, Planner: briefer})
		return e
	}

	// AI off: the run fails, and isn't retried (retrying wouldn't help).
	run, err := planner(nil).RunNow(f.ctx, f.user, a.ID)
	if err != nil || run.Status != models.RunFailed || run.Error == nil || !strings.Contains(*run.Error, "AI assistant is off") {
		t.Errorf("AI off: %+v %v", run, err)
	}

	e := planner(ai.FakeClient{})
	f.clock.Add(time.Second) // a new "Run now" press
	run, err = e.RunNow(f.ctx, f.user, a.ID)
	if err != nil || run.Status != models.RunSucceeded {
		t.Fatalf("plan: %+v %v", run, err)
	}
	// No approval needed, even in "always" mode: it only reads and notifies.
	if run.Steps[0].Status != models.AIStatusSucceeded {
		t.Errorf("step: %+v", run.Steps[0])
	}
	// Run again: today's plan is reused, no second model call or notification.
	f.clock.Add(time.Second)
	if run, err = e.RunNow(f.ctx, f.user, a.ID); err != nil || run.Status != models.RunSucceeded {
		t.Fatalf("second run: %+v %v", run, err)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM ai_briefings WHERE user_id = ? AND kind = 'morning' AND status = 'ready'`); n != 1 {
		t.Errorf("plans = %d, want 1", n)
	}
	if n := f.notifications(t, "Your morning briefing is ready"); n != 1 {
		t.Errorf("notifications = %d, want 1", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM ai_actions WHERE user_id = ? AND kind = 'llm_call'`); n != 1 {
		t.Errorf("model calls = %d, want 1", n)
	}
}

func itoa(n uint64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
