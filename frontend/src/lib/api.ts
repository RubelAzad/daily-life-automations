const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080/api/v1";

export type Health = { status: string; database: string };

export type User = {
  id: number;
  name: string;
  email: string;
  created_at: string;
  updated_at: string;
};

export class ApiError extends Error {
  status: number;
  fields?: Record<string, string>;

  constructor(status: number, message: string, fields?: Record<string, string>) {
    super(message);
    this.status = status;
    this.fields = fields;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  // FormData bodies (uploads) must let the browser set the multipart boundary.
  const isForm = typeof FormData !== "undefined" && init.body instanceof FormData;
  const response = await fetch(`${API_BASE}${path}`, {
    cache: "no-store",
    credentials: "include", // send the HttpOnly session cookie
    ...init,
    headers: isForm ? init.headers : { "Content-Type": "application/json", ...init.headers },
  });

  if (response.status === 204) return undefined as T;

  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new ApiError(response.status, body.error ?? `API request failed: ${response.status}`, body.fields);
  }
  return body as T;
}

export function getHealth() {
  return request<Health>("/health");
}

/** timezone is the browser's IANA zone; the server ignores it if unknown. */
export async function register(input: { name: string; email: string; password: string; timezone?: string }) {
  return (await request<{ user: User }>("/auth/register", { method: "POST", body: JSON.stringify(input) })).user;
}

export async function login(input: { email: string; password: string }) {
  return (await request<{ user: User }>("/auth/login", { method: "POST", body: JSON.stringify(input) })).user;
}

export function logout() {
  return request<void>("/auth/logout", { method: "POST" });
}

// ---- Settings --------------------------------------------------------------

export const WEEK_START_OPTIONS = [
  { value: 6, label: "Saturday" },
  { value: 0, label: "Sunday" },
  { value: 1, label: "Monday" },
] as const;
export type WeekStart = (typeof WEEK_START_OPTIONS)[number]["value"];
export type ApprovalMode = "always" | "high_impact_only";

export type Settings = {
  timezone: string;
  week_starts_on: WeekStart;
  quiet_hours_enabled: boolean;
  quiet_hours_start: string; // "HH:MM"
  quiet_hours_end: string;
  ai_approval_mode: ApprovalMode;
  /** Automatic AI briefings at a local "HH:MM" time; off by default. */
  morning_briefing_enabled: boolean;
  morning_briefing_time: string;
  evening_summary_enabled: boolean;
  evening_summary_time: string;
  updated_at: string;
};

export async function getSettings() {
  return (await request<{ data: Settings }>("/settings")).data;
}

export async function updateSettings(patch: Partial<Omit<Settings, "updated_at">>) {
  return (await request<{ data: Settings }>("/settings", { method: "PATCH", body: JSON.stringify(patch) })).data;
}

/** The time zone this browser/device is set to, e.g. "Asia/Dhaka". */
export function deviceTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}

// ---- Tasks -----------------------------------------------------------------

export const TASK_STATUSES = ["pending", "in_progress", "completed", "cancelled"] as const;
export const TASK_PRIORITIES = ["low", "medium", "high", "urgent"] as const;
export type TaskStatus = (typeof TASK_STATUSES)[number];
export type TaskPriority = (typeof TASK_PRIORITIES)[number];

export const RECURRENCE_FREQUENCIES = ["daily", "weekly", "monthly", "yearly"] as const;
export type RecurrenceFrequency = (typeof RECURRENCE_FREQUENCIES)[number];

export type Recurrence = {
  frequency: RecurrenceFrequency;
  interval: number;
  until: string | null;
};

export type TaskRef = { id: number; title: string; status: TaskStatus };

export type Task = {
  id: number;
  list_id: number | null;
  project_id: number | null;
  milestone_id: number | null;
  parent_id: number | null;
  title: string;
  description: string | null;
  status: TaskStatus;
  priority: TaskPriority;
  due_at: string | null;
  /** How many times it was carried forward unfinished. */
  carry_count: number;
  completed_at: string | null;
  recurrence: Recurrence | null;
  next_task_id: number | null;
  created_at: string;
  updated_at: string;
  subtask_count: number;
  subtasks_completed: number;
  dependencies: TaskRef[];
  /** True while any dependency is still open; the task cannot be completed. */
  blocked: boolean;
};

