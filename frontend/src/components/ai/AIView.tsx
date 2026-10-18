"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import AppHeader, { ErrorBanner } from "@/components/AppHeader";
import {
  getAIStatus,
  listAIActions,
  pingAI,
  type AIAction,
  type AIRisk,
  type AIStatus,
  type PageMeta,
} from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { timeAgo } from "@/lib/format";
import { useRequireUser } from "@/lib/useRequireUser";

const FILTERS = [
  { value: "", label: "All" },
  { value: "succeeded", label: "Done" },
  { value: "failed", label: "Failed" },
  { value: "rejected,expired", label: "Declined" },
];

const RISK_LABEL: Record<AIRisk, string> = { read: "Looks up", low: "Changes", high: "High impact" };
const RISK_STYLE: Record<AIRisk, string> = {
  read: "bg-zinc-100 text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300",
  low: "bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-300",
  high: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
};

const STATUS_STYLE: Record<AIAction["status"], string> = {
  succeeded: "text-green-700 dark:text-green-400",
  failed: "text-red-600 dark:text-red-400",
  pending_approval: "text-amber-700 dark:text-amber-400",
  running: "text-zinc-500",
  rejected: "text-zinc-500",
  expired: "text-zinc-500",
};

export default function AIView() {
  const { user, error: authError } = useRequireUser();
  const [status, setStatus] = useState<AIStatus | null>(null);
  const [pending, setPending] = useState(0);
  const [history, setHistory] = useState<AIAction[] | null>(null);
  const [meta, setMeta] = useState<PageMeta | null>(null);
  const [filter, setFilter] = useState("");
  const [page, setPage] = useState(1);
  const [version, setVersion] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [ping, setPing] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const showError = useCallback((err: unknown) => setError(errorMessage(err)), []);

  useEffect(() => {
    if (!user) return;
    getAIStatus().then(setStatus).catch(showError);
  }, [user, showError]);

  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    Promise.all([
      listAIActions({ status: "pending_approval", kind: "tool", per_page: 1 }),
      listAIActions({ status: filter || "succeeded,failed,rejected,expired,running", page, per_page: 25 }),
    ])
      .then(([p, h]) => {
        if (cancelled) return;
        setPending(p.meta.total);
        setHistory(h.data);
        setMeta(h.meta);
      })
      .catch(showError);
    return () => {
      cancelled = true;
    };
  }, [user, filter, page, version, showError]);

  async function onPing() {
    setBusy(true);
    setPing(null);
    try {
      const r = await pingAI();
      setPing(`Connected to ${r.model}: replied “${r.reply}” in ${(r.duration_ms / 1000).toFixed(1)} s (${r.input_tokens + r.output_tokens} tokens).`);
      setVersion((v) => v + 1);
    } catch (err) {
      showError(err);
    } finally {
      setBusy(false);
    }
  }

  if (!user) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  const section = "flex flex-col rounded-md border border-zinc-200 dark:border-zinc-800";
  const header = "flex flex-wrap items-center justify-between gap-2 border-b border-zinc-200 px-3 py-2 dark:border-zinc-800";
  const button = "rounded-md border border-zinc-300 px-3 py-1 text-sm hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800";
  const lastPage = meta ? Math.max(1, Math.ceil(meta.total / meta.per_page)) : 1;

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />
      <div className="flex flex-col gap-1">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h1 className="text-2xl font-semibold tracking-tight">AI activity</h1>
          <Link href="/ai" className="text-sm underline">
            Back to chat
          </Link>
        </div>
        <p className="text-sm text-zinc-500">
          Everything the assistant and your automations do is listed here. They can only use the actions below, and changes wait in{" "}
          <Link href="/approvals" className="underline">
            Approvals
          </Link>{" "}
          as set in{" "}
          <Link href="/settings" className="underline">
            Settings
          </Link>
          .
        </p>
      </div>

      <section className={section} aria-label="Connection">
        <div className={header}>
          <h2 className="text-sm font-medium">Connection</h2>
          {status?.enabled && (
            <button type="button" onClick={onPing} disabled={busy} className={button}>
              Test connection
            </button>
          )}
        </div>
        <div className="flex flex-col gap-1 px-3 py-3 text-sm">
          {!status ? (
            <span className="text-zinc-500">Checking…</span>
          ) : status.enabled ? (
            <span>
              <span className="text-green-700 dark:text-green-400">● On</span> · {status.provider} · <code className="text-xs">{status.model}</code>
            </span>
          ) : (
            <>
              <span>
                <span className="text-zinc-500">● Off</span> · {status.reason}
              </span>
              <span className="text-xs text-zinc-500">Add the key to backend/.env and restart the API. The key stays on the server and is never sent to the browser.</span>
            </>
          )}
          {ping && (
            <span role="status" className="text-green-700 dark:text-green-400">
              {ping}
            </span>
          )}
        </div>
      </section>

      {pending > 0 && (
        <p className="rounded-md border border-amber-300 px-3 py-2 text-sm dark:border-amber-800">
          {pending === 1 ? "1 action is" : `${pending} actions are`} waiting for your approval.{" "}
          <Link href="/approvals" className="font-medium underline">
            Review in Approvals
          </Link>
        </p>
      )}

      <section className={section} aria-label="Activity">
        <div className={header}>
          <h2 className="text-sm font-medium">Activity{meta ? ` (${meta.total})` : ""}</h2>
          <div className="flex overflow-hidden rounded-md border border-zinc-300 text-xs dark:border-zinc-700" role="tablist">
            {FILTERS.map((f) => (
              <button
                key={f.value}
                type="button"
                role="tab"
                aria-selected={filter === f.value}
                onClick={() => {
                  setFilter(f.value);
                  setPage(1);
                }}
                className="px-2.5 py-1 aria-selected:bg-zinc-900 aria-selected:text-white dark:aria-selected:bg-zinc-100 dark:aria-selected:text-zinc-900"
              >
                {f.label}
              </button>
            ))}
          </div>
        </div>
        {history === null ? (
          <p className="px-3 py-4 text-sm text-zinc-500">Loading…</p>
        ) : history.length === 0 ? (
          <p className="px-3 py-4 text-sm text-zinc-500">No activity yet.</p>
        ) : (
          <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
            {history.map((a) => (
              <li key={a.id} className="flex flex-col gap-1 px-3 py-2 text-sm">
                <div className="flex flex-wrap items-baseline gap-2">
                  {a.kind === "llm_call" ? (
                    <span className="wrap-break-word">
                      Model call <span className="text-zinc-500">({a.source})</span>
                    </span>
                  ) : (
                    <>
                      {a.risk && <RiskBadge risk={a.risk} />}
                      <span className="wrap-break-word">{a.summary ?? a.tool}</span>
                    </>
                  )}
                  <span className={`ml-auto text-xs ${STATUS_STYLE[a.status]}`}>{a.status.replace("_", " ")}</span>
                </div>
                <div className="flex flex-wrap gap-x-3 text-xs text-zinc-500">
                  <span>{timeAgo(a.created_at)}</span>
                  {a.automation_id && <span>automation</span>}
                  {a.model && <span>{a.model}</span>}
                  {a.input_tokens !== null && <span>{(a.input_tokens ?? 0) + (a.output_tokens ?? 0)} tokens</span>}
                  {a.duration_ms !== null && <span>{a.duration_ms} ms</span>}
                  {a.error && <span className="text-red-600 dark:text-red-400">{a.error}</span>}
                </div>
                {a.kind === "tool" && <Details action={a} />}
              </li>
            ))}
          </ul>
        )}
        {lastPage > 1 && (
          <div className="flex items-center justify-between border-t border-zinc-200 px-3 py-2 text-sm dark:border-zinc-800">
            <button type="button" disabled={page <= 1} onClick={() => setPage((p) => p - 1)} className={button}>
              Newer
            </button>
            <span className="text-zinc-500">
              Page {page} of {lastPage}
            </span>
            <button type="button" disabled={page >= lastPage} onClick={() => setPage((p) => p + 1)} className={button}>
              Older
            </button>
          </div>
        )}
      </section>

      {status && (
        <section className={section} aria-label="What the assistant can do">
          <div className={header}>
            <h2 className="text-sm font-medium">What the assistant can do ({status.tools.length})</h2>
          </div>
          <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
            {status.tools.map((t) => (
              <li key={t.name} className="flex items-start gap-3 px-3 py-2 text-sm">
                <RiskBadge risk={t.risk} />
                <span className="flex min-w-0 flex-col">
                  <code className="text-xs">{t.name}</code>
                  <span className="text-zinc-500">{t.description}</span>
                </span>
              </li>
            ))}
          </ul>
          <p className="border-t border-zinc-200 px-3 py-2 text-xs text-zinc-500 dark:border-zinc-800">
            &ldquo;Looks up&rdquo; actions never change anything. &ldquo;Changes&rdquo; follow your approval setting. &ldquo;High impact&rdquo; actions always ask
            first. The assistant can&rsquo;t run database queries, shell commands or web requests of its own.
          </p>
        </section>
      )}
    </div>
  );
}

function RiskBadge({ risk }: { risk: AIRisk }) {
  return <span className={`shrink-0 rounded px-1.5 py-0.5 text-xs ${RISK_STYLE[risk]}`}>{RISK_LABEL[risk]}</span>;
}

/** The (redacted) input and result, collapsed by default. */
function Details({ action }: { action: AIAction }) {
  if (action.input == null && action.output == null) return null;
  return (
    <details className="text-xs text-zinc-500">
      <summary className="cursor-pointer select-none">Details</summary>
      <div className="mt-1 flex flex-col gap-1">
        {action.input != null && (
          <pre className="overflow-x-auto rounded bg-zinc-50 p-2 dark:bg-zinc-900">
            <span className="text-zinc-400">input </span>
            {JSON.stringify(action.input, null, 2)}
          </pre>
        )}
        {action.output != null && (
          <pre className="overflow-x-auto rounded bg-zinc-50 p-2 dark:bg-zinc-900">
            <span className="text-zinc-400">result </span>
            {JSON.stringify(action.output, null, 2)}
          </pre>
        )}
      </div>
    </details>
  );
}
