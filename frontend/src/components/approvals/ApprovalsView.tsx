"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import AppHeader, { ErrorBanner } from "@/components/AppHeader";
import {
  APPROVALS_CHANGED,
  decideApproval,
  decideApprovals,
  listApprovalHistory,
  listApprovals,
  type AIRisk,
  type Approval,
  type PageMeta,
} from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatDue, timeAgo } from "@/lib/format";
import { useRequireUser } from "@/lib/useRequireUser";

const RISK_LABEL: Record<AIRisk, string> = { read: "Looks up", low: "Changes", high: "High impact" };
const RISK_STYLE: Record<AIRisk, string> = {
  read: "bg-zinc-100 text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300",
  low: "bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-300",
  high: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
};

/** How a decided request ended. */
const OUTCOME: Record<string, [string, string]> = {
  succeeded: ["Approved", "text-green-700 dark:text-green-400"],
  failed: ["Approved, but failed", "text-red-600 dark:text-red-400"],
  running: ["Approved, running", "text-zinc-500"],
  rejected: ["Declined", "text-zinc-500"],
  expired: ["Expired", "text-zinc-500"],
};

/** Requests expiring sooner than this are highlighted. */
const SOON_MS = 2 * 60 * 60 * 1000;

const PER_PAGE = 25;