export type TaskList = {
  id: number;
  name: string;
  color: string | null;
  position: number;
  open_task_count: number;
};

export type PageMeta = { page: number; per_page: number; total: number };

export type TaskQuery = {
  status?: string; // "open", or comma-separated statuses
  priority?: TaskPriority;
  list_id?: number;
  parent_id?: number; // list this task's subtasks
  project_id?: number;
  milestone_id?: number | "none";
  q?: string;
  due_from?: string; // RFC 3339, inclusive
  due_to?: string; // RFC 3339, exclusive
  sort?: "due_at" | "-due_at" | "created_at" | "-created_at" | "priority";
  page?: number;
  per_page?: number;
};

export type TaskInput = {
  title: string;
  description: string | null;
  status: TaskStatus;
  priority: TaskPriority;
  due_at: string | null; // RFC 3339
  list_id: number | null;
  /** Changing project clears a milestone from the old project. */
  project_id: number | null;
  /** Implies its project when project_id is omitted. */
  milestone_id: number | null;
  /** Requires due_at. Send the whole object to change it; null stops repeating. */
  recurrence: Recurrence | null;
};

export type NewTaskInput = Partial<TaskInput> & { title: string; parent_id?: number };

export function listTasks(query: TaskQuery = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  return request<{ data: Task[]; meta: PageMeta }>(`/tasks?${params}`);
}

export async function createTask(input: NewTaskInput) {
  return (await request<{ data: Task }>("/tasks", { method: "POST", body: JSON.stringify(input) })).data;
}

/** Fields omitted are unchanged; null clears description, due_at and list_id. */
export async function updateTask(id: number, patch: Partial<TaskInput>) {
  return (await request<{ data: Task }>(`/tasks/${id}`, { method: "PATCH", body: JSON.stringify(patch) })).data;
}

export function deleteTask(id: number) {
  return request<void>(`/tasks/${id}`, { method: "DELETE" });
}

/** Makes taskId wait for dependsOnId. Rejected (422) if it would create a cycle. */
export async function addDependency(taskId: number, dependsOnId: number) {
  return (
    await request<{ data: Task }>(`/tasks/${taskId}/dependencies`, {
      method: "POST",
      body: JSON.stringify({ depends_on_id: dependsOnId }),
    })
  ).data;
}

export async function removeDependency(taskId: number, dependsOnId: number) {
  return (await request<{ data: Task }>(`/tasks/${taskId}/dependencies/${dependsOnId}`, { method: "DELETE" })).data;
}

export async function listTaskLists() {
  return (await request<{ data: TaskList[] }>("/task-lists")).data;
}

export async function createTaskList(input: { name: string; color?: string | null }) {
  return (await request<{ data: TaskList }>("/task-lists", { method: "POST", body: JSON.stringify(input) })).data;
}

export function deleteTaskList(id: number) {
  return request<void>(`/task-lists/${id}`, { method: "DELETE" });
}

// ---- Projects --------------------------------------------------------------

export const PROJECT_STATUSES = ["active", "on_hold", "completed", "archived"] as const;
export type ProjectStatus = (typeof PROJECT_STATUSES)[number];

export type Progress = {
  total_tasks: number; // excludes cancelled
  completed_tasks: number;
  overdue_tasks: number;
  percent: number;
};

export type Milestone = {
  id: number;
  project_id: number;
  title: string;
  description: string | null;
  due_date: string | null; // "YYYY-MM-DD"
  completed: boolean;
  completed_at: string | null;
  position: number;
  progress: Progress;
};

export type Project = {
  id: number;
  name: string;
  description: string | null;
  status: ProjectStatus;
  color: string | null;
  start_date: string | null; // "YYYY-MM-DD"
  target_date: string | null;
  completed_at: string | null;
  created_at: string;
  updated_at: string;
  progress: Progress & { milestones: number; milestones_completed: number };
  /** Only present on getProject. */
  milestones?: Milestone[];
};

export type ProjectInput = {
  name: string;
  description: string | null;
  status: ProjectStatus;
  color: string | null;
  start_date: string | null;
  target_date: string | null;
};

/** Default status filter: everything except archived. */
export function listProjects(query: { status?: string; page?: number; per_page?: number } = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  return request<{ data: Project[]; meta: PageMeta }>(`/projects?${params}`);
}

