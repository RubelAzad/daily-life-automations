# LifeOS AI — Daily Life Modules

LifeOS AI is a **unified personal operating system**, not a collection of separate apps. It manages and automates all of daily life: planning, work, family, money, health routines, communication, learning and AI-powered automation.

Each module plugs into the same shared core:

| Shared core | Role |
|---|---|
| **Tasks** | Anything actionable becomes a task: a bill, an email request, a chore or a study session. |
| **Calendar** | Anything time-bound becomes an event or a time block. |
| **Notifications** | One reminder and notification pipeline, with one set of priority, quiet-hours and escalation rules. |
| **AI Assistant** | One assistant that works with every module through typed tools. |
| **Automation Engine** | Every module emits triggers and exposes actions in the same Trigger → Condition → Action model. |
| **Audit & Approvals** | Every AI or automation action is logged. High-impact actions wait for approval. |

> **Rule for every new module:** reuse the shared core. A finance bill creates *reminders* and a *task*. It does not get its own reminder system.

---

## 1. Module catalogue

Modules marked ★ are **core**, and the rest of the product depends on them.

### 1. Personal Dashboard & Daily Planner ★ — Phase 1
- Today's tasks, schedule, reminders, priorities and daily goals.
- AI-generated daily plan based on deadlines and available time.
- Morning briefing and end-of-day summary.
- Unfinished tasks carry forward automatically.

### 2. Tasks, To-Do & Projects ★ — Phase 1
- Personal, work, family, shopping and custom task lists.
- Subtasks, recurring tasks, deadlines, priorities and dependencies.
- Projects, milestones, progress tracking and notes.
- Create tasks from natural language, voice or messages.

### 3. Calendar & Time Management — Phase 1
- Meetings, appointments, events and personal schedules.
- Google Calendar integration and conflict detection.
- Travel time, preparation time and schedule suggestions.
- Automatic rescheduling when plans change.

### 4. Smart Reminders & Notifications — Phase 1
- Time-based, location-based and event-based reminders.
- Follow-up reminders while a task stays incomplete.
- Email, push and messaging notifications through supported integrations.
- Notification priority, quiet hours and reminder escalation.

### 5. Personal Finance & Bills — Phase 4
- Daily expenses, income, budgets, savings and debt tracking.
- Reminders for rent, electricity, internet, subscriptions and loan payments.
- Monthly financial summaries and spending alerts.
- Recurring payment schedules and expense categorization.

### 6. Shopping, Groceries & Errands — Phase 4
- Shopping lists, household supplies and price notes.
- Grocery lists generated from meal plans.
- Tracking of pending purchases, returns, deliveries and orders.
- Nearby errands combined into one trip.

### 7. Habits, Routines & Wellness — Phase 4
- Morning and evening routines.
- Exercise, sleep schedules, water intake and habit tracking.
- Personal goals, streaks and weekly progress.
- Routine reminders and schedule adjustments.

### 8. Email, Messages & Communication — Phase 4
- Summaries of emails, with important messages flagged.
- Drafted replies, follow-ups and meeting requests.
- Messages converted into tasks or calendar events.
- Tracking of pending replies and unanswered requests.
- **Sending requires approval by default.**

### 9. Notes, Knowledge & Document Manager — Phase 1 (notes), Phase 2 (AI search)
- Notes, ideas, links, files, receipts and important documents.
- Search across personal notes and uploaded files.
- Expiry reminders for documents and memberships.
- AI summaries, tagging and searchable personal knowledge.

### 10. Travel & Transportation — Phase 4
- Trip itineraries, tickets, hotel bookings and travel checklists.
- Departure reminders and document checklists.
- Transport schedules, route planning and estimated travel time.
- Travel expenses and reservations kept together.

### 11. Family & Household Management — Phase 4
- Shared calendars, family events and household chores.
- Reminders for maintenance, utility bills and home supplies.
- Tasks assigned to household members.
- Family shopping and meal planning.

### 12. Work & Career Management — Phase 4
- Work tasks, meetings, deadlines and follow-ups.
- CV, job applications, interview preparation and career goals.
- Learning plans, work notes and performance tracking.
- **Separate personal and professional workspaces.**

### 13. Learning & Personal Development — Phase 4
- Courses, reading lists, study schedules and practice sessions.
- Learning goals and progress tracking.
- AI-generated study plans and revision reminders.
- Saved resources and summaries of learning materials.

