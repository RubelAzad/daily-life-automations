"use client";

import { useState, type FormEvent } from "react";
import {
  deleteTask,
  TASK_PRIORITIES,
  TASK_STATUSES,
  updateTask,
  type Task,
  type TaskList,
  type TaskPriority,
  type TaskStatus,
} from "@/lib/api";
import { formatDue, fromLocalInput, isOverdue, toLocalInput } from "@/lib/format";

type Props = {
  task: Task;
  lists: TaskList[];
  showList: boolean;
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

export default function TaskItem({ task, lists, showList, onChanged, onError }: Props) {
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);

  const done = task.status === "completed";
  const closed = done || task.status === "cancelled";
  const list = lists.find((l) => l.id === task.list_id);

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
          onCancel={() => setEditing(false)}
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
        disabled={busy}
        onChange={() => run(() => updateTask(task.id, { status: done ? "pending" : "completed" }))}
        aria-label={done ? `Mark "${task.title}" as not done` : `Mark "${task.title}" as done`}
        className="mt-1 size-4 shrink-0 accent-zinc-900 dark:accent-zinc-100"
      />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <span className={`break-words ${closed ? "text-zinc-500 line-through" : ""}`}>{task.title}</span>
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
          {showList && list && (
            <span className="flex items-center gap-1 text-zinc-500">
              <span className="size-2 rounded-full" style={{ backgroundColor: list.color ?? "#a1a1aa" }} aria-hidden />
              {list.name}
            </span>
          )}
        </div>
      </div>
      <div className="flex shrink-0 gap-2 text-sm">
        <button type="button" onClick={() => setEditing(true)} className="text-zinc-600 underline dark:text-zinc-400">
          Edit
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => window.confirm(`Delete "${task.title}"?`) && run(() => deleteTask(task.id))}
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
  onSave,
  onCancel,
}: {
  task: Task;
  lists: TaskList[];
  onSave: (patch: Parameters<typeof updateTask>[1]) => void;
  onCancel: () => void;
}) {
  const [title, setTitle] = useState(task.title);
  const [description, setDescription] = useState(task.description ?? "");
  const [status, setStatus] = useState<TaskStatus>(task.status);
  const [priority, setPriority] = useState<TaskPriority>(task.priority);
  const [due, setDue] = useState(toLocalInput(task.due_at));
  const [listId, setListId] = useState<string>(task.list_id ? String(task.list_id) : "none");

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    onSave({
      title,
      description: description.trim() ? description : null,
      status,
      priority,
      due_at: fromLocalInput(due),
      list_id: listId === "none" ? null : Number(listId),
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
        <select value={listId} onChange={(e) => setListId(e.target.value)} aria-label="List" className={control}>
          <option value="none">No list</option>
          {lists.map((l) => (
            <option key={l.id} value={l.id}>
              {l.name}
            </option>
          ))}
        </select>
      </div>
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
