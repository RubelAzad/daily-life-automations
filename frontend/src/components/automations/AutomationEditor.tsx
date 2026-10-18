"use client";

import { useState, type FormEvent, type ReactNode } from "react";
import {
  ApiError,
  createAutomation,
  updateAutomation,
  type Automation,
  type AutomationAction,
  type AutomationCatalog,
  type AutomationInput,
  type Condition,
  type Schedule,
} from "@/lib/api";
import { errorMessage } from "@/lib/errors";

const control = "rounded-md border border-zinc-300 bg-transparent px-2 py-1 text-sm dark:border-zinc-700";
const small = "rounded border border-zinc-300 px-2 py-0.5 text-xs hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800";
const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const SCHEDULE_ERRORS = ["trigger.schedule", "trigger.schedule.frequency", "trigger.schedule.time", "trigger.schedule.weekdays", "trigger.schedule.month_day", "trigger.schedule.date"];
const OP_LABELS: Record<string, string> = {
  equals: "is",
  not_equals: "is not",
  contains: "contains",
  not_contains: "doesn't contain",
  in: "is one of",
  not_in: "is none of",
  is_set: "is set",
  is_not_set: "is empty",
};

/** The text fields each action offers. Time fields accept "+1d", "tomorrow 09:00", … */
const ACTION_FIELDS: Record<string, { key: string; label: string; placeholder?: string; multiline?: boolean; choices?: string[] }[]> = {
  notify: [
    { key: "title", label: "Title", placeholder: "Overdue: {{task.title}}" },
    { key: "body", label: "Message", multiline: true },
    { key: "link", label: "Link (app path)", placeholder: "/tasks" },
  ],
  create_task: [
    { key: "title", label: "Title", placeholder: "Follow up on {{task.title}}" },
    { key: "description", label: "Description", multiline: true },
    { key: "priority", label: "Priority", choices: ["", "low", "medium", "high", "urgent"] },
    { key: "due_at", label: "Due", placeholder: "+1d, tomorrow 09:00, today 17:00" },
  ],
  complete_task: [{ key: "task_id", label: "Task", placeholder: "{{task.id}}" }],
  reschedule_task: [
    { key: "task_id", label: "Task", placeholder: "{{task.id}}" },
    { key: "due_at", label: "New due time", placeholder: "+1d, tomorrow 09:00" },
  ],
  create_note: [
    { key: "title", label: "Title" },
    { key: "body", label: "Text", multiline: true },
    { key: "kind", label: "Kind", choices: ["", "note", "idea", "link", "document", "receipt"] },
    { key: "tags", label: "Tags (comma-separated)" },
  ],
  create_reminder: [
    { key: "title", label: "Title" },
    { key: "remind_at", label: "When", placeholder: "tomorrow 09:00, +2h" },
    { key: "note", label: "Note" },
  ],
  recover_task: [
    { key: "task_id", label: "Task", placeholder: "{{task.id}}" },
    { key: "minutes", label: "Time it needs (minutes)", placeholder: "30" },
  ],
  plan_day: [{ key: "refresh", label: "If today already has a plan", choices: ["", "true"] }],
  record_expense: [
    { key: "amount", label: "Amount", placeholder: "12.50" },
    { key: "category", label: "Category", placeholder: "Groceries" },
    { key: "description", label: "Note" },
    { key: "spent_on", label: "Date (YYYY-MM-DD)", placeholder: "{{today}}" },
  ],
  create_bill_task: [{ key: "bill_id", label: "Bill", placeholder: "{{bill.id}}" }],
  pay_bill: [
    { key: "bill_id", label: "Bill", placeholder: "{{bill.id}}" },
    { key: "task_id", label: "Its completed task", placeholder: "{{task.id}}" },
  ],
};

/** Labels for choice values that aren't self-explanatory. */
const CHOICE_LABELS: Record<string, Record<string, string>> = {
  refresh: { "": "Reuse it", true: "Write a new one" },
};

type Draft = {
  name: string;
  enabled: boolean;
  triggerType: string;
  schedule: Schedule;
  conditions: Condition[];
  actions: AutomationAction[];
};

function toDraft(a: Automation | null, preset?: AutomationInput): Draft {
  const src = a ?? preset;
  return {
    name: src?.name ?? "",
    enabled: src?.enabled ?? true,
    triggerType: src?.trigger?.type ?? "schedule",
    schedule: src?.trigger?.schedule ?? { frequency: "daily", time: "08:00" },
    conditions: src?.conditions ?? [],
    actions: src?.actions?.length ? src.actions : [{ type: "notify", input: { title: "" } }],
  };
}