### 14. Contacts & Important Dates — Phase 4
- Contacts, birthdays, anniversaries and special occasions.
- Follow-up reminders and meeting notes.
- Relationship reminders and gift ideas.
- Contact history and communication preferences.

### 15. Subscriptions & Renewals — Phase 4
- Tracking of streaming, software, internet, memberships and other recurring charges.
- Renewal and cancellation reminders.
- Monthly vs. yearly cost comparison.
- Unused subscriptions flagged for review.

### 16. Assets, Warranties & Maintenance — Phase 4
- Phones, computers, appliances, vehicles and other assets.
- Warranty periods, service history and maintenance schedules.
- Insurance and registration renewal reminders.
- Stored receipts, manuals and purchase information.

### 17. AI Personal Assistant ★ — Phase 2
- Chat and voice commands in natural language.
- Understands requests such as "Plan my day" or "Remind me to pay the bill."
- Searches personal tasks, notes and schedules.
- Turns requests into structured actions and explains what it did.

### 18. Workflow Automation Builder ★ — Phase 3
- Trigger → Condition → Action workflows.
- Schedules: daily, weekly, monthly or at a specific time.
- Runs when tasks, emails or events change.
- Retries, execution history, error handling and approval steps.

### 19. Reports, Insights & Goal Tracking — Phase 5
- Daily, weekly and monthly summaries.
- Time usage, expenses, task completion and habits.
- Goal progress and overdue-item reports.
- AI suggestions to improve productivity and routines.

### 20. Integrations, Security & Administration — Phase 1 (settings), Phase 6 (full)
- Connected calendar, email, messaging and other supported services.
- Management of permissions, connected accounts and API credentials.
- AI activity, automation logs and approvals.
- Data export, backups, privacy controls and account deletion.

---

## 2. How modules connect to the core

| # | Module | Owns (tables) | Creates in core | Automation triggers it emits |
|---|---|---|---|---|
| 1 | Dashboard & Planner | `daily_plans` | time blocks, notifications | `plan.generated` |
| 2 | Tasks & Projects | `tasks`, `projects`, `task_lists` | — (is core) | `task.created`, `task.overdue`, `task.completed` |
| 3 | Calendar | `calendar_events` | — (is core) | `event.upcoming`, `event.changed`, `event.conflict` |
| 4 | Reminders | `reminders`, `notifications` | — (is core) | `reminder.due` |
| 5 | Finance & Bills | `expenses`, `incomes`, `budgets`, `recurring_bills` | reminders, tasks | `bill.due_soon`, `budget.exceeded` |
| 6 | Shopping & Errands | `shopping_lists`, `shopping_items` | tasks | `item.added`, `order.delivered` |
| 7 | Habits & Wellness | `habits`, `habit_logs`, `routines` | reminders, time blocks | `habit.missed`, `streak.broken` |
| 8 | Communication | `messages` (synced metadata), `drafts` | tasks, events, approvals | `email.received`, `reply.pending` |
| 9 | Notes & Documents | `notes`, `files`, `tags` | reminders (expiry) | `document.expiring` |
| 10 | Travel | `trips`, `trip_items` | events, checklist tasks, expenses | `trip.departure_soon` |
| 11 | Family & Household | `households`, `household_members` | shared tasks, events | `chore.assigned` |
| 12 | Work & Career | `workspaces`, `job_applications` | tasks, events | `application.followup_due` |
| 13 | Learning | `courses`, `learning_goals` | study events, reminders | `revision.due` |
| 14 | Contacts & Dates | `contacts`, `important_dates` | reminders | `date.upcoming` |
| 15 | Subscriptions | `subscriptions` | reminders, expenses | `subscription.renewal_soon` |
| 16 | Assets & Warranties | `assets`, `maintenance_logs` | reminders, files | `warranty.expiring`, `maintenance.due` |
| 17 | AI Assistant | `ai_conversations`, `ai_messages` | anything, through allowlisted tools | `assistant.action_taken` |
| 18 | Automation Builder | `automations`, `workflow_runs`, `scheduled_jobs` | anything, through actions | `workflow.failed` |
| 19 | Reports & Goals | `goals`, `report_snapshots` | notifications | `goal.at_risk` |
| 20 | Integrations & Admin | `integrations`, `user_settings`, `audit_logs`, `approvals` | — | `integration.disconnected` |

Every table that holds user data has a `user_id` column, and every query filters on it.

---

## 3. Automatic workflows (cross-module examples)

