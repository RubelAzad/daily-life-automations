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

/** Returns the signed-in user, or null when not signed in. */
export async function getMe(): Promise<User | null> {
  try {
    return (await request<{ user: User }>("/auth/me")).user;
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) return null;
    throw err;
  }
}
