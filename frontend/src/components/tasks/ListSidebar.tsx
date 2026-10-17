"use client";

import { useState, type FormEvent } from "react";
import { createTaskList, deleteTaskList, type TaskList } from "@/lib/api";

type Props = {
  lists: TaskList[];
  selected: number | null;
  onSelect: (id: number | null) => void;
  /** Called after a list is created or deleted; deletedId is set on delete. */
  onChanged: (deletedId?: number) => void;
  onError: (err: unknown) => void;
};

export default function ListSidebar({ lists, selected, onSelect, onChanged, onError }: Props) {
  const [name, setName] = useState("");

  async function onCreate(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    try {
      const list = await createTaskList({ name });
      setName("");
      onChanged();
      onSelect(list.id);
    } catch (err) {
      onError(err);
    }
  }

  async function onDelete(list: TaskList) {
    if (!window.confirm(`Delete the list "${list.name}"? Its tasks are kept and moved out of the list.`)) return;
    try {
      await deleteTaskList(list.id);
      onChanged(list.id);
    } catch (err) {
      onError(err);
    }
  }

  const item = "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm";
  const active = "bg-zinc-100 font-medium dark:bg-zinc-800";

  return (
    <aside className="flex flex-col gap-1" aria-label="Task lists">
      <button type="button" onClick={() => onSelect(null)} className={`${item} ${selected === null ? active : ""}`}>
        All tasks
      </button>

      {lists.map((list) => (
        <div key={list.id} className="group flex items-center">
          <button type="button" onClick={() => onSelect(list.id)} className={`${item} ${selected === list.id ? active : ""}`}>
            <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: list.color ?? "#a1a1aa" }} aria-hidden />
            <span className="min-w-0 flex-1 truncate">{list.name}</span>
            {list.open_task_count > 0 && <span className="text-xs text-zinc-500">{list.open_task_count}</span>}
          </button>
          <button
            type="button"
            onClick={() => onDelete(list)}
            aria-label={`Delete list ${list.name}`}
            className="px-1.5 text-zinc-400 opacity-0 hover:text-red-600 focus:opacity-100 group-hover:opacity-100"
          >
            ×
          </button>
        </div>
      ))}

      <form onSubmit={onCreate} className="mt-2">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="+ New list"
          maxLength={100}
          aria-label="New list name"
          className="w-full rounded-md border border-transparent bg-transparent px-2 py-1.5 text-sm placeholder:text-zinc-500 focus:border-zinc-300 focus:outline-none dark:focus:border-zinc-700"
        />
      </form>
    </aside>
  );
}
