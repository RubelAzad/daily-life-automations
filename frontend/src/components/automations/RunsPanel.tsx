"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { getAutomationRun, listAutomationRuns, retryAutomationRun, type PageMeta, type RunStatus, type WorkflowRun } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatDue, timeAgo } from "@/lib/format";

const small = "rounded border border-zinc-300 px-2 py-0.5 text-xs hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800";

const STATUS: Record<RunStatus, [string, string]> = {
  running: ["Running", "text-blue-700 dark:text-blue-400"],
  succeeded: ["Succeeded", "text-green-700 dark:text-green-400"],
  waiting_approval: ["Waiting for approval", "text-amber-700 dark:text-amber-400"],
  retrying: ["Failed, will retry", "text-amber-700 dark:text-amber-400"],
  failed: ["Failed", "text-red-600 dark:text-red-400"],
  skipped: ["Skipped", "text-zinc-500"],
};

const STEP_STATUS: Record<string, string> = {
  succeeded: "done",
  pending_approval: "waiting for approval",
  failed: "failed",
  rejected: "declined",
  expired: "approval expired",
  not_run: "not run",
};

const ACTION_LABELS: Record<string, string> = {
  notify: "Notification",
  create_task: "Create task",
  complete_task: "Complete task",
  reschedule_task: "Reschedule task",
  create_note: "Create note",
  create_reminder: "Set reminder",
  recover_task: "Find a new time",
  plan_day: "Plan the day",
  record_expense: "Record expense",
  create_bill_task: "Add bill task",
  pay_bill: "Record bill paid",
};

type Props = {
  /** Show only this automation's runs. */
  automation?: { id: number; name: string } | null;
  onClearAutomation: () => void;
  /** A run to open (e.g. from a failure notification's link). */
  openRunId?: number | null;
  /** Bumped by the parent after it runs something, to reload. */
  version: number;
  onError: (message: string) => void;
  onChanged: () => void;
};

