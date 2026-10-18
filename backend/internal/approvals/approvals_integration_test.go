//go:build integration

// Runs the approvals inbox against the real database from backend/.env:
//
//	go test -tags integration ./internal/approvals/
//
// It creates throwaway users (e2e-appr-*@example.com) and deletes them
// again. Stop any running worker first: it shares the database and would
// pick up the tests' jobs.
package approvals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
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
	"lifeos-ai/backend/internal/workflow"
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

type fixture struct {
	ctx         context.Context
	user        uint64
	tasks       services.TaskService
	automations workflow.Service
	engine      workflow.Engine
	sched       *scheduler.Scheduler
	svc         Service
}

func newFixture(t *testing.T, approvalMode string) fixture {
	t.Helper()
	ctx := context.Background()
	user, err := repository.UserRepository{DB: testDB}.Create(ctx, "Approvals Test", fmt.Sprintf("e2e-appr-%d@example.com", time.Now().UnixNano()), "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testDB.Exec(`DELETE FROM scheduled_jobs WHERE user_id = ?`, user.ID)
		_, _ = testDB.Exec(`DELETE FROM users WHERE id = ?`, user.ID)
	})
	settings := repository.SettingsRepository{DB: testDB}
	s := models.DefaultSettings(user.ID)
	s.AIApprovalMode = approvalMode
	if err := settings.Save(ctx, s); err != nil {
		t.Fatal(err)
	}

	bus := events.Bus{}
	taskRepo, reminders, notesRepo := repository.TaskRepository{DB: testDB}, repository.ReminderRepository{DB: testDB}, repository.NoteRepository{DB: testDB}
	deps := ai.Deps{
		Tasks: services.TaskService{Tasks: taskRepo, Lists: repository.TaskListRepository{DB: testDB}, Projects: repository.ProjectRepository{DB: testDB}, Events: bus},
		Notes: services.NoteService{Notes: notesRepo, Files: repository.FileRepository{DB: testDB}, Projects: repository.ProjectRepository{DB: testDB}, Reminders: reminders, Events: bus},
		Reminders: services.ReminderService{
			Reminders: reminders, Tasks: taskRepo, Events: repository.CalendarEventRepository{DB: testDB}, Notes: notesRepo,
		},
		Notifications: repository.NotificationRepository{DB: testDB},
	}
	actions := repository.AIActionRepository{DB: testDB}
	tools := ai.AutomationTools(deps)
	engine := workflow.Engine{
		DB: testDB, Settings: settings, Events: bus,
		Executor: ai.Executor{Tools: tools, Actions: actions, Settings: settings},
	}
	sched := &scheduler.Scheduler{DB: testDB, WorkerID: "approvals-test"}
	engine.Register(sched, time.Hour)
	return fixture{
		ctx: ctx, user: user.ID, tasks: deps.Tasks, engine: engine, sched: sched,
		automations: workflow.Service{DB: testDB, Settings: settings, Tools: tools},
		svc:         Service{DB: testDB, Executor: ai.Executor{Tools: ai.BuiltinTools(deps), Actions: actions, Settings: settings}, Engine: engine},
	}
}

func (f fixture) automation(t *testing.T, raw string) models.Automation {
	t.Helper()
	var in workflow.Input
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	a, err := f.automations.Create(f.ctx, f.user, in)
	if err != nil {
		var ve *services.ValidationError
		if errors.As(err, &ve) {
			t.Fatalf("create: %v", ve.Fields)
		}
		t.Fatal(err)
	}
	return a
}

