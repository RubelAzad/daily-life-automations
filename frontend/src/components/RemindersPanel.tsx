"use client";

import { useEffect, useState } from "react";
import { createReminder, deleteReminder, listReminders, type Reminder } from "@/lib/api";
import { formatDue, formatOffset, fromLocalInput } from "@/lib/format";

type Props = { onError: (err: unknown) => void } & (
  | { taskId: number; hasDue: boolean; eventId?: undefined; noteId?: undefined }
  | { eventId: number; taskId?: undefined; noteId?: undefined; hasDue?: undefined }
  | { noteId: number; taskId?: undefined; eventId?: undefined; hasDue?: undefined }
);

const PRESETS = {
  task: [0, 15, 60, 24 * 60],
  event: [0, 5, 15, 30, 60, 24 * 60],
  note: [0, 24 * 60, 7 * 24 * 60, 30 * 24 * 60],
};
const ZERO_LABEL = { task: "At due time", event: "At start", note: "On expiry day" };
const control = "rounded-md border border-zinc-300 bg-transparent px-2 py-1 text-sm dark:border-zinc-700";

/** Lists and adds reminders for one task, event or note; changes apply immediately. */
export default function RemindersPanel(props: Props) {
  const { taskId, eventId, noteId, onError } = props;
  const kind = taskId !== undefined ? "task" : eventId !== undefined ? "event" : "note";
  const isTask = kind === "task";
  const [reminders, setReminders] = useState<Reminder[] | null>(null);
  const [version, setVersion] = useState(0);
  const [choice, setChoice] = useState<string>(isTask && !props.hasDue ? "custom" : kind === "note" ? String(30 * 24 * 60) : "15");
  const [custom, setCustom] = useState("");
  const [nag, setNag] = useState(false);

  useEffect(() => {
    let cancelled = false;
    listReminders({ status: "scheduled,sent", task_id: taskId, event_id: eventId, note_id: noteId, per_page: 50 })
      .then((r) => {
        if (!cancelled) setReminders(r.data);
      })
      .catch(onError);
    return () => {
      cancelled = true;
    };
  }, [taskId, eventId, noteId, version, onError]);

  async function run(action: () => Promise<unknown>) {
    try {
      await action();
      setVersion((v) => v + 1);
    } catch (err) {
      onError(err);
    }
  }

  function onAdd() {
    const followUp = isTask && nag ? { follow_up_minutes: 60, max_follow_ups: 3 } : {};
    const target = taskId !== undefined ? { task_id: taskId } : eventId !== undefined ? { event_id: eventId } : { note_id: noteId };
    if (choice === "custom") {
      if (!custom) return;
      run(() => createReminder({ ...target, ...followUp, remind_at: fromLocalInput(custom) }));
    } else {
      run(() => createReminder({ ...target, ...followUp, offset_minutes: Number(choice) }));
    }
    setCustom("");
  }

  function describe(r: Reminder): string {
    const when = r.offset_minutes !== null ? formatOffset(r.offset_minutes, ZERO_LABEL[kind]) : r.remind_at ? formatDue(r.remind_at) : "No time";
    const parts = [when];
    if (r.follow_up_minutes) parts.push(`then every ${formatOffset(r.follow_up_minutes).replace(" before", "")} until done (max ${r.max_follow_ups})`);
    if (r.status === "sent") parts.push("sent");
    else if (!r.remind_at) parts.push(kind === "task" ? "waiting for an upcoming due date" : kind === "event" ? "no upcoming occurrence" : "no upcoming expiry");
    return parts.join(" · ");
  }

  return (
    <div className="flex flex-col gap-2 text-sm">
      <span className="text-zinc-500">{kind === "note" ? "Expiry reminders" : "Reminders"}</span>
      {reminders?.map((r) => (
        <div key={r.id} className="flex items-center gap-2">
          <span className={`flex-1 ${r.status === "sent" ? "text-zinc-500" : ""}`}>🔔 {describe(r)}</span>
          <button type="button" onClick={() => run(() => deleteReminder(r.id))} aria-label="Remove reminder" className="text-zinc-400 hover:text-red-600">
            ×
          </button>
        </div>
      ))}
      <div className="flex flex-wrap items-center gap-2">
        <select value={choice} onChange={(e) => setChoice(e.target.value)} aria-label="When to remind" className={control}>
          {(!isTask || props.hasDue) &&
            PRESETS[kind].map((m) => (
              <option key={m} value={m}>
                {formatOffset(m, ZERO_LABEL[kind])}
              </option>
            ))}
          {isTask && <option value="custom">At a specific time…</option>}
        </select>
        {choice === "custom" && <input type="datetime-local" value={custom} onChange={(e) => setCustom(e.target.value)} aria-label="Reminder time" className={control} />}
        {isTask && (
          <label className="flex items-center gap-1.5" title="Sends a follow-up every hour, up to 3 times, while the task is still open">
            <input type="checkbox" checked={nag} onChange={(e) => setNag(e.target.checked)} />
            Keep reminding until done
          </label>
        )}
        <button type="button" onClick={onAdd} disabled={choice === "custom" && !custom} className="rounded-md border border-zinc-300 px-3 py-1 disabled:opacity-50 dark:border-zinc-700">
          Add reminder
        </button>
      </div>
    </div>
  );
}
