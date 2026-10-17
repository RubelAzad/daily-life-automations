"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import {
  ApiError,
  getMe,
  listTaskLists,
  listTasks,
  logout,
  type PageMeta,
  type Task,
  type TaskList,
  type TaskQuery,
  type User,
} from "@/lib/api";
import ListSidebar from "./ListSidebar";
import NewTaskForm from "./NewTaskForm";
import TaskItem from "./TaskItem";

const STATUS_TABS = [
  { value: "open", label: "Open" },
  { value: "completed", label: "Completed" },
  { value: "", label: "All" },
];

const SORTS: { value: NonNullable<TaskQuery["sort"]>; label: string }[] = [
  { value: "due_at", label: "Due date" },
  { value: "priority", label: "Priority" },
  { value: "-created_at", label: "Newest" },
];

const PER_PAGE = 20;

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    const fields = err.fields ? Object.entries(err.fields).map(([k, v]) => `${k} ${v}`) : [];
    return fields.length ? fields.join("; ") : err.message;
  }
  return "Could not reach the API. Is the backend running?";
}

export default function TasksView() {
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);
  const [lists, setLists] = useState<TaskList[]>([]);
  const [listId, setListId] = useState<number | null>(null);
  const [status, setStatus] = useState("open");
  const [sort, setSort] = useState<NonNullable<TaskQuery["sort"]>>("due_at");
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<{ data: Task[]; meta: PageMeta } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [version, setVersion] = useState(0);

  const reload = useCallback(() => setVersion((v) => v + 1), []);

  useEffect(() => {
    getMe()
      .then((u) => (u ? setUser(u) : router.replace("/login")))
      .catch((err) => setError(errorMessage(err)));
  }, [router]);

  // Debounce the search box so typing doesn't fire a request per keystroke.
  useEffect(() => {
    const id = setTimeout(() => {
      setQuery(search.trim());
      setPage(1);
    }, 300);
    return () => clearTimeout(id);
  }, [search]);

  useEffect(() => {
    if (!user) return;
    listTaskLists()
      .then(setLists)
      .catch((err) => setError(errorMessage(err)));
  }, [user, version]);

  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    listTasks({ status, sort, q: query, list_id: listId ?? undefined, page, per_page: PER_PAGE })
      .then((r) => {
        if (!cancelled) setResult(r);
      })
      .catch((err) => {
        if (!cancelled) setError(errorMessage(err));
      });
    return () => {
      cancelled = true;
    };
  }, [user, status, sort, query, listId, page, version]);

  async function onLogout() {
    await logout();
    router.replace("/login");
  }

  if (!user) {
    return <p className="p-6 text-zinc-500">{error ?? "Loading…"}</p>;
  }

  const currentList = lists.find((l) => l.id === listId) ?? null;
  const totalPages = result ? Math.max(1, Math.ceil(result.meta.total / PER_PAGE)) : 1;

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-4 py-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <Link href="/" className="text-lg font-semibold tracking-tight">
          LifeOS AI
        </Link>
        <div className="flex items-center gap-3 text-sm">
          <span className="text-zinc-600 dark:text-zinc-400">{user.name}</span>
          <button type="button" onClick={onLogout} className="underline">
            Sign out
          </button>
        </div>
      </header>

      {error && (
        <div className="flex items-start justify-between gap-3 rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200">
          <span>{error}</span>
          <button type="button" onClick={() => setError(null)} aria-label="Dismiss error">
            ×
          </button>
        </div>
      )}

      <div className="grid gap-6 md:grid-cols-[220px_1fr]">
        <ListSidebar
          lists={lists}
          selected={listId}
          onSelect={(id) => {
            setListId(id);
            setPage(1);
          }}
          onChanged={(deletedId) => {
            if (deletedId !== undefined && deletedId === listId) setListId(null);
            reload();
          }}
          onError={(err) => setError(errorMessage(err))}
        />

        <main className="flex min-w-0 flex-col gap-4">
          <h1 className="text-2xl font-semibold tracking-tight">{currentList?.name ?? "All tasks"}</h1>

          <NewTaskForm
            lists={lists}
            defaultListId={listId}
            onCreated={reload}
            onError={(err) => setError(errorMessage(err))}
          />

          <div className="flex flex-wrap items-center gap-3 text-sm">
            <div className="flex overflow-hidden rounded-md border border-zinc-300 dark:border-zinc-700" role="tablist">
              {STATUS_TABS.map((tab) => (
                <button
                  key={tab.value}
                  type="button"
                  role="tab"
                  aria-selected={status === tab.value}
                  onClick={() => {
                    setStatus(tab.value);
                    setPage(1);
                  }}
                  className="px-3 py-1.5 aria-selected:bg-zinc-900 aria-selected:text-white dark:aria-selected:bg-zinc-100 dark:aria-selected:text-zinc-900"
                >
                  {tab.label}
                </button>
              ))}
            </div>
            <label className="flex items-center gap-2">
              <span className="text-zinc-500">Sort</span>
              <select
                value={sort}
                onChange={(e) => setSort(e.target.value as typeof sort)}
                className="rounded-md border border-zinc-300 bg-transparent px-2 py-1.5 dark:border-zinc-700"
              >
                {SORTS.map((s) => (
                  <option key={s.value} value={s.value}>
                    {s.label}
                  </option>
                ))}
              </select>
            </label>
            <input
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search tasks…"
              className="min-w-0 flex-1 rounded-md border border-zinc-300 bg-transparent px-3 py-1.5 dark:border-zinc-700"
            />
          </div>

          {!result ? (
            <p className="text-zinc-500">Loading tasks…</p>
          ) : result.data.length === 0 ? (
            <p className="rounded-md border border-dashed border-zinc-300 px-4 py-8 text-center text-zinc-500 dark:border-zinc-700">
              {query ? "No tasks match your search." : status === "completed" ? "No completed tasks yet." : "Nothing here. Add a task above."}
            </p>
          ) : (
            <ul className="flex flex-col divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
              {result.data.map((task) => (
                <TaskItem
                  key={task.id}
                  task={task}
                  lists={lists}
                  showList={listId === null}
                  onChanged={reload}
                  onError={(err) => setError(errorMessage(err))}
                />
              ))}
            </ul>
          )}

          {result && result.meta.total > PER_PAGE && (
            <nav className="flex items-center justify-between text-sm" aria-label="Pagination">
              <button type="button" disabled={page <= 1} onClick={() => setPage(page - 1)} className="underline disabled:no-underline disabled:opacity-40">
                Previous
              </button>
              <span className="text-zinc-500">
                Page {page} of {totalPages} · {result.meta.total} tasks
              </span>
              <button type="button" disabled={page >= totalPages} onClick={() => setPage(page + 1)} className="underline disabled:no-underline disabled:opacity-40">
                Next
              </button>
            </nav>
          )}
        </main>
      </div>
    </div>
  );
}