export async function getProject(id: number) {
  return (await request<{ data: Project }>(`/projects/${id}`)).data;
}

export async function createProject(input: Partial<ProjectInput> & { name: string }) {
  return (await request<{ data: Project }>("/projects", { method: "POST", body: JSON.stringify(input) })).data;
}

export async function updateProject(id: number, patch: Partial<ProjectInput>) {
  return (await request<{ data: Project }>(`/projects/${id}`, { method: "PATCH", body: JSON.stringify(patch) })).data;
}

/** Tasks are kept and moved out of the project; milestones are deleted. */
export function deleteProject(id: number) {
  return request<void>(`/projects/${id}`, { method: "DELETE" });
}

export async function createMilestone(projectId: number, input: { title: string; due_date?: string | null }) {
  return (
    await request<{ data: Milestone }>(`/projects/${projectId}/milestones`, { method: "POST", body: JSON.stringify(input) })
  ).data;
}

export async function updateMilestone(
  id: number,
  patch: Partial<{ title: string; description: string | null; due_date: string | null; completed: boolean; position: number }>,
) {
  return (await request<{ data: Milestone }>(`/milestones/${id}`, { method: "PATCH", body: JSON.stringify(patch) })).data;
}

/** Its tasks stay in the project with no milestone. */
export function deleteMilestone(id: number) {
  return request<void>(`/milestones/${id}`, { method: "DELETE" });
}

// ---- Calendar --------------------------------------------------------------

export type CalendarEvent = {
  id: number;
  title: string;
  description: string | null;
  location: string | null;
  /** UTC instants; for all-day events, UTC midnight of the first day / the day after the last. */
  starts_at: string;
  ends_at: string;
  all_day: boolean;
  /** "YYYY-MM-DD", inclusive; only for all-day events. */
  start_date: string | null;
  end_date: string | null;
  /** Prep or travel time that also blocks the calendar before the event. */
  buffer_before_minutes: number;
  busy: boolean;
  color: string | null;
  recurrence: Recurrence | null;
  created_at: string;
  updated_at: string;
};

/** One instance in a range. For a recurring event, id is the series. */
export type EventOccurrence = CalendarEvent & { conflicts_with: number[] };

export type EventInput = {
  title: string;
  description: string | null;
  location: string | null;
  all_day: boolean;
  starts_at: string | null; // timed: RFC 3339
  ends_at: string | null;
  start_date: string | null; // all-day: "YYYY-MM-DD"
  end_date: string | null;
  buffer_before_minutes: number;
  busy: boolean;
  color: string | null;
  recurrence: Recurrence | null;
};

/** Saving never fails because of overlaps; they come back as warnings. */
export type SavedEvent = { data: CalendarEvent; conflicts: EventOccurrence[] };

export async function listEvents(from: string, to: string) {
  const params = new URLSearchParams({ from, to });
  return (await request<{ data: EventOccurrence[] }>(`/events?${params}`)).data;
}

export async function getEvent(id: number) {
  return (await request<{ data: CalendarEvent }>(`/events/${id}`)).data;
}

export function createEvent(input: Partial<EventInput> & { title: string }) {
  return request<SavedEvent>("/events", { method: "POST", body: JSON.stringify(input) });
}

export function updateEvent(id: number, patch: Partial<EventInput>) {
  return request<SavedEvent>(`/events/${id}`, { method: "PATCH", body: JSON.stringify(patch) });
}

/** Deletes the whole series for recurring events. */
export function deleteEvent(id: number) {
  return request<void>(`/events/${id}`, { method: "DELETE" });
}

// ---- Reminders & notifications ---------------------------------------------

export type ReminderStatus = "scheduled" | "sent" | "cancelled";

export type Reminder = {
  id: number;
  task_id: number | null;
  event_id: number | null;
  note_id: number | null; // fires before the note's expires_on
  title: string | null; // standalone only
  note: string | null;
  /** Minutes before the task's due time / event start; null for a fixed time. */
  offset_minutes: number | null;
  /** Next time it fires; null while inactive (e.g. task has no upcoming due date). */
  remind_at: string | null;
  occurs_at: string | null;
  follow_up_minutes: number | null;
  max_follow_ups: number;
  follow_ups_sent: number;
  status: ReminderStatus;
  last_sent_at: string | null;
  kind: "standalone" | "task" | "event" | "note";
  subject_title: string;
};

