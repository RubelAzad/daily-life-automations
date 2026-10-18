package workflow

import (
	"context"
	"encoding/json"
	"errors"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

// ErrTemplateAdded: each built-in workflow can be added once.
var ErrTemplateAdded = errors.New("this workflow is already in your automations")

// Template is a built-in workflow: a ready-made automation the user adds
// with one click. Once added it is an ordinary automation they can edit,
// turn off or delete (and then add again).
type Template struct {
	Key         string          `json:"key"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Approval    string          `json:"approval"` // what needs the user's OK
	NeedsAI     bool            `json:"needs_ai"`
	Automation  json.RawMessage `json:"automation"` // the automation it creates (Input)
}

// Templates are the built-in workflows, in display order.
var Templates = []Template{
	{
		Key:  "daily_planning",
		Name: "Plan my day",
		Description: "Every morning at 07:00, the assistant looks at today's tasks, events and reminders, " +
			"picks what to focus on, suggests a schedule around your events and sends it to you. " +
			"If today's plan already exists (e.g. from the morning briefing in Settings), it is reused.",
		Approval: "None: it only reads your day and sends a notification.",
		NeedsAI:  true,
		Automation: json.RawMessage(`{"name":"Plan my day","trigger":{"type":"schedule","schedule":{"frequency":"daily","time":"07:00"}},` +
			`"actions":[{"type":"plan_day","input":{}}]}`),
	},
	{
		Key:  "missed_task_recovery",
		Name: "Recover missed tasks",
		Description: "When a task goes overdue, LifeOS finds the next free time for it, around your events and other tasks " +
			"and outside your quiet hours (low-priority tasks from tomorrow), moves it there and updates today's plan.",
		Approval: "Moving a task follows your approval setting; by default you approve each move in Approvals.",
		Automation: json.RawMessage(`{"name":"Recover missed tasks","trigger":{"type":"task.overdue"},` +
			`"actions":[{"type":"recover_task","input":{"task_id":"{{task.id}}"}}]}`),
	},
}

// TemplateView is a template with the user's automation made from it.
type TemplateView struct {
	Template
	AutomationID *uint64 `json:"automation_id"` // set once added
}

// ListTemplates returns the built-in workflows and which the user added.
func (s Service) ListTemplates(ctx context.Context, userID uint64) ([]TemplateView, error) {
	list, err := repository.AutomationRepository{DB: s.DB}.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	added := map[string]uint64{}
	for _, a := range list {
		if a.TemplateKey != nil {
			added[*a.TemplateKey] = a.ID
		}
	}
	out := make([]TemplateView, len(Templates))
	for i, t := range Templates {
		out[i] = TemplateView{Template: t}
		if id, ok := added[t.Key]; ok {
			out[i].AutomationID = &id
		}
	}
	return out, nil
}

// AddTemplate creates the user's automation from a built-in workflow.
// Errors: repository.ErrNotFound (no such template), ErrTemplateAdded,
// validation errors.
func (s Service) AddTemplate(ctx context.Context, userID uint64, key string) (models.Automation, error) {
	for _, t := range Templates {
		if t.Key != key {
			continue
		}
		var in Input
		if err := json.Unmarshal(t.Automation, &in); err != nil {
			return models.Automation{}, err
		}
		a, err := s.create(ctx, userID, in, &t.Key)
		if errors.Is(err, repository.ErrDuplicate) {
			return a, ErrTemplateAdded
		}
		return a, err
	}
	return models.Automation{}, repository.ErrNotFound
}
