package ai

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
)

// Deps are the services the built-in tools call. Every call goes through the
// same validation and ownership checks as the REST API.
type Deps struct {
	Tasks     services.TaskService
	Calendar  services.CalendarService
	Notes     services.NoteService
	Reminders services.ReminderService
	Today     services.TodayService
	Search    services.SearchService
	// Notifications is used by the automation-only notify tool.
	Notifications repository.NotificationRepository
	// Settings (time zone, quiet hours) and Briefings (today's plan) are
	// used by recover_task; Planner by the automation-only plan_day.
	Settings  repository.SettingsRepository
	Briefings repository.BriefingRepository
	Planner   Planner
}

const (
	maxListTasks = 50
	maxListNotes = 20
)

// BuiltinTools returns the assistant's allowlist.
func BuiltinTools(d Deps) *Registry {
	return NewRegistry(
		getTodayTool(d), listTasksTool(d), createTaskTool(d), completeTaskTool(d), rescheduleTaskTool(d),
		carryForwardTool(d), deleteTaskTool(d), listEventsTool(d), createEventTool(d),
		searchNotesTool(d), searchDocumentsTool(d), createNoteTool(d), createReminderTool(d), recoverTaskTool(d),
	)
}

// ---- Compact outputs (fewer tokens for the model) ---------------------------

