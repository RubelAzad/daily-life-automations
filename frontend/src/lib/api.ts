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

export type Task = {
  id: number;
  list_id: number | null;
  title: string;
  description: string | null;
  status: TaskStatus;
  priority: TaskPriority;
  due_at: string | null;
  completed_at: string | null;
  created_at: string;
  updated_at: string;
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
  q?: string;
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
};

export function listTasks(query: TaskQuery = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  return request<{ data: Task[]; meta: PageMeta }>(`/tasks?${params}`);
}

export async function createTask(input: Partial<TaskInput> & { title: string }) {
  return (await request<{ data: Task }>("/tasks", { method: "POST", body: JSON.stringify(input) })).data;
}

/** Fields omitted are unchanged; null clears description, due_at and list_id. */
export async function updateTask(id: number, patch: Partial<TaskInput>) {
  return (await request<{ data: Task }>(`/tasks/${id}`, { method: "PATCH", body: JSON.stringify(patch) })).data;
}

export function deleteTask(id: number) {
  return request<void>(`/tasks/${id}`, { method: "DELETE" });
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
