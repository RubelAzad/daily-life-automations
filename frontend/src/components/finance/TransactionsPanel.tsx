"use client";

import { useEffect, useState, type FormEvent } from "react";
import { control, primary, section, sectionHeader, small } from "@/components/finance/styles";
import {
  ApiError,
  createExpense,
  createIncome,
  deleteExpense,
  deleteIncome,
  listExpenses,
  listIncomes,
  updateExpense,
  updateIncome,
  type Expense,
  type Income,
} from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatMoney } from "@/lib/format";

type Props = {
  month: string;
  currency: string;
  version: number;
  onChanged: (notice?: string) => void;
  onError: (message: string) => void;
};

type Kind = "expense" | "income";
type Row = { kind: Kind; id: number; amount: number; label: string; detail: string | null; date: string; billID: number | null };
type Draft = { kind: Kind; id: number | null; amount: string; label: string; detail: string; date: string };

const today = () => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
};
const empty = (kind: Kind): Draft => ({ kind, id: null, amount: "", label: "", detail: "", date: today() });

/** The month's expenses and income, newest first, with a quick-add form. */
export default function TransactionsPanel({ month, currency, version, onChanged, onError }: Props) {
  const [rows, setRows] = useState<Row[] | null>(null);
  const [draft, setDraft] = useState<Draft>(empty("expense"));
  const [fields, setFields] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [filter, setFilter] = useState<"" | Kind>("");

  useEffect(() => {
    let cancelled = false;
    Promise.all([listExpenses({ month, per_page: 100 }), listIncomes({ month, per_page: 100 })])
      .then(([e, i]) => {
        if (cancelled) return;
        setRows(
          [...e.data.map(fromExpense), ...i.data.map(fromIncome)].sort((a, b) => (a.date === b.date ? b.id - a.id : a.date < b.date ? 1 : -1)),
        );
      })
      .catch((err) => onError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, [month, version, onError]);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setFields({});
    try {
      const amount = draft.amount.trim();
      const detail = draft.detail.trim() || null;
      if (draft.kind === "expense") {
        const input = { amount, category: draft.label, description: detail, spent_on: draft.date };
        if (draft.id) await updateExpense(draft.id, input);
        else await createExpense(input);
      } else {
        const input = { amount, source: draft.label, description: detail, received_on: draft.date };
        if (draft.id) await updateIncome(draft.id, input);
        else await createIncome(input);
      }
      onChanged(`${draft.id ? "Updated" : "Added"} ${draft.kind === "expense" ? "expense" : "income"}: ${formatMoney(Number(amount), currency)}.`);
      setDraft(empty(draft.kind));
    } catch (err) {
      if (err instanceof ApiError && err.fields) setFields(err.fields);
      else onError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function remove(r: Row) {
    if (!confirm(`Delete this ${r.kind} of ${formatMoney(r.amount, currency)}?`)) return;
    try {
      await (r.kind === "expense" ? deleteExpense(r.id) : deleteIncome(r.id));
      onChanged("Deleted.");
    } catch (err) {
      onError(errorMessage(err));
    }
  }

  const fieldError = (...keys: string[]) => {
    const msg = keys.map((k) => fields[k]).find(Boolean);
    return msg ? <span className="text-xs text-red-600 dark:text-red-400">{msg}</span> : null;
  };
  const shown = (rows ?? []).filter((r) => !filter || r.kind === filter);
  const isExpense = draft.kind === "expense";

  return (
    <section className={section} aria-labelledby="tx-heading">
      <div className={sectionHeader}>
        <h2 id="tx-heading" className="text-sm font-medium">
          Transactions
        </h2>
        <div className="flex overflow-hidden rounded-md border border-zinc-300 text-xs dark:border-zinc-700" role="tablist" aria-label="Show">
          {(
            [
              ["", "All"],
              ["expense", "Spent"],
              ["income", "Income"],
            ] as const
          ).map(([v, label]) => (
            <button
              key={v}
              type="button"
              role="tab"
              aria-selected={filter === v}
              onClick={() => setFilter(v)}
              className="px-2.5 py-1 aria-selected:bg-zinc-900 aria-selected:text-white dark:aria-selected:bg-zinc-100 dark:aria-selected:text-zinc-900"
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      <form onSubmit={onSubmit} className="flex flex-col gap-2 border-b border-zinc-200 px-3 py-3 dark:border-zinc-800" aria-label={draft.id ? "Edit transaction" : "Add a transaction"}>
        <div className="flex flex-wrap items-start gap-2">
          <select
            value={draft.kind}
            onChange={(e) => setDraft({ ...draft, kind: e.target.value as Kind, label: "" })}
            disabled={draft.id !== null}
            aria-label="Type"
            className={control}
          >
            <option value="expense">Spent</option>
            <option value="income">Received</option>
          </select>
          <label className="flex flex-col gap-0.5">
            <input
              inputMode="decimal"
              value={draft.amount}
              onChange={(e) => setDraft({ ...draft, amount: e.target.value })}
              placeholder={`Amount (${currency})`}
              aria-label="Amount"
              className={`${control} w-32`}
            />
            {fieldError("amount")}
          </label>
          <label className="flex flex-col gap-0.5">
            <input
              list={isExpense ? "finance-categories" : undefined}
              value={draft.label}
              onChange={(e) => setDraft({ ...draft, label: e.target.value })}
              placeholder={isExpense ? "Category" : "Source, e.g. Salary"}
              aria-label={isExpense ? "Category" : "Source"}
              className={`${control} w-40`}
            />
            {fieldError("category", "source")}
          </label>
          <input
            value={draft.detail}
            onChange={(e) => setDraft({ ...draft, detail: e.target.value })}
            placeholder="Note (optional)"
            aria-label="Note"
            className={`${control} min-w-0 flex-1 basis-40`}
          />
          <label className="flex flex-col gap-0.5">
            <input type="date" value={draft.date} onChange={(e) => setDraft({ ...draft, date: e.target.value })} aria-label="Date" className={control} />
            {fieldError("spent_on", "received_on")}
          </label>
          <button type="submit" disabled={busy || !draft.amount.trim() || !draft.label.trim()} className={primary}>
            {draft.id ? "Save" : "Add"}
          </button>
          {draft.id && (
            <button type="button" onClick={() => setDraft(empty(draft.kind))} className="text-sm underline">
              Cancel
            </button>
          )}
        </div>
      </form>

      {rows === null ? (
        <p className="px-3 py-4 text-sm text-zinc-500">Loading…</p>
      ) : shown.length === 0 ? (
        <p className="px-3 py-4 text-sm text-zinc-500">Nothing recorded this month.</p>
      ) : (
        <ul className="divide-y divide-zinc-200 dark:divide-zinc-800">
          {shown.map((r) => (
            <li key={`${r.kind}-${r.id}`} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
              <span className="w-20 shrink-0 text-xs text-zinc-500">{shortDate(r.date)}</span>
              <span className="min-w-0 flex-1 basis-40 truncate">
                <span className="font-medium">{r.label}</span>
                {r.detail && <span className="text-zinc-500"> · {r.detail}</span>}
                {r.billID && <span className="ml-1 rounded bg-zinc-100 px-1 text-xs text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300">bill</span>}
              </span>
              <span className={`ml-auto tabular-nums ${r.kind === "income" ? "text-green-700 dark:text-green-400" : ""}`}>
                {r.kind === "income" ? "+" : "−"}
                {formatMoney(r.amount, currency)}
              </span>
              <span className="flex gap-1.5">
                <button
                  type="button"
                  onClick={() => setDraft({ kind: r.kind, id: r.id, amount: r.amount.toFixed(2), label: r.label, detail: r.detail ?? "", date: r.date })}
                  className={small}
                >
                  Edit
                </button>
                <button type="button" onClick={() => remove(r)} className={small}>
                  Delete
                </button>
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function fromExpense(e: Expense): Row {
  return { kind: "expense", id: e.id, amount: e.amount, label: e.category, detail: e.description, date: e.spent_on, billID: e.bill_id };
}

function fromIncome(i: Income): Row {
  return { kind: "income", id: i.id, amount: i.amount, label: i.source, detail: i.description, date: i.received_on, billID: null };
}

function shortDate(d: string) {
  const [y, m, day] = d.split("-").map(Number);
  return new Date(y, m - 1, day).toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short" });
}
