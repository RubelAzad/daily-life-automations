"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";
import {
  addDependency,
  deleteTask,
  getProject,
  listTasks,
  RECURRENCE_FREQUENCIES,
  removeDependency,
  TASK_PRIORITIES,
  TASK_STATUSES,
  updateTask,
  type Milestone,
  type Project,
  type RecurrenceFrequency,
  type Task,
  type TaskList,
  type TaskPriority,
  type TaskRef,
  type TaskStatus,
} from "@/lib/api";
import {
  formatDue,
  formatRecurrence,
  fromDateInputEndOfDay,
  fromLocalInput,
  isOverdue,
  toDateInput,
  toLocalInput,
} from "@/lib/format";
import RemindersPanel from "@/components/RemindersPanel";
import Subtasks from "./Subtasks";

type Props = {
  task: Task;
  lists: TaskList[];
  /** Enables the project badge and the project/milestone picker in the editor. */
  projects?: Project[];
  showList: boolean;
  showProject?: boolean;
  onChanged: () => void;
  onError: (err: unknown) => void;
};

const PRIORITY_STYLE: Record<TaskPriority, string> = {
  urgent: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
  high: "bg-orange-100 text-orange-800 dark:bg-orange-950 dark:text-orange-300",
  medium: "bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300",
  low: "bg-zinc-50 text-zinc-500 dark:bg-zinc-900 dark:text-zinc-400",
};

const label = (s: string) => (s[0].toUpperCase() + s.slice(1)).replace("_", " ");
const isClosed = (s: TaskStatus) => s === "completed" || s === "cancelled";

export default function TaskItem({ task, lists, projects, showList, showProject = true, onChanged, onError }: Props) {
  const [editing, setEditing] = useState(false);
  const [showSubtasks, setShowSubtasks] = useState(false);
  const [busy, setBusy] = useState(false);

  const done = task.status === "completed";
  const closed = isClosed(task.status);
  const list = lists.find((l) => l.id === task.list_id);
  const project = projects?.find((p) => p.id === task.project_id);
  const waitingOn = task.dependencies.filter((d) => !isClosed(d.status));
  const cannotComplete = task.blocked && !done;

  async function run(action: () => Promise<unknown>) {
    setBusy(true);
    try {
      await action();
      onChanged();
    } catch (err) {
      onError(err);
    } finally {
      setBusy(false);
    }
  }

  if (editing) {
    return (
      <li className="p-3">
        <TaskEditor
          task={task}
          lists={lists}
          projects={projects}
          onCancel={() => setEditing(false)}
          onDependenciesChanged={onChanged}
          onError={onError}
          onSave={(patch) =>
            run(async () => {
              await updateTask(task.id, patch);
              setEditing(false);
            })
          }
        />
      </li>
    );
  }

  return (
    <li className="flex items-start gap-3 p-3">
      <input
        type="checkbox"
        checked={done}
        disabled={busy || cannotComplete}
        onChange={() => run(() => updateTask(task.id, { status: done ? "pending" : "completed" }))}
        title={cannotComplete ? `Finish first: ${waitingOn.map((d) => d.title).join(", ")}` : undefined}
        aria-label={done ? `Mark "${task.title}" as not done` : `Mark "${task.title}" as done`}
        className="mt-1 size-4 shrink-0 accent-zinc-900 disabled:cursor-not-allowed dark:accent-zinc-100"
      />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <span className={`wrap-break-word ${closed ? "text-zinc-500 line-through" : ""}`}>{task.title}</span>
        {task.description && <span className="line-clamp-2 text-sm text-zinc-500">{task.description}</span>}
        <div className="flex flex-wrap items-center gap-2 text-xs">
          {task.priority !== "medium" && <span className={`rounded px-1.5 py-0.5 ${PRIORITY_STYLE[task.priority]}`}>{label(task.priority)}</span>}
          {task.status === "in_progress" && <span className="rounded bg-blue-100 px-1.5 py-0.5 text-blue-800 dark:bg-blue-950 dark:text-blue-300">In progress</span>}
          {task.status === "cancelled" && <span className="text-zinc-500">Cancelled</span>}
          {task.due_at && (
            <span className={!closed && isOverdue(task.due_at) ? "font-medium text-red-600" : "text-zinc-500"}>
              {!closed && isOverdue(task.due_at) ? "Overdue · " : ""}
              {formatDue(task.due_at)}
            </span>
          )}
          {task.recurrence && <span className="text-zinc-500">↻ {formatRecurrence(task.recurrence)}</span>}
          {task.subtask_count > 0 && (
            <button type="button" onClick={() => setShowSubtasks(!showSubtasks)} aria-expanded={showSubtasks} className="text-zinc-500 underline">
              ☑ {task.subtasks_completed}/{task.subtask_count}
            </button>
          )}
          {cannotComplete && (
            <span className="rounded bg-amber-100 px-1.5 py-0.5 text-amber-800 dark:bg-amber-950 dark:text-amber-300">
              Waiting on {waitingOn.map((d) => d.title).join(", ")}
            </span>
          )}
          {showProject && project && (
            <Link href={`/projects/${project.id}`} className="flex items-center gap-1 text-zinc-500 underline">
              <span className="size-2 rotate-45" style={{ backgroundColor: project.color ?? "#a1a1aa" }} aria-hidden />
              {project.name}
            </Link>
          )}
          {showList && list && (
            <span className="flex items-center gap-1 text-zinc-500">
              <span className="size-2 rounded-full" style={{ backgroundColor: list.color ?? "#a1a1aa" }} aria-hidden />
              {list.name}
            </span>
          )}
        </div>
        {showSubtasks && <Subtasks parentId={task.id} onChanged={onChanged} onError={onError} />}
      </div>
      <div className="flex shrink-0 gap-2 text-sm">
        {task.subtask_count === 0 && !showSubtasks && (
          <button type="button" onClick={() => setShowSubtasks(true)} className="text-zinc-600 underline dark:text-zinc-400">
            + Subtask
          </button>
        )}
        <button type="button" onClick={() => setEditing(true)} className="text-zinc-600 underline dark:text-zinc-400">
          Edit
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => window.confirm(`Delete "${task.title}"${task.subtask_count ? " and its subtasks" : ""}?`) && run(() => deleteTask(task.id))}
          className="text-red-600 underline"
        >
          Delete
        </button>
      </div>
    </li>
  );
}

