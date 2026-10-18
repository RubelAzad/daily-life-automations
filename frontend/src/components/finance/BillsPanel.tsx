"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";
import { control, primary, section, sectionHeader, small } from "@/components/finance/styles";
import {
  addAutomationTemplate,
  ApiError,
  createBill,
  createBillTask,
  deleteBill,
  listAutomationTemplates,
  listBills,
  payBill,
  updateBill,
  type Bill,
  type BillFrequency,
} from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatMoney } from "@/lib/format";

type Props = {
  currency: string;
  version: number;
  onChanged: (notice?: string) => void;
  onError: (message: string) => void;
};

type Draft = { id: number | null; name: string; amount: string; category: string; frequency: BillFrequency; due: string; remind: string; active: boolean };

const FREQUENCIES: { value: BillFrequency; label: string }[] = [
  { value: "monthly", label: "Monthly" },
  { value: "weekly", label: "Weekly" },
  { value: "quarterly", label: "Every 3 months" },
  { value: "yearly", label: "Yearly" },
];

const empty: Draft = { id: null, name: "", amount: "", category: "Utilities", frequency: "monthly", due: "", remind: "3", active: true };

function localToday() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

function daysUntil(date: string) {
  const [y, m, d] = date.split("-").map(Number);
  const [ty, tm, td] = localToday().split("-").map(Number);
  return Math.round((Date.UTC(y, m - 1, d) - Date.UTC(ty, tm - 1, td)) / 86_400_000);
}

function dueText(date: string) {
  const n = daysUntil(date);
  if (n < 0) return `${-n} day${n === -1 ? "" : "s"} overdue`;
  if (n === 0) return "due today";
  if (n === 1) return "due tomorrow";
  const [y, m, d] = date.split("-").map(Number);
  return `due ${new Date(y, m - 1, d).toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short" })} (in ${n} days)`;
}

