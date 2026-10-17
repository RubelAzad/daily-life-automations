"use client";

import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import AppHeader, { ErrorBanner } from "@/components/AppHeader";
import { ApiError, deviceTimeZone, getSettings, updateSettings, WEEK_START_OPTIONS, type ApprovalMode, type Settings, type WeekStart } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { useRequireUser } from "@/lib/useRequireUser";

type Draft = Omit<Settings, "updated_at">;

const APPROVAL_OPTIONS: { value: ApprovalMode; label: string; detail: string }[] = [
  {
    value: "always",
    label: "Ask before every change",
    detail: "The assistant suggests; nothing is created, changed or sent until you approve it.",
  },
  {
    value: "high_impact_only",
    label: "Routine changes without asking",
    detail: "The assistant may create and update tasks, notes, events and reminders on its own.",
  },
];

/** All IANA zones the browser knows, with UTC and the given extras included. */
function timeZoneList(extra: string[]): string[] {
  const supported = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];
  return Array.from(new Set(["UTC", ...supported, ...extra])).sort();
}

/** "Sat 17 Oct, 18:25" in the given zone. */
function clockIn(timeZone: string, now: Date): string {
  try {
    return now.toLocaleString(undefined, { timeZone, weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
  } catch {
    return "";
  }
}

export default function SettingsView() {
  const { user, error: authError } = useRequireUser();
  const [saved, setSaved] = useState<Settings | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [device, setDevice] = useState<string | null>(null);

  const showError = useCallback((err: unknown) => setError(errorMessage(err)), []);

  useEffect(() => {
    if (!user) return;
    getSettings()
      .then((s) => {
        setDevice(deviceTimeZone()); // read in the browser only, not during prerender
        setSaved(s);
        setDraft(s);
      })
      .catch(showError);
  }, [user, showError]);

  const zones = useMemo(() => timeZoneList([saved?.timezone, device].filter((z): z is string => !!z)), [saved?.timezone, device]);

  function set<K extends keyof Draft>(key: K, value: Draft[K]) {
    setDraft((d) => (d ? { ...d, [key]: value } : d));
    setNotice(null);
  }

  async function onSave(event: FormEvent) {
    event.preventDefault();
    if (!draft) return;
    setSaving(true);
    setFields({});
    setError(null);
    try {
      const s = await updateSettings(draft);
      setSaved(s);
      setDraft(s);
      setNotice("Settings saved.");
    } catch (err) {
      if (err instanceof ApiError && err.fields) setFields(err.fields);
      showError(err);
    } finally {
      setSaving(false);
    }
  }

  if (!user) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  const control = "rounded-md border border-zinc-300 bg-transparent px-3 py-1.5 text-sm dark:border-zinc-700";
  const section = "flex flex-col gap-3 rounded-md border border-zinc-200 p-4 dark:border-zinc-800";
  const fieldError = (name: string) => fields[name] && <span className="text-xs text-red-600 dark:text-red-400">{fields[name]}</span>;
  const dirty = !!draft && !!saved && JSON.stringify({ ...saved, updated_at: undefined }) !== JSON.stringify({ ...draft, updated_at: undefined });
  const now = new Date();

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />
      <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>

      {!draft ? (
        <p className="text-zinc-500">Loading settings…</p>
      ) : (
        <form onSubmit={onSave} className="flex flex-col gap-4">
          <section className={section} aria-labelledby="tz-heading">
            <h2 id="tz-heading" className="font-medium">
              Time zone
            </h2>
            <p className="text-sm text-zinc-500">
              Used for repeating tasks and events, &ldquo;today&rdquo;, expiry reminders (09:00 on the day) and quiet hours.
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <select value={draft.timezone} onChange={(e) => set("timezone", e.target.value)} aria-label="Time zone" className={control}>
                {zones.map((z) => (
                  <option key={z} value={z}>
                    {z.replaceAll("_", " ")}
                  </option>
                ))}
              </select>
              <span className="text-sm text-zinc-500">Now: {clockIn(draft.timezone, now)}</span>
            </div>
            {fieldError("timezone")}
            {device && device !== draft.timezone && (
              <p className="flex flex-wrap items-center gap-2 rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:bg-amber-950 dark:text-amber-200">
                This device is set to {device.replaceAll("_", " ")}. Screens show times in the device&rsquo;s zone, so keep the two the same.
                <button type="button" onClick={() => set("timezone", device)} className="font-medium underline">
                  Use {device.replaceAll("_", " ")}
                </button>
              </p>
            )}
          </section>

          <section className={section} aria-labelledby="week-heading">
            <h2 id="week-heading" className="font-medium">
              Week starts on
            </h2>
            <div className="flex overflow-hidden self-start rounded-md border border-zinc-300 text-sm dark:border-zinc-700" role="radiogroup" aria-labelledby="week-heading">
              {WEEK_START_OPTIONS.map((o) => (
                <button
                  key={o.value}
                  type="button"
                  role="radio"
                  aria-checked={draft.week_starts_on === o.value}
                  onClick={() => set("week_starts_on", o.value as WeekStart)}
                  className="px-3 py-1.5 aria-checked:bg-zinc-900 aria-checked:text-white dark:aria-checked:bg-zinc-100 dark:aria-checked:text-zinc-900"
                >
                  {o.label}
                </button>
              ))}
            </div>
            {fieldError("week_starts_on")}
          </section>

          <section className={section} aria-labelledby="quiet-heading">
            <h2 id="quiet-heading" className="font-medium">
              Quiet hours
            </h2>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={draft.quiet_hours_enabled} onChange={(e) => set("quiet_hours_enabled", e.target.checked)} />
              Hold reminders during quiet hours
            </label>
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <span className="text-zinc-500">From</span>
              <input type="time" value={draft.quiet_hours_start} onChange={(e) => set("quiet_hours_start", e.target.value)} aria-label="Quiet hours start" disabled={!draft.quiet_hours_enabled} className={control} />
              <span className="text-zinc-500">to</span>
              <input type="time" value={draft.quiet_hours_end} onChange={(e) => set("quiet_hours_end", e.target.value)} aria-label="Quiet hours end" disabled={!draft.quiet_hours_enabled} className={control} />
            </div>
            {fieldError("quiet_hours_start")}
            {fieldError("quiet_hours_end")}
            <p className="text-xs text-zinc-500">
              Reminders that come due in this window arrive when it ends. A reminder still comes straight away if what it&rsquo;s about happens before then, e.g.
              an early flight.
            </p>
          </section>

          <section className={section} aria-labelledby="ai-heading">
            <h2 id="ai-heading" className="font-medium">
              AI assistant approvals
            </h2>
            <div className="flex flex-col gap-2" role="radiogroup" aria-labelledby="ai-heading">
              {APPROVAL_OPTIONS.map((o) => (
                <label key={o.value} className="flex items-start gap-2 text-sm">
                  <input type="radio" name="ai_approval_mode" checked={draft.ai_approval_mode === o.value} onChange={() => set("ai_approval_mode", o.value)} className="mt-1" />
                  <span>
                    <span className="font-medium">{o.label}</span>
                    <span className="block text-zinc-500">{o.detail}</span>
                  </span>
                </label>
              ))}
            </div>
            {fieldError("ai_approval_mode")}
            <p className="text-xs text-zinc-500">
              Sending messages, spending money, deleting data and making bookings always need your approval. The assistant arrives in Phase 2.
            </p>
          </section>

          <div className="flex items-center gap-3">
            <button
              type="submit"
              disabled={saving || !dirty}
              className="rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900"
            >
              {saving ? "Saving…" : "Save settings"}
            </button>
            {dirty && (
              <button type="button" onClick={() => saved && setDraft(saved)} className="text-sm underline">
                Discard changes
              </button>
            )}
            {notice && !dirty && (
              <span role="status" className="text-sm text-green-700 dark:text-green-400">
                {notice}
              </span>
            )}
          </div>
        </form>
      )}
    </div>
  );
}
