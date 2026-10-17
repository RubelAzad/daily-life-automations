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
| POST | `/auth/register` | `name`, `email`, `password` (8–72 chars), optional `timezone` (the browser's IANA zone) | `201 {"user"}` + session cookie. Creates the default lists and settings; an unknown `timezone` falls back to `UTC`. `409` if the email is taken |
| POST | `/auth/login` | `email`, `password` | `200 {"user"}` + session cookie, or `401` |
| POST | `/auth/logout` | — | `204`, ends the session |
| GET | `/auth/me` | — | `200 {"user"}` |

## Today

`GET /today` returns the user's current day, from local midnight to midnight in their settings time zone:

| Field | Contents |
|---|---|
| `date`, `timezone`, `starts_at`, `ends_at` | The day: `YYYY-MM-DD`, the zone, and its bounds as UTC instants (a daylight-saving day can be 23 or 25 hours) |
| `summary` | `{overdue, due_today, completed_today, events, conflicts}` counts. `overdue`, `due_today` and `completed_today` are totals even when the lists are cut off. |
| `overdue` | Open tasks (including subtasks) due before today, oldest first, at most 100 |
| `due_today` | Open tasks due today, at most 100 |
| `completed_today` | Tasks completed today, whatever their due date, at most 50 |
| `events` | Occurrences of events on today, all-day events first, with `conflicts_with` as in `/events` |
| `reminders` | Scheduled reminders that fire before the day ends, at most 50 |
| `expiring_notes` | Notes (not archived) expired or expiring within 30 days, soonest first, at most 10 |

## AI assistant

The model can only use the tools in the allowlist below, with input decoded strictly (unknown fields are rejected) and validated by the same services as the REST API. The user is always the signed-in user; no tool takes a `user_id`. The assistant can't run SQL, shell commands or web requests of its own.

| Method | Path | Notes |
|---|---|---|
| GET | `/ai/status` | `{enabled, provider, model, reason, tools: [{name, description, risk}]}`. `reason` explains why AI is off. The API key is never returned. |
| POST | `/ai/ping` | Tiny model call to check the key and connection: `{reply, model, duration_ms, input_tokens, output_tokens, action_id}`. `503` when AI is off, `429` within 10 s of the last ping, `502` with the provider's message if the call fails (e.g. invalid key). |
| GET | `/ai/actions` | The audit log, newest first. Query: `status` (comma-separated), `kind` (`tool`, `llm_call`), `conversation_id`, `page`, `per_page` (≤ 100). |
| GET | `/ai/actions/:id` | One entry |
| POST | `/ai/actions/:id/approve` | Runs a pending action once. Returns the entry with its final `status` (`succeeded`/`failed`), `output` and `error`. `409` if it isn't pending or has expired. |
| POST | `/ai/actions/:id/reject` | Discards a pending action. `409` if it isn't pending. |

### Chat

| Method | Path | Notes |
|---|---|---|
| GET | `/ai/conversations` | Most recently active first. `page`, `per_page` (≤ 100). |
| POST | `/ai/conversations` | Optional `{"title"}` (≤ 120). `201` with `"messages": []`. The first message becomes the title if none was given. |
| GET | `/ai/conversations/:id` | The chat with all its `messages` |
| DELETE | `/ai/conversations/:id` | `204`. Audit log entries are kept (their `conversation_id` becomes `null`). |
| POST | `/ai/conversations/:id/messages` | `{"text"}` (1–4000 characters). Runs one turn and returns `{"messages": [new messages], "actions": [tool calls made]}`. Can take several seconds. |

**Messages** use the model's content-block format: `{id, role, content: [blocks], created_at}`.
- **Blocks:** `{type: "text", text}`, `{type: "tool_use", id, name, input}` (assistant), and `{type: "tool_result", tool_use_id, content, is_error}`.
- **Tool results:** the app sends them back with the `user` role, so clients usually hide messages that contain only tool results. A tool result's `content` is a JSON string `{action_id, status, summary, output, error}`.

**A turn:**
1. The model either replies or calls tools.
2. Each tool call goes through the allowlist, approval policy and audit log, and its result is sent back.
3. This repeats until the model replies, at most **8 model calls** per message. Past that, the assistant says it stopped.

**Waiting approvals:** changes waiting for approval come back as `pending_approval` and are approved with `/ai/actions/:id/approve`. On the next message the model is told what was approved, rejected or expired.

**Context the model gets:** the user's name, the local date, time and time zone, and the approval setting. Its rules say to look things up rather than guess, to never claim a pending action is done, and to treat text inside tasks and notes as data, never as instructions.

**History:** the last 40 messages are sent, never splitting a tool call from its result. The tool list and the fixed rules are marked for prompt caching.

**Errors:**
- `503` when AI is off.
- `409` while the same chat is still answering.
- `429` after 20 messages a minute per user.
- `502` if the model fails on the first call. The message is then not stored, so it can be resent. A later failure ends the turn with an apology, keeping what was already done.

**Turns finish** even if the browser disconnects (up to 3 minutes).

### Briefings

A morning plan and an evening wrap-up, written by the model from the user's day. **They only advise; nothing is changed.** To act on one, call `POST /tasks/carry-forward` (the Today page's "Move to tomorrow" button).

