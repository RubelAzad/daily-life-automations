"use client";

import { useEffect, useState, type FormEvent } from "react";
import { createTask, deleteTask, listTasks, updateTask, type Task } from "@/lib/api";

type Props = {
  parentId: number;
  /** Called after any change so the parent row can refresh its progress count. */
  onChanged: () => void;
  onError: (err: unknown) => void;
};

export default function Subtasks({ parentId, onChanged, onError }: Props) {
  const [subtasks, setSubtasks] = useState<Task[] | null>(null);
  const [title, setTitle] = useState("");
  const [version, setVersion] = useState(0);

  useEffect(() => {
    let cancelled = false;
    listTasks({ parent_id: parentId, per_page: 100 })
      .then((r) => {
        if (!cancelled) setSubtasks(r.data);
      })
      .catch(onError);
    return () => {
      cancelled = true;
    };
  }, [parentId, version, onError]);

  async function run(action: () => Promise<unknown>) {
    try {
      await action();
      setVersion((v) => v + 1);
      onChanged();
    } catch (err) {
      onError(err);
    }
  }

  function onAdd(event: FormEvent) {
    event.preventDefault();
    if (!title.trim()) return;
    run(async () => {
      await createTask({ title, parent_id: parentId });
      setTitle("");
    });
  }

  return (
    <div className="mt-2 flex flex-col gap-1 border-l-2 border-zinc-200 pl-3 dark:border-zinc-800">
      {subtasks === null ? (
        <span className="text-sm text-zinc-500">Loading…</span>
      ) : (
        subtasks.map((st) => {
          const done = st.status === "completed";
          return (
            <div key={st.id} className="group flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={done}
                onChange={() => run(() => updateTask(st.id, { status: done ? "pending" : "completed" }))}
                aria-label={done ? `Mark "${st.title}" as not done` : `Mark "${st.title}" as done`}
                className="size-3.5 accent-zinc-900 dark:accent-zinc-100"
              />
              <span className={`min-w-0 flex-1 wrap-break-word ${done ? "text-zinc-500 line-through" : ""}`}>{st.title}</span>
              <button
                type="button"
                onClick={() => run(() => deleteTask(st.id))}
                aria-label={`Delete subtask ${st.title}`}
                className="px-1 text-zinc-400 opacity-0 hover:text-red-600 focus:opacity-100 group-hover:opacity-100"
              >
                ×
              </button>
            </div>
          );
        })
      )}
      <form onSubmit={onAdd}>
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="+ Add subtask"
          maxLength={255}
          aria-label="New subtask"
          className="w-full bg-transparent py-1 text-sm outline-none placeholder:text-zinc-500"
        />
      </form>
    </div>
  );
}
