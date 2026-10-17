"use client";

import Link from "next/link";
import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react";
import AppHeader, { ErrorBanner } from "@/components/AppHeader";
import BriefingPanel from "@/components/today/BriefingPanel";
import { carryForward, createTask, deviceTimeZone, getToday, updateTask, type EventOccurrence, type Task, type Today } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatDate, formatDue, formatTime, fromDateInputEndOfDay, isPastDate } from "@/lib/format";
import { useRequireUser } from "@/lib/useRequireUser";

/** "Saturday, 17 October" for a "YYYY-MM-DD" date. */
function longDate(value: string): string {
  const [y, m, d] = value.split("-").map(Number);
  return new Date(y, m - 1, d).toLocaleDateString(undefined, { weekday: "long", day: "numeric", month: "long" });
}

function greeting(now: Date): string {
  const h = now.getHours();
  return h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
}

export default function TodayView() {
  const { user, error: authError } = useRequireUser();
  const [today, setToday] = useState<Today | null>(null);
  const [device, setDevice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [version, setVersion] = useState(0);
  const [busy, setBusy] = useState(false);
  const [title, setTitle] = useState("");
  const [showDone, setShowDone] = useState(false);

  const showError = useCallback((err: unknown) => setError(errorMessage(err)), []);

  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    getToday()
      .then((t) => {
        if (cancelled) return;
        setToday(t);
        setDevice(deviceTimeZone());
      })
      .catch(showError);
    return () => {
      cancelled = true;
    };
  }, [user, version, showError]);

  async function run(action: () => Promise<string | void>) {
    setBusy(true);
    try {
      const message = await action();
      setNotice(message || null);
      setVersion((v) => v + 1);
    } catch (err) {
      showError(err);
    } finally {
      setBusy(false);
    }
  }

  const setStatus = (task: Task, done: boolean) =>
    run(async () => {
      await updateTask(task.id, { status: done ? "completed" : "pending" });
    });

  const move = (to: "today" | "tomorrow", ids?: number[]) =>
    run(async () => {
      const { meta } = await carryForward({ task_ids: ids, to });
      return `Moved ${meta.moved} task${meta.moved === 1 ? "" : "s"} to ${to}.`;
    });

  function onAdd(event: FormEvent) {
    event.preventDefault();
    if (!today || !title.trim()) return;
    run(async () => {
      await createTask({ title: title.trim(), due_at: fromDateInputEndOfDay(today.date) });
      setTitle("");
    });
  }

  if (!user) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  const control = "rounded-md border border-zinc-300 bg-transparent px-3 py-1.5 text-sm dark:border-zinc-700";
  const small = "rounded border border-zinc-300 px-2 py-0.5 text-xs hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800";

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-6 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      {!today ? (
        <p className="text-zinc-500">Loading your day…</p>
      ) : (
        <>
          <div className="flex flex-col gap-1">
            <p className="text-sm text-zinc-500">
              {greeting(new Date())}, {user.name}
            </p>
            <h1 className="text-2xl font-semibold tracking-tight">{longDate(today.date)}</h1>
            <Summary today={today} />
            {device && device !== today.timezone && (
              <p className="text-xs text-amber-700 dark:text-amber-400">
                Your day is based on {today.timezone.replaceAll("_", " ")}, but this device is set to {device.replaceAll("_", " ")}.{" "}
                <Link href="/settings" className="underline">
                  Check settings
                </Link>
              </p>
            )}
          </div>

          <BriefingPanel
            openTaskIds={new Set([...today.overdue, ...today.due_today].map((t) => t.id))}
            onChanged={() => setVersion((v) => v + 1)}
          />

          {notice && (
            <p role="status" className="text-sm text-green-700 dark:text-green-400">
              {notice}
            </p>
          )}

          <form onSubmit={onAdd} className="flex gap-2">
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Add a task for today…"
              maxLength={255}
              aria-label="New task for today"
              className={`${control} min-w-0 flex-1`}
            />
            <button type="submit" disabled={busy || !title.trim()} className="rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900">
              Add
            </button>
          </form>

          {today.overdue.length > 0 && (
            <Section
              title={`Overdue (${today.summary.overdue})`}
              tone="warn"
              actions={
                <>
                  <button type="button" disabled={busy} onClick={() => move("today")} className={small}>
                    Move all to today
                  </button>
                  <button type="button" disabled={busy} onClick={() => move("tomorrow")} className={small}>
                    Move all to tomorrow
                  </button>
                </>
              }
            >
              <TaskList tasks={today.overdue} busy={busy} onToggle={setStatus}>
                {(t) => (
                  <>
                    <button type="button" disabled={busy} onClick={() => move("today", [t.id])} className={small}>
                      Today
                    </button>
                    <button type="button" disabled={busy} onClick={() => move("tomorrow", [t.id])} className={small}>
                      Tomorrow
                    </button>
                  </>
                )}
              </TaskList>
              {today.summary.overdue > today.overdue.length && (
                <p className="px-3 pb-2 text-xs text-zinc-500">
                  Showing the oldest {today.overdue.length}. &ldquo;Move all&rdquo; moves every overdue task.
                </p>
              )}
            </Section>
          )}

          <div className="grid gap-6 md:grid-cols-2">
            <Section title={`Due today (${today.summary.due_today})`}>
              {today.due_today.length === 0 ? (
                <Empty>Nothing due today.</Empty>
              ) : (
                <TaskList tasks={today.due_today} busy={busy} onToggle={setStatus}>
                  {(t) => (
                    <button type="button" disabled={busy} onClick={() => move("tomorrow", [t.id])} className={small} title="Move to tomorrow">
                      Tomorrow
                    </button>
                  )}
                </TaskList>
              )}
            </Section>

            <Section
              title="Schedule"
              actions={
                <Link href="/calendar" className="text-xs underline">
                  Calendar
                </Link>
              }
            >
              {today.events.length === 0 ? <Empty>No events today.</Empty> : <EventList events={today.events} />}
            </Section>
          </div>

          <div className="grid gap-6 md:grid-cols-2">
            <Section
              title="Reminders still to come"
              actions={
                <Link href="/reminders" className="text-xs underline">
                  All reminders
                </Link>
              }
            >
              {today.reminders.length === 0 ? (
                <Empty>No more reminders today.</Empty>
              ) : (
                <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
                  {today.reminders.map((r) => (
                    <li key={r.id} className="flex items-baseline gap-3 px-3 py-2 text-sm">
                      <span className="w-12 shrink-0 text-zinc-500 tabular-nums">{r.remind_at && formatTime(r.remind_at)}</span>
                      <span className="min-w-0 wrap-break-word">{r.subject_title}</span>
                      <span className="ml-auto text-xs text-zinc-500 capitalize">{r.kind}</span>
                    </li>
                  ))}
                </ul>
              )}
            </Section>

            <Section
              title="Expiring soon"
              actions={
                <Link href="/notes" className="text-xs underline">
                  Notes
                </Link>
              }
            >
              {today.expiring_notes.length === 0 ? (
                <Empty>Nothing expires in the next 30 days.</Empty>
              ) : (
                <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
                  {today.expiring_notes.map((n) => (
                    <li key={n.id} className="flex items-baseline gap-3 px-3 py-2 text-sm">
                      <Link href={`/notes?id=${n.id}`} className="min-w-0 wrap-break-word underline-offset-2 hover:underline">
                        {n.title}
                      </Link>
                      {n.expires_on && (
                        <span className={`ml-auto shrink-0 text-xs ${isPastDate(n.expires_on) ? "text-red-600 dark:text-red-400" : "text-zinc-500"}`}>
                          {isPastDate(n.expires_on) ? "Expired " : ""}
                          {formatDate(n.expires_on)}
                        </span>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </Section>
          </div>

          {today.completed_today.length > 0 && (
            <Section
              title={`Done today (${today.summary.completed_today})`}
              actions={
                <button type="button" onClick={() => setShowDone((s) => !s)} className="text-xs underline" aria-expanded={showDone}>
                  {showDone ? "Hide" : "Show"}
                </button>
              }
            >
              {showDone && <TaskList tasks={today.completed_today} busy={busy} onToggle={setStatus} />}
            </Section>
          )}
        </>
      )}
    </div>
  );
}

function Summary({ today }: { today: Today }) {
  const s = today.summary;
  const parts = [
    s.overdue > 0 && `${s.overdue} overdue`,
    `${s.due_today} due today`,
    `${s.events} event${s.events === 1 ? "" : "s"}`,
    s.conflicts > 0 && `${s.conflicts} overlapping`,
    `${s.completed_today} done`,
  ].filter(Boolean);
  return <p className="text-sm text-zinc-600 dark:text-zinc-400">{parts.join(" · ")}</p>;
}

function Section({ title, tone, actions, children }: { title: string; tone?: "warn"; actions?: ReactNode; children: ReactNode }) {
  const border = tone === "warn" ? "border-amber-300 dark:border-amber-800" : "border-zinc-200 dark:border-zinc-800";
  return (
    <section className={`flex flex-col rounded-md border ${border}`} aria-label={title}>
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-inherit px-3 py-2">
        <h2 className="text-sm font-medium">{title}</h2>
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </div>
      {children}
    </section>
  );
}

function Empty({ children }: { children: ReactNode }) {
  return <p className="px-3 py-4 text-sm text-zinc-500">{children}</p>;
}

function TaskList({
  tasks,
  busy,
  onToggle,
  children,
}: {
  tasks: Task[];
  busy: boolean;
  onToggle: (task: Task, done: boolean) => void;
  children?: (task: Task) => ReactNode;
}) {
  return (
    <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
      {tasks.map((t) => {
        const done = t.status === "completed";
        return (
          <li key={t.id} className="flex items-start gap-3 px-3 py-2">
            <input
              type="checkbox"
              checked={done}
              disabled={busy || (!done && t.blocked)}
              onChange={(e) => onToggle(t, e.target.checked)}
              aria-label={done ? `Reopen ${t.title}` : `Complete ${t.title}`}
              title={t.blocked && !done ? "Waiting on another task" : undefined}
              className="mt-1"
            />
            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className={`text-sm wrap-break-word ${done ? "text-zinc-500 line-through" : ""}`}>{t.title}</span>
              <span className="flex flex-wrap gap-x-2 text-xs text-zinc-500">
                {t.due_at && !done && <span>{formatDue(t.due_at)}</span>}
                {(t.priority === "high" || t.priority === "urgent") && <span className="text-red-600 capitalize dark:text-red-400">{t.priority}</span>}
                {t.parent_id && <span>Subtask</span>}
                {t.blocked && !done && <span>Blocked</span>}
                {t.carry_count > 0 && (
                  <span className="text-amber-700 dark:text-amber-400" title="Times carried forward unfinished">
                    Carried {t.carry_count}×
                  </span>
                )}
              </span>
            </div>
            {children && !done && <div className="flex shrink-0 gap-1">{children(t)}</div>}
          </li>
        );
      })}
    </ul>
  );
}

function EventList({ events }: { events: EventOccurrence[] }) {
  return (
    <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
      {events.map((e) => (
        <li key={`${e.id}-${e.starts_at}`} className="flex items-baseline gap-3 px-3 py-2 text-sm">
          <span className="w-24 shrink-0 text-zinc-500 tabular-nums">{e.all_day ? "All day" : `${formatTime(e.starts_at)}–${formatTime(e.ends_at)}`}</span>
          <span className="flex min-w-0 flex-col">
            <span className="wrap-break-word">
              {e.color && <span className="mr-1.5 inline-block size-2 rounded-full" style={{ backgroundColor: e.color }} aria-hidden />}
              {e.title}
            </span>
            {e.location && <span className="text-xs text-zinc-500">{e.location}</span>}
          </span>
          {e.conflicts_with.length > 0 && <span className="ml-auto shrink-0 text-xs text-amber-700 dark:text-amber-400">Overlaps</span>}
        </li>
      ))}
    </ul>
  );
}