export default function RunsPanel({ automation, onClearAutomation, openRunId, version, onError, onChanged }: Props) {
  const [failedOnly, setFailedOnly] = useState(false);
  const [page, setPage] = useState(1);
  const [runs, setRuns] = useState<WorkflowRun[] | null>(null);
  const [meta, setMeta] = useState<PageMeta | null>(null);
  const [open, setOpen] = useState<number | null>(openRunId ?? null);
  const [linked, setLinked] = useState<WorkflowRun | null>(null);
  const [busy, setBusy] = useState<number | null>(null);
  const [reload, setReload] = useState(0);

  useEffect(() => {
    let cancelled = false;
    listAutomationRuns({ automation_id: automation?.id, status: failedOnly ? "failed,retrying" : undefined, page, per_page: 15 })
      .then((r) => {
        if (cancelled) return;
        setRuns(r.data);
        setMeta(r.meta);
      })
      .catch((err) => onError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, [automation?.id, failedOnly, page, version, reload, onError]);

  // A linked run may be older than the first page: load it on its own.
  useEffect(() => {
    if (!openRunId) return;
    getAutomationRun(openRunId)
      .then(setLinked)
      .catch(() => setLinked(null));
  }, [openRunId, version, reload]);

  async function retry(run: WorkflowRun) {
    setBusy(run.id);
    try {
      const r = await retryAutomationRun(run.id);
      if (r.status === "failed") onError(`Retry failed: ${r.error ?? "unknown error"}`);
      setOpen(r.id);
      onChanged();
    } catch (err) {
      onError(errorMessage(err));
    } finally {
      setBusy(null);
      setReload((n) => n + 1);
    }
  }

  const shown = runs ?? [];
  const list = linked && !shown.some((r) => r.id === linked.id) ? [linked, ...shown] : shown;
  const pages = meta ? Math.max(1, Math.ceil(meta.total / meta.per_page)) : 1;

  return (
    <section className="flex flex-col rounded-md border border-zinc-200 dark:border-zinc-800" aria-labelledby="runs-heading">
      <div className="flex flex-wrap items-center gap-2 border-b border-zinc-200 px-3 py-2 dark:border-zinc-800">
        <h2 id="runs-heading" className="text-sm font-medium">
          Run history{automation ? `: ${automation.name}` : ""}
          {meta ? ` (${meta.total})` : ""}
        </h2>
        {automation && (
          <button type="button" onClick={onClearAutomation} className="text-xs underline">
            show all
          </button>
        )}
        <label className="ml-auto flex items-center gap-1.5 text-xs">
          <input
            type="checkbox"
            checked={failedOnly}
            onChange={(e) => {
              setFailedOnly(e.target.checked);
              setPage(1);
            }}
          />
          Failures only
        </label>
      </div>

      {runs === null ? (
        <p className="px-3 py-4 text-sm text-zinc-500">Loading…</p>
      ) : list.length === 0 ? (
        <p className="px-3 py-4 text-sm text-zinc-500">{failedOnly ? "No failed runs." : "No runs yet. Runs appear here when a trigger fires, or when you press Run now."}</p>
      ) : (
        <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
          {list.map((run) => {
            const [label, tone] = STATUS[run.status] ?? [run.status, ""];
            const expanded = open === run.id;
            return (
              <li key={run.id} id={`run-${run.id}`} className={`flex flex-col gap-1.5 px-3 py-2 text-sm ${run.id === openRunId ? "bg-zinc-50 dark:bg-zinc-900" : ""}`}>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <button type="button" onClick={() => setOpen(expanded ? null : run.id)} aria-expanded={expanded} className="flex min-w-0 items-center gap-2 text-left">
                    <span aria-hidden className="text-xs text-zinc-400">
                      {expanded ? "▾" : "▸"}
                    </span>
                    <span className={`shrink-0 text-xs font-medium ${tone}`}>{label}</span>
                    <span className="truncate font-medium">{run.automation_name}</span>
                  </button>
                  {run.trigger_summary && <span className="truncate text-xs text-zinc-500">{run.trigger_summary}</span>}
                  <span className="ml-auto flex items-center gap-2 text-xs text-zinc-500">
                    {run.attempt > 1 && <span>attempt {run.attempt}</span>}
                    <span title={new Date(run.started_at).toLocaleString()}>{timeAgo(run.started_at)}</span>
                    {(run.status === "failed" || run.status === "retrying") && run.automation_id !== null && (
                      <button type="button" disabled={busy === run.id} onClick={() => retry(run)} className={small}>
                        {busy === run.id ? "Retrying…" : "Retry now"}
                      </button>
                    )}
                  </span>
                </div>
                {run.error && <p className="text-xs text-red-600 dark:text-red-400">{run.error}</p>}
                {run.status === "retrying" && run.next_retry_at && (
                  <p className="text-xs text-zinc-500">Trying again automatically {formatDue(run.next_retry_at)}.</p>
                )}
                {expanded && (
                  <div className="flex flex-col gap-1 pl-5">
                    <ol className="flex flex-col gap-1">
                      {run.steps.map((s) => (
                        <li key={s.index} className="flex flex-wrap items-baseline gap-x-2 text-xs">
                          <span className="text-zinc-500">{s.index + 1}.</span>
                          <span className="font-medium">{ACTION_LABELS[s.type] ?? s.type}</span>
                          <span className={s.status === "succeeded" ? "text-green-700 dark:text-green-400" : s.status === "not_run" ? "text-zinc-400" : s.status === "pending_approval" ? "text-amber-700 dark:text-amber-400" : "text-red-600 dark:text-red-400"}>
                            {STEP_STATUS[s.status] ?? s.status}
                          </span>
                          {s.summary && <span className="text-zinc-500">{s.summary}</span>}
                          {s.error && s.status !== "succeeded" && <span className="text-red-600 dark:text-red-400">{s.error}</span>}
                          {s.attempt > 1 && <span className="text-zinc-400">(attempt {s.attempt})</span>}
                          {s.status === "pending_approval" && (
                            <Link href="/approvals" className="underline">
                              review
                            </Link>
                          )}
                        </li>
                      ))}
                    </ol>
                    <p className="text-xs text-zinc-400">
                      {run.trigger_source === "schedule" ? "Scheduled" : run.trigger_source === "manual" ? "Run by hand" : `Event: ${run.trigger_type}`}
                      {run.duration_ms !== null && ` · ${run.duration_ms < 1000 ? `${run.duration_ms} ms` : `${(run.duration_ms / 1000).toFixed(1)} s`}`}
                      {run.automation_id === null && " · the automation was deleted"}
                    </p>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}

      {pages > 1 && (
        <div className="flex items-center gap-2 border-t border-zinc-200 px-3 py-2 text-xs dark:border-zinc-800">
          <button type="button" disabled={page <= 1} onClick={() => setPage(page - 1)} className={small}>
            Newer
          </button>
          <span className="text-zinc-500">
            Page {page} of {pages}
          </span>
          <button type="button" disabled={page >= pages} onClick={() => setPage(page + 1)} className={small}>
            Older
          </button>
        </div>
      )}
    </section>
  );
}