type taskOut struct {
	ID         uint64     `json:"id"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	Priority   string     `json:"priority"`
	DueAt      *time.Time `json:"due_at,omitempty"`
	ParentID   *uint64    `json:"parent_id,omitempty"`
	Blocked    bool       `json:"blocked,omitempty"`
	CarryCount int        `json:"carry_count,omitempty"`
}

func compactTask(t models.Task) taskOut {
	return taskOut{ID: t.ID, Title: t.Title, Status: t.Status, Priority: t.Priority, DueAt: t.DueAt, ParentID: t.ParentID, Blocked: t.Blocked, CarryCount: t.CarryCount}
}

func compactTasks(ts []models.Task) []taskOut {
	out := make([]taskOut, len(ts))
	for i, t := range ts {
		out[i] = compactTask(t)
	}
	return out
}

type eventOut struct {
	ID        uint64       `json:"id"`
	Title     string       `json:"title"`
	StartsAt  time.Time    `json:"starts_at"`
	EndsAt    time.Time    `json:"ends_at"`
	AllDay    bool         `json:"all_day,omitempty"`
	StartDate *models.Date `json:"start_date,omitempty"`
	EndDate   *models.Date `json:"end_date,omitempty"`
	Location  *string      `json:"location,omitempty"`
	Conflicts []uint64     `json:"conflicts_with,omitempty"`
}

func compactEvent(e models.CalendarEvent, conflicts []uint64) eventOut {
	return eventOut{ID: e.ID, Title: e.Title, StartsAt: e.StartsAt, EndsAt: e.EndsAt, AllDay: e.AllDay,
		StartDate: e.StartDate, EndDate: e.EndDate, Location: e.Location, Conflicts: conflicts}
}

func compactOccurrences(os []models.EventOccurrence) []eventOut {
	out := make([]eventOut, len(os))
	for i, o := range os {
		out[i] = compactEvent(o.CalendarEvent, o.ConflictsWith)
	}
	return out
}

type noteOut struct {
	ID        uint64       `json:"id"`
	Title     string       `json:"title"`
	Kind      string       `json:"kind"`
	Body      string       `json:"body,omitempty"` // first 300 characters
	URL       *string      `json:"url,omitempty"`
	ExpiresOn *models.Date `json:"expires_on,omitempty"`
	Tags      []string     `json:"tags,omitempty"`
}

func compactNote(n models.Note) noteOut {
	out := noteOut{ID: n.ID, Title: n.Title, Kind: n.Kind, URL: n.URL, ExpiresOn: n.ExpiresOn}
	if n.Body != nil {
		out.Body = truncateRunes(*n.Body, 300)
	}
	for _, t := range n.Tags {
		out.Tags = append(out.Tags, t.Name)
	}
	return out
}

func quote(s string) string { return "“" + truncateRunes(s, 80) + "”" }

func set[T any](v T) services.Optional[T] { return services.Optional[T]{Set: true, Value: v} }

// taskLabel names a task for an approval summary, falling back to its ID.
func taskLabel(ctx context.Context, d Deps, userID, id uint64) string {
	if t, err := d.Tasks.Get(ctx, userID, id); err == nil {
		return quote(t.Title)
	}
	return "#" + strconv.FormatUint(id, 10)
}

// ---- Tools -----------------------------------------------------------------

func getTodayTool(d Deps) Tool {
	return define("get_today",
		"Get the user's day: today's date and time zone, overdue tasks, tasks due today, tasks completed today, today's events, reminders still to come and documents expiring soon.",
		RiskRead, `{"type":"object","properties":{},"additionalProperties":false}`,
		func(context.Context, uint64, struct{}) string { return "Look at today" },
		func(ctx context.Context, userID uint64, _ struct{}) (any, error) {
			t, err := d.Today.Get(ctx, userID)
			if err != nil {
				return nil, err
			}
			notes := make([]noteOut, len(t.ExpiringNotes))
			for i, n := range t.ExpiringNotes {
				notes[i] = compactNote(n)
			}
			rems := make([]map[string]any, len(t.Reminders))
			for i, r := range t.Reminders {
				rems[i] = map[string]any{"id": r.ID, "title": r.SubjectTitle, "remind_at": r.RemindAt, "kind": r.Kind}
			}
			return map[string]any{
				"date": t.Date, "timezone": t.Timezone, "summary": t.Summary,
				"overdue": compactTasks(t.Overdue), "due_today": compactTasks(t.DueToday), "completed_today": compactTasks(t.CompletedToday),
				"events": compactOccurrences(t.Events), "reminders": rems, "expiring_notes": notes,
			}, nil
		})
}

type listTasksInput struct {
	Status  string `json:"status"`
	Query   string `json:"query"`
	DueFrom string `json:"due_from"`
	DueTo   string `json:"due_to"`
	Limit   int    `json:"limit"`
}

func listTasksTool(d Deps) Tool {
	return define("list_tasks",
		"List the user's tasks, soonest due first. Filter by status (\"open\" = pending or in progress), text, or a due-date range (RFC 3339, from inclusive, to exclusive).",
		RiskRead, `{"type":"object","properties":{
			"status":{"type":"string","enum":["open","pending","in_progress","completed","cancelled"]},
			"query":{"type":"string","maxLength":200,"description":"Text to find in the title or description"},
			"due_from":{"type":"string","format":"date-time"},
			"due_to":{"type":"string","format":"date-time"},
			"limit":{"type":"integer","minimum":1,"maximum":50,"description":"Default 20"}
		},"additionalProperties":false}`,
		func(context.Context, uint64, listTasksInput) string { return "Look up tasks" },
		func(ctx context.Context, userID uint64, in listTasksInput) (any, error) {
			if in.Limit == 0 {
				in.Limit = 20
			}
			if in.Limit < 1 || in.Limit > maxListTasks {
				return nil, &InputError{Message: "limit must be between 1 and 50"}
			}
			tasks, meta, err := d.Tasks.List(ctx, userID, services.ListTasksInput{
				Status: in.Status, Query: in.Query, DueFrom: in.DueFrom, DueTo: in.DueTo, PerPage: strconv.Itoa(in.Limit),
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{"tasks": compactTasks(tasks), "total": meta.Total}, nil
		})
}

type createTaskInput struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Priority    string  `json:"priority"`
	DueAt       *string `json:"due_at"`
}

func createTaskTool(d Deps) Tool {
	return define("create_task",
		"Create a task. due_at is RFC 3339 with the user's offset, e.g. 2026-10-18T17:00:00+06:00.",
		RiskLow, `{"type":"object","properties":{
			"title":{"type":"string","minLength":1,"maxLength":255},
			"description":{"type":"string","maxLength":10000},
			"priority":{"type":"string","enum":["low","medium","high","urgent"]},
			"due_at":{"type":"string","format":"date-time"}
		},"required":["title"],"additionalProperties":false}`,
		func(_ context.Context, _ uint64, in createTaskInput) string {
			s := "Create task " + quote(in.Title)
			if in.DueAt != nil {
				s += ", due " + *in.DueAt
			}
			return s
		},
		func(ctx context.Context, userID uint64, in createTaskInput) (any, error) {
			t, err := d.Tasks.Create(ctx, userID, services.CreateTaskInput{Title: in.Title, Description: in.Description, Priority: in.Priority, DueAt: in.DueAt})
			if err != nil {
				return nil, err
			}
			return compactTask(t), nil
		})
}

type taskIDInput struct {
	TaskID uint64 `json:"task_id"`
}

const taskIDSchema = `{"type":"object","properties":{"task_id":{"type":"integer","minimum":1}},"required":["task_id"],"additionalProperties":false}`

func requireTaskID(id uint64) error {
	if id == 0 {
		return &InputError{Message: "task_id is required"}
	}
	return nil
}

func completeTaskTool(d Deps) Tool {
	return define("complete_task", "Mark a task as completed. Repeating tasks then get their next occurrence.",
		RiskLow, taskIDSchema,
		func(ctx context.Context, userID uint64, in taskIDInput) string {
			return "Complete task " + taskLabel(ctx, d, userID, in.TaskID)
		},
		func(ctx context.Context, userID uint64, in taskIDInput) (any, error) {
			if err := requireTaskID(in.TaskID); err != nil {
				return nil, err
			}
			t, err := d.Tasks.Update(ctx, userID, in.TaskID, services.UpdateTaskInput{Status: set(models.TaskCompleted)})
			if err != nil {
				return nil, err
			}
			return compactTask(t), nil
		})
}

type rescheduleInput struct {
	TaskID uint64 `json:"task_id"`
	DueAt  string `json:"due_at"`
}

func rescheduleTaskTool(d Deps) Tool {
	return define("reschedule_task", "Change a task's due date and time (RFC 3339). Its reminders move with it.",
		RiskLow, `{"type":"object","properties":{
			"task_id":{"type":"integer","minimum":1},
			"due_at":{"type":"string","format":"date-time"}
		},"required":["task_id","due_at"],"additionalProperties":false}`,
		func(ctx context.Context, userID uint64, in rescheduleInput) string {
			return "Move task " + taskLabel(ctx, d, userID, in.TaskID) + " to " + in.DueAt
		},
		func(ctx context.Context, userID uint64, in rescheduleInput) (any, error) {
			if err := requireTaskID(in.TaskID); err != nil {
				return nil, err
			}
			t, err := d.Tasks.Update(ctx, userID, in.TaskID, services.UpdateTaskInput{DueAt: set(in.DueAt)})
			if err != nil {
				return nil, err
			}
			return compactTask(t), nil
		})
}

func carryForwardTool(d Deps) Tool {
	return define("carry_forward_tasks",
		"Move unfinished tasks to today or tomorrow at the same time of day. Without task_ids, moves every overdue task.",
		RiskLow, `{"type":"object","properties":{
			"task_ids":{"type":"array","items":{"type":"integer","minimum":1},"maxItems":200},
			"to":{"type":"string","enum":["today","tomorrow"]}
		},"additionalProperties":false}`,
		func(_ context.Context, _ uint64, in services.CarryForwardInput) string {
			to := in.To
			if to == "" {
				to = "today"
			}
			if len(in.TaskIDs) == 0 {
				return "Move all overdue tasks to " + to
			}
			return fmt.Sprintf("Move %d task(s) to %s", len(in.TaskIDs), to)
		},
		func(ctx context.Context, userID uint64, in services.CarryForwardInput) (any, error) {
			moved, err := d.Tasks.CarryForward(ctx, userID, in)
			if err != nil {
				return nil, err
			}
			return map[string]any{"moved": len(moved), "tasks": compactTasks(moved)}, nil
		})
}

func deleteTaskTool(d Deps) Tool {
	return define("delete_task", "Delete a task and its subtasks permanently. Always needs the user's approval.",
		RiskHigh, taskIDSchema,
		func(ctx context.Context, userID uint64, in taskIDInput) string {
			return "Delete task " + taskLabel(ctx, d, userID, in.TaskID) + " permanently"
		},
		func(ctx context.Context, userID uint64, in taskIDInput) (any, error) {
			if err := requireTaskID(in.TaskID); err != nil {
				return nil, err
			}
			if err := d.Tasks.Delete(ctx, userID, in.TaskID); err != nil {
				return nil, err
			}
			return map[string]any{"deleted": in.TaskID}, nil
		})
}

type listEventsInput struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func listEventsTool(d Deps) Tool {
	return define("list_events",
		"List calendar events between two RFC 3339 times (at most 366 days apart), with overlaps in conflicts_with.",
		RiskRead, `{"type":"object","properties":{
			"from":{"type":"string","format":"date-time"},
			"to":{"type":"string","format":"date-time"}
		},"required":["from","to"],"additionalProperties":false}`,
		func(context.Context, uint64, listEventsInput) string { return "Look up events" },
		func(ctx context.Context, userID uint64, in listEventsInput) (any, error) {
			occs, err := d.Calendar.List(ctx, userID, in.From, in.To)
			if err != nil {
				return nil, err
			}
			return map[string]any{"events": compactOccurrences(occs)}, nil
		})
}

type createEventInput struct {
	Title     string  `json:"title"`
	StartsAt  *string `json:"starts_at"`
	EndsAt    *string `json:"ends_at"`
	AllDay    bool    `json:"all_day"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
	Location  *string `json:"location"`
}

