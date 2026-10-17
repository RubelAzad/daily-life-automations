"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import AppHeader, { ErrorBanner } from "@/components/AppHeader";
import { getSettings, listEvents, listTasks, updateTask, type EventOccurrence, type Task } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatTime, localDateKey } from "@/lib/format";
import { useRequireUser } from "@/lib/useRequireUser";
import EventEditor, { type EditorTarget } from "./EventEditor";

/** Midnight of the first day of d's week; weekStartsOn: 0 Sunday, 1 Monday, 6 Saturday. */
function startOfWeek(d: Date, weekStartsOn: number): Date {
  const day = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const diff = (day.getDay() - weekStartsOn + 7) % 7;
  day.setDate(day.getDate() - diff);
  return day;
}

function addDays(d: Date, n: number): Date {
  const out = new Date(d);
  out.setDate(out.getDate() + n);
  return out;
}

export default function CalendarView() {
  const { user, error: authError } = useRequireUser();
  // Weeks relative to the current one. The actual dates are only computed
  // after sign-in has loaded, never during prerendering (cacheComponents
  // forbids reading the clock there).
  const [weekOffset, setWeekOffset] = useState(0);
  // From the user's settings; null until loaded.
  const [weekStartsOn, setWeekStartsOn] = useState<number | null>(null);
  const [events, setEvents] = useState<EventOccurrence[] | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [editor, setEditor] = useState<EditorTarget | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [version, setVersion] = useState(0);

  const showError = useCallback((err: unknown) => setError(errorMessage(err)), []);

  useEffect(() => {
    if (!user) return;
    getSettings()
      .then((s) => setWeekStartsOn(s.week_starts_on))
      .catch((err) => {
        setWeekStartsOn(1);
        showError(err);
      });
  }, [user, showError]);

  useEffect(() => {
    if (!user || weekStartsOn === null) return;
    let cancelled = false;
    const weekStart = addDays(startOfWeek(new Date(), weekStartsOn), weekOffset * 7);
    const from = weekStart.toISOString();
    const to = addDays(weekStart, 7).toISOString();
    Promise.all([listEvents(from, to), listTasks({ status: "open", due_from: from, due_to: to, per_page: 100 })])
      .then(([evs, ts]) => {
        if (cancelled) return;
        setEvents(evs);
        setTasks(ts.data);
      })
      .catch(showError);
    return () => {
      cancelled = true;
    };
  }, [user, weekStartsOn, weekOffset, version, showError]);

  const titles = useMemo(() => new Map((events ?? []).map((e) => [e.id, e.title])), [events]);

  if (!user || weekStartsOn === null) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  const now = new Date();
  const weekStart = addDays(startOfWeek(now, weekStartsOn), weekOffset * 7);
  const days = Array.from({ length: 7 }, (_, i) => addDays(weekStart, i));
  const todayKey = localDateKey(now);
  const rangeLabel = `${days[0].toLocaleDateString(undefined, { day: "numeric", month: "short" })} – ${days[6].toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    year: "numeric",
  })}`;
  const weekConflicts = new Set((events ?? []).filter((e) => e.conflicts_with.length > 0).map((e) => e.id)).size;

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-6 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">Calendar</h1>
          <span className="text-zinc-500">{rangeLabel}</span>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <button type="button" onClick={() => setWeekOffset((w) => w - 1)} className="rounded-md border border-zinc-300 px-3 py-1.5 dark:border-zinc-700" aria-label="Previous week">
            ←
          </button>
          <button type="button" onClick={() => setWeekOffset(0)} className="rounded-md border border-zinc-300 px-3 py-1.5 dark:border-zinc-700">
            Today
          </button>
          <button type="button" onClick={() => setWeekOffset((w) => w + 1)} className="rounded-md border border-zinc-300 px-3 py-1.5 dark:border-zinc-700" aria-label="Next week">
            →
          </button>
          <button
            type="button"
            onClick={() => setEditor({ kind: "new", day: weekOffset === 0 ? todayKey : localDateKey(weekStart) })}
            className="rounded-md bg-zinc-900 px-4 py-1.5 font-medium text-white dark:bg-zinc-100 dark:text-zinc-900"
          >
            New event
          </button>
        </div>
      </div>

      {weekConflicts > 0 && (
        <p className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
          ⚠ {weekConflicts} event{weekConflicts === 1 ? "" : "s"} this week overlap{weekConflicts === 1 ? "s" : ""} with another busy event (including travel/prep time).
        </p>
      )}
      {notice && (
        <div role="status" className="flex items-start justify-between gap-3 rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
          <span>{notice}</span>
          <button type="button" onClick={() => setNotice(null)} aria-label="Dismiss">
            ×
          </button>
        </div>
      )}

      {editor && (
        <EventEditor
          target={editor}
          onClose={() => setEditor(null)}
          onError={showError}
          onSaved={(conflicts) => {
            setEditor(null);
            setVersion((v) => v + 1);
            setNotice(
              conflicts.length
                ? `Saved, but it overlaps: ${conflicts
                    .slice(0, 5)
                    .map((c) => `${c.title} (${new Date(c.starts_at).toLocaleString(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit" })})`)
                    .join(", ")}${conflicts.length > 5 ? ` and ${conflicts.length - 5} more` : ""}`
                : null,
            );
          }}
        />
      )}

      {events === null ? (
        <p className="text-zinc-500">Loading calendar…</p>
      ) : (
        <div className="grid gap-3 md:grid-cols-7">
          {days.map((day) => {
            const key = localDateKey(day);
            const dayEnd = addDays(day, 1);
            const allDay = events.filter((e) => e.all_day && e.start_date! <= key && key <= e.end_date!);
            const timed = events.filter((e) => !e.all_day && new Date(e.starts_at) < dayEnd && new Date(e.ends_at) > day);
            const due = tasks.filter((t) => t.due_at && localDateKey(new Date(t.due_at)) === key);
            const isToday = key === todayKey;
            return (
              <section
                key={key}
                aria-label={day.toLocaleDateString(undefined, { weekday: "long", day: "numeric", month: "long" })}
                className={`flex min-h-40 flex-col gap-2 rounded-lg border p-2 ${isToday ? "border-zinc-900 dark:border-zinc-100" : "border-zinc-200 dark:border-zinc-800"}`}
              >
                <div className="flex items-center justify-between">
                  <span className={`text-sm ${isToday ? "font-semibold" : "text-zinc-600 dark:text-zinc-400"}`}>
                    {day.toLocaleDateString(undefined, { weekday: "short", day: "numeric" })}
                  </span>
                  <button
                    type="button"
                    onClick={() => setEditor({ kind: "new", day: key })}
                    aria-label={`Add event on ${key}`}
                    className="rounded px-1.5 text-zinc-400 hover:bg-zinc-100 hover:text-zinc-900 dark:hover:bg-zinc-800 dark:hover:text-zinc-100"
                  >
                    +
                  </button>
                </div>

                {allDay.map((e) => (
                  <button
                    key={`${e.id}-${e.starts_at}`}
                    type="button"
                    onClick={() => setEditor({ kind: "edit", id: e.id })}
                    className="truncate rounded px-2 py-1 text-left text-xs font-medium text-white"
                    style={{ backgroundColor: e.color ?? "#52525b" }}
                  >
                    {e.recurrence && "↻ "}
                    {e.title}
                  </button>
                ))}

                {timed.map((e) => {
                  const conflict = e.conflicts_with.length > 0;
                  return (
                    <button
                      key={`${e.id}-${e.starts_at}`}
                      type="button"
                      onClick={() => setEditor({ kind: "edit", id: e.id })}
                      title={conflict ? `Overlaps: ${e.conflicts_with.map((id) => titles.get(id) ?? "another event").join(", ")}` : undefined}
                      className={`flex flex-col rounded border-l-4 bg-zinc-50 px-2 py-1 text-left text-xs dark:bg-zinc-900 ${e.busy ? "" : "opacity-60"} ${
                        conflict ? "ring-1 ring-amber-500" : ""
                      }`}
                      style={{ borderLeftColor: e.color ?? "#71717a" }}
                    >
                      <span className="text-zinc-500">
                        {formatTime(e.starts_at)}–{formatTime(e.ends_at)}
                        {e.buffer_before_minutes > 0 && ` · ${e.buffer_before_minutes}m before`}
                      </span>
                      <span className="font-medium wrap-break-word">
                        {conflict && <span className="text-amber-600">⚠ </span>}
                        {e.recurrence && "↻ "}
                        {e.title}
                      </span>
                      {e.location && <span className="truncate text-zinc-500">{e.location}</span>}
                    </button>
                  );
                })}

                {due.length > 0 && (
                  <div className="mt-auto flex flex-col gap-1 border-t border-zinc-200 pt-2 dark:border-zinc-800">
                    {due.map((t) => (
                      <label key={t.id} className="flex items-start gap-1.5 text-xs">
                        <input
                          type="checkbox"
                          disabled={t.blocked}
                          onChange={() =>
                            updateTask(t.id, { status: "completed" })
                              .then(() => setVersion((v) => v + 1))
                              .catch(showError)
                          }
                          aria-label={`Complete task ${t.title}`}
                          className="mt-0.5"
                        />
                        <span className="wrap-break-word">
                          <span className="text-zinc-500">Due {formatTime(t.due_at!)} · </span>
                          {t.title}
                        </span>
                      </label>
                    ))}
                  </div>
                )}
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}