/** Text form of an action input value, for editing. */
function fieldText(v: unknown): string {
  if (Array.isArray(v)) return v.join(", ");
  return v === undefined || v === null ? "" : String(v);
}

/** Drops empty optional values and turns tags into a list, task_id into a number when it is one. */
function cleanInput(type: string, input: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(input)) {
    if (typeof v === "string" && v.trim() === "") continue;
    if (k === "tags" && typeof v === "string") out[k] = v.split(",").map((t) => t.trim()).filter(Boolean);
    else if ((k === "task_id" || k === "minutes" || k === "bill_id") && typeof v === "string" && /^\d+$/.test(v.trim())) out[k] = Number(v);
    else if (k === "refresh") out[k] = v === true || v === "true";
    else out[k] = v;
  }
  if ((type === "complete_task" || type === "reschedule_task" || type === "recover_task") && out.task_id === undefined) out.task_id = "{{task.id}}";
  if ((type === "create_bill_task" || type === "pay_bill") && out.bill_id === undefined) out.bill_id = "{{bill.id}}";
  return out;
}

type Props = {
  catalog: AutomationCatalog;
  automation: Automation | null; // null for a new one
  preset?: AutomationInput;
  onSaved: (a: Automation) => void;
  onCancel: () => void;
};

export default function AutomationEditor({ catalog, automation, preset, onSaved, onCancel }: Props) {
  const [draft, setDraft] = useState<Draft>(() => toDraft(automation, preset));
  const [fields, setFields] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const spec = catalog.triggers.find((t) => t.type === draft.triggerType);
  const update = (patch: Partial<Draft>) => setDraft((d) => ({ ...d, ...patch }));
  const fieldError = (key: string) => (fields[key] ? <span className="text-xs text-red-600 dark:text-red-400">{fields[key]}</span> : null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    setFields({});
    setError(null);
    const input: AutomationInput = {
      name: draft.name,
      enabled: draft.enabled,
      trigger: draft.triggerType === "schedule" ? { type: "schedule", schedule: draft.schedule } : { type: draft.triggerType },
      conditions: draft.conditions,
      actions: draft.actions.map((a) => ({ type: a.type, input: cleanInput(a.type, a.input) })),
    };
    try {
      onSaved(automation ? await updateAutomation(automation.id, input) : await createAutomation(input));
    } catch (err) {
      if (err instanceof ApiError && err.fields) setFields(err.fields);
      else setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4 rounded-md border border-zinc-200 p-4 dark:border-zinc-800">
      <h2 className="font-medium">{automation ? "Edit automation" : "New automation"}</h2>
      {error && <p className="text-sm text-red-600 dark:text-red-400">{error}</p>}

      <label className="flex flex-col gap-1 text-sm">
        Name
        <input value={draft.name} onChange={(e) => update({ name: e.target.value })} maxLength={100} required className={control} />
        {fieldError("name")}
      </label>

      <Step title="When">
        <select
          value={draft.triggerType}
          onChange={(e) => update({ triggerType: e.target.value, conditions: [] })}
          aria-label="Trigger"
          className={control}
        >
          {catalog.triggers.map((t) => (
            <option key={t.type} value={t.type}>
              {t.label}
            </option>
          ))}
        </select>
        {spec && <p className="text-xs text-zinc-500">{spec.description}</p>}
        {fieldError("trigger.type")}
        {draft.triggerType === "schedule" && <ScheduleFields schedule={draft.schedule} onChange={(schedule) => update({ schedule })} fieldError={fieldError} />}
      </Step>

      {spec && spec.fields.length > 0 && (
        <Step title="Only if (all must hold)">
          {draft.conditions.length === 0 && <p className="text-xs text-zinc-500">No conditions: runs every time.</p>}
          {draft.conditions.map((c, i) => {
            const field = spec.fields.find((f) => f.name === c.field) ?? spec.fields[0];
            const ops = catalog.operators[field.type];
            const set = (patch: Partial<Condition>) =>
              update({ conditions: draft.conditions.map((x, j) => (j === i ? { ...x, ...patch } : x)) });
            return (
              <div key={i} className="flex flex-wrap items-center gap-2">
                <select
                  value={field.name}
                  onChange={(e) => {
                    const f = spec.fields.find((x) => x.name === e.target.value)!;
                    set({ field: f.name, op: catalog.operators[f.type][0], value: undefined });
                  }}
                  aria-label="Field"
                  className={control}
                >
                  {spec.fields.map((f) => (
                    <option key={f.name} value={f.name}>
                      {f.label}
                    </option>
                  ))}
                </select>
                <select value={c.op} onChange={(e) => set({ op: e.target.value, value: undefined })} aria-label="Operator" className={control}>
                  {ops.map((op) => (
                    <option key={op} value={op}>
                      {OP_LABELS[op] ?? op}
                    </option>
                  ))}
                </select>
                <ConditionValue condition={c} field={field} onChange={(value) => set({ value })} />
                <button type="button" onClick={() => update({ conditions: draft.conditions.filter((_, j) => j !== i) })} className={small}>
                  Remove
                </button>
                {fieldError(`conditions[${i}]`)}
              </div>
            );
          })}
          {draft.conditions.length < 10 && (
            <button
              type="button"
              onClick={() => {
                const f = spec.fields[0];
                update({ conditions: [...draft.conditions, { field: f.name, op: catalog.operators[f.type][0] }] });
              }}
              className={`${small} self-start`}
            >
              Add condition
            </button>
          )}
          {fieldError("conditions")}
        </Step>
      )}

      <Step title="Then">
        {draft.actions.map((a, i) => (
          <ActionFields
            key={i}
            index={i}
            action={a}
            catalog={catalog}
            onChange={(next) => update({ actions: draft.actions.map((x, j) => (j === i ? next : x)) })}
            onRemove={draft.actions.length > 1 ? () => update({ actions: draft.actions.filter((_, j) => j !== i) }) : undefined}
            error={fields[`actions[${i}]`]}
          />
        ))}
        {draft.actions.length < 5 && (
          <button type="button" onClick={() => update({ actions: [...draft.actions, { type: "notify", input: {} }] })} className={`${small} self-start`}>
            Add action
          </button>
        )}
        {fieldError("actions")}
        {spec && (
          <p className="text-xs text-zinc-500">
            Placeholders: {spec.placeholders.map((p) => `{{${p}}}`).join(" ")}. Times can be relative: <code>+30m</code>, <code>+2h</code>, <code>+1d 09:00</code>,{" "}
            <code>tomorrow 09:00</code>.
          </p>
        )}
        <p className="text-xs text-zinc-500">
          Actions follow your approval setting: with &ldquo;Always ask&rdquo;, changes wait in Approvals. Notifications and day plans never need approval.
        </p>
      </Step>

      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={draft.enabled} onChange={(e) => update({ enabled: e.target.checked })} />
        On
      </label>

      <div className="flex items-center gap-3">
        <button
          type="submit"
          disabled={saving}
          className="rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900"
        >
          {saving ? "Saving…" : "Save"}
        </button>
        <button type="button" onClick={onCancel} className="text-sm underline">
          Cancel
        </button>
      </div>
    </form>
  );
}

function Step({ title, children }: { title: string; children: ReactNode }) {
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-1 text-xs font-medium tracking-wide text-zinc-500 uppercase">{title}</legend>
      {children}
    </fieldset>
  );
}

