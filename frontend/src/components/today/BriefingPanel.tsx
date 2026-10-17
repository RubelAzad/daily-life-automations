"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import {
  carryForward,
  createBriefing,
  getAIStatus,
  getTodayBriefings,
  type Briefing,
  type BriefingKind,
  type EveningSummary,
  type MorningPlan,
} from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { timeAgo } from "@/lib/format";

const SUGGESTION_LABEL = { tomorrow: "Tomorrow", later: "Later", drop: "Consider dropping" } as const;

/**
 * The AI morning plan and evening wrap-up for today. Briefings only advise;
 * the one action here (moving tasks to tomorrow) runs when the user clicks it.
 * openTaskIds are today's still-open tasks, so finished ones aren't offered.
 */
export default function BriefingPanel({ openTaskIds, onChanged }: { openTaskIds: Set<number>; onChanged: () => void }) {
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [briefings, setBriefings] = useState<{ morning: Briefing | null; evening: Briefing | null } | null>(null);
  const [tab, setTab] = useState<BriefingKind | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(() => {
    Promise.all([getAIStatus(), getTodayBriefings()])
      .then(([status, today]) => {
        setEnabled(status.enabled);
        setBriefings(today);
      })
      .catch((err) => setError(errorMessage(err)));
  }, []);

  useEffect(load, [load]);

  async function generate(kind: BriefingKind) {
    setBusy(kind === "morning" ? "Planning your day…" : "Looking back on your day…");
    setError(null);
    setNotice(null);
    try {
      const b = await createBriefing(kind);
      setBriefings((prev) => ({ morning: prev?.morning ?? null, evening: prev?.evening ?? null, [kind]: b }));
      setTab(kind);
    } catch (err) {
      setError(errorMessage(err));
      load(); // a failed briefing is stored too
    } finally {
      setBusy(null);
    }
  }

  async function moveToTomorrow(ids: number[]) {
    setBusy("Moving…");
    setError(null);
    try {
      const { meta } = await carryForward({ task_ids: ids, to: "tomorrow" });
      setNotice(`Moved ${meta.moved} task${meta.moved === 1 ? "" : "s"} to tomorrow.`);
      onChanged();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  if (enabled === null || !briefings) return null;
  if (!enabled && !briefings.morning && !briefings.evening) {
    return (
      <p className="text-xs text-zinc-500">
        Turn on the AI assistant for a daily plan and evening wrap-up.{" "}
        <Link href="/ai/activity" className="underline">
          How
        </Link>
      </p>
    );
  }

  // Default to the evening wrap-up once it exists (or late in the day).
  const shown: BriefingKind = tab ?? (briefings.evening || new Date().getHours() >= 17 ? "evening" : "morning");
  const current = briefings[shown];
  const button = "rounded-md border border-zinc-300 px-2.5 py-1 text-xs hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800";

  return (
    <section className="flex flex-col rounded-md border border-violet-200 dark:border-violet-900" aria-label="AI briefing">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-violet-200 px-3 py-2 dark:border-violet-900">
        <div className="flex overflow-hidden rounded-md border border-zinc-300 text-xs dark:border-zinc-700" role="tablist">
          {(["morning", "evening"] as const).map((k) => (
            <button
              key={k}
              type="button"
              role="tab"
              aria-selected={shown === k}
              onClick={() => setTab(k)}
              className="px-2.5 py-1 aria-selected:bg-zinc-900 aria-selected:text-white dark:aria-selected:bg-zinc-100 dark:aria-selected:text-zinc-900"
            >
              {k === "morning" ? "Plan for today" : "Day in review"}
            </button>
          ))}
        </div>
        {enabled && (
          <button type="button" disabled={busy !== null} onClick={() => generate(shown)} className={button}>
            {current ? "Write again" : shown === "morning" ? "Plan my day" : "Wrap up my day"}
          </button>
        )}
      </div>

      <div className="flex flex-col gap-3 px-3 py-3 text-sm">
        {busy && (
          <p role="status" className="text-zinc-500">
            {busy}
          </p>
        )}
        {error && <p className="text-red-600 dark:text-red-400">{error}</p>}
        {notice && (
          <p role="status" className="text-green-700 dark:text-green-400">
            {notice}
          </p>
        )}
        {!current ? (
          !busy && (
            <p className="text-zinc-500">
              {shown === "morning"
                ? "Ask the assistant for a plan: what to focus on and when, around your events."
                : "Ask for a wrap-up: what you got done, and what to do with the rest."}
            </p>
          )
        ) : current.status === "failed" ? (
          <p className="text-red-600 dark:text-red-400">The last attempt failed: {current.error ?? "unknown error"}</p>
        ) : current.kind === "morning" ? (
          <Morning plan={current.content as MorningPlan} />
        ) : (
          <Evening summary={current.content as EveningSummary} openTaskIds={openTaskIds} busy={busy !== null} onMove={moveToTomorrow} />
        )}
        {current && current.status === "ready" && (
          <p className="text-xs text-zinc-400">
            {current.trigger === "scheduled" ? "Sent automatically" : "Written"} {timeAgo(current.created_at)}
            {current.model ? ` · ${current.model}` : ""} · Suggestions only; nothing was changed.
          </p>
        )}
      </div>
    </section>
  );
}

function Morning({ plan }: { plan: MorningPlan }) {
  return (
    <>
      <p>{plan.summary}</p>
      {plan.focus.length > 0 && (
        <div className="flex flex-col gap-1">
          <h3 className="text-xs font-medium tracking-wide text-zinc-500 uppercase">Focus on</h3>
          <ol className="flex list-decimal flex-col gap-1 pl-5">
            {plan.focus.map((f) => (
              <li key={f.task_id}>
                <span className="font-medium">{f.title}</span>
                {f.reason && <span className="text-zinc-500"> · {f.reason}</span>}
              </li>
            ))}
          </ol>
        </div>
      )}
      {plan.schedule.length > 0 && (
        <div className="flex flex-col gap-1">
          <h3 className="text-xs font-medium tracking-wide text-zinc-500 uppercase">Suggested schedule</h3>
          <ul className="flex flex-col">
            {plan.schedule.map((s, i) => (
              <li key={i} className="flex items-baseline gap-3 py-0.5">
                <span className="w-24 shrink-0 text-zinc-500 tabular-nums">
                  {s.start}–{s.end}
                </span>
                <span className={s.kind === "break" ? "text-zinc-500 italic" : s.kind === "event" ? "font-medium" : ""}>{s.title}</span>
                {s.kind === "event" && <span className="text-xs text-zinc-400">event</span>}
              </li>
            ))}
          </ul>
        </div>
      )}
      {plan.warnings.length > 0 && (
        <ul className="flex flex-col gap-1 rounded-md bg-amber-50 px-3 py-2 text-amber-900 dark:bg-amber-950 dark:text-amber-200">
          {plan.warnings.map((w, i) => (
            <li key={i}>⚠ {w}</li>
          ))}
        </ul>
      )}
    </>
  );
}

function Evening({
  summary,
  openTaskIds,
  busy,
  onMove,
}: {
  summary: EveningSummary;
  openTaskIds: Set<number>;
  busy: boolean;
  onMove: (ids: number[]) => void;
}) {
  const open = summary.unfinished.filter((u) => openTaskIds.has(u.task_id));
  const toTomorrow = open.filter((u) => u.suggestion === "tomorrow").map((u) => u.task_id);
  return (
    <>
      <p>{summary.summary}</p>
      {summary.wins.length > 0 && (
        <ul className="flex flex-col gap-0.5">
          {summary.wins.map((w, i) => (
            <li key={i}>✓ {w}</li>
          ))}
        </ul>
      )}
      {open.length > 0 && (
        <div className="flex flex-col gap-1">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-xs font-medium tracking-wide text-zinc-500 uppercase">Still open</h3>
            {toTomorrow.length > 0 && (
              <button
                type="button"
                disabled={busy}
                onClick={() => onMove(toTomorrow)}
                className="rounded-md bg-zinc-900 px-2.5 py-1 text-xs font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900"
              >
                Move {toTomorrow.length} to tomorrow
              </button>
            )}
          </div>
          <ul className="flex flex-col gap-1">
            {open.map((u) => (
              <li key={u.task_id} className="flex flex-wrap items-baseline gap-2">
                <span>{u.title}</span>
                <span className="rounded bg-zinc-100 px-1.5 py-0.5 text-xs text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300">{SUGGESTION_LABEL[u.suggestion]}</span>
                {u.reason && <span className="text-xs text-zinc-500">{u.reason}</span>}
              </li>
            ))}
          </ul>
        </div>
      )}
      {summary.tomorrow && <p className="text-zinc-600 dark:text-zinc-400">Tomorrow: {summary.tomorrow}</p>}
    </>
  );
}
