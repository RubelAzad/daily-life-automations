"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import AppHeader, { ErrorBanner, ProgressBar } from "@/components/AppHeader";
import NewTaskForm from "@/components/tasks/NewTaskForm";
import TaskItem from "@/components/tasks/TaskItem";
import {
  ApiError,
  createMilestone,
  deleteMilestone,
  deleteProject,
  getProject,
  listProjects,
  listTaskLists,
  listTasks,
  PROJECT_STATUSES,
  updateMilestone,
  updateProject,
  type Milestone,
  type Project,
  type ProjectStatus,
  type Task,
  type TaskList,
} from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatDate, isPastDate } from "@/lib/format";
import { useRequireUser } from "@/lib/useRequireUser";
import { STATUS_LABEL, StatusBadge } from "./StatusBadge";

const control = "rounded-md border border-zinc-300 bg-transparent px-2 py-1.5 text-sm dark:border-zinc-700";

export default function ProjectDetail({ id }: { id: number }) {
  const router = useRouter();
  const { user, error: authError } = useRequireUser();
  const [project, setProject] = useState<Project | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [lists, setLists] = useState<TaskList[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [showDone, setShowDone] = useState(false);
  const [editing, setEditing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [version, setVersion] = useState(0);

  const reload = useCallback(() => setVersion((v) => v + 1), []);
  const showError = useCallback((err: unknown) => setError(errorMessage(err)), []);

  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    Promise.all([
      getProject(id),
      listTasks({ project_id: id, status: showDone ? "" : "open", sort: "due_at", per_page: 100 }),
    ])
      .then(([p, t]) => {
        if (cancelled) return;
        setProject(p);
        setTasks(t.data);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 404) setNotFound(true);
        else showError(err);
      });
    return () => {
      cancelled = true;
    };
  }, [user, id, showDone, version, showError]);

  useEffect(() => {
    if (!user) return;
    listTaskLists().then(setLists).catch(showError);
    listProjects({ per_page: 100 })
      .then((r) => setProjects(r.data))
      .catch(showError);
  }, [user, showError]);

  async function run(action: () => Promise<unknown>) {
    try {
      await action();
      reload();
    } catch (err) {
      showError(err);
    }
  }

  async function onDelete() {
    if (!project) return;
    if (!window.confirm(`Delete "${project.name}"? Its milestones are deleted; its tasks are kept and moved out of the project.`)) return;
    try {
      await deleteProject(project.id);
      router.replace("/projects");
    } catch (err) {
      showError(err);
    }
  }

  if (!user) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  if (notFound) {
    return (
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-4 py-6">
        <AppHeader user={user} />
        <p>
          Project not found.{" "}
          <Link href="/projects" className="underline">
            Back to projects
          </Link>
        </p>
      </div>
    );
  }

  const milestones = project?.milestones ?? [];
  const otherTasks = tasks.filter((t) => t.milestone_id === null);
  const late = project?.target_date && project.status !== "completed" && isPastDate(project.target_date);

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      <Link href="/projects" className="text-sm text-zinc-500 underline">
        ← All projects
      </Link>

      {!project ? (
        <p className="text-zinc-500">Loading project…</p>
      ) : editing ? (
        <ProjectEditor
          project={project}
          onCancel={() => setEditing(false)}
          onSave={(patch) =>
            run(async () => {
              await updateProject(project.id, patch);
              setEditing(false);
            })
          }
        />
      ) : (
        <section className="flex flex-col gap-3">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="flex min-w-0 items-center gap-3">
              {project.color && <span className="size-3 shrink-0 rotate-45" style={{ backgroundColor: project.color }} aria-hidden />}
              <h1 className="text-2xl font-semibold tracking-tight wrap-break-word">{project.name}</h1>
              <StatusBadge status={project.status} />
            </div>
            <div className="flex items-center gap-3 text-sm">
              <select
                value={project.status}
                onChange={(e) => run(() => updateProject(project.id, { status: e.target.value as ProjectStatus }))}
                aria-label="Project status"
                className={control}
              >
                {PROJECT_STATUSES.map((s) => (
                  <option key={s} value={s}>
                    {STATUS_LABEL[s]}
                  </option>
                ))}
              </select>
              <button type="button" onClick={() => setEditing(true)} className="underline">
                Edit
              </button>
              <button type="button" onClick={onDelete} className="text-red-600 underline">
                Delete
              </button>
            </div>
          </div>

          <div className="flex flex-wrap gap-x-4 text-sm text-zinc-500">
            {project.start_date && <span>Started {formatDate(project.start_date)}</span>}
            {project.target_date && <span className={late ? "font-medium text-red-600" : ""}>Target {formatDate(project.target_date)}</span>}
          </div>

          {project.description && <p className="whitespace-pre-wrap text-zinc-700 dark:text-zinc-300">{project.description}</p>}

          <div className="flex flex-col gap-1.5">
            <div className="flex justify-between text-sm text-zinc-500">
              <span>
                {project.progress.completed_tasks} of {project.progress.total_tasks} tasks done
                {project.progress.milestones > 0 && ` · ${project.progress.milestones_completed}/${project.progress.milestones} milestones`}
                {project.progress.overdue_tasks > 0 && <span className="font-medium text-red-600"> · {project.progress.overdue_tasks} overdue</span>}
              </span>
              <span className="font-medium text-zinc-900 dark:text-zinc-100">{project.progress.percent}%</span>
            </div>
            <ProgressBar percent={project.progress.percent} color={project.color} label="Project progress" />
          </div>
        </section>
      )}

      {project && (
        <>
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold">Milestones & tasks</h2>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={showDone} onChange={(e) => setShowDone(e.target.checked)} />
              Show completed tasks
            </label>
          </div>

          {milestones.map((m) => (
            <MilestoneSection
              key={m.id}
              milestone={m}
              tasks={tasks.filter((t) => t.milestone_id === m.id)}
              lists={lists}
              projects={projects}
              projectId={project.id}
              onChanged={reload}
              onError={showError}
              onToggle={() => run(() => updateMilestone(m.id, { completed: !m.completed }))}
              onDelete={() =>
                window.confirm(`Delete milestone "${m.title}"? Its tasks stay in the project.`) && run(() => deleteMilestone(m.id))
              }
            />
          ))}

          <section className="flex flex-col gap-2">
            <h3 className="font-medium">{milestones.length ? "Other tasks" : "Tasks"}</h3>
            <TaskGroup tasks={otherTasks} lists={lists} projects={projects} onChanged={reload} onError={showError} />
            <NewTaskForm lists={lists} defaultListId={null} projectId={project.id} onCreated={reload} onError={showError} />
          </section>

          <AddMilestoneForm onAdd={(title, due) => run(() => createMilestone(project.id, { title, due_date: due || null }))} />
        </>
      )}
    </div>
  );
}

