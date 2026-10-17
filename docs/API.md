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
| GET | `/tasks` | Top-level tasks only. Query: `status` (`open`, or comma-separated values from `pending,in_progress,completed,cancelled`), `priority`, `list_id`, `project_id`, `milestone_id` (an ID, or `none` for tasks without one), `parent_id` (returns that task's subtasks instead, oldest first), `q` (searches title and description), `due_from`, `due_to`, `sort` (`due_at` default, `-due_at`, `created_at`, `-created_at`, `priority`), `page`, `per_page` (≤ 100, default 20) |
| POST | `/tasks` | `title` (required, ≤ 255), `description`, `status` (default `pending`), `priority` (`low`/`medium`/`high`/`urgent`, default `medium`), `due_at`, `list_id`, `project_id`, `milestone_id`, `parent_id`, `recurrence` |
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

### Projects on tasks
- Giving only `milestone_id` sets `project_id` to that milestone's project. If both are given, they must match (`422`).
- Changing `project_id` in a PATCH without also sending `milestone_id` clears the old milestone.
- Subtasks belong to their parent's project and can't set one.
- When a recurring task in a project is completed, the next occurrence keeps the same project and milestone.

## Projects

A project groups tasks toward a goal. It's separate from lists: a task can be in the Work list *and* in a project.

| Method | Path | Notes |
|---|---|---|
| GET | `/projects` | Query: `status` (comma-separated values from `active,on_hold,completed,archived`; default is everything except archived), `page`, `per_page`. Ordered by active first, then nearest target date. |
| POST | `/projects` | `name` (required, ≤ 150), `description` (project notes, ≤ 10 000), `status` (default `active`), `color` (`#rrggbb`), `start_date`, `target_date` (both `YYYY-MM-DD`; target must be on or after start) |
| GET | `/projects/:id` | Includes `milestones` (always an array). List responses leave it out. |
| PATCH | `/projects/:id` | Any of the POST fields. Setting `status` to `completed` sets `completed_at`. |
| DELETE | `/projects/:id` | `204`. Deletes the project's milestones. Its tasks are kept, with `project_id` and `milestone_id` set to `null`. |
| POST | `/projects/:id/milestones` | `title` (required), `description`, `due_date` (`YYYY-MM-DD`) |
| PATCH | `/milestones/:id` | `title`, `description`, `due_date`, `completed` (`true`/`false`), `position` |
| DELETE | `/milestones/:id` | `204`. Its tasks stay in the project with no milestone. |

**Progress** counts top-level tasks only (subtasks don't count) and leaves out cancelled tasks:
- Projects: `progress: {total_tasks, completed_tasks, overdue_tasks, percent, milestones, milestones_completed}`
- Milestones: `progress: {total_tasks, completed_tasks, overdue_tasks, percent}`
- `percent` is rounded down, and is `0` when there are no tasks. `overdue_tasks` counts open tasks whose `due_at` has passed.
- A milestone is completed manually; it isn't completed automatically when its tasks are.

**Dates:** `start_date`, `target_date` and `due_date` are calendar dates (`YYYY-MM-DD`) with no time zone, so "1 Dec" stays 1 Dec for every viewer.

## Calendar events

| Method | Path | Notes |
|---|---|---|
| GET | `/events?from=&to=` | **Required** RFC 3339 range, at most 366 days. Returns every **occurrence** that overlaps the range, sorted by start; repeating events are expanded. Each item is the event plus `conflicts_with: [event ids]`. |
| POST | `/events` | See fields below. Returns `201 {"data": event, "conflicts": [occurrences]}` |
| GET | `/events/:id` | The event, or the series for a repeating event. |
| PATCH | `/events/:id` | Any field. Returns `{"data", "conflicts"}`. Changes a repeating event's **whole series**. |
| DELETE | `/events/:id` | `204`. Deletes the whole series. |

**Fields:**

| Field | Type | Notes |
|---|---|---|
| `title` | string | Required, ≤ 255 |
| `description` | string | Notes, ≤ 10 000 |
| `location` | string | ≤ 255 |
| `all_day` | bool | Default `false` |
| `starts_at`, `ends_at` | RFC 3339 | Required for timed events. End must be after start; at most 31 days long. |
| `start_date`, `end_date` | `YYYY-MM-DD` | Required for all-day events; the end date is inclusive and defaults to `start_date`. In responses, all-day events also have `starts_at` set to UTC midnight of the first day and `ends_at` to UTC midnight after the last day. |
| `buffer_before_minutes` | 0–1440 | Travel or prep time that blocks the calendar before the event starts |
| `busy` | bool | Default `true`. Free events never conflict. |
| `color` | `#rrggbb` | |
| `recurrence` | object | `{frequency, interval, until}`, same as tasks. Occurrences are calculated from the first start, so "monthly on the 31st" doesn't drift. |

**Switching kinds:** setting `all_day: true` with no dates keeps the event's start date. Setting `all_day: false` requires `starts_at` and `ends_at`.

**Conflicts** are warnings and never stop a save.
- Two **busy, timed** events of *different* events conflict when their blocked time overlaps. Blocked time runs from start minus `buffer_before_minutes` to end.
- Back-to-back events don't conflict.
- All-day and free (`busy: false`) events are never in conflict.
- On save, conflicts are checked across the event's span. A repeating event is checked over its next 90 days, or up to `until` if that's sooner. At most 20 conflicts are returned.

**All-day events and time zones:** all-day events are matched with ±1 day of padding, so the list can include an all-day event from just outside the range. Clients should place them by `start_date`/`end_date`.

**Not yet supported:** editing or deleting a single occurrence of a repeating event (exceptions), and attendees and invitations. Google Calendar sync comes in Step 6.1.

## Reminders

| Method | Path | Notes |
|---|---|---|
| GET | `/reminders` | Query: `status` (`scheduled` by default; also `sent`, `cancelled`, comma-separated values, or `all`), `task_id`, `event_id`, `page`, `per_page`. Ordered by next fire time; inactive reminders come last. |
| POST | `/reminders` | See the kinds below. |
| PATCH | `/reminders/:id` | `title` (standalone only), `note`, `remind_at`, `offset_minutes`, `follow_up_minutes`, `max_follow_ups`. Any change re-arms the reminder. You can't move a reminder to a different task or event. |
| DELETE | `/reminders/:id` | `204` |
| POST | `/reminders/:id/snooze` | `{"minutes": 1–10080}`. Fires again after that delay. Doesn't use up follow-ups. |

**Kinds** (`kind` in responses):

| Kind | Create with | Behaviour |
|---|---|---|
| `standalone` | `title`, `remind_at` (future), optional `note` | Fires once |
| `task` | `task_id`, plus `offset_minutes` (minutes before `due_at`, 0–43 200) **or** a fixed `remind_at` | Not delivered if the task is completed or cancelled by then (its status becomes `cancelled`). Offset reminders move when the due date changes, are inactive (`remind_at: null`) while there's no upcoming due date, and are copied to the next occurrence of a recurring task. |
| `event` | `event_id`, `offset_minutes` (required) | Fires before **each** occurrence of a repeating event, then re-arms for the next one. Moves when the event changes. |
| `note` | `note_id`, `offset_minutes` (required; e.g. 43 200 = 30 days) | Expiry reminder before the note's `expires_on`. The expiry is treated as 09:00 UTC on that date until Step 1.8 adds time zones. Moves when `expires_on` changes. Not delivered if the note is archived or its expiry date is removed. |

**Follow-ups** (tasks only): `follow_up_minutes` (5–10 080) and `max_follow_ups` (1–10, default 3). After the first reminder, LifeOS keeps reminding every `follow_up_minutes` while the task is still open, up to the maximum. `follow_ups_sent` counts them.

**Read-only fields:** `kind`, `subject_title` (the reminder's, task's or event's title), `occurs_at` (the due time or occurrence start that the next firing refers to), `status` (`scheduled`/`sent`/`cancelled`), `last_sent_at`.

## Notifications

The worker (`go run ./cmd/worker`) creates notifications. They are in-app only for now; email and push delivery come with integrations in Phase 6.

| Method | Path | Notes |
|---|---|---|
| GET | `/notifications` | Newest first. Query: `unread=true`, `page`, `per_page`. Fields: `kind`, `title`, `body`, `link`, `priority` (`high` for urgent or high-priority tasks), `reminder_id` (for snoozing), `occurs_at`, `read_at`. |
| GET | `/notifications/unread-count` | `{"data": {"unread": n}}` |
| POST | `/notifications/:id/read` | `204`. Safe to repeat. |
| POST | `/notifications/read-all` | `{"data": {"marked": n}}` |

**Delivery guarantees:** each reminder is claimed with `SELECT … FOR UPDATE SKIP LOCKED` in its own transaction. Each notification carries the unique key `reminder:<id>:<fire time>` and is inserted with `INSERT IGNORE`. Running several workers, or rerunning one after a crash, never duplicates a notification.

**Not yet:** quiet hours, escalation to other channels, and location-based reminders. Quiet hours come with user settings in Step 1.8; the other channels with integrations in Phase 6.

## Notes

Notes cover module 9: notes, ideas, links, documents and receipts.

| Method | Path | Notes |
|---|---|---|
| GET | `/notes` | Query: `q` (searches title, text, URL, tag names and attachment file names), `kind`, `tag_id`, `project_id`, `pinned=true\|false`, `archived=true` (shows the archive; the default list excludes it), `expiring_within_days` (also includes notes that have already expired; sorts by expiry), `sort` (`-updated_at` default with pinned first, `title`, `expires_on`), `page`, `per_page`. Each note includes `tags` and `file_count`. |
| POST | `/notes` | `title` (required, ≤ 255), `body` (≤ 100 000), `kind` (`note`/`idea`/`link`/`document`/`receipt`; default `note`), `url` (http/https only; required for `link`), `pinned`, `expires_on` (`YYYY-MM-DD`), `project_id`, `tags` (list of names) |
| GET | `/notes/:id` | Includes `files` |
| PATCH | `/notes/:id` | Any POST field, plus `archived`. `tags` **replaces** the set; `[]` clears it. Changing `expires_on` moves the note's expiry reminders. |
| DELETE | `/notes/:id` | `204`. Also deletes its files, from disk too, and its reminders. |

**Tags:** names are trimmed, de-duplicated without regard to case, at most 50 characters, and can't contain `,` or `#`. A note can have at most 20. Unknown names are created when you use them, so "Documents" and "documents" are the same tag.

| Method | Path | Notes |
|---|---|---|
| GET | `/tags` | Each tag includes `note_count`, which leaves out archived notes. |
| PATCH | `/tags/:id` | `name`, `color`. `422` if the new name already exists. |
| DELETE | `/tags/:id` | `204`. Removes the tag from every note. |

## Files (attachments)

| Method | Path | Notes |
|---|---|---|
| POST | `/notes/:id/files` | `multipart/form-data` with a single field named `file`. Returns `201` with `{id, note_id, name, content_type, size_bytes, sha256, created_at}`. A note can have at most 50 files. |
| GET | `/files/:id/download` | Streams the file to its owner. Images and PDFs open in the browser; `?download=1` forces a download. Every other type is always downloaded. |
| DELETE | `/files/:id` | `204`. Also removes it from disk. |

**Upload safety:**
- **Allowed extensions:** `.pdf .png .jpg .jpeg .gif .webp .txt .md .csv .docx .xlsx .pptx .doc .xls .zip`. Everything else is rejected (`422`), including HTML, SVG, scripts and executables.
- **Content must match the extension:** the first bytes are checked, so an HTML file renamed to `.pdf` is rejected.
- **Size:** at most `MAX_UPLOAD_MB` (default 20). Larger files get `413`.
- **Storage names:** files are stored under random names, `storage/<user id>/<random hex>`. The uploaded name is only for display, is cut down to its base name (`../../x.pdf` becomes `x.pdf`) and is stripped of control characters.
- **Download headers:** `X-Content-Type-Options: nosniff`, a sandboxing `Content-Security-Policy` and `Cache-Control: private, no-store`.
- **Not yet:** virus scanning, and searching inside file contents. Searching inside PDFs comes with the AI knowledge search in Step 2.4.

## Task lists

New accounts start with Personal, Work, Family and Shopping.

| Method | Path | Notes |
|---|---|---|
| GET | `/task-lists` | Ordered by `position`. Each list includes `open_task_count`. |
| POST | `/task-lists` | `name` (required, ≤ 100, unique per user, case-insensitive), `color` (`#rrggbb`) |
| PATCH | `/task-lists/:id` | `name`, `color`, `position` |
| DELETE | `/task-lists/:id` | `204`. The list's tasks are kept and have their `list_id` set to `null`. |