export type ReminderInput = {
  title?: string;
  note?: string | null;
  remind_at?: string | null;
  task_id?: number;
  event_id?: number;
  note_id?: number;
  offset_minutes?: number | null;
  follow_up_minutes?: number | null;
  max_follow_ups?: number;
};

/** Named to avoid clashing with the browser's Notification type. */
export type AppNotification = {
  id: number;
  kind: string;
  title: string;
  body: string | null;
  link: string | null;
  priority: "low" | "normal" | "high";
  reminder_id: number | null;
  occurs_at: string | null;
  read_at: string | null;
  created_at: string;
};

export function listReminders(query: { status?: string; task_id?: number; event_id?: number; note_id?: number; per_page?: number } = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  return request<{ data: Reminder[]; meta: PageMeta }>(`/reminders?${params}`);
}

export async function createReminder(input: ReminderInput) {
  return (await request<{ data: Reminder }>("/reminders", { method: "POST", body: JSON.stringify(input) })).data;
}

export function deleteReminder(id: number) {
  return request<void>(`/reminders/${id}`, { method: "DELETE" });
}

export async function snoozeReminder(id: number, minutes: number) {
  return (await request<{ data: Reminder }>(`/reminders/${id}/snooze`, { method: "POST", body: JSON.stringify({ minutes }) })).data;
}

export function listNotifications(query: { unread?: boolean; per_page?: number } = {}) {
  const params = new URLSearchParams();
  if (query.unread) params.set("unread", "true");
  if (query.per_page) params.set("per_page", String(query.per_page));
  return request<{ data: AppNotification[]; meta: PageMeta }>(`/notifications?${params}`);
}

export async function getUnreadCount() {
  return (await request<{ data: { unread: number } }>("/notifications/unread-count")).data.unread;
}

export function markNotificationRead(id: number) {
  return request<void>(`/notifications/${id}/read`, { method: "POST" });
}

export function markAllNotificationsRead() {
  return request<{ data: { marked: number } }>("/notifications/read-all", { method: "POST" });
}

// ---- Notes, tags & files ---------------------------------------------------

export const NOTE_KINDS = ["note", "idea", "link", "document", "receipt"] as const;
export type NoteKind = (typeof NOTE_KINDS)[number];

export type Tag = { id: number; name: string; color: string | null; note_count: number };

export type NoteFile = {
  id: number;
  note_id: number;
  name: string;
  content_type: string;
  size_bytes: number;
  sha256: string;
  /** Whether the file's text can be searched; filled in shortly after upload. */
  text_status: "pending" | "processing" | "ready" | "unsupported" | "failed";
  text_error: string | null;
  text_chars: number | null;
  created_at: string;
};

export type Note = {
  id: number;
  project_id: number | null;
  title: string;
  body: string | null;
  kind: NoteKind;
  url: string | null;
  pinned: boolean;
  archived: boolean;
  expires_on: string | null; // "YYYY-MM-DD"
  created_at: string;
  updated_at: string;
  tags: Tag[];
  file_count: number;
  /** Only present on getNote. */
  files?: NoteFile[];
};

export type NoteInput = {
  title: string;
  body: string | null;
  kind: NoteKind;
  url: string | null;
  pinned: boolean;
  archived: boolean;
  expires_on: string | null;
  project_id: number | null;
  /** Replaces the note's tags; unknown names are created. */
  tags: string[];
};

export type NoteQuery = {
  q?: string;
  kind?: NoteKind;
  tag_id?: number;
  project_id?: number;
  pinned?: boolean;
  archived?: boolean;
  expiring_within_days?: number;
  sort?: "-updated_at" | "title" | "expires_on";
  page?: number;
  per_page?: number;
};

export function listNotes(query: NoteQuery = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  return request<{ data: Note[]; meta: PageMeta }>(`/notes?${params}`);
}

export async function getNote(id: number) {
  return (await request<{ data: Note }>(`/notes/${id}`)).data;
}

export async function createNote(input: Partial<NoteInput> & { title: string }) {
  return (await request<{ data: Note }>("/notes", { method: "POST", body: JSON.stringify(input) })).data;
}

export async function updateNote(id: number, patch: Partial<NoteInput>) {
  return (await request<{ data: Note }>(`/notes/${id}`, { method: "PATCH", body: JSON.stringify(patch) })).data;
}

