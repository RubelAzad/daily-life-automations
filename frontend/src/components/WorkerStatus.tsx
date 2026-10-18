"use client";

import { useEffect, useState } from "react";
import { getSystemStatus, type WorkerStatus } from "@/lib/api";
import { timeAgo } from "@/lib/format";

const JOB_LABELS: Record<string, string> = {
  "reminders.deliver": "Reminder delivery",
  "briefings.run": "Scheduled briefings",
  "search.index": "Search indexing",
  "automation.scan_overdue": "Overdue-task check for automations",
  "approvals.expire": "Expiring old approval requests",
  "finance.scan_bills": "Bill reminders",
  "scheduler.prune": "Clean-up of old jobs",
};

/** Polls the worker status every minute while mounted. null until loaded or if it can't be read. */
function useWorkerStatus(): WorkerStatus | null {
  const [status, setStatus] = useState<WorkerStatus | null>(null);
  useEffect(() => {
    let cancelled = false;
    const load = () =>
      getSystemStatus()
        .then((s) => !cancelled && setStatus(s))
        .catch(() => {}); // a status check failing shouldn't show an error of its own
    load();
    const id = setInterval(load, 60_000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);
  return status;
}

/** A warning shown only when the background worker isn't running. */
export function WorkerWarning() {
  const status = useWorkerStatus();
  if (!status || status.running) return null;
  return (
    <p role="alert" className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
      The background worker isn&rsquo;t running, so reminders and scheduled briefings aren&rsquo;t being delivered.
      {status.last_seen_at ? ` It was last active ${timeAgo(status.last_seen_at)}.` : ""} Start it with <code>go run ./cmd/worker</code> (see setup).
    </p>
  );
}

/** The worker's state and its repeating jobs, for the Settings page. */
export function WorkerSection({ className }: { className: string }) {
  const status = useWorkerStatus();
  return (
    <section className={className} aria-labelledby="worker-heading">
      <h2 id="worker-heading" className="font-medium">
        Background worker
      </h2>
      {!status ? (
        <p className="text-sm text-zinc-500">Checking…</p>
      ) : (
        <>
          <p className="flex items-center gap-2 text-sm">
            <span aria-hidden className={`inline-block size-2 rounded-full ${status.running ? "bg-green-600" : "bg-amber-500"}`} />
            {status.running ? "Running" : "Not running"}
            {status.last_seen_at && <span className="text-zinc-500">· last active {timeAgo(status.last_seen_at)}</span>}
          </p>
          {status.jobs.length > 0 && (
            <ul className="flex flex-col gap-1 text-sm">
              {status.jobs.map((j) => (
                <li key={j.kind} className="flex flex-wrap items-baseline justify-between gap-x-3">
                  <span>{JOB_LABELS[j.kind] ?? j.kind}</span>
                  <span className="text-xs text-zinc-500">
                    {j.running
                      ? "running now"
                      : j.last_finished_at
                        ? `${j.last_status === "failed" ? "failed" : "ran"} ${timeAgo(j.last_finished_at)}`
                        : "not run yet"}
                    {j.last_status === "failed" && !j.running && <span className="text-amber-700 dark:text-amber-400"> · will retry</span>}
                  </span>
                </li>
              ))}
            </ul>
          )}
          <p className="text-xs text-zinc-500">
            The worker delivers reminders, writes scheduled briefings, runs your automations and makes uploaded files searchable. Its jobs are saved in the database, so nothing is lost
            if it restarts.
          </p>
        </>
      )}
    </section>
  );
}
