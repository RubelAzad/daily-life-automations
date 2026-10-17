# LifeOS AI — Step-by-Step Roadmap

All 20 modules from [DAILY_LIFE_MODULES.md](DAILY_LIFE_MODULES.md), split into small steps that are built in order. Each step is finished before the next one starts.

**A step is done when:** its migration is added, its API endpoints work, its UI page works, and it has been verified end to end against MySQL.

## Phase 1 — Core personal management

| Step | Scope | Module | Status |
|---|---|---|---|
| 1.1 | Foundation: migration runner, auth (register, login, logout, me), sessions | 20 | ✅ Done |
| 1.2 | Tasks CRUD: list (filter, paginate), create, view, update, delete; task lists | 2 | ✅ Done |
| 1.3 | Subtasks, recurring tasks, dependencies | 2 | ✅ Done |
| 1.4 | Projects, milestones, progress | 2 | ✅ Done |
| 1.5 | Calendar events CRUD, conflict detection | 3 | ✅ Done |
| 1.6 | Reminders, in-app notifications, first worker process | 4 | ✅ Done |
| 1.7 | Notes, tags, file uploads (local storage) | 9 | ✅ Done |
| 1.8 | User settings: time zone, quiet hours, approval preferences | 20 | ✅ Done |
| 1.9 | Dashboard "Today" view, carry-forward of unfinished tasks | 1 | ✅ Done |

## Phase 2 — AI assistant

| Step | Scope | Module | Status |
|---|---|---|---|
| 2.1 | LLM client, typed tool allowlist, AI audit log | 17 | ✅ Done |
| 2.2 | Chat endpoint: intent → planner → tools (`create_task`, `list_tasks`, …) | 17 | ✅ Done |
| 2.3 | AI daily plan, morning briefing, end-of-day summary | 1, 17 | ✅ Done |
| 2.4 | Personal knowledge search across notes and files | 9, 17 | ✅ Done |

## Phase 3 — Automation engine

| Step | Scope | Module | Status |
|---|---|---|---|
| 3.1 | Durable scheduler (`scheduled_jobs`), idempotency keys | 18 | ✅ Done |
| 3.2 | Automations: triggers, conditions, actions; internal event bus | 18 | ✅ Done |
| 3.3 | Workflow runs: history, retries, error handling | 18 | ⬜ Next |
| 3.4 | Approvals inbox (approve / reject) | 18, 20 | ⬜ |
| 3.5 | Built-in workflows: A (daily planning) and D (missed task recovery) | 1, 2, 18 | ⬜ |

## Phase 4 — Everyday life modules

| Step | Scope | Module | Status |
|---|---|---|---|
| 4.1 | Finance: expenses, income, budgets, recurring bills; workflow C | 5 | ⬜ |
| 4.2 | Subscriptions & renewals | 15 | ⬜ |
| 4.3 | Shopping lists, groceries, errands | 6 | ⬜ |
| 4.4 | Habits, routines, streaks | 7 | ⬜ |
| 4.5 | Contacts & important dates | 14 | ⬜ |
| 4.6 | Assets, warranties, maintenance | 16 | ⬜ |
| 4.7 | Family & household: members, shared tasks and chores | 11 | ⬜ |
| 4.8 | Work & career: workspaces, job applications | 12 | ⬜ |
| 4.9 | Learning: courses, study plans, revision reminders | 13 | ⬜ |
| 4.10 | Travel: trips, itineraries, checklists | 10 | ⬜ |
| 4.11 | Communication: email integration, drafts; workflow B | 8 | ⬜ |

## Phase 5 — Advanced intelligence

| Step | Scope | Module | Status |
|---|---|---|---|
| 5.1 | Reports, insights, goal tracking | 19 | ⬜ |
| 5.2 | Recommendations and cross-module workflows | 18, 19 | ⬜ |
| 5.3 | Voice commands | 17 | ⬜ |
| 5.4 | Mobile app on the same API | — | ⬜ |

## Phase 6 — Production readiness

| Step | Scope | Module | Status |
|---|---|---|---|
| 6.1 | Integrations: Google Calendar, email, connected accounts | 3, 20 | ⬜ |
| 6.2 | Security: login rate limiting, account-isolation tests, dedicated DB user | 20 | ⬜ |
| 6.3 | Data export, backups, account deletion | 20 | ⬜ |
| 6.4 | Monitoring and deployment | — | ⬜ |