/** Also deletes its attachments. */
export function deleteNote(id: number) {
  return request<void>(`/notes/${id}`, { method: "DELETE" });
}

export async function uploadFile(noteId: number, file: File) {
  const form = new FormData();
  form.append("file", file);
  return (await request<{ data: NoteFile }>(`/notes/${noteId}/files`, { method: "POST", body: form })).data;
}

export function deleteFile(id: number) {
  return request<void>(`/files/${id}`, { method: "DELETE" });
}

/** Opens images/PDFs in the browser; pass download to always save the file. */
export function fileUrl(id: number, download = false) {
  return `${API_BASE}/files/${id}/download${download ? "?download=1" : ""}`;
}

export async function listTags() {
  return (await request<{ data: Tag[] }>("/tags")).data;
}

export async function updateTag(id: number, patch: { name?: string; color?: string | null }) {
  return (await request<{ data: Tag }>(`/tags/${id}`, { method: "PATCH", body: JSON.stringify(patch) })).data;
}

export function deleteTag(id: number) {
  return request<void>(`/tags/${id}`, { method: "DELETE" });
}

// ---- Auth ------------------------------------------------------------------

/** Returns the signed-in user, or null when not signed in. */
export async function getMe(): Promise<User | null> {
  try {
    return (await request<{ user: User }>("/auth/me")).user;
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) return null;
    throw err;
  }
}

// ---- Today -----------------------------------------------------------------

export type TodaySummary = { overdue: number; due_today: number; completed_today: number; events: number; conflicts: number };

/** The user's current day in their settings time zone. */
export type Today = {
  date: string; // "YYYY-MM-DD"
  timezone: string;
  starts_at: string;
  ends_at: string;
  summary: TodaySummary;
  overdue: Task[]; // open, due before today, oldest first (at most 100; summary has the total)
  due_today: Task[];
  completed_today: Task[];
  events: EventOccurrence[]; // all-day first
  reminders: Reminder[]; // still to fire today
  expiring_notes: Note[]; // expired or expiring within 30 days
};

export async function getToday() {
  return (await request<{ data: Today }>("/today")).data;
}

/** Moves open tasks to today or tomorrow at the same time of day; no ids = every overdue task. */
export async function carryForward(input: { task_ids?: number[]; to?: "today" | "tomorrow" }) {
  return request<{ data: Task[]; meta: { moved: number } }>("/tasks/carry-forward", { method: "POST", body: JSON.stringify(input) });
}

// ---- AI assistant ----------------------------------------------------------

export type AIRisk = "read" | "low" | "high";
export type AITool = { name: string; description: string; risk: AIRisk };

export type AIStatus = {
  enabled: boolean;
  provider?: string;
  model?: string;
  reason?: string; // why it's off
  tools: AITool[];
};

export type AIActionStatus = "succeeded" | "failed" | "pending_approval" | "running" | "rejected" | "expired";

/** One audit-log entry: a model call (metadata only) or a tool call. */
export type AIAction = {
  id: number;
  kind: "tool" | "llm_call";
  source: string;
  conversation_id: number | null;
  /** The automation that took this action, if any. */
  automation_id: number | null;
  tool: string | null;
  risk: AIRisk | null;
  summary: string | null;
  input: unknown; // redacted
  output: unknown; // redacted
  status: AIActionStatus;
  error: string | null;
  model: string | null;
  input_tokens: number | null;
  output_tokens: number | null;
  duration_ms: number | null;
  decided_at: string | null;
  executed_at: string | null;
  expires_at: string | null; // while pending
  created_at: string;
};

export type AIPing = { reply: string; model: string; duration_ms: number; input_tokens: number; output_tokens: number; action_id: number };

export async function getAIStatus() {
  return (await request<{ data: AIStatus }>("/ai/status")).data;
}

export async function pingAI() {
  return (await request<{ data: AIPing }>("/ai/ping", { method: "POST" })).data;
}

export function listAIActions(query: { status?: string; kind?: "tool" | "llm_call"; conversation_id?: number; page?: number; per_page?: number } = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  return request<{ data: AIAction[]; meta: PageMeta }>(`/ai/actions?${params}`);
}