func (f fixture) runNow(t *testing.T, a models.Automation) models.WorkflowRun {
	t.Helper()
	run, err := f.engine.RunNow(f.ctx, f.user, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func (f fixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := testDB.QueryRow(q, append([]any{f.user}, args...)...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f fixture) lastStatus(t *testing.T, id uint64) string {
	t.Helper()
	a, err := f.automations.Get(f.ctx, f.user, id)
	if err != nil {
		t.Fatal(err)
	}
	return *a.LastStatus
}

// notes is a manual automation named Notes that creates a note per title,
// then runs the extra actions.
func notes(titles []string, extra ...string) string {
	var actions []string
	for _, title := range titles {
		actions = append(actions, fmt.Sprintf(`{"type":"create_note","input":{"title":%q}}`, title))
	}
	actions = append(actions, extra...)
	return `{"name":"Notes","trigger":{"type":"manual"},"actions":[` + strings.Join(actions, ",") + `]}`
}

func TestInboxAndDecisions(t *testing.T) {
	f := newFixture(t, models.ApprovalAlways)
	a := f.automation(t, notes([]string{"Keep", "Drop"}, `{"type":"notify","input":{"title":"Asked"}}`))
	run := f.runNow(t, a)
	if run.Status != models.RunWaitingApproval || run.Steps[2].Status != models.AIStatusSucceeded {
		t.Fatalf("run: %+v", run)
	}

	// A request from a chat as well.
	chat, err := repository.AIConversationRepository{DB: testDB}.Create(f.ctx, f.user, "Planning chat")
	if err != nil {
		t.Fatal(err)
	}
	fromChat, err := f.svc.Executor.Propose(f.ctx, f.user, ai.Origin{Source: "chat", ConversationID: &chat.ID}, "create_task", []byte(`{"title":"From chat"}`))
	if err != nil || fromChat.Status != models.AIStatusPendingApproval {
		t.Fatalf("chat proposal: %+v %v", fromChat, err)
	}

	list, meta, err := f.svc.Pending(f.ctx, f.user, PageInput{})
	if err != nil || meta.Total != 3 || len(list) != 3 {
		t.Fatalf("pending: %d %v", meta.Total, err)
	}
	first := list[0]
	if first.ID != *run.Steps[0].ActionID || first.Automation == nil || first.Automation.Name != "Notes" ||
		first.Run == nil || first.Run.ID != run.ID || first.WorkflowRunID == nil || first.ExpiresAt == nil {
		t.Errorf("oldest first, with its automation and run: %+v", first)
	}
	if last := list[2]; last.ID != fromChat.ActionID || last.Conversation == nil || last.Conversation.Name != "Planning chat" || last.Automation != nil {
		t.Errorf("chat request: %+v", last)
	}
	if n, _ := f.svc.Count(f.ctx, f.user); n != 3 {
		t.Errorf("count = %d", n)
	}

	// Approve one: the run keeps waiting for the other.
	got, err := f.svc.Decide(f.ctx, f.user, *run.Steps[0].ActionID, true)
	if err != nil || got.Status != models.AIStatusSucceeded {
		t.Fatalf("approve: %+v %v", got, err)
	}
	if got.Run == nil || got.Run.Status != models.RunWaitingApproval {
		t.Errorf("run still waiting: %+v", got.Run)
	}
	if f.count(t, `SELECT COUNT(*) FROM notes WHERE user_id = ? AND title = 'Keep'`) != 1 {
		t.Error("approved note not created")
	}

	// Reject the rest at once; duplicates are skipped, unknown IDs reported.
	results, err := f.svc.DecideMany(f.ctx, f.user, []uint64{*run.Steps[1].ActionID, fromChat.ActionID, *run.Steps[1].ActionID, 99999999999}, false)
	if err != nil || len(results) != 3 || !results[0].OK || !results[1].OK || results[2].OK || results[2].Error != "not found" {
		t.Fatalf("bulk reject: %+v %v", results, err)
	}
	if results[0].Action.Run == nil || results[0].Action.Run.Status != models.RunFailed {
		t.Errorf("run failed once declined: %+v", results[0].Action.Run)
	}
	stored, err := (repository.WorkflowRunRepository{DB: testDB}).Get(f.ctx, f.user, run.ID)
	if err != nil || stored.Status != models.RunFailed || stored.Error == nil || !strings.Contains(*stored.Error, "you declined it") {
		t.Errorf("stored run: %+v %v", stored, err)
	}
	if s := f.lastStatus(t, a.ID); s != models.RunFailed {
		t.Errorf("automation last status = %s", s)
	}
	if f.count(t, `SELECT COUNT(*) FROM tasks WHERE user_id = ? AND title = 'From chat'`) != 0 {
		t.Error("rejected task was created")
	}

	if n, _ := f.svc.Count(f.ctx, f.user); n != 0 {
		t.Errorf("count after deciding = %d", n)
	}
	hist, meta, err := f.svc.History(f.ctx, f.user, PageInput{})
	if err != nil || meta.Total != 3 || hist[0].Status != models.AIStatusRejected || hist[2].Status != models.AIStatusSucceeded {
		t.Errorf("history, latest decision first: %d %v", meta.Total, err)
	}

	if _, err := f.svc.Decide(f.ctx, f.user, fromChat.ActionID, true); !errors.Is(err, ai.ErrNotPending) {
		t.Errorf("deciding twice: %v", err)
	}
	if _, err := f.svc.Decide(f.ctx, f.user+1_000_000, *run.Steps[0].ActionID, true); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("another user's action: %v", err)
	}
	for _, ids := range [][]uint64{nil, make([]uint64, MaxBatch+1)} {
		var ve *services.ValidationError
		if _, err := f.svc.DecideMany(f.ctx, f.user, ids, true); !errors.As(err, &ve) {
			t.Errorf("%d ids: %v", len(ids), err)
		}
	}
}

func TestApprovingEverythingFinishesTheRun(t *testing.T) {
	f := newFixture(t, models.ApprovalAlways)
	a := f.automation(t, notes([]string{"One", "Two"}))
	run := f.runNow(t, a)
	results, err := f.svc.DecideMany(f.ctx, f.user, []uint64{*run.Steps[0].ActionID, *run.Steps[1].ActionID}, true)
	if err != nil || !results[0].OK || !results[1].OK {
		t.Fatalf("%+v %v", results, err)
	}
	stored, _ := (repository.WorkflowRunRepository{DB: testDB}).Get(f.ctx, f.user, run.ID)
	if stored.Status != models.RunSucceeded || stored.Steps[0].Status != models.AIStatusSucceeded || stored.Steps[1].Status != models.AIStatusSucceeded {
		t.Errorf("run: %+v", stored)
	}
	if s := f.lastStatus(t, a.ID); s != models.RunSucceeded {
		t.Errorf("automation last status = %s", s)
	}

	// A newer run is what the automation shows: deciding on an older run's
	// request doesn't change it.
	older := f.runNow(t, a)
	newer := f.runNow(t, a)
	if _, err := f.svc.DecideMany(f.ctx, f.user, []uint64{*older.Steps[0].ActionID, *older.Steps[1].ActionID}, true); err != nil {
		t.Fatal(err)
	}
	if s := f.lastStatus(t, a.ID); s != models.RunWaitingApproval {
		t.Errorf("after deciding the older run, last status = %s (newer run %d still waits)", s, newer.ID)
	}
}

// An approved action that an automation proposed acts as the automation:
// its changes don't trigger that automation again.
func TestApprovedActionDoesNotRetriggerItsAutomation(t *testing.T) {
	f := newFixture(t, models.ApprovalAlways)
	a := f.automation(t, `{"name":"Follow up","trigger":{"type":"task.created"},"actions":[{"type":"create_task","input":{"title":"Follow up: {{task.title}}"}}]}`)
	if _, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: "Call the bank"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sched.Drain(f.ctx); err != nil {
		t.Fatal(err)
	}
	list, _, err := f.svc.Pending(f.ctx, f.user, PageInput{})
	if err != nil || len(list) != 1 {
		t.Fatalf("pending: %d %v", len(list), err)
	}
	if _, err := f.svc.Decide(f.ctx, f.user, list[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sched.Drain(f.ctx); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM tasks WHERE user_id = ? AND title = 'Follow up: Call the bank'`); n != 1 {
		t.Errorf("follow-up tasks = %d", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM workflow_runs WHERE user_id = ? AND automation_id = ?`, a.ID); n != 1 {
		t.Errorf("runs = %d, want 1 (not re-triggered by its own task)", n)
	}
	if n, _ := f.svc.Count(f.ctx, f.user); n != 0 {
		t.Errorf("pending = %d", n)
	}
}

func TestExpiry(t *testing.T) {
	f := newFixture(t, models.ApprovalAlways)
	a := f.automation(t, notes([]string{"Late"}))
	swept := f.runNow(t, a)
	tried := f.runNow(t, a)
	// Age the requests rather than moving the clock, so the sweep leaves
	// other users' requests alone.
	if _, err := testDB.Exec(`UPDATE ai_actions SET created_at = created_at - INTERVAL 25 HOUR WHERE user_id = ?`, f.user); err != nil {
		t.Fatal(err)
	}
	if list, meta, _ := f.svc.Pending(f.ctx, f.user, PageInput{}); len(list) != 0 || meta.Total != 0 {
		t.Errorf("expired requests listed: %d", meta.Total)
	}
	if n, _ := f.svc.Count(f.ctx, f.user); n != 0 {
		t.Errorf("count = %d", n)
	}

	// Trying to approve one that expired reports it and fails its run.
	if _, err := f.svc.Decide(f.ctx, f.user, *tried.Steps[0].ActionID, true); !errors.Is(err, ai.ErrApprovalExpired) {
		t.Errorf("approving an expired request: %v", err)
	}
	stored, _ := (repository.WorkflowRunRepository{DB: testDB}).Get(f.ctx, f.user, tried.ID)
	if stored.Status != models.RunFailed {
		t.Errorf("run after a late approval: %+v", stored)
	}

	// The sweep expires the other one and tells the user about its run.
	n, err := f.svc.Expire(f.ctx)
	if err != nil || n < 1 {
		t.Fatalf("expire: %d %v", n, err)
	}
	stored, _ = (repository.WorkflowRunRepository{DB: testDB}).Get(f.ctx, f.user, swept.ID)
	if stored.Status != models.RunFailed || stored.Error == nil || !strings.Contains(*stored.Error, "expired") {
		t.Errorf("swept run: %+v", stored)
	}
	if f.count(t, `SELECT COUNT(*) FROM ai_actions WHERE user_id = ? AND status = 'expired'`) != 2 {
		t.Error("both requests should be expired")
	}
	title := "“Notes” didn't finish: an approval expired"
	link := fmt.Sprintf("/automations?run=%d", swept.ID)
	if f.count(t, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND title = ? AND link = ?`, title, link) != 1 {
		t.Error("no notification for the swept run")
	}
	if _, err := f.svc.Expire(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND title = ?`, title) != 1 {
		t.Error("the sweep notified twice")
	}
}