/** Recurring bills: when they're due, mark paid, and the reminder workflow. */
export default function BillsPanel({ currency, version, onChanged, onError }: Props) {
  const [bills, setBills] = useState<Bill[] | null>(null);
  const [reminders, setReminders] = useState<boolean | null>(null); // "Bill reminders" workflow added
  const [draft, setDraft] = useState<Draft | null>(null);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<number | "form" | "workflow" | null>(null);

  useEffect(() => {
    let cancelled = false;
    Promise.all([listBills(), listAutomationTemplates()])
      .then(([b, t]) => {
        if (cancelled) return;
        setBills(b);
        setReminders(t.some((x) => x.key === "bill_reminders" && x.automation_id !== null));
      })
      .catch((err) => onError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, [version, onError]);

  async function run(key: number | "form" | "workflow", fn: () => Promise<string | void>) {
    setBusy(key);
    try {
      const msg = await fn();
      onChanged(msg || undefined);
    } catch (err) {
      if (err instanceof ApiError && err.fields && key === "form") setFields(err.fields);
      else onError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!draft) return;
    setFields({});
    run("form", async () => {
      const input = {
        name: draft.name,
        amount: draft.amount.trim(),
        category: draft.category,
        frequency: draft.frequency,
        next_due_on: draft.due,
        remind_days_before: Number(draft.remind),
        active: draft.active,
      };
      if (draft.id) await updateBill(draft.id, input);
      else await createBill(input);
      setDraft(null);
      return `Saved “${draft.name.trim()}”.`;
    });
  }

  function markPaid(b: Bill) {
    const raw = prompt(`Record a payment for “${b.name}”. Amount (${currency}):`, b.amount.toFixed(2));
    if (raw === null) return;
    run(b.id, async () => {
      const r = await payBill(b.id, { amount: raw.trim() });
      return `Recorded ${formatMoney(r.expense.amount, currency)} for “${b.name}”. Next due ${r.bill.next_due_on}.`;
    });
  }

  const fieldError = (key: string) => (fields[key] ? <span className="text-xs text-red-600 dark:text-red-400">{fields[key]}</span> : null);
  const monthlyTotal = (bills ?? [])
    .filter((b) => b.active)
    .reduce((sum, b) => sum + (b.frequency === "weekly" ? (b.amount * 52) / 12 : b.frequency === "quarterly" ? b.amount / 3 : b.frequency === "yearly" ? b.amount / 12 : b.amount), 0);

  return (
    <section className={section} aria-labelledby="bills-heading">
      <div className={sectionHeader}>
        <h2 id="bills-heading" className="text-sm font-medium">
          Bills{bills && bills.length > 0 && <span className="font-normal text-zinc-500"> · about {formatMoney(monthlyTotal, currency)} a month</span>}
        </h2>
        {!draft && (
          <button type="button" onClick={() => setDraft({ ...empty, due: localToday() })} className={small}>
            Add bill
          </button>
        )}
      </div>

      {reminders === false && (
        <div className="flex flex-wrap items-center gap-2 border-b border-zinc-200 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-zinc-800 dark:bg-amber-950 dark:text-amber-200">
          <span>Get a reminder and a “Pay …” task before each bill is due; completing the task records the payment.</span>
          <button
            type="button"
            disabled={busy !== null}
            onClick={() =>
              run("workflow", async () => {
                await addAutomationTemplate("bill_reminders");
                return "Bill reminders are on. Edit them on the Automations page.";
              })
            }
            className={small}
          >
            Turn on bill reminders
          </button>
        </div>
      )}
      {reminders && (
        <p className="border-b border-zinc-200 px-3 py-1.5 text-xs text-zinc-500 dark:border-zinc-800">
          Reminders are on (<Link href="/automations" className="underline">Bill reminders</Link>): a notification and a “Pay …” task before each due date.
        </p>
      )}

      {draft && (
        <form onSubmit={onSubmit} className="flex flex-col gap-2 border-b border-zinc-200 px-3 py-3 dark:border-zinc-800" aria-label="Bill">
          <div className="flex flex-wrap items-start gap-2">
            <label className="flex flex-col gap-0.5 text-xs text-zinc-500">
              Name
              <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} placeholder="Electricity" className={`${control} w-44`} />
              {fieldError("name")}
            </label>
            <label className="flex flex-col gap-0.5 text-xs text-zinc-500">
              Amount ({currency})
              <input inputMode="decimal" value={draft.amount} onChange={(e) => setDraft({ ...draft, amount: e.target.value })} className={`${control} w-32`} />
              {fieldError("amount")}
            </label>
            <label className="flex flex-col gap-0.5 text-xs text-zinc-500">
              Category
              <input list="finance-categories" value={draft.category} onChange={(e) => setDraft({ ...draft, category: e.target.value })} className={`${control} w-40`} />
              {fieldError("category")}
            </label>
            <label className="flex flex-col gap-0.5 text-xs text-zinc-500">
              Repeats
              <select value={draft.frequency} onChange={(e) => setDraft({ ...draft, frequency: e.target.value as BillFrequency })} className={control}>
                {FREQUENCIES.map((f) => (
                  <option key={f.value} value={f.value}>
                    {f.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-0.5 text-xs text-zinc-500">
              Next due
              <input type="date" value={draft.due} onChange={(e) => setDraft({ ...draft, due: e.target.value })} className={control} />
              {fieldError("next_due_on")}
            </label>
            <label className="flex flex-col gap-0.5 text-xs text-zinc-500">
              Remind days before
              <input type="number" min={0} max={14} value={draft.remind} onChange={(e) => setDraft({ ...draft, remind: e.target.value })} className={`${control} w-20`} />
              {fieldError("remind_days_before")}
            </label>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            {draft.id && (
              <label className="flex items-center gap-1.5 text-sm">
                <input type="checkbox" checked={draft.active} onChange={(e) => setDraft({ ...draft, active: e.target.checked })} />
                Active
              </label>
            )}
            <button type="submit" disabled={busy === "form" || !draft.name.trim() || !draft.amount.trim() || !draft.due} className={primary}>
              Save
            </button>
            <button type="button" onClick={() => setDraft(null)} className="text-sm underline">
              Cancel
            </button>
            <span className="text-xs text-zinc-500">A bill due on the 31st is due on the last day of shorter months.</span>
          </div>
        </form>
      )}

      {bills === null ? (
        <p className="px-3 py-4 text-sm text-zinc-500">Loading…</p>
      ) : bills.length === 0 ? (
        <p className="px-3 py-4 text-sm text-zinc-500">No bills yet. Add rent, electricity, internet or loan payments to see what&rsquo;s coming up.</p>
      ) : (
        <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
          {bills.map((b) => {
            const n = daysUntil(b.next_due_on);
            const tone = !b.active ? "text-zinc-400" : n < 0 ? "text-red-600 dark:text-red-400" : n <= b.remind_days_before ? "text-amber-700 dark:text-amber-400" : "text-zinc-500";
            return (
              <li key={b.id} className={`flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm ${b.active ? "" : "opacity-60"}`}>
                <span className="min-w-0 flex-1 basis-56">
                  <span className="font-medium">{b.name}</span> <span className="text-xs text-zinc-500">· {b.category} · {FREQUENCIES.find((f) => f.value === b.frequency)?.label}</span>
                  <span className={`block text-xs ${tone}`}>
                    {b.active ? dueText(b.next_due_on) : "paused"}
                    {b.pending_task_id && b.active && (
                      <>
                        {" · "}
                        <Link href="/tasks" className="underline">
                          “Pay {b.name}” task open
                        </Link>
                      </>
                    )}
                    {b.last_paid_on && <span className="text-zinc-500"> · last paid {b.last_paid_on}</span>}
                  </span>
                </span>
                <span className="ml-auto tabular-nums">{formatMoney(b.amount, currency)}</span>
                <span className="flex gap-1.5">
                  {b.active && (
                    <button type="button" disabled={busy === b.id} onClick={() => markPaid(b)} className={small}>
                      Mark paid
                    </button>
                  )}
                  {b.active && !b.pending_task_id && (
                    <button
                      type="button"
                      disabled={busy === b.id}
                      onClick={() =>
                        run(b.id, async () => {
                          const r = await createBillTask(b.id);
                          return `Added “${r.task.title}” to your tasks.`;
                        })
                      }
                      className={small}
                    >
                      Add task
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() =>
                      setDraft({
                        id: b.id,
                        name: b.name,
                        amount: b.amount.toFixed(2),
                        category: b.category,
                        frequency: b.frequency,
                        due: b.next_due_on,
                        remind: String(b.remind_days_before),
                        active: b.active,
                      })
                    }
                    className={small}
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      if (confirm(`Delete “${b.name}”? Payments already recorded stay as expenses.`)) run(b.id, async () => (await deleteBill(b.id), `Deleted “${b.name}”.`));
                    }}
                    className={small}
                  >
                    Delete
                  </button>
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