func createEventTool(d Deps) Tool {
	return define("create_event",
		"Create a calendar event. Timed events need starts_at and ends_at (RFC 3339). All-day events set all_day and start_date (YYYY-MM-DD), optionally end_date (inclusive). The result lists any overlapping events.",
		RiskLow, `{"type":"object","properties":{
			"title":{"type":"string","minLength":1,"maxLength":255},
			"starts_at":{"type":"string","format":"date-time"},
			"ends_at":{"type":"string","format":"date-time"},
			"all_day":{"type":"boolean"},
			"start_date":{"type":"string","format":"date"},
			"end_date":{"type":"string","format":"date"},
			"location":{"type":"string","maxLength":255}
		},"required":["title"],"additionalProperties":false}`,
		func(_ context.Context, _ uint64, in createEventInput) string {
			s := "Add event " + quote(in.Title)
			switch {
			case in.AllDay && in.StartDate != nil:
				s += " on " + *in.StartDate
			case in.StartsAt != nil:
				s += " at " + *in.StartsAt
			}
			return s
		},
		func(ctx context.Context, userID uint64, in createEventInput) (any, error) {
			e, conflicts, err := d.Calendar.Create(ctx, userID, services.CreateEventInput{
				Title: in.Title, StartsAt: in.StartsAt, EndsAt: in.EndsAt, AllDay: in.AllDay,
				StartDate: in.StartDate, EndDate: in.EndDate, Location: in.Location,
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{"event": compactEvent(e, nil), "conflicts": compactOccurrences(conflicts)}, nil
		})
}

type searchNotesInput struct {
	Query string `json:"query"`
	Kind  string `json:"kind"`
	Limit int    `json:"limit"`
}

func searchNotesTool(d Deps) Tool {
	return define("search_notes",
		"Search the user's notes, ideas, links, documents and receipts by text (title, body, URL, tags, attachment names).",
		RiskRead, `{"type":"object","properties":{
			"query":{"type":"string","maxLength":200},
			"kind":{"type":"string","enum":["note","idea","link","document","receipt"]},
			"limit":{"type":"integer","minimum":1,"maximum":20,"description":"Default 10"}
		},"additionalProperties":false}`,
		func(context.Context, uint64, searchNotesInput) string { return "Search notes" },
		func(ctx context.Context, userID uint64, in searchNotesInput) (any, error) {
			if in.Limit == 0 {
				in.Limit = 10
			}
			if in.Limit < 1 || in.Limit > maxListNotes {
				return nil, &InputError{Message: "limit must be between 1 and 20"}
			}
			notes, meta, err := d.Notes.List(ctx, userID, services.ListNotesInput{Query: in.Query, Kind: in.Kind, PerPage: strconv.Itoa(in.Limit)})
			if err != nil {
				return nil, err
			}
			out := make([]noteOut, len(notes))
			for i, n := range notes {
				out[i] = compactNote(n)
			}
			return map[string]any{"notes": out, "total": meta.Total}, nil
		})
}

type createNoteInput struct {
	Title string   `json:"title"`
	Body  *string  `json:"body"`
	Kind  string   `json:"kind"`
	URL   *string  `json:"url"`
	Tags  []string `json:"tags"`
}

func createNoteTool(d Deps) Tool {
	return define("create_note", "Save a note, idea, link, document or receipt, with optional tags.",
		RiskLow, `{"type":"object","properties":{
			"title":{"type":"string","minLength":1,"maxLength":255},
			"body":{"type":"string","maxLength":100000},
			"kind":{"type":"string","enum":["note","idea","link","document","receipt"]},
			"url":{"type":"string","maxLength":2048,"description":"Required for links; http(s) only"},
			"tags":{"type":"array","items":{"type":"string","maxLength":50},"maxItems":20}
		},"required":["title"],"additionalProperties":false}`,
		func(_ context.Context, _ uint64, in createNoteInput) string {
			s := "Save note " + quote(in.Title)
			if len(in.Tags) > 0 {
				s += " tagged " + strings.Join(in.Tags, ", ")
			}
			return s
		},
		func(ctx context.Context, userID uint64, in createNoteInput) (any, error) {
			n, err := d.Notes.Create(ctx, userID, services.CreateNoteInput{Title: in.Title, Body: in.Body, Kind: in.Kind, URL: in.URL, Tags: in.Tags})
			if err != nil {
				return nil, err
			}
			return compactNote(n), nil
		})
}

type createReminderInput struct {
	Title         *string `json:"title"`
	RemindAt      *string `json:"remind_at"`
	TaskID        *uint64 `json:"task_id"`
	OffsetMinutes *int    `json:"offset_minutes"`
	Note          *string `json:"note"`
}

func createReminderTool(d Deps) Tool {
	return define("create_reminder",
		"Set a reminder. Either a standalone one (title + remind_at, RFC 3339), or one for a task (task_id + offset_minutes before its due time, or task_id + remind_at).",
		RiskLow, `{"type":"object","properties":{
			"title":{"type":"string","maxLength":255},
			"remind_at":{"type":"string","format":"date-time"},
			"task_id":{"type":"integer","minimum":1},
			"offset_minutes":{"type":"integer","minimum":0,"maximum":43200},
			"note":{"type":"string","maxLength":2000}
		},"additionalProperties":false}`,
		func(ctx context.Context, userID uint64, in createReminderInput) string {
			var s string
			switch {
			case in.TaskID != nil:
				s = "Remind me about task " + taskLabel(ctx, d, userID, *in.TaskID)
			case in.Title != nil:
				s = "Remind me: " + quote(*in.Title)
			default:
				s = "Set a reminder"
			}
			switch {
			case in.RemindAt != nil:
				s += " at " + *in.RemindAt
			case in.OffsetMinutes != nil:
				s += fmt.Sprintf(", %d min before it is due", *in.OffsetMinutes)
			}
			return s
		},
		func(ctx context.Context, userID uint64, in createReminderInput) (any, error) {
			r, err := d.Reminders.Create(ctx, userID, services.CreateReminderInput{
				Title: in.Title, RemindAt: in.RemindAt, TaskID: in.TaskID, OffsetMinutes: in.OffsetMinutes, Note: in.Note,
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{"id": r.ID, "kind": r.Kind, "title": r.SubjectTitle, "remind_at": r.RemindAt}, nil
		})
}

type searchDocumentsInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type documentHit struct {
	NoteID    uint64  `json:"note_id"`
	NoteTitle string  `json:"note_title"`
	FileID    *uint64 `json:"file_id,omitempty"`
	FileName  *string `json:"file_name,omitempty"`
	Excerpt   string  `json:"excerpt"`
}

func searchDocumentsTool(d Deps) Tool {
	return define("search_documents",
		"Search inside the user's notes and the text of their attached files (PDF, Word, Excel, PowerPoint, text). Use a few distinctive keywords, not a question (e.g. \"car insurance policy\"). Matches word starts, so \"insur\" finds \"insurance\". If nothing is found, try synonyms or fewer words. Returns passages to answer from; cite the note or file title.",
		RiskRead, `{"type":"object","properties":{
			"query":{"type":"string","minLength":1,"maxLength":200},
			"limit":{"type":"integer","minimum":1,"maximum":10,"description":"Default 6"}
		},"required":["query"],"additionalProperties":false}`,
		func(_ context.Context, _ uint64, in searchDocumentsInput) string {
			return "Search documents for " + quote(in.Query)
		},
		func(ctx context.Context, userID uint64, in searchDocumentsInput) (any, error) {
			if in.Limit == 0 {
				in.Limit = 6
			}
			if in.Limit < 1 || in.Limit > 10 {
				return nil, &InputError{Message: "limit must be between 1 and 10"}
			}
			results, err := d.Search.Query(ctx, userID, in.Query, in.Limit)
			if err != nil {
				return nil, err
			}
			hits := make([]documentHit, len(results))
			for i, r := range results {
				hits[i] = documentHit{NoteID: r.NoteID, NoteTitle: r.NoteTitle, FileID: r.FileID, FileName: r.FileName, Excerpt: r.Excerpt}
			}
			return map[string]any{"results": hits, "count": len(hits)}, nil
		})
}
