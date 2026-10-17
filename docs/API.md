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
| GET | `/tasks` | Query: `status` (`open`, or comma-separated values from `pending,in_progress,completed,cancelled`), `priority`, `list_id`, `q` (searches title and description), `due_from`, `due_to`, `sort` (`due_at` default, `-due_at`, `created_at`, `-created_at`, `priority`), `page`, `per_page` (≤ 100, default 20) |
| POST | `/tasks` | `title` (required, ≤ 255), `description`, `status` (default `pending`), `priority` (`low`/`medium`/`high`/`urgent`, default `medium`), `due_at`, `list_id` |
| GET | `/tasks/:id` | |
| PATCH | `/tasks/:id` | Any of the POST fields. Setting `status` to `completed` sets `completed_at`; changing it to anything else clears it. |
| DELETE | `/tasks/:id` | `204` |

## Task lists

New accounts start with Personal, Work, Family and Shopping.

| Method | Path | Notes |
|---|---|---|
| GET | `/task-lists` | Ordered by `position`. Each list includes `open_task_count`. |
| POST | `/task-lists` | `name` (required, ≤ 100, unique per user, case-insensitive), `color` (`#rrggbb`) |
| PATCH | `/task-lists/:id` | `name`, `color`, `position` |
| DELETE | `/task-lists/:id` | `204`. The list's tasks are kept and have their `list_id` set to `null`. |