export default function ApprovalsView() {
  const { user, error: authError } = useRequireUser();
  const [pending, setPending] = useState<Approval[] | null>(null);
  const [pendingMeta, setPendingMeta] = useState<PageMeta | null>(null);
  const [loadedAt, setLoadedAt] = useState(0); // when pending was loaded, to judge "expires soon"
  const [history, setHistory] = useState<Approval[] | null>(null);
  const [historyMeta, setHistoryMeta] = useState<PageMeta | null>(null);
  const [historyPage, setHistoryPage] = useState(1);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [busy, setBusy] = useState(false);
  const [version, setVersion] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const showError = useCallback((err: unknown) => setError(errorMessage(err)), []);

  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    listApprovals({ per_page: 100 })
      .then((r) => {
        if (cancelled) return;
        setPending(r.data);
        setPendingMeta(r.meta);
        setLoadedAt(Date.now());
        // Drop selections that are no longer waiting.
        setSelected((sel) => new Set(r.data.filter((a) => sel.has(a.id)).map((a) => a.id)));
      })
      .catch(showError);
    return () => {
      cancelled = true;
    };
  }, [user, version, showError]);

  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    listApprovalHistory({ page: historyPage, per_page: PER_PAGE })
      .then((r) => {
        if (cancelled) return;
        setHistory(r.data);
        setHistoryMeta(r.meta);
      })
      .catch(showError);
    return () => {
      cancelled = true;
    };
  }, [user, version, historyPage, showError]);

  function changed() {
    setVersion((v) => v + 1);
    window.dispatchEvent(new Event(APPROVALS_CHANGED));
  }

  async function decideOne(a: Approval, approve: boolean) {
    setBusy(true);
    setNotice(null);
    try {
      const r = await decideApproval(a.id, approve);
      if (r.status === "failed") setError(`Couldn't do it: ${r.error ?? "unknown error"}`);
      else setNotice(`${approve ? "Approved" : "Declined"}: ${label(a)}`);
    } catch (err) {
      showError(err);
    } finally {
      setBusy(false);
      changed();
    }
  }

  async function decideSelected(approve: boolean) {
    const ids = (pending ?? []).filter((a) => selected.has(a.id)).map((a) => a.id);
    if (ids.length === 0) return;
    if (approve && ids.length > 1 && !window.confirm(`Approve ${ids.length} actions? Each one runs now.`)) return;
    setBusy(true);
    setNotice(null);
    try {
      const results = await decideApprovals(ids, approve);
      const done = results.filter((r) => r.ok && r.action?.status !== "failed").length;
      const failed = results.filter((r) => r.ok && r.action?.status === "failed");
      const skipped = results.filter((r) => !r.ok);
      setNotice(`${approve ? "Approved" : "Declined"} ${done} of ${results.length}.`);
      const problems = [
        ...failed.map((r) => `${r.action ? label(r.action) : `#${r.id}`}: ${r.action?.error ?? "failed"}`),
        ...skipped.map((r) => `${r.action ? label(r.action) : `#${r.id}`}: ${r.error}`),
      ];
      if (problems.length) setError(problems.join(" · "));
      setSelected(new Set());
    } catch (err) {
      showError(err);
    } finally {
      setBusy(false);
      changed();
    }
  }

  if (!user) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  const section = "flex flex-col rounded-md border border-zinc-200 dark:border-zinc-800";
  const header = "flex flex-wrap items-center justify-between gap-2 border-b border-zinc-200 px-3 py-2 dark:border-zinc-800";
  const button = "rounded-md border border-zinc-300 px-3 py-1 text-sm hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800";
  const primary = "rounded-md bg-zinc-900 px-3 py-1 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900";
  const list = pending ?? [];
  const allSelected = list.length > 0 && list.every((a) => selected.has(a.id));
  const historyPages = historyMeta ? Math.max(1, Math.ceil(historyMeta.total / historyMeta.per_page)) : 1;
  function toggle(id: number) {
    setSelected((sel) => {
      const next = new Set(sel);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Approvals</h1>
        <p className="text-sm text-zinc-500">
          Actions from the assistant and your automations that wait for your OK. Nothing here runs until you approve it, and requests expire after 24 hours.
          What needs approval is set in{" "}
          <Link href="/settings" className="underline">
            Settings
          </Link>
          ; everything that ran is in the{" "}
          <Link href="/ai/activity" className="underline">
            activity log
          </Link>
          .
        </p>
      </div>
      {notice && (
        <p role="status" className="text-sm text-green-700 dark:text-green-400">
          {notice}
        </p>
      )}

      <section className={`${section} ${list.length ? "border-amber-300 dark:border-amber-800" : ""}`} aria-labelledby="pending-heading">
        <div className={header}>
          <div className="flex items-center gap-2">
            {list.length > 0 && (
              <input
                type="checkbox"
                aria-label="Select all"
                checked={allSelected}
                onChange={() => setSelected(allSelected ? new Set() : new Set(list.map((a) => a.id)))}
              />
            )}
            <h2 id="pending-heading" className="text-sm font-medium">
              Waiting for you{pendingMeta ? ` (${pendingMeta.total})` : ""}
            </h2>
          </div>
          {selected.size > 0 && (
            <div className="flex items-center gap-2">
              <button type="button" disabled={busy} onClick={() => decideSelected(true)} className={primary}>
                Approve selected ({selected.size})
              </button>
              <button type="button" disabled={busy} onClick={() => decideSelected(false)} className={button}>
                Reject selected
              </button>
            </div>
          )}
        </div>
        {pending === null ? (
          <p className="px-3 py-4 text-sm text-zinc-500">Loading…</p>
        ) : list.length === 0 ? (
          <p className="px-3 py-4 text-sm text-zinc-500">Nothing waiting. When an action needs your OK, it shows up here and in your notifications.</p>
        ) : (
          <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
            {list.map((a) => {
              const soon = a.expires_at !== null && new Date(a.expires_at).getTime() - loadedAt < SOON_MS;
              return (
                <li key={a.id} className="flex gap-3 px-3 py-3">
                  <input type="checkbox" aria-label={`Select ${label(a)}`} checked={selected.has(a.id)} onChange={() => toggle(a.id)} className="mt-1" />
                  <div className="flex min-w-0 flex-1 flex-col gap-2">
                    <div className="flex flex-wrap items-center gap-2">
                      {a.risk && <RiskBadge risk={a.risk} />}
                      <span className="text-sm font-medium wrap-break-word">{label(a)}</span>
                    </div>
                    <Source approval={a} />
                    <Details approval={a} />
                    <div className="flex flex-wrap items-center gap-2">
                      <button type="button" disabled={busy} onClick={() => decideOne(a, true)} className={primary}>
                        Approve
                      </button>
                      <button type="button" disabled={busy} onClick={() => decideOne(a, false)} className={button}>
                        Reject
                      </button>
                      <span className="text-xs text-zinc-500">Asked {timeAgo(a.created_at)}</span>
                      {a.expires_at && (
                        <span className={`text-xs ${soon ? "font-medium text-amber-700 dark:text-amber-400" : "text-zinc-500"}`}>
                          · expires {formatDue(a.expires_at).toLowerCase()}
                        </span>
                      )}
                    </div>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
        {pendingMeta && pendingMeta.total > list.length && (
          <p className="border-t border-zinc-200 px-3 py-2 text-xs text-zinc-500 dark:border-zinc-800">
            Showing the {list.length} that expire soonest. Decide on these to see the rest.
          </p>
        )}
      </section>

      <section className={section} aria-labelledby="history-heading">
        <div className={header}>
          <h2 id="history-heading" className="text-sm font-medium">
            Decided{historyMeta ? ` (${historyMeta.total})` : ""}
          </h2>
        </div>
        {history === null ? (
          <p className="px-3 py-4 text-sm text-zinc-500">Loading…</p>
        ) : history.length === 0 ? (
          <p className="px-3 py-4 text-sm text-zinc-500">No decisions yet.</p>
        ) : (
          <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
            {history.map((a) => {
              const [outcome, tone] = OUTCOME[a.status] ?? [a.status, "text-zinc-500"];
              return (
                <li key={a.id} className="flex flex-col gap-1 px-3 py-2 text-sm">
                  <div className="flex flex-wrap items-baseline gap-2">
                    {a.risk && <RiskBadge risk={a.risk} />}
                    <span className="wrap-break-word">{label(a)}</span>
                    <span className={`ml-auto text-xs ${tone}`}>{outcome}</span>
                  </div>
                  <div className="flex flex-wrap items-baseline gap-x-3 text-xs text-zinc-500">
                    {a.decided_at && <span title={new Date(a.decided_at).toLocaleString()}>{timeAgo(a.decided_at)}</span>}
                    <Source approval={a} compact />
                    {a.error && a.status === "failed" && <span className="text-red-600 dark:text-red-400">{a.error}</span>}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
        {historyPages > 1 && (
          <div className="flex items-center justify-between border-t border-zinc-200 px-3 py-2 text-sm dark:border-zinc-800">
            <button type="button" disabled={historyPage <= 1} onClick={() => setHistoryPage((p) => p - 1)} className={button}>
              Newer
            </button>
            <span className="text-zinc-500">
              Page {historyPage} of {historyPages}
            </span>
            <button type="button" disabled={historyPage >= historyPages} onClick={() => setHistoryPage((p) => p + 1)} className={button}>
              Older
            </button>
          </div>
        )}
      </section>
    </div>
  );
}

function label(a: Approval) {
  return a.summary ?? a.tool ?? `Action #${a.id}`;
}

function RiskBadge({ risk }: { risk: AIRisk }) {
  return <span className={`shrink-0 rounded px-1.5 py-0.5 text-xs ${RISK_STYLE[risk]}`}>{RISK_LABEL[risk]}</span>;
}

/** Who asked: an automation (and the run, with what triggered it) or a chat. */
function Source({ approval: a, compact = false }: { approval: Approval; compact?: boolean }) {
  const cls = compact ? "" : "text-xs text-zinc-500";
  if (a.automation || a.automation_id) {
    const name = a.automation ? `“${a.automation.name}”` : "a deleted automation";
    return (
      <span className={cls}>
        From automation {name}
        {a.run && (
          <>
            {a.run.trigger_summary && <> · {a.run.trigger_summary}</>} ·{" "}
            <Link href={`/automations?run=${a.run.id}`} className="underline">
              view run
            </Link>
          </>
        )}
      </span>
    );
  }
  if (a.conversation_id) {
    return (
      <span className={cls}>
        From the assistant
        {a.conversation ? (
          <>
            {" "}
            in{" "}
            <Link href={`/ai?c=${a.conversation.id}`} className="underline">
              “{a.conversation.name}”
            </Link>
          </>
        ) : (
          " (chat deleted)"
        )}
      </span>
    );
  }
  return <span className={cls}>From the assistant</span>;
}

/** The (redacted) input, collapsed by default. */
function Details({ approval }: { approval: Approval }) {
  if (approval.input == null) return null;
  return (
    <details className="text-xs text-zinc-500">
      <summary className="cursor-pointer select-none">Details</summary>
      <pre className="mt-1 overflow-x-auto rounded bg-zinc-50 p-2 dark:bg-zinc-900">
        <span className="text-zinc-400">{approval.tool} </span>
        {JSON.stringify(approval.input, null, 2)}
      </pre>
    </details>
  );
}
