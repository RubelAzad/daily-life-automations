"use client";

import { useCallback, useEffect, useState } from "react";
import AppHeader, { ErrorBanner } from "@/components/AppHeader";
import BillsPanel from "@/components/finance/BillsPanel";
import BudgetsPanel from "@/components/finance/BudgetsPanel";
import { section, sectionHeader, small } from "@/components/finance/styles";
import TransactionsPanel from "@/components/finance/TransactionsPanel";
import { getFinanceCategories, getFinanceSummary, type FinanceSummary } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatMoney } from "@/lib/format";
import { useRequireUser } from "@/lib/useRequireUser";

/** "YYYY-MM" of a date in local time. */
function monthOf(d: Date) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

function shiftMonth(month: string, by: number) {
  const [y, m] = month.split("-").map(Number);
  return monthOf(new Date(y, m - 1 + by, 1));
}

function monthLabel(month: string) {
  const [y, m] = month.split("-").map(Number);
  return new Date(y, m - 1, 1).toLocaleDateString(undefined, { month: "long", year: "numeric" });
}

export default function FinanceView() {
  const { user, error: authError } = useRequireUser();
  const [month, setMonth] = useState(() => monthOf(new Date()));
  const [summary, setSummary] = useState<FinanceSummary | null>(null);
  const [categories, setCategories] = useState<string[]>([]);
  const [version, setVersion] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const showError = useCallback((msg: string) => setError(msg), []);
  const changed = useCallback((msg?: string) => {
    setNotice(msg ?? null);
    setVersion((v) => v + 1);
  }, []);

  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    Promise.all([getFinanceSummary(month), getFinanceCategories()])
      .then(([s, c]) => {
        if (cancelled) return;
        setSummary(s);
        setCategories(c);
      })
      .catch((err) => setError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, [user, month, version]);

  if (!user) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  const cur = summary?.currency ?? "USD";
  const isThisMonth = month === monthOf(new Date());
  const change = summary && summary.last_month_expenses > 0 ? Math.round(((summary.expenses - summary.last_month_expenses) / summary.last_month_expenses) * 100) : null;
  const maxDay = summary ? Math.max(1, ...summary.by_day.map((d) => d.total)) : 1;
  const maxCat = summary?.by_category[0]?.total ?? 1;

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Finance</h1>
          <p className="text-sm text-zinc-500">Spending, income, budgets and bills, in {cur}. LifeOS only keeps track; it never pays anything.</p>
        </div>
        <div className="flex items-center gap-2 text-sm" role="group" aria-label="Month">
          <button type="button" onClick={() => setMonth(shiftMonth(month, -1))} className={small} aria-label="Previous month">
            ‹
          </button>
          <span className="min-w-32 text-center font-medium">{monthLabel(month)}</span>
          <button type="button" onClick={() => setMonth(shiftMonth(month, 1))} className={small} aria-label="Next month">
            ›
          </button>
          {!isThisMonth && (
            <button type="button" onClick={() => setMonth(monthOf(new Date()))} className="text-xs underline">
              This month
            </button>
          )}
        </div>
      </div>

      {notice && (
        <p role="status" className="text-sm text-green-700 dark:text-green-400">
          {notice}
        </p>
      )}

      {summary && (
        <>
          <div className="grid gap-3 sm:grid-cols-3">
            <Stat label="Spent" value={formatMoney(summary.expenses, cur)}>
              {change !== null && (
                <span className={change > 0 ? "text-red-600 dark:text-red-400" : "text-green-700 dark:text-green-400"}>
                  {change > 0 ? "▲" : "▼"} {Math.abs(change)}% vs last month
                </span>
              )}
            </Stat>
            <Stat label="Income" value={formatMoney(summary.income, cur)} />
            <Stat label="Left over" value={formatMoney(summary.net, cur)} tone={summary.net < 0 ? "text-red-600 dark:text-red-400" : ""} />
          </div>

          <section className={section} aria-labelledby="where-heading">
            <div className={sectionHeader}>
              <h2 id="where-heading" className="text-sm font-medium">
                Where it went
              </h2>
            </div>
            {summary.by_category.length === 0 ? (
              <p className="px-3 py-4 text-sm text-zinc-500">No spending recorded this month.</p>
            ) : (
              <div className="flex flex-col gap-4 px-3 py-3">
                <ul className="flex flex-col gap-2">
                  {summary.by_category.map((c) => (
                    <li key={c.category} className="flex flex-col gap-1 text-sm">
                      <div className="flex justify-between gap-2">
                        <span>
                          {c.category} <span className="text-xs text-zinc-500">· {c.count}</span>
                        </span>
                        <span className="tabular-nums">{formatMoney(c.total, cur)}</span>
                      </div>
                      <div className="h-1.5 overflow-hidden rounded-full bg-zinc-100 dark:bg-zinc-800">
                        <div className="h-full rounded-full bg-sky-500" style={{ width: `${(c.total / maxCat) * 100}%` }} />
                      </div>
                    </li>
                  ))}
                </ul>
                <div aria-label="Spending per day" className="flex h-16 items-end gap-px" role="img">
                  {daysOf(month).map((day) => {
                    const total = summary.by_day.find((d) => d.date === day)?.total ?? 0;
                    return (
                      <div
                        key={day}
                        title={`${day}: ${formatMoney(total, cur)}`}
                        className={`flex-1 rounded-t ${total ? "bg-sky-500/80" : "bg-zinc-100 dark:bg-zinc-800"}`}
                        style={{ height: `${Math.max(4, (total / maxDay) * 100)}%` }}
                      />
                    );
                  })}
                </div>
              </div>
            )}
          </section>
        </>
      )}

      <TransactionsPanel month={month} currency={cur} version={version} onChanged={changed} onError={showError} />
      <BudgetsPanel currency={cur} budgets={summary?.budgets ?? null} onChanged={changed} onError={showError} />
      <BillsPanel currency={cur} version={version} onChanged={changed} onError={showError} />
      {/* Suggestions for every category field on the page. */}
      <datalist id="finance-categories">
        {categories.map((c) => (
          <option key={c} value={c} />
        ))}
      </datalist>
    </div>
  );
}

function Stat({ label, value, tone = "", children }: { label: string; value: string; tone?: string; children?: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md border border-zinc-200 px-3 py-2 dark:border-zinc-800">
      <span className="text-xs text-zinc-500">{label}</span>
      <span className={`text-xl font-semibold tabular-nums ${tone}`}>{value}</span>
      {children && <span className="text-xs">{children}</span>}
    </div>
  );
}

/** Every day of the month, "YYYY-MM-DD". */
function daysOf(month: string) {
  const [y, m] = month.split("-").map(Number);
  const n = new Date(y, m, 0).getDate();
  return Array.from({ length: n }, (_, i) => `${month}-${String(i + 1).padStart(2, "0")}`);
}
