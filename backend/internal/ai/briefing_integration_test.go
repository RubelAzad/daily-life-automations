//go:build integration

package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
)

type failingClient struct{ FakeClient }

func (failingClient) Complete(context.Context, Request) (Response, error) {
	return Response{}, &APIError{Status: 529, Type: "overloaded_error", Message: "Overloaded"}
}

func brieferFixture(t *testing.T, client Client) (fixture, Briefer, *recordingClient) {
	t.Helper()
	f := newFixture(t)
	rec := &recordingClient{}
	if client == nil {
		client = rec
	}
	tasks := repository.TaskRepository{DB: testDB}
	events, reminders, notes := repository.CalendarEventRepository{DB: testDB}, repository.ReminderRepository{DB: testDB}, repository.NoteRepository{DB: testDB}
	calendar := services.CalendarService{Events: events, Reminders: reminders}
	deps := Deps{Tasks: f.tasks, Calendar: calendar, Today: services.TodayService{Tasks: tasks, Calendar: calendar, Reminders: reminders, Notes: notes}}
	svc := NewService(client, f.exec, repository.AIConversationRepository{DB: testDB}, repository.UserRepository{DB: testDB})
	return f, NewBriefer(svc, deps, repository.BriefingRepository{DB: testDB}, repository.NotificationRepository{DB: testDB}), rec
}

func (f fixture) addTask(t *testing.T, title string, due time.Time) uint64 {
	t.Helper()
	d := due.UTC().Format(time.RFC3339)
	task, err := f.tasks.Create(f.ctx, f.user, services.CreateTaskInput{Title: title, DueAt: &d})
	if err != nil {
		t.Fatal(err)
	}
	return task.ID
}

func TestMorningBriefing(t *testing.T) {
	f, b, rec := brieferFixture(t, nil)
	_, end := services.DayBounds(time.Now(), time.UTC)
	rent := f.addTask(t, "Pay rent", end.Add(-time.Minute))
	late := f.addTask(t, "Overdue report", time.Now().Add(-48*time.Hour))

	br, err := b.Generate(f.ctx, f.user, models.BriefingMorning, false)
	if err != nil {
		t.Fatal(err)
	}
	if br.Status != models.BriefingReady || br.Trigger != "manual" || deref(br.Model) != "fake" {
		t.Fatalf("got %+v", br)
	}
	var plan models.MorningPlan
	if err := json.Unmarshal(br.Content, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Focus) != 2 || plan.Focus[0].TaskID != late || plan.Focus[1].TaskID != rent || plan.Focus[1].Title != "Pay rent" {
		t.Errorf("focus (invented id dropped): %+v", plan.Focus)
	}
	for _, s := range plan.Schedule {
		if s.Start == "25:00" || (s.TaskID != nil && *s.TaskID == 999999999) {
			t.Errorf("invalid block kept: %+v", s)
		}
	}

	req := rec.last()
	if req.ToolChoice == nil || req.ToolChoice.Name != "submit_morning_plan" || len(req.Tools) != 1 {
		t.Error("briefings must force the structured-output tool")
	}
	text := req.Messages[0].Content[0].Text
	if !strings.Contains(text, "<day_data>") || !strings.Contains(text, `"title":"Pay rent"`) || strings.Contains(text, "user_id") {
		t.Errorf("prompt: %s", text)
	}
	if _, err := b.Generate(f.ctx, f.user, models.BriefingMorning, false); !errors.Is(err, ErrBriefingRateLimited) {
		t.Errorf("second manual briefing within a minute: %v", err)
	}
	today, err := b.Today(f.ctx, f.user)
	if err != nil || today.Morning == nil || today.Morning.ID != br.ID || today.Evening != nil {
		t.Errorf("today: %+v %v", today, err)
	}
	calls, _, _ := f.exec.Actions.List(f.ctx, f.user, repository.AIActionFilter{Kind: models.AIActionLLMCall, Limit: 5})
	if len(calls) != 1 || calls[0].Source != "briefing" {
		t.Errorf("model call audited: %+v", calls)
	}
}

