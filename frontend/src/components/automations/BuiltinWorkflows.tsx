"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { addAutomationTemplate, getAIStatus, listAutomationTemplates, type Automation, type AutomationTemplate } from "@/lib/api";
import { errorMessage } from "@/lib/errors";

type Props = {
  /** Bumped by the parent when automations change (e.g. one was deleted). */
  version: number;
  onAdded: (a: Automation) => void;
  onError: (message: string) => void;
};

/** Ready-made workflows, added as ordinary automations with one click. */
export default function BuiltinWorkflows({ version, onAdded, onError }: Props) {
  const [templates, setTemplates] = useState<AutomationTemplate[] | null>(null);
  const [aiOn, setAiOn] = useState<boolean | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    listAutomationTemplates()
      .then((t) => !cancelled && setTemplates(t))
      .catch((err) => onError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, [version, onError]);

  useEffect(() => {
    getAIStatus()
      .then((s) => setAiOn(s.enabled))
      .catch(() => setAiOn(null));
  }, []);

  async function add(t: AutomationTemplate) {
    setBusy(t.key);
    try {
      onAdded(await addAutomationTemplate(t.key));
    } catch (err) {
      onError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  if (!templates) return null;
  return (
    <section className="flex flex-col rounded-md border border-zinc-200 dark:border-zinc-800" aria-labelledby="builtin-heading">
      <h2 id="builtin-heading" className="border-b border-zinc-200 px-3 py-2 text-sm font-medium dark:border-zinc-800">
        Built-in workflows
      </h2>
      <ul className="grid gap-px bg-zinc-200 sm:grid-cols-2 dark:bg-zinc-800">
        {templates.map((t) => (
          <li key={t.key} className="flex flex-col gap-2 bg-white px-3 py-3 dark:bg-zinc-950">
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium">{t.name}</span>
              {t.automation_id ? (
                <span className="text-xs text-green-700 dark:text-green-400">Added ✓</span>
              ) : (
                <button
                  type="button"
                  disabled={busy !== null}
                  onClick={() => add(t)}
                  className="rounded-md bg-zinc-900 px-3 py-1 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900"
                >
                  {busy === t.key ? "Adding…" : "Add"}
                </button>
              )}
            </div>
            <p className="text-sm text-zinc-600 dark:text-zinc-400">{t.description}</p>
            <p className="text-xs text-zinc-500">Approval: {t.approval}</p>
            {t.needs_ai && aiOn === false && (
              <p className="text-xs text-amber-700 dark:text-amber-400">
                Needs the AI assistant, which is off.{" "}
                <Link href="/ai/activity" className="underline">
                  How to turn it on
                </Link>
              </p>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
