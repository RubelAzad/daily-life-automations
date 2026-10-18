//go:build integration

// Runs automations end to end against the real database from backend/.env:
//
//	go test -tags integration ./internal/workflow/
//
// It creates throwaway users (e2e-wf-*@example.com) and deletes them again.
// Stop any running worker first: it shares the database and would pick up
// the tests' jobs, which run on the tests' own clock.
package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/config"
	"lifeos-ai/backend/internal/database"
	"lifeos-ai/backend/internal/events"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/scheduler"
	"lifeos-ai/backend/internal/services"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	if err := os.Chdir("../.."); err != nil { // backend/, where .env lives
		panic(err)
	}
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	if testDB, err = database.Open(cfg.DB.DSN()); err != nil {
		panic(err)
	}
	code := m.Run()
	testDB.Close()
	os.Exit(code)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Set(t time.Time)     { c.mu.Lock(); c.t = t; c.mu.Unlock() }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type fixture struct {
	ctx      context.Context
	user     uint64
	clock    *clock
	tasks    services.TaskService
	notes    services.NoteService
	service  Service
	engine   Engine
	sched    *scheduler.Scheduler
	settings repository.SettingsRepository
}

func newFixture(t *testing.T, approvalMode string) fixture {
	t.Helper()
	ctx := context.Background()
	user, err := repository.UserRepository{DB: testDB}.Create(ctx, "WF Test", fmt.Sprintf("e2e-wf-%d@example.com", time.Now().UnixNano()), "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() { // show the user's jobs, to tell "never queued" from "never ran"
			rows, err := testDB.Query(`SELECT id, kind, status, run_at, attempts, COALESCE(locked_by, ''), COALESCE(last_error, '') FROM scheduled_jobs WHERE user_id = ?`, user.ID)
			if err == nil {
				for rows.Next() {
					var id, attempts int
					var kind, status, runAt, lockedBy, lastErr string
					_ = rows.Scan(&id, &kind, &status, &runAt, &attempts, &lockedBy, &lastErr)
					t.Logf("job %d %s %s run_at=%s attempts=%d locked_by=%q error=%q", id, kind, status, runAt, attempts, lockedBy, lastErr)
				}
				rows.Close()
			}
			for _, q := range []string{
				`SELECT CONCAT('notification: ', title, ' @', created_at) FROM notifications WHERE user_id = ?`,
				`SELECT CONCAT('automation: ', name, ' next=', COALESCE(next_run_at, '-'), ' last=', COALESCE(last_status, '-'), ' runs=', run_count, ' err=', COALESCE(last_error, '')) FROM automations WHERE user_id = ?`,
				`SELECT CONCAT('action: ', COALESCE(tool, ''), ' ', status, ' ', COALESCE(error, ''), ' key=', COALESCE(idempotency_key, '')) FROM ai_actions WHERE user_id = ?`,
			} {
				if rows, err := testDB.Query(q, user.ID); err == nil {
					for rows.Next() {
						var line string
						_ = rows.Scan(&line)
						t.Log(line)
					}
					rows.Close()
				}
			}
		}
		_, _ = testDB.Exec(`DELETE FROM scheduled_jobs WHERE user_id = ?`, user.ID)
		_, _ = testDB.Exec(`DELETE FROM users WHERE id = ?`, user.ID)
	})
	settings := repository.SettingsRepository{DB: testDB}
	s := models.DefaultSettings(user.ID)
	s.Timezone = "Asia/Dhaka"
	s.AIApprovalMode = approvalMode
	if err := settings.Save(ctx, s); err != nil {
		t.Fatal(err)
	}

	c := &clock{t: time.Now().UTC().Truncate(time.Millisecond)}
	bus := events.Bus{Now: c.Now}
	taskRepo, reminders, notesRepo := repository.TaskRepository{DB: testDB}, repository.ReminderRepository{DB: testDB}, repository.NoteRepository{DB: testDB}
	tasks := services.TaskService{Tasks: taskRepo, Lists: repository.TaskListRepository{DB: testDB}, Projects: repository.ProjectRepository{DB: testDB}, Events: bus}
	notes := services.NoteService{Notes: notesRepo, Files: repository.FileRepository{DB: testDB}, Projects: repository.ProjectRepository{DB: testDB}, Reminders: reminders, Events: bus}
	tools := ai.AutomationTools(ai.Deps{
		Tasks: tasks, Notes: notes, Notifications: repository.NotificationRepository{DB: testDB},
		Reminders: services.ReminderService{Reminders: reminders, Tasks: taskRepo, Events: repository.CalendarEventRepository{DB: testDB}, Notes: notesRepo},
	})
	engine := Engine{
		DB: testDB, Settings: settings, Events: bus, Now: c.Now,
		Executor: ai.Executor{Tools: tools, Actions: repository.AIActionRepository{DB: testDB}, Settings: settings, Now: c.Now},
	}
	sched := &scheduler.Scheduler{DB: testDB, WorkerID: "wf-test", Now: c.Now}
	sched.Handle(events.JobKind, engine.dispatch)
	sched.Handle(JobRun, engine.runJob)
	sched.Handle(JobRetry, engine.retryJob)
	return fixture{
		ctx: ctx, user: user.ID, clock: c, tasks: tasks, notes: notes, settings: settings, engine: engine, sched: sched,
		service: Service{DB: testDB, Settings: settings, Tools: tools, Now: c.Now},
	}
}