/** Runs a pending action; its outcome is in the returned status/output/error. */
export async function approveAIAction(id: number) {
  return (await request<{ data: AIAction }>(`/ai/actions/${id}/approve`, { method: "POST" })).data;
}

export async function rejectAIAction(id: number) {
  return (await request<{ data: AIAction }>(`/ai/actions/${id}/reject`, { method: "POST" })).data;
}

// ---- Chat ------------------------------------------------------------------

/** A content block in the model's format. tool_result blocks are sent back by the app. */
export type ChatBlock =
  | { type: "text"; text: string }
  | { type: "tool_use"; id: string; name: string; input: unknown }
  | { type: "tool_result"; tool_use_id: string; content: string; is_error?: boolean };

export type ChatMessage = { id: number; role: "user" | "assistant"; content: ChatBlock[]; created_at: string };

export type Conversation = { id: number; title: string; created_at: string; updated_at: string; messages?: ChatMessage[] };

/** What a tool_result's content holds (JSON). */
export type ToolResult = { action_id: number; status: AIActionStatus; summary?: string; output?: unknown; error?: string };

export function listConversations(query: { page?: number; per_page?: number } = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined) params.set(key, String(value));
  }
  return request<{ data: Conversation[]; meta: PageMeta }>(`/ai/conversations?${params}`);
}

export async function createConversation(title?: string) {
  return (await request<{ data: Conversation }>("/ai/conversations", { method: "POST", body: JSON.stringify({ title: title ?? "" }) })).data;
}

export async function getConversation(id: number) {
  return (await request<{ data: Conversation }>(`/ai/conversations/${id}`)).data;
}

export function deleteConversation(id: number) {
  return request<void>(`/ai/conversations/${id}`, { method: "DELETE" });
}

/** Runs one chat turn; can take several seconds. */
export async function sendChatMessage(conversationId: number, text: string) {
  return (
    await request<{ data: { messages: ChatMessage[]; actions: AIAction[] } }>(`/ai/conversations/${conversationId}/messages`, {
      method: "POST",
      body: JSON.stringify({ text }),
    })
  ).data;
}

// ---- Briefings -------------------------------------------------------------

export type BriefingKind = "morning" | "evening";

export type MorningPlan = {
  summary: string;
  focus: { task_id: number; title: string; reason: string }[];
  schedule: { start: string; end: string; kind: "event" | "task" | "break"; title: string; task_id?: number; event_id?: number }[];
  warnings: string[];
};

export type EveningSummary = {
  summary: string;
  wins: string[];
  unfinished: { task_id: number; title: string; suggestion: "tomorrow" | "later" | "drop"; reason: string }[];
  tomorrow: string;
};

export type Briefing = {
  id: number;
  kind: BriefingKind;
  date: string;
  trigger: "manual" | "scheduled";
  status: "generating" | "ready" | "failed";
  content: MorningPlan | EveningSummary | null;
  error: string | null;
  model: string | null; // null when no model was needed (an empty day)
  created_at: string;
};

export async function getTodayBriefings() {
  return (await request<{ data: { date: string; morning: Briefing | null; evening: Briefing | null } }>("/ai/briefings/today")).data;
}

/** Writes a briefing now (a few seconds). It only advises; nothing is changed. */
export async function createBriefing(kind: BriefingKind) {
  return (await request<{ data: Briefing }>("/ai/briefings", { method: "POST", body: JSON.stringify({ kind }) })).data;
}

// ---- Search ----------------------------------------------------------------

/** The best-matching passage of a note, or of one of its files. */
export type SearchResult = {
  note_id: number;
  note_title: string;
  note_kind: NoteKind;
  file_id: number | null;
  file_name: string | null;
  snippet: string;
  score: number;
};

/** Searches notes and the text inside their attachments (PDF, Word, Excel, PowerPoint, text). */
export async function searchDocuments(q: string, limit = 20) {
  return (await request<{ data: SearchResult[] }>(`/search?${new URLSearchParams({ q, limit: String(limit) })}`)).data;
}

// ---- System ----------------------------------------------------------------

/** One of the worker's repeating jobs (reminders, briefings, search indexing, cleanup). */
export type SystemJob = {
  kind: string;
  every_seconds: number;
  running: boolean;
  last_status: "succeeded" | "failed" | null;
  last_finished_at: string | null;
  next_run_at: string | null;
};