The modules become most useful when they work together. Each example below is a normal Automation Engine workflow. None of them is a special case written into the code.

### Example A — Automatic daily planning
| Step | Detail |
|---|---|
| Trigger | Schedule, every day at 07:00 (user's time zone) |
| Gather | Today's tasks, calendar events and pending reminders |
| AI | Prioritizes by deadline, priority and free time between events |
| Action | Saves a `daily_plan` and sends the morning-briefing notification |
| Approval | None (read-only plus a notification) |

### Example B — Turn an email into a task
| Step | Detail |
|---|---|
| Trigger | `email.received` from an authorized integration |
| AI | Extracts the request, the deadline and a suggested action |
| Action | Drafts a task linked to the source email |
| Approval | Saved automatically **or** sent for confirmation, depending on user settings |

### Example C — Monthly bill management
| Step | Detail |
|---|---|
| Trigger | `bill.due_soon` (for example, 3 days before the due date) |
| Action | Sends a reminder with the expected amount and creates a "Pay bill" task |
| On completion | When the task is marked paid: records an `expense` and schedules the next due date |
| Approval | None. LifeOS never makes payments itself. |

### Example D — Missed task recovery
| Step | Detail |
|---|---|
| Trigger | `task.overdue` |
| AI | Checks priority and calendar availability, then proposes a new time |
| Action | Reschedules the task and updates today's plan |
| Approval | According to the user's settings (default: ask) |

### Approval policy (defaults)
| Action type | Default |
|---|---|
| Read, summarize, notify the user | Automatic |
| Create or update the user's own tasks, notes or events | Automatic (configurable) |
| Reschedule existing commitments | Ask |
| Send messages or emails to other people | **Always ask** |
| Delete data, spend money, make bookings | **Always ask** |

---

## 4. Development order

Do not build all 20 modules at once. Build the foundation first, then expand.

| Phase | Focus | Modules / scope |
|---|---|---|
| **1** | Core personal management | Dashboard (1), Tasks & Projects (2), Calendar (3), Reminders (4), Notes (9), user settings (20) |
| **2** | AI assistant | Assistant (17): natural-language commands, intent detection, task creation, daily summaries, knowledge search |
| **3** | Automation engine | Automation Builder (18): triggers, conditions, actions, schedules, history, retries, approvals |
| **4** | Everyday life modules | Finance (5), Shopping (6), Habits (7), Communication (8), Travel (10), Family (11), Career (12), Learning (13), Contacts (14), Subscriptions (15), Assets (16) |
| **5** | Advanced intelligence | Reports (19), recommendations, cross-module workflows, voice commands, mobile app |
| **6** | Production readiness | Integrations & Admin (20): security, monitoring, backups, testing, deployment |

**Current status:** Phase 1 has started. The API, the MySQL connection and the health check are running, and the `users` and `tasks` tables exist. See [LOCAL_SETUP.md](LOCAL_SETUP.md).

---

## 5. Technology architecture

| Layer | Choice |
|---|---|
| Frontend | Next.js + TypeScript |
| Backend | Go + Gin REST API |
| Database | MySQL 8 (`dailytask`) |
| Background processing | Go worker process with a durable, MySQL-backed scheduler and idempotency keys |
| AI | Go-based agent orchestration (intent → planner → policy check → typed tools → verifier) calling an external LLM API. Keys stay on the server. |
| File storage | Local disk for development, private object storage in production |
| Mobile | Dedicated app later, using the same Go API |

### Planned MySQL tables
Grouped by phase. Tables are added as versioned migrations in `backend/migrations` when their phase starts.

- **Phase 1:** `users` ✅, `tasks` ✅, `projects`, `task_lists`, `calendar_events`, `reminders`, `notifications`, `notes`, `tags`, `files`, `user_settings`, `daily_plans`
- **Phase 2:** `ai_conversations`, `ai_messages`
- **Phase 3:** `automations`, `workflow_runs`, `scheduled_jobs`, `approvals`, `audit_logs`
- **Phase 4:** `expenses`, `incomes`, `budgets`, `recurring_bills`, `shopping_lists`, `shopping_items`, `habits`, `habit_logs`, `routines`, `messages`, `drafts`, `trips`, `trip_items`, `households`, `household_members`, `workspaces`, `job_applications`, `courses`, `learning_goals`, `contacts`, `important_dates`, `subscriptions`, `assets`, `maintenance_logs`
- **Phase 5–6:** `goals`, `report_snapshots`, `integrations`