func (f fixture) create(t *testing.T, raw string) models.Automation {
	t.Helper()
	var in Input
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	a, err := f.service.Create(f.ctx, f.user, in)
	if err != nil {
		var ve *services.ValidationError
		if errors.As(err, &ve) {
			t.Fatalf("create: %v", ve.Fields)
		}
		t.Fatal(err)
	}
	return a
}

// drain runs the user's due jobs. Other users' jobs may run too; tests only
// look at their own user's data.
func (f fixture) drain(t *testing.T) {
	t.Helper()
	if _, err := f.sched.Drain(f.ctx); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := testDB.QueryRow(q, append([]any{f.user}, args...)...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f fixture) tasksTitled(t *testing.T, prefix string) int {
	return f.count(t, `SELECT COUNT(*) FROM tasks WHERE user_id = ? AND title LIKE ?`, prefix+"%")
}

func (f fixture) notifications(t *testing.T, title string) int {
	return f.count(t, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND title = ?`, title)
}

func (f fixture) reload(t *testing.T, id uint64) models.Automation {
	t.Helper()
	a, err := f.service.Get(f.ctx, f.user, id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestValidation(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	cases := map[string]string{
		`{"name":"x","trigger":{"type":"weather.changed"},"actions":[{"type":"notify","input":{"title":"hi"}}]}`:                                                                "trigger.type",
		`{"name":"x","trigger":{"type":"manual"},"actions":[]}`:                                                                                                                 "actions",
		`{"name":"x","trigger":{"type":"manual"},"actions":[{"type":"delete_task","input":{"task_id":1}}]}`:                                                                     "actions[0]",
		`{"name":"x","trigger":{"type":"manual"},"actions":[{"type":"notify","input":{"title":"{{task.title}}"}}]}`:                                                             "actions[0]",
		`{"name":"x","trigger":{"type":"manual"},"actions":[{"type":"notify","input":{}}]}`:                                                                                     "actions[0]",
		`{"name":"x","trigger":{"type":"manual"},"actions":[{"type":"notify","input":{"title":"hi","link":"https://evil.example"}}]}`:                                           "actions[0]",
		`{"name":"x","trigger":{"type":"manual"},"actions":[{"type":"create_task","input":{"title":"t","due_at":"next week"}}]}`:                                                "actions[0]",
		`{"name":"x","trigger":{"type":"manual"},"actions":[{"type":"create_task","input":{"title":"t","priority":"critical"}}]}`:                                               "actions[0]",
		`{"name":"x","trigger":{"type":"manual"},"actions":[{"type":"create_task","input":{"title":"t","colour":"red"}}]}`:                                                      "actions[0]",
		`{"name":"x","trigger":{"type":"schedule"},"actions":[{"type":"notify","input":{"title":"hi"}}]}`:                                                                       "trigger.schedule",
		`{"name":"x","trigger":{"type":"task.created"},"conditions":[{"field":"note.kind","op":"equals","value":"idea"}],"actions":[{"type":"notify","input":{"title":"hi"}}]}`: "conditions[0]",
		`{"name":"","trigger":{"type":"manual"},"actions":[{"type":"notify","input":{"title":"hi"}}]}`:                                                                          "name",
	}
	for raw, field := range cases {
		var in Input
		_ = json.Unmarshal([]byte(raw), &in)
		_, err := f.service.Create(f.ctx, f.user, in)
		var ve *services.ValidationError
		if !errors.As(err, &ve) || ve.Fields[field] == "" {
			t.Errorf("%s: want an error on %s, got %v", raw, field, err)
		}
	}
}

func TestTaskCreatedWithConditions(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	a := f.create(t, `{"name":"Urgent follow-up","trigger":{"type":"task.created"},
		"conditions":[{"field":"task.priority","op":"in","value":["high","urgent"]}],
		"actions":[
			{"type":"notify","input":{"title":"Urgent: {{task.title}}","link":"/tasks"}},
			{"type":"create_task","input":{"title":"Prepare for {{task.title}}","priority":"high","due_at":"tomorrow 09:00"}}
		]}`)

	if _, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: "Board meeting", Priority: "urgent"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: "Water plants", Priority: "low"}); err != nil {
		t.Fatal(err)
	}
	f.drain(t)

	if n := f.notifications(t, "Urgent: Board meeting"); n != 1 {
		t.Errorf("notifications: %d", n)
	}
	if n := f.notifications(t, "Urgent: Water plants"); n != 0 {
		t.Errorf("low priority should not match: %d", n)
	}
	if n := f.tasksTitled(t, "Prepare for Board meeting"); n != 1 {
		t.Errorf("follow-up tasks: %d", n)
	}
	var due time.Time
	_ = testDB.QueryRow(`SELECT due_at FROM tasks WHERE user_id = ? AND title = 'Prepare for Board meeting'`, f.user).Scan(&due)
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	if local := due.In(dhaka); local.Hour() != 9 || local.Format("2006-01-02") != f.clock.Now().In(dhaka).AddDate(0, 0, 1).Format("2006-01-02") {
		t.Errorf("due at %s, want tomorrow 09:00 Dhaka", due.In(dhaka))
	}
	got := f.reload(t, a.ID)
	if got.RunCount != 1 || got.LastStatus == nil || *got.LastStatus != models.RunSucceeded {
		t.Errorf("run record: %+v", got)
	}
	// Both actions are in the audit log, linked to the automation.
	if n := f.count(t, `SELECT COUNT(*) FROM ai_actions WHERE user_id = ? AND automation_id = ? AND source = 'automation' AND status = 'succeeded'`, a.ID); n != 2 {
		t.Errorf("audited actions: %d", n)
	}

	// Turned off: nothing happens.
	off := false
	if _, err := f.service.Update(f.ctx, f.user, a.ID, Input{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: "Second meeting", Priority: "high"}); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if n := f.notifications(t, "Urgent: Second meeting"); n != 0 {
		t.Error("disabled automation ran")
	}
}

func TestEventsOnlyQueuedWhenSomeoneListens(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	if _, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: "Nobody listens"}); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM scheduled_jobs WHERE user_id = ?`); n != 0 {
		t.Errorf("queued %d jobs with no automations", n)
	}
}

func TestApprovalSettingApplies(t *testing.T) {
	f := newFixture(t, models.ApprovalAlways)
	a := f.create(t, `{"name":"Log completions","trigger":{"type":"task.completed"},
		"actions":[{"type":"notify","input":{"title":"Done: {{task.title}}"}},{"type":"create_note","input":{"title":"Finished {{task.title}}"}}]}`)
	task, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: "File taxes"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.Update(f.ctx, f.user, task.ID, services.UpdateTaskInput{Status: services.Optional[string]{Set: true, Value: models.TaskCompleted}}); err != nil {
		t.Fatal(err)
	}
	f.drain(t)

	if n := f.notifications(t, "Done: File taxes"); n != 1 {
		t.Errorf("notify never needs approval: %d", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM notes WHERE user_id = ?`); n != 0 {
		t.Error("the note must wait for approval")
	}
	if n := f.count(t, `SELECT COUNT(*) FROM ai_actions WHERE user_id = ? AND automation_id = ? AND status = 'pending_approval'`, a.ID); n != 1 {
		t.Errorf("pending actions: %d", n)
	}
	if n := f.notifications(t, "“Log completions” is waiting for your approval"); n != 1 {
		t.Errorf("approval notification: %d", n)
	}
	if got := f.reload(t, a.ID); got.LastStatus == nil || *got.LastStatus != models.RunWaitingApproval {
		t.Errorf("status: %+v", got.LastStatus)
	}
}

func TestLoopsAreStopped(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	// Each copies every new task: A feeds B, B feeds A.
	f.create(t, `{"name":"A","trigger":{"type":"task.created"},"actions":[{"type":"create_task","input":{"title":"a {{task.title}}"}}]}`)
	f.create(t, `{"name":"B","trigger":{"type":"task.created"},"actions":[{"type":"create_task","input":{"title":"b {{task.title}}"}}]}`)
	if _, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: "seed"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		f.drain(t)
	}
	// The seed's event has depth 0 and each copy's event one more. Events up
	// to MaxDepth still run automations, and A never reacts to its own task,
	// so each of the MaxDepth+1 levels adds two tasks; then the chain stops.
	want := 1 + 2*(events.MaxDepth+1)
	if n := f.count(t, `SELECT COUNT(*) FROM tasks WHERE user_id = ?`); n != want {
		t.Errorf("got %d tasks, want %d", n, want)
	}
}

func TestSchedule(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	// Fix the clock at 06:00 Dhaka so the 07:30 run is today.
	start := time.Date(2026, 11, 2, 6, 0, 0, 0, dhaka).UTC()
	f.clock.Set(start)
	a := f.create(t, `{"name":"Morning","trigger":{"type":"schedule","schedule":{"frequency":"daily","time":"07:30"}},
		"actions":[{"type":"notify","input":{"title":"Plan the day ({{today}})"}}]}`)
	first := time.Date(2026, 11, 2, 7, 30, 0, 0, dhaka).UTC()
	if a.NextRunAt == nil || !a.NextRunAt.Equal(first) {
		t.Fatalf("next run %v, want %s", a.NextRunAt, first)
	}
	// Saving again doesn't queue a second run.
	name := "Morning plan"
	if _, err := f.service.Update(f.ctx, f.user, a.ID, Input{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM scheduled_jobs WHERE user_id = ? AND kind = 'automation.run' AND status = 'pending'`); n != 1 {
		t.Fatalf("pending runs: %d", n)
	}

	f.drain(t)
	if n := f.notifications(t, "Plan the day (2026-11-02)"); n != 0 {
		t.Fatal("ran early")
	}
	f.clock.Set(first.Add(time.Second))
	f.drain(t)
	if n := f.notifications(t, "Plan the day (2026-11-02)"); n != 1 {
		t.Fatalf("first run: %d", n)
	}
	got := f.reload(t, a.ID)
	if !got.NextRunAt.Equal(first.Add(24 * time.Hour)) {
		t.Errorf("next run %s", got.NextRunAt)
	}

	// Changing the time: the run queued for the old time does nothing.
	sched := models.Trigger{Type: models.TriggerSchedule, Schedule: &models.Schedule{Frequency: "daily", Time: "08:00"}}
	if _, err := f.service.Update(f.ctx, f.user, a.ID, Input{Trigger: &sched}); err != nil {
		t.Fatal(err)
	}
	f.clock.Set(first.Add(24*time.Hour + time.Second)) // 07:30 next day
	f.drain(t)
	if n := f.notifications(t, "Plan the day (2026-11-03)"); n != 0 {
		t.Fatal("stale run fired")
	}
	f.clock.Set(time.Date(2026, 11, 3, 8, 0, 1, 0, dhaka).UTC())
	f.drain(t)
	if n := f.notifications(t, "Plan the day (2026-11-03)"); n != 1 {
		t.Fatalf("run at the new time: %d", n)
	}

	// Much too late (worker down): skipped, and the schedule moves on.
	f.clock.Set(time.Date(2026, 11, 4, 14, 0, 0, 0, dhaka).UTC())
	f.drain(t)
	got = f.reload(t, a.ID)
	if n := f.notifications(t, "Plan the day (2026-11-04)"); n != 0 || got.LastStatus == nil || *got.LastStatus != models.RunSkipped {
		t.Errorf("late run: %d notifications, status %v", n, got.LastStatus)
	}
	if want := time.Date(2026, 11, 5, 8, 0, 0, 0, dhaka).UTC(); !got.NextRunAt.Equal(want) {
		t.Errorf("after a late run, next %s want %s", got.NextRunAt, want)
	}
}

func TestOverdueAndIdempotentRuns(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	a := f.create(t, `{"name":"Nag","trigger":{"type":"task.overdue"},"actions":[{"type":"notify","input":{"title":"Overdue: {{task.title}}"}}]}`)
	due := f.clock.Now().Add(-time.Minute).Format(time.RFC3339)
	if _, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: "Renew passport", DueAt: &due}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // two scans publish one event
		if err := f.engine.scanOverdue(f.ctx, scheduler.Job{}); err != nil {
			t.Fatal(err)
		}
		f.drain(t)
	}
	if n := f.notifications(t, "Overdue: Renew passport"); n != 1 {
		t.Errorf("overdue notifications: %d", n)
	}

	// A run job retried after a crash (same run key) resumes the same run
	// and doesn't repeat its actions.
	key := fmt.Sprintf("test-run-key-%d", time.Now().UnixNano())
	var ids []uint64
	for i := 0; i < 2; i++ {
		run, err := f.engine.start(f.ctx, f.reload(t, a.ID), trigger{source: "event", runKey: key, data: map[string]any{"task": map[string]any{"title": "Retry"}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, run.ID)
	}
	if n := f.notifications(t, "Overdue: Retry"); n != 1 || ids[0] != ids[1] {
		t.Errorf("crash-retried run: %d notifications, runs %v", n, ids)
	}
}

func TestRunNowAndRateLimit(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	manual := f.create(t, `{"name":"Ping","trigger":{"type":"manual"},"actions":[{"type":"notify","input":{"title":"Ping"}}]}`)
	res, err := f.engine.RunNow(f.ctx, f.user, manual.ID)
	if err != nil || res.Status != models.RunSucceeded || len(res.Steps) != 1 || res.Steps[0].Status != models.AIStatusSucceeded || res.TriggerSource != "manual" {
		t.Fatalf("run now: %+v %v", res, err)
	}
	event := f.create(t, `{"name":"On note","trigger":{"type":"note.created"},"actions":[{"type":"notify","input":{"title":"Saved {{note.title}}"}}]}`)
	if _, err := f.engine.RunNow(f.ctx, f.user, event.ID); !errors.Is(err, ErrCantRunNow) {
		t.Errorf("event automations can't run by hand: %v", err)
	}
	if _, err := f.engine.RunNow(f.ctx, f.user+1_000_000, manual.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("another user's automation: %v", err)
	}

	for i := 0; i < maxActionsPerHour; i++ {
		f.clock.Add(time.Millisecond) // distinct run keys
		if _, err := f.engine.RunNow(f.ctx, f.user, manual.ID); err != nil {
			t.Fatal(err)
		}
	}
	got := f.reload(t, manual.ID)
	if got.LastStatus == nil || *got.LastStatus != models.RunFailed || got.LastError == nil || !strings.Contains(*got.LastError, "Paused") {
		t.Errorf("rate limit: %+v %v", got.LastStatus, got.LastError)
	}
}

func TestNoteCreatedWithTags(t *testing.T) {
	f := newFixture(t, models.ApprovalHighImpactOnly)
	f.create(t, `{"name":"Bills","trigger":{"type":"note.created"},
		"conditions":[{"field":"note.tags","op":"contains","value":"bills"}],
		"actions":[{"type":"create_task","input":{"title":"Pay {{note.title}}","due_at":"+3d"}}]}`)
	for _, n := range []services.CreateNoteInput{{Title: "Electricity", Tags: []string{"Bills"}}, {Title: "Recipe", Tags: []string{"food"}}} {
		if _, err := f.notes.Create(f.ctx, f.user, n); err != nil {
			t.Fatal(err)
		}
	}
	f.drain(t)
	if f.tasksTitled(t, "Pay Electricity") != 1 || f.tasksTitled(t, "Pay Recipe") != 0 {
		t.Error("tag condition")
	}
}
