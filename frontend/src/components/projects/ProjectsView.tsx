"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import AppHeader, { ErrorBanner, ProgressBar } from "@/components/AppHeader";
import { createProject, listProjects, type Project } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatDate, isPastDate } from "@/lib/format";
import { useRequireUser } from "@/lib/useRequireUser";
import { StatusBadge } from "./StatusBadge";

const TABS = [
  { value: "", label: "Current" },
  { value: "archived", label: "Archived" },
];

export default function ProjectsView() {
  const router = useRouter();
  const { user, error: authError } = useRequireUser();
  const [tab, setTab] = useState("");
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [targetDate, setTargetDate] = useState("");
  const [creating, setCreating] = useState(false);

  const showError = useCallback((err: unknown) => setError(errorMessage(err)), []);

  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    listProjects({ status: tab, per_page: 100 })
      .then((r) => {
        if (!cancelled) setProjects(r.data);
      })
      .catch(showError);
    return () => {
      cancelled = true;
    };
  }, [user, tab, showError]);

  async function onCreate(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    setCreating(true);
    try {
      const project = await createProject({ name, target_date: targetDate || null });
      router.push(`/projects/${project.id}`);
    } catch (err) {
      showError(err);
      setCreating(false);
    }
  }

  if (!user) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  const control = "rounded-md border border-zinc-300 bg-transparent px-3 py-1.5 text-sm dark:border-zinc-700";

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">Projects</h1>
        <div className="flex overflow-hidden rounded-md border border-zinc-300 text-sm dark:border-zinc-700" role="tablist">
          {TABS.map((t) => (
            <button
              key={t.value}
              type="button"
              role="tab"
              aria-selected={tab === t.value}
              onClick={() => {
                setTab(t.value);
                setProjects(null);
              }}
              className="px-3 py-1.5 aria-selected:bg-zinc-900 aria-selected:text-white dark:aria-selected:bg-zinc-100 dark:aria-selected:text-zinc-900"
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      <form onSubmit={onCreate} className="flex flex-wrap items-center gap-2 rounded-md border border-zinc-200 p-3 dark:border-zinc-800">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="New project, e.g. Move to new flat"
          maxLength={150}
          aria-label="Project name"
          className="min-w-0 flex-1 bg-transparent px-1 py-1 text-base outline-none"
        />
        <label className="flex items-center gap-2 text-sm">
          <span className="text-zinc-500">Target</span>
          <input type="date" value={targetDate} onChange={(e) => setTargetDate(e.target.value)} aria-label="Target date" className={control} />
        </label>
        <button
          type="submit"
          disabled={creating || !name.trim()}
          className="rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900"
        >
          {creating ? "Creating…" : "Create project"}
        </button>
      </form>

      {projects === null ? (
        <p className="text-zinc-500">Loading projects…</p>
      ) : projects.length === 0 ? (
        <p className="rounded-md border border-dashed border-zinc-300 px-4 py-8 text-center text-zinc-500 dark:border-zinc-700">
          {tab === "archived" ? "No archived projects." : "No projects yet. Create one above to group related tasks and milestones."}
        </p>
      ) : (
        <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {projects.map((p) => (
            <li key={p.id}>
              <ProjectCard project={p} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function ProjectCard({ project: p }: { project: Project }) {
  const late = p.target_date && p.status !== "completed" && isPastDate(p.target_date);
  return (
    <Link
      href={`/projects/${p.id}`}
      className="flex h-full flex-col gap-3 rounded-lg border border-zinc-200 p-4 transition-colors hover:border-zinc-400 dark:border-zinc-800 dark:hover:border-zinc-600"
      style={{ borderTopColor: p.color ?? undefined, borderTopWidth: p.color ? 3 : undefined }}
    >
      <div className="flex items-start justify-between gap-2">
        <h2 className="font-medium wrap-break-word">{p.name}</h2>
        <StatusBadge status={p.status} />
      </div>
      {p.description && <p className="line-clamp-2 text-sm text-zinc-500">{p.description}</p>}
      <div className="mt-auto flex flex-col gap-1.5">
        <div className="flex justify-between text-xs text-zinc-500">
          <span>
            {p.progress.completed_tasks}/{p.progress.total_tasks} tasks
            {p.progress.milestones > 0 && ` · ${p.progress.milestones_completed}/${p.progress.milestones} milestones`}
          </span>
          <span>{p.progress.percent}%</span>
        </div>
        <ProgressBar percent={p.progress.percent} color={p.color} label={`${p.name} progress`} />
        <div className="flex flex-wrap gap-x-3 text-xs">
          {p.target_date && <span className={late ? "font-medium text-red-600" : "text-zinc-500"}>Target {formatDate(p.target_date)}</span>}
          {p.progress.overdue_tasks > 0 && <span className="font-medium text-red-600">{p.progress.overdue_tasks} overdue</span>}
        </div>
      </div>
    </Link>
  );
}