export type WorkerStatus = { running: boolean; last_seen_at: string | null; jobs: SystemJob[] };

/** Whether the background worker is processing jobs. */
export async function getSystemStatus() {
  return (await request<{ data: { worker: WorkerStatus } }>("/system/status")).data.worker;
}

// ---- Automations -----------------------------------------------------------

export type Schedule = {
  frequency: "daily" | "weekly" | "monthly" | "once";
  time: string; // HH:MM, your time zone
  weekdays?: number[]; // weekly: 1 = Monday … 7 = Sunday
  month_day?: number; // monthly
  date?: string; // once: YYYY-MM-DD
};

export type Trigger = { type: string; schedule?: Schedule };
export type Condition = { field: string; op: string; value?: string | string[] };
/** Strings in input may use {{placeholders}}; due_at and remind_at may be relative ("+1d", "tomorrow 09:00"). */
export type AutomationAction = { type: string; input: Record<string, unknown> };
export type RunStatus = "running" | "succeeded" | "waiting_approval" | "retrying" | "failed" | "skipped";

export type Automation = {
  id: number;
  name: string;
  enabled: boolean;
  trigger: Trigger;
  conditions: Condition[];
  actions: AutomationAction[];
  next_run_at: string | null;
  last_run_at: string | null;
  last_status: RunStatus | null;
  last_error: string | null;
  run_count: number;
  created_at: string;
  updated_at: string;
};

export type AutomationField = { name: string; label: string; type: "text" | "choice" | "tags" | "date"; choices?: string[] };
export type TriggerSpec = { type: string; label: string; description: string; fields: AutomationField[]; placeholders: string[]; run_now: boolean };
export type ActionSpec = { type: string; label: string; description: string };
export type AutomationCatalog = { triggers: TriggerSpec[]; actions: ActionSpec[]; operators: Record<AutomationField["type"], string[]> };

export type AutomationInput = Partial<Pick<Automation, "name" | "enabled" | "trigger" | "conditions" | "actions">>;
/** One step of a run: an action's outcome, linked to its entry on the AI activity page. */
export type RunStep = { index: number; type: string; status: string; action_id?: number; summary?: string; error?: string; attempt: number };

/** One run of an automation. */
export type WorkflowRun = {
  id: number;
  automation_id: number | null; // null once the automation is deleted
  automation_name: string;
  trigger_type: string;
  trigger_source: "schedule" | "event" | "manual";
  trigger_summary: string | null;
  status: RunStatus;
  error: string | null;
  steps: RunStep[];
  attempt: number;
  next_retry_at: string | null;
  started_at: string;
  finished_at: string | null;
  duration_ms: number | null;
};

export async function getAutomationCatalog() {
  return (await request<{ data: AutomationCatalog }>("/automations/catalog")).data;
}

export async function listAutomations() {
  return (await request<{ data: Automation[] }>("/automations")).data;
}

export async function createAutomation(input: AutomationInput) {
  return (await request<{ data: Automation }>("/automations", { method: "POST", body: JSON.stringify(input) })).data;
}

export async function updateAutomation(id: number, input: AutomationInput) {
  return (await request<{ data: Automation }>(`/automations/${id}`, { method: "PATCH", body: JSON.stringify(input) })).data;
}

export function deleteAutomation(id: number) {
  return request<void>(`/automations/${id}`, { method: "DELETE" });
}

/** Runs a schedule or manual automation now. */
export async function runAutomation(id: number) {
  return (await request<{ data: WorkflowRun }>(`/automations/${id}/run`, { method: "POST" })).data;
}

export async function listAutomationRuns(params: { automation_id?: number; status?: string; page?: number; per_page?: number } = {}) {
  const q = new URLSearchParams(Object.entries(params).filter(([, v]) => v !== undefined).map(([k, v]) => [k, String(v)]));
  return request<{ data: WorkflowRun[]; meta: PageMeta }>(`/automations/runs?${q}`);
}

export async function getAutomationRun(id: number) {
  return (await request<{ data: WorkflowRun }>(`/automations/runs/${id}`)).data;
}

/** Retries a failed run now, from the step that failed. */
export async function retryAutomationRun(id: number) {
  return (await request<{ data: WorkflowRun }>(`/automations/runs/${id}/retry`, { method: "POST" })).data;
}
