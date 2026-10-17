"use client";

import { useEffect, useState, type FormEvent } from "react";
import {
  createEvent,
  deleteEvent,
  getEvent,
  RECURRENCE_FREQUENCIES,
  updateEvent,
  type CalendarEvent,
  type EventInput,
  type EventOccurrence,
  type RecurrenceFrequency,
} from "@/lib/api";
import { fromDateInputEndOfDay, fromLocalInput, toDateInput, toLocalInput } from "@/lib/format";

/** "new" pre-fills the given local day (YYYY-MM-DD); "edit" loads the series. */
export type EditorTarget = { kind: "new"; day: string } | { kind: "edit"; id: number };

type Props = {
  target: EditorTarget;
  onSaved: (conflicts: EventOccurrence[]) => void;
  onClose: () => void;
  onError: (err: unknown) => void;
};

const control = "rounded-md border border-zinc-300 bg-transparent px-2 py-1.5 text-sm dark:border-zinc-700";
const label = (s: string) => s[0].toUpperCase() + s.slice(1);

export default function EventEditor({ target, onSaved, onClose, onError }: Props) {
  const [event, setEvent] = useState<CalendarEvent | null>(null);

  useEffect(() => {
    if (target.kind !== "edit") return;
    let cancelled = false;
    getEvent(target.id)
      .then((e) => {
        if (!cancelled) setEvent(e);
      })
      .catch(onError);
    return () => {
      cancelled = true;
    };
  }, [target, onError]);

  if (target.kind === "edit" && !event) {
    return <p className="rounded-lg border border-zinc-200 p-4 text-zinc-500 dark:border-zinc-800">Loading event…</p>;
  }
  // Remount the form when switching events so its state re-initializes.
  return <EventForm key={event?.id ?? `new-${target.kind === "new" ? target.day : ""}`} event={event} day={target.kind === "new" ? target.day : null} onSaved={onSaved} onClose={onClose} onError={onError} />;
}

function EventForm({
  event,
  day,
  onSaved,
  onClose,
  onError,
}: {
  event: CalendarEvent | null;
  day: string | null;
  onSaved: (conflicts: EventOccurrence[]) => void;
  onClose: () => void;
  onError: (err: unknown) => void;
}) {
  const [title, setTitle] = useState(event?.title ?? "");
  const [allDay, setAllDay] = useState(event?.all_day ?? false);
  const [start, setStart] = useState(event && !event.all_day ? toLocalInput(event.starts_at) : `${day}T09:00`);
  const [end, setEnd] = useState(event && !event.all_day ? toLocalInput(event.ends_at) : `${day}T10:00`);
  const [startDate, setStartDate] = useState(event?.start_date ?? (event ? toDateInput(event.starts_at) : day!));
  const [endDate, setEndDate] = useState(event?.end_date ?? (event ? toDateInput(event.starts_at) : day!));
  const [location, setLocation] = useState(event?.location ?? "");
  const [buffer, setBuffer] = useState(event?.buffer_before_minutes ?? 0);
  const [busy, setBusy] = useState(event?.busy ?? true);
  const [description, setDescription] = useState(event?.description ?? "");
  const [frequency, setFrequency] = useState<RecurrenceFrequency | "none">(event?.recurrence?.frequency ?? "none");
  const [interval, setRepeatEvery] = useState(event?.recurrence?.interval ?? 1);
  const [until, setUntil] = useState(toDateInput(event?.recurrence?.until ?? null));
  const [saving, setSaving] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const input: Partial<EventInput> & { title: string } = {
      title,
      all_day: allDay,
      location: location.trim() || null,
      description: description.trim() || null,
      buffer_before_minutes: buffer,
      busy,
      recurrence: frequency === "none" ? null : { frequency, interval, until: fromDateInputEndOfDay(until) },
      ...(allDay ? { start_date: startDate, end_date: endDate || startDate } : { starts_at: fromLocalInput(start), ends_at: fromLocalInput(end) }),
    };
    setSaving(true);
    try {
      const saved = event ? await updateEvent(event.id, input) : await createEvent(input);
      onSaved(saved.conflicts);
    } catch (err) {
      onError(err);
      setSaving(false);
    }
  }

  async function onDelete() {
    if (!event) return;
    const what = event.recurrence ? `every occurrence of "${event.title}"` : `"${event.title}"`;
    if (!window.confirm(`Delete ${what}?`)) return;
    try {
      await deleteEvent(event.id);
      onSaved([]);
    } catch (err) {
      onError(err);
    }
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-3 rounded-lg border border-zinc-300 p-4 shadow-sm dark:border-zinc-700">
      <div className="flex items-center justify-between">
        <h2 className="font-semibold">{event ? "Edit event" : "New event"}</h2>
        <button type="button" onClick={onClose} aria-label="Close" className="text-zinc-500">
          ×
        </button>
      </div>
      {event?.recurrence && <p className="text-xs text-zinc-500">Changes apply to every occurrence of this repeating event.</p>}

      <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Title" maxLength={255} aria-label="Title" className={`${control} text-base`} autoFocus />

      <div className="flex flex-wrap items-center gap-3 text-sm">
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={allDay} onChange={(e) => setAllDay(e.target.checked)} />
          All day
        </label>
        {allDay ? (
          <>
            <input type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} aria-label="Start date" className={control} />
            <span className="text-zinc-500">to</span>
            <input type="date" value={endDate} min={startDate} onChange={(e) => setEndDate(e.target.value)} aria-label="End date" className={control} />
          </>
        ) : (
          <>
            <input type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} aria-label="Starts" className={control} />
            <span className="text-zinc-500">to</span>
            <input type="datetime-local" value={end} min={start} onChange={(e) => setEnd(e.target.value)} aria-label="Ends" className={control} />
          </>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-3 text-sm">
        <input value={location} onChange={(e) => setLocation(e.target.value)} placeholder="Location" maxLength={255} aria-label="Location" className={`${control} min-w-0 flex-1`} />
        {!allDay && (
          <label className="flex items-center gap-2" title="Blocks time before the event for travel or preparation">
            <span className="text-zinc-500">Travel/prep</span>
            <input
              type="number"
              min={0}
              max={1440}
              step={5}
              value={buffer}
              onChange={(e) => setBuffer(Math.min(1440, Math.max(0, Number(e.target.value) || 0)))}
              aria-label="Travel or preparation minutes before"
              className={`${control} w-20`}
            />
            <span className="text-zinc-500">min</span>
          </label>
        )}
        <label className="flex items-center gap-2" title="Free events never cause conflicts">
          <input type="checkbox" checked={busy} onChange={(e) => setBusy(e.target.checked)} />
          Busy
        </label>
      </div>

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
          </>
        )}
      </fieldset>

      <textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} placeholder="Notes" aria-label="Notes" className={control} />

      <div className="flex items-center justify-between gap-2">
        {event ? (
          <button type="button" onClick={onDelete} className="text-sm text-red-600 underline">
            Delete
          </button>
        ) : (
          <span />
        )}
        <div className="flex gap-2">
          <button type="button" onClick={onClose} className="px-3 py-1.5 text-sm underline">
            Cancel
          </button>
          <button type="submit" disabled={saving || !title.trim()} className="rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900">
            {saving ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </form>
  );
}
