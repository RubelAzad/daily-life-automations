# LifeOS AI — Local Setup

Stack: Next.js (frontend) → Go/Gin REST API (backend) → MySQL 8.

## Prerequisites

Git, Go 1.26+, Node.js 20+, MySQL 8 (this machine runs it as the `MySQL80` Windows service on port 3306).

## Database

```sql
CREATE DATABASE IF NOT EXISTS dailytask CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

Apply migrations (run again after every pull; it only applies new files):

```powershell
cd backend
go run ./cmd/migrate
```

Migrations are `backend/migrations/NNNNNN_name.up.sql`, embedded into the binary and tracked in the `schema_migrations` table. Add a new file with the next number; never edit one that has already been applied. `.down.sql` files are kept for manual rollback.

## Backend

```powershell
cd backend
copy .env.example .env   # then fill in DB_PASSWORD
go run ./cmd/api
```

Uploaded files are stored in `backend/storage/` (set `STORAGE_DIR` to change it; it's git-ignored). `MAX_UPLOAD_MB` limits the size of each file (default 20). Back this folder up together with the database, because notes refer to the files in it.

DB settings are separate variables (`DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`) and the DSN is built in code, so passwords with `@`, `#`, `/` need no URL-escaping. Quote the password in `.env` if it contains `#`.

Check: http://localhost:8080/api/v1/health → `{"database":"connected","status":"ok"}`

## AI assistant (optional)

AI features use the Claude API and stay off until a key is set. In `backend/.env`:

```
ANTHROPIC_API_KEY=sk-ant-...      # from https://console.anthropic.com/ — never commit it
AI_MODEL=claude-sonnet-5-5        # optional
```

Restart the API. The startup log says `AI enabled` or `AI disabled`. **AI** in the menu opens the chat; *Activity log & approvals* (`/ai/activity`) has a *Test connection* button. Each chat message and test is a real, paid call (usually a few cents or less).

For offline development, `AI_PROVIDER=fake` uses a stand-in that makes no network calls (only allowed with `APP_ENV=local`). It follows scripts typed in the chat:

| Script | Effect |
|---|---|
| `fake:tool create_task {"title":"Milk"}` | Calls that tool, then summarises the results. Join several with ` && `. |
| `fake:loop` | Calls a tool until the step limit is reached |
| `fake:error` | Fails at once |
| `fake:error-after-tool` | Calls one tool, then fails |

Executor tests that use the real database run separately: `go test -tags integration ./internal/ai/`.

## Worker (background jobs)

The worker is a separate process that runs background jobs: it turns due reminders into notifications, writes scheduled AI briefings, runs automations (Automations page) and keeps the search index current. Without it, reminders are saved but never delivered; Today and Settings show a warning when it isn't running.

```powershell
cd backend
go run ./cmd/worker            # system jobs every 30 s (set WORKER_INTERVAL_SECONDS to change it)
go run ./cmd/worker -once      # run every system job and anything else that is due, then exit
```

**Durable scheduler.** Jobs are rows in the `scheduled_jobs` table, so nothing is lost when the worker stops or crashes:

- Each job is claimed with `FOR UPDATE SKIP LOCKED` and a 2-minute lease that is renewed while it runs. If a worker dies mid-job, another worker (or the next start) takes the job over once the lease runs out.
- A failed attempt is retried after 30 s, 1 min, 2 min, … (up to an hour), at most 5 times for one-off jobs. Repeating jobs give up on a run after 3 attempts and wait for the next one.
- Adding a job with an idempotency key that already exists does nothing, so the same work is never scheduled twice. Jobs can be added in the same transaction as the change that needs them.
- Finished one-off jobs are deleted after 30 days (failed ones after 90).

It's safe to run more than one worker, or to restart one at any time: each system job runs in one worker at a time, reminders are also claimed with a row lock, and each notification has a unique key, so nothing is delivered twice.

Automations run here: their schedules, the events that trigger them (`task.created`, …, queued by the API in the same transaction as the change) and the overdue-task check. Without a worker they don't run, except **Run now**, which runs in the API.

Scheduled AI briefings are only made for users who turned them on in Settings, and only when AI is configured. Search indexing extracts text from uploads the API didn't get to (and from files uploaded before search existed) and re-indexes changed notes.

The worker respects each user's quiet hours (Settings page): reminders due in that window are held until it ends.

`-once` prints one line per job, then a total:

```
reminders: delivered=N skipped=N held=N errors=N
briefings: generated=N failed=N
search: indexed files=N notes=N
scheduler: pruned N finished jobs
jobs: succeeded=N retrying=N failed=N
```

## Frontend

```powershell
cd frontend
copy .env.example .env.local
npm install
npm run dev
```

Open http://localhost:3000. The home page shows the live API/database status and sign-in state; create an account at `/register`. After signing in you land on **Today** (`/today`): overdue and due tasks, today's events, reminders still to come, and expiring documents. Restart `npm run dev` after changing `.env.local`.

New accounts take the browser's time zone. Check it on the Settings page (your name in the header): the server uses it for repeating dates, expiry reminders and quiet hours, while screens use the device's time zone.

## Daily run order

1. MySQL running (the `MySQL80` service starts with Windows).
2. `cd backend; go run ./cmd/migrate` (after pulling new changes).
3. Terminal 1: `cd backend; go run ./cmd/api`
4. Terminal 2: `cd backend; go run ./cmd/worker`
5. Terminal 3: `cd frontend; npm run dev`, then open http://localhost:3000

## Notes

- `.env` and `.env.local` are git-ignored. Never commit them.
- CORS allows exactly `WEB_ORIGIN` (default `http://localhost:3000`).
- Using `root` is acceptable locally only; create a dedicated user before sharing or deploying.