func TestEveningBriefingAndEmptyDay(t *testing.T) {
	f, b, rec := brieferFixture(t, nil)
	br, err := b.Generate(f.ctx, f.user, models.BriefingEvening, false)
	if err != nil || br.Status != models.BriefingReady || br.Model != nil {
		t.Fatalf("empty day: %+v %v", br, err)
	}
	if len(rec.reqs) != 0 {
		t.Error("an empty day must not call the model")
	}

	_, end := services.DayBounds(time.Now(), time.UTC)
	id := f.addTask(t, "Write report", end.Add(-time.Minute))
	f.addTask(t, "Dentist", end.Add(10*time.Hour)) // tomorrow
	b.recent = &briefingLimiter{last: map[string]time.Time{}}
	br, err = b.Generate(f.ctx, f.user, models.BriefingEvening, false)
	if err != nil {
		t.Fatal(err)
	}
	var s models.EveningSummary
	_ = json.Unmarshal(br.Content, &s)
	if len(s.Unfinished) != 1 || s.Unfinished[0].TaskID != id || s.Unfinished[0].Suggestion != "tomorrow" {
		t.Errorf("unfinished: %+v", s.Unfinished)
	}
	if text := rec.last().Messages[0].Content[0].Text; !strings.Contains(text, `"tomorrow_tasks":[{`) || !strings.Contains(text, "Dentist") {
		t.Errorf("evening prompt should include tomorrow: %s", text)
	}
}

func TestScheduledBriefings(t *testing.T) {
	f, b, rec := brieferFixture(t, nil)
	f.addTask(t, "Pay rent", time.Now().Add(time.Hour))
	now := time.Now().UTC()
	s := models.DefaultSettings(f.user)
	s.MorningBriefingEnabled, s.MorningBriefingTime = true, now.Add(-time.Hour).Format("15:04")
	s.EveningSummaryEnabled, s.EveningSummaryTime = true, now.Add(-5*time.Hour).Format("15:04") // outside the window
	if err := f.settings.Save(f.ctx, s); err != nil {
		t.Fatal(err)
	}
	if now.Hour() < 1 || now.Hour() >= 23 {
		t.Skip("too close to midnight UTC for this test")
	}

	if _, err := b.RunScheduled(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunScheduled(f.ctx); err != nil { // a second worker pass makes nothing new
		t.Fatal(err)
	}
	list, _, _ := b.Briefings.List(f.ctx, f.user, 10, 0)
	if len(list) != 1 || list[0].Kind != models.BriefingMorning || list[0].Trigger != "scheduled" {
		t.Fatalf("scheduled briefings: %+v", list)
	}
	// RunScheduled serves every user with a briefing due, including other
	// accounts in the shared database, so count this user's calls only.
	var calls int
	_ = testDB.QueryRow(`SELECT COUNT(*) FROM ai_actions WHERE user_id = ? AND kind = 'llm_call'`, f.user).Scan(&calls)
	if calls != 1 || len(rec.reqs) < 1 {
		t.Errorf("model calls for this user: %d", calls)
	}
	var n int
	_ = testDB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND kind = 'briefing' AND idempotency_key = ?`,
		f.user, fmt.Sprintf("briefing:%d", list[0].ID)).Scan(&n)
	if n != 1 {
		t.Errorf("notification: %d", n)
	}
	if _, err := b.Generate(f.ctx, f.user, models.BriefingMorning, true); !errors.Is(err, ErrAlreadyGenerated) {
		t.Errorf("scheduled twice: %v", err)
	}
}

func TestBriefingFailureIsStored(t *testing.T) {
	f, b, _ := brieferFixture(t, failingClient{})
	f.addTask(t, "Pay rent", time.Now().Add(time.Hour))
	br, err := b.Generate(f.ctx, f.user, models.BriefingMorning, false)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || br.Status != models.BriefingFailed || !strings.Contains(deref(br.Error), "overloaded") {
		t.Errorf("got %+v %v", br, err)
	}
	failed, _, _ := f.exec.Actions.List(f.ctx, f.user, repository.AIActionFilter{Statuses: []string{models.AIStatusFailed}, Limit: 5})
	if len(failed) != 1 {
		t.Errorf("failed call audited: %d", len(failed))
	}
	other := newFixture(t)
	if _, err := b.Briefings.Get(other.ctx, other.user, br.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("other user: %v", err)
	}
}
