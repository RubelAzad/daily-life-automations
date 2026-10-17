"use client";

import { useState, type FormEvent } from "react";
import { createTask, TASK_PRIORITIES, type TaskList, type TaskPriority } from "@/lib/api";
import { fromLocalInput } from "@/lib/format";

type Props = {
  lists: TaskList[];
  defaultListId: number | null;
  onCreated: () => void;
  onError: (err: unknown) => void;
};

export default function NewTaskForm({ lists, defaultListId, onCreated, onError }: Props) {
  const [title, setTitle] = useState("");
  const [priority, setPriority] = useState<TaskPriority>("medium");
  const [due, setDue] = useState("");
  // null = follow the selected list; a number or "none" = user's explicit choice
  const [listChoice, setListChoice] = useState<number | "none" | null>(null);
  const [saving, setSaving] = useState(false);

  const listValue = listChoice ?? defaultListId ?? "none";

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!title.trim()) return;
    setSaving(true);
    try {
      await createTask({
        title,
        priority,
        due_at: fromLocalInput(due),
        list_id: listValue === "none" ? null : listValue,
      });
      setTitle("");
      setDue("");
      setPriority("medium");
      setListChoice(null);
      onCreated();
    } catch (err) {
      onError(err);
    } finally {
      setSaving(false);
    }
  }

  const control = "rounded-md border border-zinc-300 bg-transparent px-2 py-1.5 text-sm dark:border-zinc-700";

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-2 rounded-md border border-zinc-200 p-3 dark:border-zinc-800">
      <input
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        placeholder="Add a task…"
        maxLength={255}
        aria-label="Task title"
        className="w-full bg-transparent px-1 py-1 text-base outline-none"
      />
      <div className="flex flex-wrap items-center gap-2">
        <select value={priority} onChange={(e) => setPriority(e.target.value as TaskPriority)} aria-label="Priority" className={control}>
          {TASK_PRIORITIES.map((p) => (
            <option key={p} value={p}>
              {p[0].toUpperCase() + p.slice(1)} priority
            </option>
          ))}
        </select>
        <input type="datetime-local" value={due} onChange={(e) => setDue(e.target.value)} aria-label="Due date" className={control} />
        <select
          value={String(listValue)}
          onChange={(e) => setListChoice(e.target.value === "none" ? "none" : Number(e.target.value))}
          aria-label="List"
          className={control}
        >
          <option value="none">No list</option>
          {lists.map((l) => (
            <option key={l.id} value={l.id}>
              {l.name}
            </option>
          ))}
        </select>
        <button
          type="submit"
          disabled={saving || !title.trim()}
          className="ml-auto rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900"
        >
          {saving ? "Adding…" : "Add task"}
        </button>
      </div>
    </form>
  );
}