function MilestoneSection(props: {
  milestone: Milestone;
  tasks: Task[];
  lists: TaskList[];
  projects: Project[];
  projectId: number;
  onChanged: () => void;
  onError: (err: unknown) => void;
  onToggle: () => void;
  onDelete: () => void;
}) {
  const { milestone: m, tasks, lists, projects, projectId, onChanged, onError, onToggle, onDelete } = props;
  const late = m.due_date && !m.completed && isPastDate(m.due_date);
  return (
    <section className="flex flex-col gap-2 rounded-lg border border-zinc-200 p-3 dark:border-zinc-800">
      <div className="flex flex-wrap items-center gap-3">
        <input
          type="checkbox"
          checked={m.completed}
          onChange={onToggle}
          aria-label={m.completed ? `Reopen milestone ${m.title}` : `Complete milestone ${m.title}`}
          className="size-4 accent-zinc-900 dark:accent-zinc-100"
        />
        <h3 className={`min-w-0 flex-1 font-medium wrap-break-word ${m.completed ? "text-zinc-500 line-through" : ""}`}>{m.title}</h3>
        {m.due_date && <span className={`text-sm ${late ? "font-medium text-red-600" : "text-zinc-500"}`}>Due {formatDate(m.due_date)}</span>}
        <span className="text-sm text-zinc-500">
          {m.progress.completed_tasks}/{m.progress.total_tasks}
        </span>
        <button type="button" onClick={onDelete} aria-label={`Delete milestone ${m.title}`} className="text-zinc-400 hover:text-red-600">
          ×
        </button>
      </div>
      {m.progress.total_tasks > 0 && <ProgressBar percent={m.progress.percent} label={`${m.title} progress`} />}
      <TaskGroup tasks={tasks} lists={lists} projects={projects} onChanged={onChanged} onError={onError} />
      <NewTaskForm
        lists={lists}
        defaultListId={null}
        projectId={projectId}
        milestoneId={m.id}
        placeholder={`Add a task to "${m.title}"…`}
        onCreated={onChanged}
        onError={onError}
      />
    </section>
  );
}

