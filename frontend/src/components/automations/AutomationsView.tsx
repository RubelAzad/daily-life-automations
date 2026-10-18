"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import AppHeader, { ErrorBanner } from "@/components/AppHeader";
import AutomationEditor from "@/components/automations/AutomationEditor";
import RunsPanel from "@/components/automations/RunsPanel";
import { WorkerWarning } from "@/components/WorkerStatus";
import {
  deleteAutomation,
  getAutomationCatalog,
  listAutomations,
  runAutomation,
  updateAutomation,
  type Automation,
  type AutomationCatalog,
  type AutomationInput,
  type Schedule,
} from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatDue, timeAgo } from "@/lib/format";
import { useRequireUser } from "@/lib/useRequireUser";

const small = "rounded border border-zinc-300 px-2 py-0.5 text-xs hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800";
const DAY_NAMES = ["", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

/** Starting points for common automations. */
const PRESETS: { label: string; input: AutomationInput }[] = [
  {
    label: "Weekday planning task",
    input: {
      name: "Plan my day",
      trigger: { type: "schedule", schedule: { frequency: "weekly", time: "08:00", weekdays: [1, 2, 3, 4, 5] } },
      actions: [{ type: "create_task", input: { title: "Plan the day ({{today}})", priority: "high", due_at: "today 10:00" } }],
    },
  },
  {
    label: "Nudge me about overdue tasks",
    input: {
      name: "Overdue nudge",
      trigger: { type: "task.overdue" },
      conditions: [{ field: "task.priority", op: "in", value: ["high", "urgent"] }],
      actions: [{ type: "notify", input: { title: "Overdue: {{task.title}}", body: "It was due {{task.due}}.", link: "/today" } }],
    },
  },
  {
    label: "Bill notes become tasks",
    input: {
      name: "Pay bills I save",
      trigger: { type: "note.created" },
      conditions: [{ field: "note.tags", op: "contains", value: "bills" }],
      actions: [{ type: "create_task", input: { title: "Pay: {{note.title}}", due_at: "+3d 10:00" } }],
    },
  },
];

const STATUS_TEXT: Record<string, [string, string]> = {
  running: ["running", "text-blue-700 dark:text-blue-400"],
  retrying: ["failed, retrying", "text-amber-700 dark:text-amber-400"],
  succeeded: ["ran", "text-green-700 dark:text-green-400"],
  waiting_approval: ["waiting for approval", "text-amber-700 dark:text-amber-400"],
  failed: ["failed", "text-red-600 dark:text-red-400"],
  skipped: ["skipped", "text-zinc-500"],
};

function describeSchedule(s: Schedule): string {
  switch (s.frequency) {
    case "daily":
      return `Every day at ${s.time}`;
    case "weekly":
      return (s.weekdays ?? []).join(",") === "1,2,3,4,5" ? `Weekdays at ${s.time}` : `${(s.weekdays ?? []).map((d) => DAY_NAMES[d]).join(", ")} at ${s.time}`;
    case "monthly":
      return `Monthly on day ${s.month_day} at ${s.time}`;
    case "once":
      return `Once on ${s.date} at ${s.time}`;
  }
}

export default function AutomationsView() {
  const { user, error: authError } = useRequireUser();
  const [catalog, setCatalog] = useState<AutomationCatalog | null>(null);
  const [list, setList] = useState<Automation[] | null>(null);
  const [editing, setEditing] = useState<{ automation: Automation | null; preset?: AutomationInput } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState<number | null>(null);
  const [version, setVersion] = useState(0);
  const [historyFor, setHistoryFor] = useState<{ id: number; name: string } | null>(null);
  const runParam = useSearchParams().get("run");
  const openRunId = runParam && /^\d+$/.test(runParam) ? Number(runParam) : null;

  const showError = useCallback((err: unknown) => setError(errorMessage(err)), []);
  const reload = () => setVersion((v) => v + 1);

  useEffect(() => {
    if (!user) return;
    getAutomationCatalog().then(setCatalog).catch(showError);
  }, [user, showError]);

  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    listAutomations()
      .then((l) => !cancelled && setList(l))
      .catch(showError);
    return () => {
      cancelled = true;
    };
  }, [user, version, showError]);

  if (!user) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  async function act(a: Automation, fn: () => Promise<void>) {
    setBusy(a.id);
    setError(null);
    setNotice(null);
    try {
      await fn();
    } catch (err) {
      showError(err);
    } finally {
      setBusy(null);
      reload();
    }
  }

  const triggerLabel = (a: Automation) =>
    a.trigger.type === "schedule" && a.trigger.schedule
      ? describeSchedule(a.trigger.schedule)
      : (catalog?.triggers.find((t) => t.type === a.trigger.type)?.label ?? a.trigger.type);

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />
      <WorkerWarning />

      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Automations</h1>
          <p className="text-sm text-zinc-500">
            When something happens, or on a schedule, LifeOS does it for you. Every action is listed on the{" "}
            <Link href="/ai/activity" className="underline">
              AI activity page
            </Link>
            .
          </p>
        </div>
        {!editing && catalog && (
          <button
            type="button"
            onClick={() => setEditing({ automation: null })}
            className="rounded-md bg-zinc-900 px-3 py-1.5 text-sm font-medium text-white dark:bg-zinc-100 dark:text-zinc-900"
          >
            New automation
          </button>
        )}
      </div>

      {notice && (
        <p role="status" className="text-sm text-green-700 dark:text-green-400">
          {notice}
        </p>
      )}

      {editing && catalog && (
        <AutomationEditor
          catalog={catalog}
          automation={editing.automation}
          preset={editing.preset}
          onSaved={(a) => {
            setEditing(null);
            setNotice(`Saved “${a.name}”.`);
            reload();
          }}
          onCancel={() => setEditing(null)}
        />
      )}

      {list === null ? (
        <p className="text-zinc-500">Loading…</p>
      ) : list.length === 0 && !editing ? (
        <div className="flex flex-col gap-3 rounded-md border border-dashed border-zinc-300 p-4 dark:border-zinc-700">
          <p className="text-sm text-zinc-500">No automations yet. Start from one of these:</p>
          <div className="flex flex-wrap gap-2">
            {PRESETS.map((p) => (
              <button key={p.label} type="button" onClick={() => setEditing({ automation: null, preset: p.input })} className={small}>
                {p.label}
              </button>
            ))}
          </div>
        </div>
      ) : (
        <ul className="flex flex-col gap-2">
          {list.map((a) => {
            const canRun = catalog?.triggers.find((t) => t.type === a.trigger.type)?.run_now;
            const [statusText, statusTone] = a.last_status ? STATUS_TEXT[a.last_status] : ["", ""];
            return (
              <li key={a.id} className="flex flex-col gap-1.5 rounded-md border border-zinc-200 px-3 py-2 dark:border-zinc-800">
                <div className="flex flex-wrap items-center gap-2">
                  <label className="flex items-center gap-2" title={a.enabled ? "On" : "Off"}>
                    <input
                      type="checkbox"
                      checked={a.enabled}
                      disabled={busy === a.id}
                      onChange={() => act(a, async () => void (await updateAutomation(a.id, { enabled: !a.enabled })))}
                      aria-label={`${a.name} on`}
                    />
                    <span className={`font-medium ${a.enabled ? "" : "text-zinc-500"}`}>{a.name}</span>
                  </label>
                  <span className="text-xs text-zinc-500">
                    {triggerLabel(a)}
                    {a.conditions.length > 0 && ` · ${a.conditions.length} condition${a.conditions.length > 1 ? "s" : ""}`} · {a.actions.length} action
                    {a.actions.length > 1 ? "s" : ""}
                  </span>
                  <span className="ml-auto flex gap-1.5">
                    {canRun && (
                      <button
                        type="button"
                        disabled={busy === a.id}
                        onClick={() =>
                          act(a, async () => {
                            const r = await runAutomation(a.id);
                            if (r.status === "failed") setError(r.error ?? "The run failed.");
                            else setNotice(r.status === "waiting_approval" ? `“${a.name}” is waiting for your approval.` : `Ran “${a.name}”.`);
                          })
                        }
                        className={small}
                      >
                        Run now
                      </button>
                    )}
                    <button type="button" onClick={() => setHistoryFor({ id: a.id, name: a.name })} className={small}>
                      History
                    </button>
                    <button type="button" onClick={() => setEditing({ automation: a })} className={small}>
                      Edit
                    </button>
                    <button
                      type="button"
                      disabled={busy === a.id}
                      onClick={() => {
                        if (confirm(`Delete “${a.name}”?`)) act(a, () => deleteAutomation(a.id));
                      }}
                      className={small}
                    >
                      Delete
                    </button>
                  </span>
                </div>
                <div className="flex flex-wrap gap-x-3 text-xs text-zinc-500">
                  {a.last_run_at ? (
                    <span>
                      Last <span className={statusTone}>{statusText}</span> {timeAgo(a.last_run_at)} · {a.run_count} run{a.run_count === 1 ? "" : "s"}
                    </span>
                  ) : (
                    <span>Not run yet</span>
                  )}
                  {a.enabled && a.next_run_at && <span>Next run: {formatDue(a.next_run_at)}</span>}
                  {a.trigger.type === "schedule" && a.enabled && !a.next_run_at && <span>No more runs</span>}
                </div>
                {a.last_error && (a.last_status === "failed" || a.last_status === "retrying" || a.last_status === "skipped") && (
                  <p className="text-xs text-red-600 dark:text-red-400">{a.last_error}</p>
                )}
              </li>
            );
          })}
        </ul>
      )}

      {list !== null && (list.length > 0 || openRunId) && (
        <RunsPanel
          automation={historyFor}
          onClearAutomation={() => setHistoryFor(null)}
          openRunId={openRunId}
          version={version}
          onError={setError}
          onChanged={reload}
        />
      )}
    </div>
  );
}
