"use client";

import Link from "next/link";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import AppHeader, { ErrorBanner } from "@/components/AppHeader";
import { createReminder, deleteReminder, listReminders, snoozeReminder, type Reminder } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatDue, formatOffset, fromLocalInput, timeAgo } from "@/lib/format";
import { useRequireUser } from "@/lib/useRequireUser";

const TABS = [
  { value: "scheduled", label: "Upcoming" },
  { value: "sent", label: "Sent" },
];

const KIND_LINK: Record<Reminder["kind"], string> = { task: "/tasks", event: "/calendar", standalone: "" };

export default function RemindersView() {
  const { user, error: authError } = useRequireUser();
  const [tab, setTab] = useState("scheduled");
  const [reminders, setReminders] = useState<Reminder[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [version, setVersion] = useState(0);
  const [title, setTitle] = useState("");
  const [when, setWhen] = useState("");
  const [note, setNote] = useState("");

  const showError = useCallback((err: unknown) => setError(errorMessage(err)), []);

  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    listReminders({ status: tab, per_page: 100 })
      .then((r) => {
        if (!cancelled) setReminders(r.data);
      })
      .catch(showError);
    return () => {
      cancelled = true;
    };
  }, [user, tab, version, showError]);

  async function run(action: () => Promise<unknown>) {
    try {
      await action();
      setVersion((v) => v + 1);
    } catch (err) {
      showError(err);
    }
  }

  function onCreate(event: FormEvent) {
    event.preventDefault();
    if (!title.trim() || !when) return;
    run(async () => {
      await createReminder({ title, remind_at: fromLocalInput(when), note: note.trim() || null });
      setTitle("");
      setWhen("");
      setNote("");
      setTab("scheduled");
    });
  }

  if (!user) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  const control = "rounded-md border border-zinc-300 bg-transparent px-3 py-1.5 text-sm dark:border-zinc-700";

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">Reminders</h1>
        <div className="flex overflow-hidden rounded-md border border-zinc-300 text-sm dark:border-zinc-700" role="tablist">
          {TABS.map((t) => (
            <button
              key={t.value}
              type="button"
              role="tab"
              aria-selected={tab === t.value}
              onClick={() => {
                setTab(t.value);
                setReminders(null);
              }}
              className="px-3 py-1.5 aria-selected:bg-zinc-900 aria-selected:text-white dark:aria-selected:bg-zinc-100 dark:aria-selected:text-zinc-900"
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      <form onSubmit={onCreate} className="flex flex-col gap-2 rounded-md border border-zinc-200 p-3 dark:border-zinc-800">
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Remind me to… e.g. Call mom"
          maxLength={255}
          aria-label="Reminder"
          className="w-full bg-transparent px-1 py-1 text-base outline-none"
        />
        <div className="flex flex-wrap items-center gap-2">
          <input type="datetime-local" value={when} onChange={(e) => setWhen(e.target.value)} aria-label="When" className={control} />
          <input value={note} onChange={(e) => setNote(e.target.value)} placeholder="Note (optional)" maxLength={2000} aria-label="Note" className={`${control} min-w-0 flex-1`} />
          <button type="submit" disabled={!title.trim() || !when} className="rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900">
            Add reminder
          </button>
        </div>
        <p className="text-xs text-zinc-500">For tasks and events, add reminders from the task or event editor so they move when the time changes.</p>
      </form>

      {reminders === null ? (
        <p className="text-zinc-500">Loading reminders…</p>
      ) : reminders.length === 0 ? (
        <p className="rounded-md border border-dashed border-zinc-300 px-4 py-8 text-center text-zinc-500 dark:border-zinc-700">
          {tab === "scheduled" ? "No upcoming reminders." : "Nothing sent yet."}
        </p>
      ) : (
        <ul className="flex flex-col divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
          {reminders.map((r) => (
            <li key={r.id} className="flex items-start gap-3 p-3">
              <span className="mt-0.5 rounded bg-zinc-100 px-1.5 py-0.5 text-xs text-zinc-600 capitalize dark:bg-zinc-800 dark:text-zinc-300">{r.kind}</span>
              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                {KIND_LINK[r.kind] ? (
                  <Link href={KIND_LINK[r.kind]} className="font-medium wrap-break-word underline-offset-2 hover:underline">
                    {r.subject_title}
                  </Link>
                ) : (
                  <span className="font-medium wrap-break-word">{r.subject_title}</span>
                )}
                {r.note && <span className="text-sm text-zinc-500">{r.note}</span>}
                <span className="text-xs text-zinc-500">
                  {r.status === "sent" && r.last_sent_at
                    ? `Sent ${timeAgo(r.last_sent_at)}`
                    : r.remind_at
                      ? `${formatDue(r.remind_at)}${r.offset_minutes !== null ? ` · ${formatOffset(r.offset_minutes, r.kind === "event" ? "At start" : "At due time").toLowerCase()}` : ""}`
                      : "Waiting for an upcoming due date"}
                  {r.follow_up_minutes && ` · follow-ups ${r.follow_ups_sent}/${r.max_follow_ups}`}
                </span>
              </div>
              <div className="flex shrink-0 gap-2 text-sm">
                {r.status === "sent" && r.kind === "standalone" && (
                  <button type="button" onClick={() => run(() => snoozeReminder(r.id, 60))} className="underline">
                    Remind again in 1 h
                  </button>
                )}
                <button type="button" onClick={() => run(() => deleteReminder(r.id))} className="text-red-600 underline">
                  Delete
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