function TaskEditor({
  task,
  lists,
  projects,
  onSave,
  onCancel,
  onDependenciesChanged,
  onError,
}: {
  task: Task;
  lists: TaskList[];
  projects?: Project[];
  onSave: (patch: Parameters<typeof updateTask>[1]) => void;
  onCancel: () => void;
  onDependenciesChanged: () => void;
  onError: (err: unknown) => void;
}) {
  const [title, setTitle] = useState(task.title);
  const [description, setDescription] = useState(task.description ?? "");
  const [status, setStatus] = useState<TaskStatus>(task.status);
  const [priority, setPriority] = useState<TaskPriority>(task.priority);
  const [due, setDue] = useState(toLocalInput(task.due_at));
  const [listId, setListId] = useState<string>(task.list_id ? String(task.list_id) : "none");
  const [frequency, setFrequency] = useState<RecurrenceFrequency | "none">(task.recurrence?.frequency ?? "none");
  const [interval, setRepeatEvery] = useState(task.recurrence?.interval ?? 1);
  const [until, setUntil] = useState(toDateInput(task.recurrence?.until ?? null));
  const [projectId, setProjectId] = useState<string>(task.project_id ? String(task.project_id) : "none");
  const [milestoneId, setMilestoneId] = useState<string>(task.milestone_id ? String(task.milestone_id) : "none");
  const [milestones, setMilestones] = useState<Milestone[]>([]);

  useEffect(() => {
    if (projectId === "none") return;
    let cancelled = false;
    getProject(Number(projectId))
      .then((p) => {
        if (!cancelled) setMilestones(p.milestones ?? []);
      })
      .catch(onError);
    return () => {
      cancelled = true;
    };
  }, [projectId, onError]);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    const projectPatch = projects
      ? { project_id: projectId === "none" ? null : Number(projectId), milestone_id: milestoneId === "none" ? null : Number(milestoneId) }
      : {};
    onSave({
      ...projectPatch,
      title,
      description: description.trim() ? description : null,
      status,
      priority,
      due_at: fromLocalInput(due),
      list_id: listId === "none" ? null : Number(listId),
      recurrence: frequency === "none" ? null : { frequency, interval, until: fromDateInputEndOfDay(until) },
    });
  }

  const control = "rounded-md border border-zinc-300 bg-transparent px-2 py-1.5 text-sm dark:border-zinc-700";

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-2">
      <input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={255} aria-label="Title" className={`${control} text-base`} autoFocus />
      <textarea
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        rows={3}
        placeholder="Notes"
        aria-label="Description"
        className={control}
      />
      <div className="flex flex-wrap gap-2">
        <select value={status} onChange={(e) => setStatus(e.target.value as TaskStatus)} aria-label="Status" className={control}>
          {TASK_STATUSES.map((s) => (
            <option key={s} value={s}>
              {label(s)}
            </option>
          ))}
        </select>
        <select value={priority} onChange={(e) => setPriority(e.target.value as TaskPriority)} aria-label="Priority" className={control}>
          {TASK_PRIORITIES.map((p) => (
            <option key={p} value={p}>
              {label(p)} priority
            </option>
          ))}
        </select>
        <input type="datetime-local" value={due} onChange={(e) => setDue(e.target.value)} aria-label="Due date" className={control} />
        {task.parent_id === null && (
          <select value={listId} onChange={(e) => setListId(e.target.value)} aria-label="List" className={control}>
            <option value="none">No list</option>
            {lists.map((l) => (
              <option key={l.id} value={l.id}>
                {l.name}
              </option>
            ))}
          </select>
        )}
      </div>

      {task.parent_id === null && projects && (
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <span className="text-zinc-500">Project</span>
          <select
            value={projectId}
            onChange={(e) => {
              setProjectId(e.target.value);
              setMilestoneId("none"); // milestones belong to one project
            }}
            aria-label="Project"
            className={control}
          >
            <option value="none">No project</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          {projectId !== "none" && milestones.some((m) => m.project_id === Number(projectId)) && (
            <select value={milestoneId} onChange={(e) => setMilestoneId(e.target.value)} aria-label="Milestone" className={control}>
              <option value="none">No milestone</option>
              {milestones
                .filter((m) => m.project_id === Number(projectId))
                .map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.title}
                  </option>
                ))}
            </select>
          )}
        </div>
      )}

      {task.parent_id === null && (
        <fieldset className="flex flex-wrap items-center gap-2 text-sm">
          <legend className="sr-only">Repeat</legend>
          <span className="text-zinc-500">Repeat</span>
          <select value={frequency} onChange={(e) => setFrequency(e.target.value as typeof frequency)} aria-label="Repeat frequency" className={control}>
            <option value="none">Never</option>
            {RECURRENCE_FREQUENCIES.map((f) => (
              <option key={f} value={f}>
                {label(f)}
              </option>
            ))}
          </select>
          {frequency !== "none" && (
            <>
              <label className="flex items-center gap-1">
                <span className="text-zinc-500">every</span>
                <input
                  type="number"
                  min={1}
                  max={365}
                  value={interval}
                  onChange={(e) => setRepeatEvery(Math.max(1, Number(e.target.value) || 1))}
                  aria-label="Repeat interval"
                  className={`${control} w-16`}
                />
              </label>
              <label className="flex items-center gap-1">
                <span className="text-zinc-500">until</span>
                <input type="date" value={until} onChange={(e) => setUntil(e.target.value)} aria-label="Repeat until" className={control} />
              </label>
              {!due && <span className="text-amber-700 dark:text-amber-400">Set a due date to repeat</span>}
            </>
          )}
        </fieldset>
      )}

      <DependencyEditor task={task} onChanged={onDependenciesChanged} onError={onError} />
      <RemindersPanel taskId={task.id} hasDue={task.due_at !== null} onError={onError} />

      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="px-3 py-1.5 text-sm underline">
          Cancel
        </button>
        <button type="submit" disabled={!title.trim()} className="rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900">
          Save
        </button>
      </div>
    </form>
  );
}

