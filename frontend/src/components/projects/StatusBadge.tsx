import type { ProjectStatus } from "@/lib/api";

const STYLE: Record<ProjectStatus, string> = {
  active: "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300",
  on_hold: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
  completed: "bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300",
  archived: "bg-zinc-50 text-zinc-500 dark:bg-zinc-900 dark:text-zinc-400",
};

export const STATUS_LABEL: Record<ProjectStatus, string> = {
  active: "Active",
  on_hold: "On hold",
  completed: "Completed",
  archived: "Archived",
};

export function StatusBadge({ status }: { status: ProjectStatus }) {
  return <span className={`shrink-0 rounded px-1.5 py-0.5 text-xs ${STYLE[status]}`}>{STATUS_LABEL[status]}</span>;
}
