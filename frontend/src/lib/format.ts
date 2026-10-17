/** "Today 09:00", "Tomorrow 14:30", or "Mon, 20 Oct 09:00" in the viewer's time zone. */
export function formatDue(iso: string, now = new Date()): string {
  const date = new Date(iso);
  const time = date.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  const days = Math.round((startOfDay(date) - startOfDay(now)) / 86_400_000);
  if (days === 0) return `Today ${time}`;
  if (days === 1) return `Tomorrow ${time}`;
  if (days === -1) return `Yesterday ${time}`;
  return date.toLocaleString(undefined, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
}

export function isOverdue(iso: string, now = new Date()): boolean {
  return new Date(iso).getTime() < now.getTime();
}

/** Converts an RFC 3339 string to the "YYYY-MM-DDTHH:mm" value a datetime-local input expects. */
export function toLocalInput(iso: string | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Converts a datetime-local value (viewer's local time) to RFC 3339 UTC, or null if empty. */
export function fromLocalInput(value: string): string | null {
  return value ? new Date(value).toISOString() : null;
}

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}