function TaskGroup(props: { tasks: Task[]; lists: TaskList[]; projects: Project[]; onChanged: () => void; onError: (err: unknown) => void }) {
  if (props.tasks.length === 0) return null;
  return (
    <ul className="flex flex-col divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
      {props.tasks.map((t) => (
        <TaskItem
          key={t.id}
          task={t}
          lists={props.lists}
          projects={props.projects}
          showList
          showProject={false}
          onChanged={props.onChanged}
          onError={props.onError}
        />
      ))}
    </ul>
  );
}

function AddMilestoneForm({ onAdd }: { onAdd: (title: string, due: string) => Promise<void> }) {
  const [title, setTitle] = useState("");
  const [due, setDue] = useState("");

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!title.trim()) return;
    await onAdd(title, due);
    setTitle("");
    setDue("");
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-wrap items-center gap-2 rounded-md border border-dashed border-zinc-300 p-3 dark:border-zinc-700">
      <input
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        placeholder="+ New milestone"
        maxLength={255}
        aria-label="Milestone title"
        className="min-w-0 flex-1 bg-transparent px-1 py-1 outline-none"
      />
      <input type="date" value={due} onChange={(e) => setDue(e.target.value)} aria-label="Milestone due date" className={control} />
      <button type="submit" disabled={!title.trim()} className="rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900">
        Add milestone
      </button>
    </form>
  );
}

function ProjectEditor({
  project,
  onSave,
  onCancel,
}: {
  project: Project;
  onSave: (patch: Parameters<typeof updateProject>[1]) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description ?? "");
  const [startDate, setStartDate] = useState(project.start_date ?? "");
  const [targetDate, setTargetDate] = useState(project.target_date ?? "");
  const [color, setColor] = useState(project.color ?? "#3b82f6");
  const [useColor, setUseColor] = useState(project.color !== null);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    onSave({
      name,
      description: description.trim() ? description : null,
      start_date: startDate || null,
      target_date: targetDate || null,
      color: useColor ? color : null,
    });
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-3 rounded-lg border border-zinc-200 p-4 dark:border-zinc-800">
      <input value={name} onChange={(e) => setName(e.target.value)} maxLength={150} aria-label="Project name" className={`${control} text-lg`} autoFocus />
      <textarea
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        rows={4}
        placeholder="Notes: goals, links, decisions…"
        aria-label="Project notes"
        className={control}
      />
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <label className="flex items-center gap-2">
          <span className="text-zinc-500">Start</span>
          <input type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} className={control} />
        </label>
        <label className="flex items-center gap-2">
          <span className="text-zinc-500">Target</span>
          <input type="date" value={targetDate} onChange={(e) => setTargetDate(e.target.value)} className={control} />
        </label>
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={useColor} onChange={(e) => setUseColor(e.target.checked)} />
          <span className="text-zinc-500">Colour</span>
          <input type="color" value={color} disabled={!useColor} onChange={(e) => setColor(e.target.value)} aria-label="Project colour" />
        </label>
      </div>
      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="px-3 py-1.5 text-sm underline">
          Cancel
        </button>
        <button type="submit" disabled={!name.trim()} className="rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900">
          Save
        </button>
      </div>
    </form>
  );
}