function ScheduleFields({
  schedule,
  onChange,
  fieldError,
}: {
  schedule: Schedule;
  onChange: (s: Schedule) => void;
  fieldError: (key: string) => ReactNode;
}) {
  const set = (patch: Partial<Schedule>) => onChange({ ...schedule, ...patch });
  return (
    <div className="flex flex-col gap-2 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <select
          value={schedule.frequency}
          onChange={(e) => {
            const frequency = e.target.value as Schedule["frequency"];
            set({
              frequency,
              weekdays: frequency === "weekly" ? (schedule.weekdays?.length ? schedule.weekdays : [1, 2, 3, 4, 5]) : undefined,
              month_day: frequency === "monthly" ? (schedule.month_day ?? 1) : undefined,
              date: frequency === "once" ? (schedule.date ?? "") : undefined,
            });
          }}
          aria-label="Frequency"
          className={control}
        >
          <option value="daily">Every day</option>
          <option value="weekly">On weekdays I choose</option>
          <option value="monthly">Every month</option>
          <option value="once">Once</option>
        </select>
        {schedule.frequency === "monthly" && (
          <label className="flex items-center gap-1">
            on day
            <input
              type="number"
              min={1}
              max={31}
              value={schedule.month_day ?? 1}
              onChange={(e) => set({ month_day: Number(e.target.value) })}
              className={`${control} w-16`}
            />
          </label>
        )}
        {schedule.frequency === "once" && (
          <input type="date" value={schedule.date ?? ""} onChange={(e) => set({ date: e.target.value })} aria-label="Date" className={control} />
        )}
        <label className="flex items-center gap-1">
          at
          <input type="time" value={schedule.time} onChange={(e) => set({ time: e.target.value })} required className={control} />
        </label>
      </div>
      {schedule.frequency === "weekly" && (
        <div className="flex flex-wrap gap-2" role="group" aria-label="Days">
          {WEEKDAYS.map((label, i) => {
            const day = i + 1;
            const on = schedule.weekdays?.includes(day) ?? false;
            return (
              <label key={day} className="flex items-center gap-1">
                <input
                  type="checkbox"
                  checked={on}
                  onChange={() => set({ weekdays: on ? schedule.weekdays!.filter((d) => d !== day) : [...(schedule.weekdays ?? []), day].sort() })}
                />
                {label}
              </label>
            );
          })}
        </div>
      )}
      {SCHEDULE_ERRORS.map((k) => fieldError(k) && <span key={k}>{fieldError(k)}</span>)}
      <p className="text-xs text-zinc-500">In your time zone (Settings). A run missed by more than 3 hours, because the worker wasn&rsquo;t running, is skipped.</p>
    </div>
  );
}

