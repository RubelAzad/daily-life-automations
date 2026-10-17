//go:build integration

// Runs the executor against the real database from backend/.env:
//
//	go test -tags integration ./internal/ai/
//
// It creates throwaway users (e2e-ai-*@example.com) and deletes them again.
package ai

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

	"lifeos-ai/backend/internal/config"
	"lifeos-ai/backend/internal/database"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
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

type fixture struct {
	ctx      context.Context
	exec     Executor
	tasks    services.TaskService
	settings repository.SettingsRepository
	user     uint64
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	user, err := repository.UserRepository{DB: testDB}.Create(ctx, "AI Test", fmt.Sprintf("e2e-ai-%d@example.com", time.Now().UnixNano()), "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testDB.Exec(`DELETE FROM users WHERE id = ?`, user.ID) })

	tasks := services.TaskService{Tasks: repository.TaskRepository{DB: testDB}, Lists: repository.TaskListRepository{DB: testDB}, Projects: repository.ProjectRepository{DB: testDB}}
	events, reminders, notes := repository.CalendarEventRepository{DB: testDB}, repository.ReminderRepository{DB: testDB}, repository.NoteRepository{DB: testDB}
	calendar := services.CalendarService{Events: events, Reminders: reminders}
	deps := Deps{
		Tasks:     tasks,
		Calendar:  calendar,
		Notes:     services.NoteService{Notes: notes, Files: repository.FileRepository{DB: testDB}, Projects: repository.ProjectRepository{DB: testDB}, Reminders: reminders},
		Reminders: services.ReminderService{Reminders: reminders, Tasks: repository.TaskRepository{DB: testDB}, Events: events, Notes: notes},
		Today:     services.TodayService{Tasks: repository.TaskRepository{DB: testDB}, Calendar: calendar, Reminders: reminders, Notes: notes},
	}
	settings := repository.SettingsRepository{DB: testDB}
	return fixture{
		ctx:      ctx,
		exec:     Executor{Tools: BuiltinTools(deps), Actions: repository.AIActionRepository{DB: testDB}, Settings: settings},
		tasks:    tasks,
		settings: settings,
		user:     user.ID,
	}
}

func (f fixture) setMode(t *testing.T, mode string) {
	t.Helper()
	s := models.DefaultSettings(f.user)
	s.AIApprovalMode = mode
	if err := f.settings.Save(f.ctx, s); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) propose(t *testing.T, tool, input string) Result {
	t.Helper()
	r, err := f.exec.Propose(f.ctx, f.user, "test", tool, json.RawMessage(input))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (f fixture) taskCount(t *testing.T) int {
	t.Helper()
	_, meta, err := f.tasks.List(f.ctx, f.user, services.ListTasksInput{})
	if err != nil {
		t.Fatal(err)
	}
	return meta.Total
}

func TestReadToolsRunWithoutApproval(t *testing.T) {
	f := newFixture(t) // default mode: always ask
	r := f.propose(t, "get_today", `{}`)
	if r.Status != models.AIStatusSucceeded || r.Output == nil {
		t.Fatalf("got %+v", r)
	}
	a, _ := f.exec.Actions.Get(f.ctx, f.user, r.ActionID)
	if a.Status != models.AIStatusSucceeded || deref(a.Risk) != "read" || a.DurationMS == nil || a.Output == nil {
		t.Errorf("audit entry: %+v", a)
	}
}

func TestApproveFlow(t *testing.T) {
	f := newFixture(t)
	r := f.propose(t, "create_task", `{"title":"Buy milk","priority":"high"}`)
	if r.Status != models.AIStatusPendingApproval || r.Summary != "Create task “Buy milk”" {
		t.Fatalf("got %+v", r)
	}
	if n := f.taskCount(t); n != 0 {
		t.Fatalf("nothing may be created before approval, got %d tasks", n)
	}

	a, err := f.exec.Approve(f.ctx, f.user, r.ActionID)
	if err != nil || a.Status != models.AIStatusSucceeded || a.DecidedAt == nil || a.ExecutedAt == nil || a.PendingInput != nil {
		t.Fatalf("approve: %+v %v", a, err)
	}
	if !strings.Contains(string(a.Output), `"title":"Buy milk"`) {
		t.Errorf("output: %s", a.Output)
	}
	if n := f.taskCount(t); n != 1 {
		t.Errorf("want 1 task, got %d", n)
	}
	if _, err := f.exec.Approve(f.ctx, f.user, r.ActionID); !errors.Is(err, ErrNotPending) {
		t.Errorf("second approve: %v", err)
	}
	if n := f.taskCount(t); n != 1 {
		t.Errorf("approving twice must not run twice, got %d tasks", n)
	}
}

func TestHighImpactOnlyMode(t *testing.T) {
	f := newFixture(t)
	f.setMode(t, models.ApprovalHighImpactOnly)
	r := f.propose(t, "create_task", `{"title":"Call the bank"}`)
	if r.Status != models.AIStatusSucceeded {
		t.Fatalf("routine change should run, got %+v", r)
	}
	id := r.Output.(taskOut).ID

	del := f.propose(t, "delete_task", fmt.Sprintf(`{"task_id":%d}`, id))
	if del.Status != models.AIStatusPendingApproval || del.Summary != "Delete task “Call the bank” permanently" {
		t.Fatalf("deleting must wait for approval: %+v", del)
	}
	a, err := f.exec.Reject(f.ctx, f.user, del.ActionID)
	if err != nil || a.Status != models.AIStatusRejected || a.PendingInput != nil {
		t.Fatalf("reject: %+v %v", a, err)
	}
	if n := f.taskCount(t); n != 1 {
		t.Error("rejected delete must not run")
	}
	if _, err := f.exec.Approve(f.ctx, f.user, del.ActionID); !errors.Is(err, ErrNotPending) {
		t.Errorf("approve after reject: %v", err)
	}
}

func TestBadCallsAreAuditedAndHarmless(t *testing.T) {
	f := newFixture(t)
	f.setMode(t, models.ApprovalHighImpactOnly)
	cases := []struct {
		tool, input, status, err string
	}{
		{"run_sql", `{"query":"DROP TABLE users"}`, models.AIStatusRejected, "unknown tool"},
		{"create_task", `{"title":"x","user_id":2}`, models.AIStatusFailed, `unknown field "user_id"`},
		{"create_task", `{"title":42}`, models.AIStatusFailed, "title must be string"},
		{"create_task", `{"title":"  "}`, models.AIStatusFailed, "invalid input: title is required"},
		{"complete_task", `{"task_id":999999999}`, models.AIStatusFailed, "not found"},
		{"list_tasks", `{"limit":500}`, models.AIStatusFailed, "limit must be between 1 and 50"},
	}
	for _, c := range cases {
		r := f.propose(t, c.tool, c.input)
		if r.Status != c.status || !strings.Contains(r.Error, c.err) {
			t.Errorf("%s %s: got %s %q", c.tool, c.input, r.Status, r.Error)
		}
	}
	if n := f.taskCount(t); n != 0 {
		t.Errorf("bad calls created %d tasks", n)
	}
	_, total, _ := f.exec.Actions.List(f.ctx, f.user, repository.AIActionFilter{Limit: 50})
	if total != len(cases) {
		t.Errorf("every attempt must be audited: %d of %d", total, len(cases))
	}
}

func TestOtherUsersCannotDecide(t *testing.T) {
	a, b := newFixture(t), newFixture(t)
	r := a.propose(t, "create_task", `{"title":"Private"}`)
	if _, err := b.exec.Approve(b.ctx, b.user, r.ActionID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("approve: %v", err)
	}
	if _, err := b.exec.Reject(b.ctx, b.user, r.ActionID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("reject: %v", err)
	}
	if got, _ := a.exec.Actions.Get(a.ctx, a.user, r.ActionID); got.Status != models.AIStatusPendingApproval {
		t.Errorf("status changed to %s", got.Status)
	}
}

func TestApprovalExpires(t *testing.T) {
	f := newFixture(t)
	r := f.propose(t, "create_task", `{"title":"Old idea"}`)
	if _, err := testDB.Exec(`UPDATE ai_actions SET created_at = NOW() - INTERVAL 25 HOUR WHERE id = ?`, r.ActionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.exec.Approve(f.ctx, f.user, r.ActionID); !errors.Is(err, ErrApprovalExpired) {
		t.Fatalf("got %v", err)
	}
	a, _ := f.exec.Actions.Get(f.ctx, f.user, r.ActionID)
	if a.Status != models.AIStatusExpired || a.PendingInput != nil {
		t.Errorf("got %+v", a)
	}
	if n := f.taskCount(t); n != 0 {
		t.Error("expired action ran")
	}
}

func TestAuditLogIsRedacted(t *testing.T) {
	f := newFixture(t)
	r := f.propose(t, "create_note", `{"title":"Card","body":"Visa 4111 1111 1111 1111, PIN in safe"}`)
	a, _ := f.exec.Actions.Get(f.ctx, f.user, r.ActionID)
	if strings.Contains(string(a.Input), "4111") || !strings.Contains(string(a.Input), "card number removed") {
		t.Errorf("audit input not redacted: %s", a.Input)
	}
	// The approved action still gets the exact input.
	a, err := f.exec.Approve(f.ctx, f.user, r.ActionID)
	if err != nil || a.Status != models.AIStatusSucceeded {
		t.Fatalf("approve: %+v %v", a, err)
	}
	var body string
	if err := testDB.QueryRow(`SELECT body FROM notes WHERE user_id = ?`, f.user).Scan(&body); err != nil || !strings.Contains(body, "4111 1111 1111 1111") {
		t.Errorf("note body: %q %v", body, err)
	}
	if strings.Contains(string(a.Output), "4111") {
		t.Errorf("audit output not redacted: %s", a.Output)
	}
}