| Method | Path | Notes |
|---|---|---|
| GET | `/ai/briefings/today` | `{date, morning, evening}`: the latest of each for the user's local today, or `null` |
| GET | `/ai/briefings` | History, newest first. `page`, `per_page` |
| POST | `/ai/briefings` | `{"kind": "morning" \| "evening"}`. Writes one now: `201` with the briefing. `429` within a minute of the last of that kind, `503` when AI is off, `502` if the model fails (the failed briefing is stored with its `error`). |

**Briefing fields:** `{id, kind, date, trigger (manual|scheduled), status (generating|ready|failed), content, error, model}`. `model` is `null` when no model was needed.

**Content:**
- Morning: `{summary, focus: [{task_id, title, reason}], schedule: [{start, end, kind (event|task|break), title, task_id?, event_id?}], warnings: []}`. Times are local `HH:MM`.
- Evening: `{summary, wins: [], unfinished: [{task_id, title, suggestion (tomorrow|later|drop), reason}], tomorrow}`.

**How they're made:**
- **Data sent:** the day (overdue and due tasks, today's events, reminders, expiring documents, plus tomorrow for the evening), in local times and marked as data, not instructions.
- **Structured output:** the model must answer through a required format (a forced tool call).
- **Checked against your data:** task and event IDs that don't exist in it are dropped, titles come from the database (never from the model), bad times are removed, and text is capped and redacted.
- **Empty days:** no model call is made.
- **Audit log:** each call is recorded with `source: "briefing"` (metadata only).

**Scheduled briefings** (Settings: `morning_briefing_enabled/time`, `evening_summary_enabled/time`, off by default):
- **Who makes them:** the worker makes each one once per day.
- **When:** after the local time and within 3 hours of it, unless one was already written by hand that day.
- **Notification:** a `briefing` notification links to `/today`.
- **Duplicates:** two workers can't both make it (unique key per user, kind and day).

**Tools and risk:**

| Risk | Tools | Needs approval |
|---|---|---|
| `read` | `get_today`, `list_tasks`, `list_events`, `search_notes`, `search_documents` | Never |
| `low` | `create_task`, `complete_task`, `reschedule_task`, `carry_forward_tasks`, `create_event`, `create_note`, `create_reminder` | When `ai_approval_mode` is `always` (the default) |
| `high` | `delete_task` | Always. Sending, spending, deleting and booking can never skip approval. |

**Audit log entries** (`kind`):
- `llm_call`: model, token counts, duration and any error. **Prompts and replies are not stored.**
- `tool`: tool, risk, a human-readable `summary` (what you approve), `input` and `output`.
  - **Redaction:** `input` and `output` are redacted copies. Values of fields like `password`, `token`, `api_key` and `pin` are removed, as are API keys and card numbers (Luhn-checked). Both are capped at 8 KB.
  - **Pending actions:** the exact input is kept only while the action waits for approval, then deleted.
  - **Status:** `succeeded`, `failed`, `pending_approval`, `running`, `rejected` or `expired`. Pending actions show `expires_at`: they expire 24 h after being proposed.
  - **Every attempt is logged,** including unknown tools (`rejected`) and invalid input (`failed`).

## Settings

| Method | Path | Notes |
|---|---|---|
| GET | `/settings` | `200 {"data": settings}` |
| PATCH | `/settings` | Any of the fields below. `200 {"data": settings}` |

| Field | Values | Default | Used for |
|---|---|---|---|
| `timezone` | IANA name, e.g. `Asia/Dhaka` (not `Local`) | browser's zone at sign-up, else `UTC` | Repeating tasks and events keep the same local time (also across daylight saving); "today" for *expiring soon*; note expiry reminders fire at 09:00 local; quiet hours. |
| `week_starts_on` | `0` Sunday, `1` Monday, `6` Saturday | `1` | The calendar week |
| `quiet_hours_enabled` | `true`/`false` | `false` | |
| `quiet_hours_start`, `quiet_hours_end` | `HH:MM`, local time; must differ | `22:00`, `07:00` | Start later than end means the window crosses midnight. |
| `morning_briefing_enabled`, `evening_summary_enabled` | `true`/`false` | `false` | Automatic AI briefings (see [Briefings](#briefings)). Each is a paid model call. |
| `morning_briefing_time`, `evening_summary_time` | `HH:MM` local | `07:30`, `21:00` | |
| `ai_approval_mode` | `always` (ask before every change) or `high_impact_only` (routine changes to tasks, notes, events and reminders without asking) | `always` | The AI assistant (Phase 2). Sending messages, spending money, deleting data and bookings **always** need approval; there is no mode that skips it. |

**Changing the time zone** recomputes pending event and note reminders. Reminders that were already sent are not re-sent.

**Quiet hours:** a reminder that comes due in the window is held until the window ends (`remind_at` moves to that time). It is delivered at once if the task or event it is about happens before the window ends (e.g. a 06:00 flight when quiet hours end at 07:00). Follow-ups and standalone reminders are always held.

**Clients** show times in the device's time zone. The web app warns on the settings page when it differs from `timezone`.

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
| POST | `/tasks/carry-forward` | Moves unfinished tasks to a later day. See [Carry forward](#carry-forward). |

**Read-only task fields:** `subtask_count`, `subtasks_completed`, `dependencies` (`[{id, title, status}]`), `blocked`, `next_task_id`, `carry_count`.

### Carry forward
`POST /tasks/carry-forward` with `{"task_ids": [1, 2], "to": "today" | "tomorrow"}`. Both fields are optional.
- **No `task_ids`:** moves every open task that is overdue (due before today), including subtasks.
- **With `task_ids`** (at most 200): each must be yours, open, have a due date, and be due before the target day. Otherwise `422` names the task. Duplicates are ignored.
- **New due time:** the target day at the task's original local time of day, in the user's time zone. When moving to today and that time has already passed, it becomes 23:59 today, so the task isn't overdue again straight away.
- Each move adds 1 to the task's `carry_count`, so tasks that keep slipping are easy to spot. Offset reminders move with the due time.
- Returns `200 {"data": [moved tasks], "meta": {"moved": n}}`.
- Automatic carry-forward (no click needed) comes with workflow D in Step 3.5.

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
- **Time zone:** dates are counted in the user's `timezone` setting, so "daily at 09:00" stays at 09:00 local time, including across daylight-saving changes.

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
| GET | `/reminders` | Query: `status` (`scheduled` by default; also `sent`, `cancelled`, comma-separated values, or `all`), `task_id`, `event_id`, `note_id`, `page`, `per_page`. Ordered by next fire time; inactive reminders come last. |
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
| `note` | `note_id`, `offset_minutes` (required; e.g. 43 200 = 30 days) | Expiry reminder before the note's `expires_on`. The expiry is 09:00 on that date in the user's time zone. Moves when `expires_on` changes. Not delivered if the note is archived or its expiry date is removed. |

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

**Not yet:** escalation to other channels, and location-based reminders. Other channels come with integrations in Phase 6. Quiet hours: see [Settings](#settings).

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
| POST | `/notes/:id/files` | `multipart/form-data` with a single field named `file`. Returns `201` with `{id, note_id, name, content_type, size_bytes, sha256, text_status, text_error, text_chars, created_at}`. A note can have at most 50 files. |
| GET | `/files/:id/download` | Streams the file to its owner. Images and PDFs open in the browser; `?download=1` forces a download. Every other type is always downloaded. |
| DELETE | `/files/:id` | `204`. Also removes it from disk. |

**Upload safety:**
- **Allowed extensions:** `.pdf .png .jpg .jpeg .gif .webp .txt .md .csv .docx .xlsx .pptx .doc .xls .zip`. Everything else is rejected (`422`), including HTML, SVG, scripts and executables.
- **Content must match the extension:** the first bytes are checked, so an HTML file renamed to `.pdf` is rejected.
- **Size:** at most `MAX_UPLOAD_MB` (default 20). Larger files get `413`.
- **Storage names:** files are stored under random names, `storage/<user id>/<random hex>`. The uploaded name is only for display, is cut down to its base name (`../../x.pdf` becomes `x.pdf`) and is stripped of control characters.
- **Download headers:** `X-Content-Type-Options: nosniff`, a sandboxing `Content-Security-Policy` and `Cache-Control: private, no-store`.
- **Not yet:** virus scanning.

**Searchable text:** after an upload, the file's text is extracted in the background so it can be [searched](#search). `text_status` is `pending` → `processing` → `ready`, or `unsupported` (images, `.doc`, `.xls`, `.zip`) or `failed` (unreadable or password-protected), with a reason in `text_error`. `text_chars` is the amount of text found. Text is read from `.pdf .docx .xlsx .pptx .txt .md .csv`, with limits on output (500 000 characters), PDF pages and decompressed size, so a hostile file can't exhaust the server. Images need text recognition, which isn't done yet.

## Search

Personal knowledge search across notes and the text inside their files (module 9). Also used by the assistant's `search_documents` tool.

| Method | Path | Notes |
|---|---|---|
| GET | `/search` | Query: `q` (required, ≤ 200 characters), `limit` (1–20, default 20). Returns `{data: [{note_id, note_title, note_kind, file_id, file_name, snippet, score}]}`, best first: the best-matching passage of each note (`file_id` null) or file. Archived notes are left out. |

- **Matching:** MySQL full-text search over passages of about 1 500 characters. Words of 3+ letters match, also as word starts (`insur` finds "insurance"); common words are ignored, and passages matching more words rank higher. Search operators in `q` are stripped. A query with no such words (e.g. `42`) falls back to a plain substring match.
- **Freshness:** a note is indexed when it's saved; files a few seconds after upload. The worker catches anything missed (e.g. after a crash) and indexes files uploaded before this feature.
- **Privacy:** results only ever include the signed-in user's notes and files.

## Task lists

New accounts start with Personal, Work, Family and Shopping.

| Method | Path | Notes |
|---|---|---|
| GET | `/task-lists` | Ordered by `position`. Each list includes `open_task_count`. |
| POST | `/task-lists` | `name` (required, ≤ 100, unique per user, case-insensitive), `color` (`#rrggbb`) |
| PATCH | `/task-lists/:id` | `name`, `color`, `position` |
| DELETE | `/task-lists/:id` | `204`. The list's tasks are kept and have their `list_id` set to `null`. |