function ConditionValue({
  condition,
  field,
  onChange,
}: {
  condition: Condition;
  field: { type: string; choices?: string[] };
  onChange: (v: string | string[]) => void;
}) {
  if (condition.op === "is_set" || condition.op === "is_not_set") return null;
  if (condition.op === "in" || condition.op === "not_in") {
    const chosen = Array.isArray(condition.value) ? condition.value : [];
    return (
      <span className="flex flex-wrap gap-2 text-sm">
        {(field.choices ?? []).map((c) => (
          <label key={c} className="flex items-center gap-1">
            <input type="checkbox" checked={chosen.includes(c)} onChange={() => onChange(chosen.includes(c) ? chosen.filter((x) => x !== c) : [...chosen, c])} />
            {c}
          </label>
        ))}
      </span>
    );
  }
  if (field.choices) {
    return (
      <select value={typeof condition.value === "string" ? condition.value : ""} onChange={(e) => onChange(e.target.value)} aria-label="Value" className={control}>
        <option value="" disabled>
          choose…
        </option>
        {field.choices.map((c) => (
          <option key={c} value={c}>
            {c}
          </option>
        ))}
      </select>
    );
  }
  return (
    <input
      value={typeof condition.value === "string" ? condition.value : ""}
      onChange={(e) => onChange(e.target.value)}
      maxLength={200}
      aria-label="Value"
      className={control}
    />
  );
}

function ActionFields({
  index,
  action,
  catalog,
  onChange,
  onRemove,
  error,
}: {
  index: number;
  action: AutomationAction;
  catalog: AutomationCatalog;
  onChange: (a: AutomationAction) => void;
  onRemove?: () => void;
  error?: string;
}) {
  const spec = catalog.actions.find((a) => a.type === action.type);
  const set = (key: string, value: string) => onChange({ ...action, input: { ...action.input, [key]: value } });
  return (
    <div className="flex flex-col gap-2 rounded-md border border-zinc-200 p-3 dark:border-zinc-800">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-zinc-500">{index + 1}.</span>
        <select value={action.type} onChange={(e) => onChange({ type: e.target.value, input: {} })} aria-label={`Action ${index + 1}`} className={control}>
          {catalog.actions.map((a) => (
            <option key={a.type} value={a.type}>
              {a.label}
            </option>
          ))}
        </select>
        {onRemove && (
          <button type="button" onClick={onRemove} className={`${small} ml-auto`}>
            Remove
          </button>
        )}
      </div>
      {spec && <p className="text-xs text-zinc-500">{spec.description}</p>}
      {(ACTION_FIELDS[action.type] ?? []).map((f) => (
        <label key={f.key} className="flex flex-col gap-1 text-sm">
          {f.label}
          {f.choices ? (
            <select value={fieldText(action.input[f.key])} onChange={(e) => set(f.key, e.target.value)} className={control}>
              {f.choices.map((c) => (
                <option key={c} value={c}>
                  {CHOICE_LABELS[f.key]?.[c] ?? (c || "(default)")}
                </option>
              ))}
            </select>
          ) : f.multiline ? (
            <textarea value={fieldText(action.input[f.key])} onChange={(e) => set(f.key, e.target.value)} placeholder={f.placeholder} rows={2} className={control} />
          ) : (
            <input value={fieldText(action.input[f.key])} onChange={(e) => set(f.key, e.target.value)} placeholder={f.placeholder} className={control} />
          )}
        </label>
      ))}
      {error && <span className="text-xs text-red-600 dark:text-red-400">{error}</span>}
    </div>
  );
}
