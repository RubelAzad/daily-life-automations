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
  const response = await fetch(`${API_BASE}${path}`, {
    cache: "no-store",
    credentials: "include", // send the HttpOnly session cookie
    ...init,
    headers: { "Content-Type": "application/json", ...init.headers },
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

export async function register(input: { name: string; email: string; password: string }) {
  return (await request<{ user: User }>("/auth/register", { method: "POST", body: JSON.stringify(input) })).user;
}

export async function login(input: { email: string; password: string }) {
  return (await request<{ user: User }>("/auth/login", { method: "POST", body: JSON.stringify(input) })).user;
}

export function logout() {
  return request<void>("/auth/logout", { method: "POST" });
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
  kind: "standalone" | "task" | "event";
  subject_title: string;
};

export type ReminderInput = {
  title?: string;
  note?: string | null;
  remind_at?: string | null;
  task_id?: number;
  event_id?: number;
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

export function listReminders(query: { status?: string; task_id?: number; event_id?: number; per_page?: number } = {}) {
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
