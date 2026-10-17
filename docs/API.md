# LifeOS AI — API Reference

Base URL: `http://localhost:8080/api/v1`. Request and response bodies are JSON.

## Conventions

- **Auth:** `POST /auth/login` (or `/auth/register`) sets an HttpOnly `lifeos_session` cookie. Browser clients send it with `credentials: "include"`. Every route except health and auth returns `401` without a valid session.
- **Ownership:** every record belongs to the signed-in user. Another user's record returns `404`, the same as one that doesn't exist.
- **Responses:** a single resource is `{"data": {...}}`. A list is `{"data": [...], "meta": {"page", "per_page", "total"}}`.
- **Errors:** `{"error": "message"}`. Validation errors are `422` with per-field messages: `{"error": "validation failed", "fields": {"title": "is required"}}`.
- **Times:** RFC 3339 in requests (`2026-10-20T09:00:00+06:00`). Responses are always UTC (`2026-10-20T03:00:00Z`).
- **PATCH:** fields you leave out are unchanged. `null` clears a nullable field.

## Health

| Method | Path | Notes |
|---|---|---|
| GET | `/health` | `{"status":"ok","database":"connected"}`, or `503` when the database is down |

## Auth

| Method | Path | Body | Result |
|---|---|---|---|
| POST | `/auth/register` | `name`, `email`, `password` (8–72 chars) | `201 {"user"}` + session cookie. Creates the default lists. `409` if the email is taken |
| POST | `/auth/login` | `email`, `password` | `200 {"user"}` + session cookie, or `401` |
| POST | `/auth/logout` | — | `204`, ends the session |
| GET | `/auth/me` | — | `200 {"user"}` |

## Tasks

| Method | Path | Notes |
|---|---|---|
| GET | `/tasks` | Top-level tasks only. Query: `status` (`open`, or comma-separated values from `pending,in_progress,completed,cancelled`), `priority`, `list_id`, `parent_id` (returns that task's subtasks instead, oldest first), `q` (searches title and description), `due_from`, `due_to`, `sort` (`due_at` default, `-due_at`, `created_at`, `-created_at`, `priority`), `page`, `per_page` (≤ 100, default 20) |
| POST | `/tasks` | `title` (required, ≤ 255), `description`, `status` (default `pending`), `priority` (`low`/`medium`/`high`/`urgent`, default `medium`), `due_at`, `list_id`, `parent_id`, `recurrence` |
| GET | `/tasks/:id` | |
| PATCH | `/tasks/:id` | Any of the POST fields except `parent_id`. Setting `status` to `completed` sets `completed_at`; changing it to anything else clears it. |
| DELETE | `/tasks/:id` | `204`. Also deletes the task's subtasks and dependency links. |
| POST | `/tasks/:id/dependencies` | `depends_on_id`. This task then waits for that one. Returns the updated task. `422` for self, cycle or unknown task. |
| DELETE | `/tasks/:id/dependencies/:depends_on_id` | Removes the link. Returns the updated task. |

**Read-only task fields:** `subtask_count`, `subtasks_completed`, `dependencies` (`[{id, title, status}]`), `blocked`, `next_task_id`.

### Subtasks
- Create a subtask with `POST /tasks` and `parent_id`. Subtasks are one level deep: a subtask can't have subtasks of its own. A task can have at most 100.
- A subtask has no `list_id` of its own; it belongs to its parent's list. Subtasks can't repeat.

### Recurring tasks
- `recurrence: {"frequency": "daily|weekly|monthly|yearly", "interval": 1–365, "until": RFC 3339 | null}`. It requires `due_at`.
- To change recurrence, send the whole object. `"recurrence": null` stops the repeat.
- **Completing** a recurring task creates the next occurrence in the same transaction: a new `pending` task with the same title, notes, priority, list and recurrence, plus fresh open copies of its subtasks. The completed task's `next_task_id` points to it. Re-opening and completing again does **not** create a second copy.
- **Next due date:** the first date in the series after both the previous due date and *now*. A daily task completed three days late is next due tomorrow; it doesn't create three catch-up copies.
- **Month ends:** dates are calculated from the series' first due date and clamped to month length, so "monthly on the 31st" gives Jan 31 → Feb 28 → Mar 31.
- **Series end:** no new occurrence is created past `until`.
- **Known limitation:** dates are calculated in UTC until Step 1.8 adds user time zones. For Asia/Dhaka (UTC+6), a monthly task due between 00:00 and 05:59 local time can be clamped by its UTC date near month ends.

### Dependencies
- A task with any open dependency has `blocked: true`. It **cannot be set to `completed`** (`422`), but it can still be started (`in_progress`) or cancelled.
- Dependencies must belong to the same user, and circular chains are rejected (A → B → C → A).

## Task lists

New accounts start with Personal, Work, Family and Shopping.

| Method | Path | Notes |
|---|---|---|
| GET | `/task-lists` | Ordered by `position`. Each list includes `open_task_count`. |
| POST | `/task-lists` | `name` (required, ≤ 100, unique per user, case-insensitive), `color` (`#rrggbb`) |
| PATCH | `/task-lists/:id` | `name`, `color`, `position` |
| DELETE | `/task-lists/:id` | `204`. The list's tasks are kept and have their `list_id` set to `null`. |
