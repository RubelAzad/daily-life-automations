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

const UNITS: Record<string, [string, string]> = {
  daily: ["Daily", "days"],
  weekly: ["Weekly", "weeks"],
  monthly: ["Monthly", "months"],
  yearly: ["Yearly", "years"],
};

/** "Weekly", "Every 2 months", "Daily until 5 May". */
export function formatRecurrence(r: { frequency: string; interval: number; until: string | null }): string {
  const [single, plural] = UNITS[r.frequency] ?? [r.frequency, r.frequency];
  const base = r.interval > 1 ? `Every ${r.interval} ${plural}` : single;
  if (!r.until) return base;
  return `${base} until ${new Date(r.until).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })}`;
}

/** "YYYY-MM-DD" for a date input, in the viewer's time zone. */
export function toDateInput(iso: string | null): string {
  return toLocalInput(iso).slice(0, 10);
}

/** End of the chosen local day as RFC 3339, or null if empty. */
export function fromDateInputEndOfDay(value: string): string | null {
  return value ? new Date(`${value}T23:59:59`).toISOString() : null;
}

/** Parses a calendar date "YYYY-MM-DD" as local midnight (not UTC). */
function parseDate(value: string): Date {
  const [y, m, d] = value.split("-").map(Number);
  return new Date(y, m - 1, d);
}

/** "1 Dec 2026" for a "YYYY-MM-DD" calendar date. */
export function formatDate(value: string): string {
  return parseDate(value).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
}

/** True if the calendar date is before today. */
export function isPastDate(value: string, now = new Date()): boolean {
  return parseDate(value).getTime() < startOfDay(now);
}

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}
