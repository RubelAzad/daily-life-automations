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
	// Companions are added with it (if not added yet): they complete it.
	Companions []string `json:"companions,omitempty"`
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
	{
		Key:  "bill_reminders",
		Name: "Bill reminders",
		Description: "A few days before each recurring bill is due (you choose how many, per bill), you get a notification with the expected amount " +
			"and a “Pay …” task due that day. Adds “Record paid bills” too, so completing the task records the payment.",
		Approval:   "The notification never needs approval; adding the task follows your approval setting. LifeOS never pays anything itself.",
		Companions: []string{"bill_payments"},
		Automation: json.RawMessage(`{"name":"Bill reminders","trigger":{"type":"bill.due_soon"},"actions":[` +
			`{"type":"notify","input":{"title":"{{bill.name}} is due {{bill.due}}","body":"Expected amount: {{bill.amount}}.","link":"/finance"}},` +
			`{"type":"create_bill_task","input":{"bill_id":"{{bill.id}}"}}]}`),
	},
	{
		Key:  "bill_payments",
		Name: "Record paid bills",
		Description: "When you complete a bill's “Pay …” task, the payment is recorded as an expense (the bill's amount, today) " +
			"and the bill moves to its next due date.",
		Approval: "Recording the expense follows your approval setting.",
		Automation: json.RawMessage(`{"name":"Record paid bills","trigger":{"type":"task.completed"},` +
			`"conditions":[{"field":"task.bill","op":"is_set"}],` +
			`"actions":[{"type":"pay_bill","input":{"bill_id":"{{bill.id}}","task_id":"{{task.id}}"}}]}`),
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

// AddTemplate creates the user's automation from a built-in workflow, and
// its companions not added yet. Errors: repository.ErrNotFound (no such
// template), ErrTemplateAdded, validation errors.
func (s Service) AddTemplate(ctx context.Context, userID uint64, key string) (models.Automation, error) {
	t, ok := template(key)
	if !ok {
		return models.Automation{}, repository.ErrNotFound
	}
	a, err := s.addTemplate(ctx, userID, t)
	if err != nil {
		return a, err
	}
	for _, k := range t.Companions {
		c, ok := template(k)
		if !ok {
			continue
		}
		if _, err := s.addTemplate(ctx, userID, c); err != nil && !errors.Is(err, ErrTemplateAdded) {
			return a, err
		}
	}
	return a, nil
}

func (s Service) addTemplate(ctx context.Context, userID uint64, t Template) (models.Automation, error) {
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

func template(key string) (Template, bool) {
	for _, t := range Templates {
		if t.Key == key {
			return t, true
		}
	}
	return Template{}, false
}
