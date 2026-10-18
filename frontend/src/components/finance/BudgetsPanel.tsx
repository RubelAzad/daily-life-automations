"use client";

import { useState, type FormEvent } from "react";
import { control, primary, section, sectionHeader, small } from "@/components/finance/styles";
import { ApiError, createBudget, deleteBudget, updateBudget, type Budget } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatMoney } from "@/lib/format";

type Props = {
  currency: string;
  budgets: Budget[] | null; // with the month's spending, from the summary
  onChanged: (notice?: string) => void;
  onError: (message: string) => void;
};

type Draft = { id: number | null; category: string; limit: string; alert: string };
const empty: Draft = { id: null, category: "", limit: "", alert: "80" };

/** Monthly limits per category (or overall) with this month's progress. */
export default function BudgetsPanel({ currency, budgets, onChanged, onError }: Props) {
  const [draft, setDraft] = useState<Draft | null>(null);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!draft) return;
    setBusy(true);
    setFields({});
    try {
      const input = { category: draft.category.trim() || null, monthly_limit: draft.limit.trim(), alert_percent: Number(draft.alert) };
      if (draft.id) await updateBudget(draft.id, input);
      else await createBudget(input);
      onChanged(`Budget saved: ${draft.category.trim() || "Overall"}.`);
      setDraft(null);
    } catch (err) {
      if (err instanceof ApiError && err.fields) setFields(err.fields);
      else onError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function remove(b: Budget) {
    if (!confirm(`Delete the ${b.category ?? "overall"} budget?`)) return;
    try {
      await deleteBudget(b.id);
      onChanged("Budget deleted.");
    } catch (err) {
      onError(errorMessage(err));
    }
  }

  const fieldError = (key: string) => (fields[key] ? <span className="text-xs text-red-600 dark:text-red-400">{fields[key]}</span> : null);

  return (
    <section className={section} aria-labelledby="budgets-heading">
      <div className={sectionHeader}>
        <h2 id="budgets-heading" className="text-sm font-medium">
          Budgets
        </h2>
        {!draft && (
          <button type="button" onClick={() => setDraft(empty)} className={small}>
            Add budget
          </button>
        )}
      </div>

      {draft && (
        <form onSubmit={onSubmit} className="flex flex-wrap items-start gap-2 border-b border-zinc-200 px-3 py-3 dark:border-zinc-800" aria-label="Budget">
          <label className="flex flex-col gap-0.5 text-xs text-zinc-500">
            Category (empty = all spending)
            <input list="finance-categories" value={draft.category} onChange={(e) => setDraft({ ...draft, category: e.target.value })} className={`${control} w-44`} />
            {fieldError("category")}
          </label>
          <label className="flex flex-col gap-0.5 text-xs text-zinc-500">
            Monthly limit ({currency})
            <input inputMode="decimal" value={draft.limit} onChange={(e) => setDraft({ ...draft, limit: e.target.value })} className={`${control} w-32`} />
            {fieldError("monthly_limit")}
          </label>
          <label className="flex flex-col gap-0.5 text-xs text-zinc-500">
            Alert at (%)
            <input type="number" min={1} max={100} value={draft.alert} onChange={(e) => setDraft({ ...draft, alert: e.target.value })} className={`${control} w-20`} />
            {fieldError("alert_percent")}
          </label>
          <div className="flex items-center gap-2 self-end">
            <button type="submit" disabled={busy || !draft.limit.trim()} className={primary}>
              Save
            </button>
            <button type="button" onClick={() => setDraft(null)} className="text-sm underline">
              Cancel
            </button>
          </div>
        </form>
      )}

      {budgets === null ? (
        <p className="px-3 py-4 text-sm text-zinc-500">Loading…</p>
      ) : budgets.length === 0 ? (
        <p className="px-3 py-4 text-sm text-zinc-500">No budgets. Add one to get an alert when spending in a category gets close to its limit.</p>
      ) : (
        <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
          {budgets.map((b) => {
            const pct = Math.round((b.spent / b.monthly_limit) * 100);
            const tone = pct >= 100 ? "bg-red-500" : pct >= b.alert_percent ? "bg-amber-500" : "bg-green-600";
            return (
              <li key={b.id} className="flex flex-col gap-1.5 px-3 py-2 text-sm">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{b.category ?? "Overall"}</span>
                  <span className="text-xs text-zinc-500">alert at {b.alert_percent}%</span>
                  <span className="ml-auto text-right tabular-nums">
                    {formatMoney(b.spent, currency)} <span className="text-zinc-500">of {formatMoney(b.monthly_limit, currency)}</span>
                  </span>
                  <span className={`w-12 text-right text-xs tabular-nums ${pct >= 100 ? "text-red-600 dark:text-red-400" : "text-zinc-500"}`}>{pct}%</span>
                  <button
                    type="button"
                    onClick={() => setDraft({ id: b.id, category: b.category ?? "", limit: b.monthly_limit.toFixed(2), alert: String(b.alert_percent) })}
                    className={small}
                  >
                    Edit
                  </button>
                  <button type="button" onClick={() => remove(b)} className={small}>
                    Delete
                  </button>
                </div>
                <div
                  className="h-2 overflow-hidden rounded-full bg-zinc-100 dark:bg-zinc-800"
                  role="progressbar"
                  aria-label={`${b.category ?? "Overall"} budget used`}
                  aria-valuenow={pct}
                  aria-valuemin={0}
                  aria-valuemax={100}
                >
                  <div className={`h-full rounded-full ${tone}`} style={{ width: `${Math.min(100, pct)}%` }} />
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
