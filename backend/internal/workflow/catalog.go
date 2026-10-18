// Package workflow is the automation engine: automations (trigger →
// conditions → actions), their schedules and the worker jobs that run them.
// Actions are tool calls through ai.Executor, so automations get the same
// allowlist, approval policy and audit log as the assistant.
package workflow

import "lifeos-ai/backend/internal/models"

// Field is something a condition can test.
type Field struct {
	Name    string   `json:"name"` // e.g. task.priority
	Label   string   `json:"label"`
	Type    string   `json:"type"` // text | choice | tags | date
	Choices []string `json:"choices,omitempty"`
}

// TriggerSpec describes a trigger type for clients.
type TriggerSpec struct {
	Type         string   `json:"type"`
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	Fields       []Field  `json:"fields"`       // what conditions can test
	Placeholders []string `json:"placeholders"` // usable as {{name}} in actions
	RunNow       bool     `json:"run_now"`      // can be run by hand
}

// ActionSpec describes an action type for clients.
type ActionSpec struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Operators per field type. Text comparisons ignore case.
var opsByType = map[string][]string{
	"text":   {"equals", "not_equals", "contains", "not_contains", "is_set", "is_not_set"},
	"choice": {"equals", "not_equals", "in", "not_in"},
	"tags":   {"contains", "not_contains", "is_set", "is_not_set"},
	"date":   {"is_set", "is_not_set"},
}

var (
	taskFields = []Field{
		{Name: "task.title", Label: "Title", Type: "text"},
		{Name: "task.description", Label: "Description", Type: "text"},
		{Name: "task.priority", Label: "Priority", Type: "choice", Choices: models.TaskPriorities},
		{Name: "task.list", Label: "List", Type: "text"},
		{Name: "task.project", Label: "Project", Type: "text"},
		{Name: "task.due_at", Label: "Due date", Type: "date"},
	}
	noteFields = []Field{
		{Name: "note.title", Label: "Title", Type: "text"},
		{Name: "note.kind", Label: "Kind", Type: "choice", Choices: models.NoteKinds},
		{Name: "note.tags", Label: "Tags", Type: "tags"},
		{Name: "note.url", Label: "Link", Type: "text"},
		{Name: "note.expires_on", Label: "Expiry date", Type: "date"},
	}
	runFields = []Field{
		{Name: "run.automation", Label: "Automation name", Type: "text"},
		{Name: "run.error", Label: "Error", Type: "text"},
	}
	common           = []string{"automation.name", "today", "now"}
	taskPlaceholders = append([]string{"task.id", "task.title", "task.priority", "task.status", "task.list", "task.project", "task.due"}, common...)
	notePlaceholders = append([]string{"note.id", "note.title", "note.kind", "note.url", "note.tags"}, common...)
	runPlaceholders  = append([]string{"run.id", "run.automation", "run.error"}, common...)
)

// Triggers is the trigger catalog, in display order.
var Triggers = []TriggerSpec{
	{Type: models.TriggerSchedule, Label: "On a schedule", Description: "Every day, on chosen weekdays, monthly, or once, at a time in your time zone.", Fields: []Field{}, Placeholders: common, RunNow: true},
	{Type: models.TriggerTaskCreated, Label: "Task created", Description: "When you add a task.", Fields: taskFields, Placeholders: taskPlaceholders},
	{Type: models.TriggerTaskCompleted, Label: "Task completed", Description: "When a task is marked completed.", Fields: taskFields, Placeholders: taskPlaceholders},
	{Type: models.TriggerTaskOverdue, Label: "Task overdue", Description: "When an open task passes its due time (checked about every 30 seconds).", Fields: taskFields, Placeholders: taskPlaceholders},
	{Type: models.TriggerNoteCreated, Label: "Note created", Description: "When you save a new note, link, document or receipt.", Fields: noteFields, Placeholders: notePlaceholders},
	{Type: models.TriggerWorkflowFailed, Label: "Another automation failed", Description: "When a run of one of your other automations fails and won't be retried.", Fields: runFields, Placeholders: runPlaceholders},
	{Type: models.TriggerManual, Label: "Manually", Description: "Only when you press Run.", Fields: []Field{}, Placeholders: common, RunNow: true},
}

// Actions is the action catalog; each is a tool in ai.AutomationTools.
var Actions = []ActionSpec{
	{Type: "notify", Label: "Send me a notification", Description: "An in-app notification. Never needs approval."},
	{Type: "create_task", Label: "Create a task", Description: "Title, optional description, priority and due time."},
	{Type: "complete_task", Label: "Complete the task", Description: "Marks the triggering task (task_id: {{task.id}}) completed."},
	{Type: "reschedule_task", Label: "Reschedule the task", Description: "Moves a task's due time, e.g. due_at: \"+1d\"."},
	{Type: "create_note", Label: "Create a note", Description: "Title, optional body, kind and tags."},
	{Type: "create_reminder", Label: "Set a reminder", Description: "A reminder at a time, e.g. remind_at: \"tomorrow 09:00\", or for a task."},
	{Type: "recover_task", Label: "Find a new time for the task", Description: "Moves the task (task_id: {{task.id}}) to the next free slot around your events and quiet hours; optional minutes (default 30). Today's plan is updated."},
	{Type: "plan_day", Label: "Plan my day", Description: "The assistant plans today (focus and a suggested schedule) and sends it to you. Reuses today's plan if there is one; refresh: true writes a new one. Needs the AI assistant."},
}

func triggerSpec(t string) (TriggerSpec, bool) {
	for _, s := range Triggers {
		if s.Type == t {
			return s, true
		}
	}
	return TriggerSpec{}, false
}

func actionAllowed(t string) bool {
	for _, a := range Actions {
		if a.Type == t {
			return true
		}
	}
	return false
}

// CatalogData is the catalog as served to clients.
func CatalogData() map[string]any {
	return map[string]any{"triggers": Triggers, "actions": Actions, "operators": opsByType}
}