/** Adds/removes "waits for" links immediately (not on Save). */
function DependencyEditor({ task, onChanged, onError }: { task: Task; onChanged: () => void; onError: (err: unknown) => void }) {
  const [deps, setDeps] = useState<TaskRef[]>(task.dependencies);
  const [candidates, setCandidates] = useState<Task[]>([]);

  useEffect(() => {
    listTasks({ status: "open", per_page: 100, sort: "due_at" })
      .then((r) => setCandidates(r.data))
      .catch(onError);
  }, [onError]);

  async function apply(action: () => Promise<Task>) {
    try {
      const updated = await action();
      setDeps(updated.dependencies);
      onChanged();
    } catch (err) {
      onError(err);
    }
  }

  const available = candidates.filter((c) => c.id !== task.id && !deps.some((d) => d.id === c.id));
  const control = "rounded-md border border-zinc-300 bg-transparent px-2 py-1.5 text-sm dark:border-zinc-700";

  return (
    <div className="flex flex-wrap items-center gap-2 text-sm">
      <span className="text-zinc-500">Waits for</span>
      {deps.map((d) => (
        <span key={d.id} className="flex items-center gap-1 rounded bg-zinc-100 px-2 py-0.5 dark:bg-zinc-800">
          <span className={isClosed(d.status) ? "text-zinc-500 line-through" : ""}>{d.title}</span>
          <button type="button" onClick={() => apply(() => removeDependency(task.id, d.id))} aria-label={`Remove dependency on ${d.title}`} className="text-zinc-500 hover:text-red-600">
            ×
          </button>
        </span>
      ))}
      <select
        value=""
        onChange={(e) => e.target.value && apply(() => addDependency(task.id, Number(e.target.value)))}
        aria-label="Add a task this one waits for"
        className={control}
      >
        <option value="">+ Add task…</option>
        {available.map((c) => (
          <option key={c.id} value={c.id}>
            {c.title}
          </option>
        ))}
      </select>
    </div>
  );
}
