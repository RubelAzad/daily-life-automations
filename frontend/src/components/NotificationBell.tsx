"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import {
  getUnreadCount,
  listNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  snoozeReminder,
  type AppNotification,
} from "@/lib/api";
import { formatDue, timeAgo } from "@/lib/format";

const POLL_MS = 30_000;

/** Unread badge (polled) with a dropdown of recent notifications. */
export default function NotificationBell() {
  const [count, setCount] = useState(0);
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<AppNotification[] | null>(null);
  const [version, setVersion] = useState(0);

  useEffect(() => {
    let cancelled = false;
    const load = () =>
      getUnreadCount()
        .then((n) => {
          if (!cancelled) setCount(n);
        })
        .catch(() => {}); // the bell is best-effort; pages show API errors
    load();
    const id = setInterval(load, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [version]);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    listNotifications({ per_page: 15 })
      .then((r) => {
        if (!cancelled) setItems(r.data);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [open, version]);

  const refresh = () => setVersion((v) => v + 1);

  async function act(action: () => Promise<unknown>) {
    try {
      await action();
    } finally {
      refresh();
    }
  }

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        aria-label={count ? `Notifications, ${count} unread` : "Notifications"}
        className="relative rounded-md px-2 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800"
      >
        <svg viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
          <path d="M6 8a6 6 0 1 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
          <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
        </svg>
        {count > 0 && (
          <span className="absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-red-600 px-1 text-center text-[10px] leading-4 font-semibold text-white">
            {count > 99 ? "99+" : count}
          </span>
        )}
      </button>

      {open && (
        <>
          <button type="button" aria-label="Close notifications" className="fixed inset-0 z-10 cursor-default" onClick={() => setOpen(false)} />
          <div className="absolute right-0 z-20 mt-2 flex max-h-[70vh] w-[min(22rem,calc(100vw-2rem))] flex-col overflow-hidden rounded-lg border border-zinc-200 bg-white shadow-lg dark:border-zinc-800 dark:bg-zinc-950">
            <div className="flex items-center justify-between border-b border-zinc-200 px-3 py-2 dark:border-zinc-800">
              <span className="text-sm font-semibold">Notifications</span>
              {count > 0 && (
                <button type="button" onClick={() => act(markAllNotificationsRead)} className="text-xs underline">
                  Mark all read
                </button>
              )}
            </div>
            <ul className="flex-1 divide-y divide-zinc-100 overflow-y-auto dark:divide-zinc-900">
              {items === null ? (
                <li className="px-3 py-4 text-sm text-zinc-500">Loading…</li>
              ) : items.length === 0 ? (
                <li className="px-3 py-6 text-center text-sm text-zinc-500">No notifications yet. Reminders you set will show up here.</li>
              ) : (
                items.map((n) => (
                  <li key={n.id} className={`flex flex-col gap-1 px-3 py-2 text-sm ${n.read_at ? "opacity-60" : ""}`}>
                    <div className="flex items-start gap-2">
                      {!n.read_at && <span className="mt-1.5 size-2 shrink-0 rounded-full bg-blue-600" aria-label="Unread" />}
                      <Link
                        href={n.link ?? "/"}
                        onClick={() => {
                          setOpen(false);
                          if (!n.read_at) act(() => markNotificationRead(n.id));
                        }}
                        className="min-w-0 flex-1"
                      >
                        <span className={`block font-medium wrap-break-word ${n.priority === "high" ? "text-red-700 dark:text-red-400" : ""}`}>{n.title}</span>
                        {n.body && <span className="block text-zinc-600 dark:text-zinc-400">{n.body}</span>}
                        <span className="block text-xs text-zinc-500">
                          {n.occurs_at && `${formatDue(n.occurs_at)} · `}
                          {timeAgo(n.created_at)}
                        </span>
                      </Link>
                    </div>
                    <div className="flex gap-3 pl-4 text-xs">
                      {n.reminder_id && (
                        <>
                          <button type="button" onClick={() => act(() => snoozeReminder(n.reminder_id!, 10).then(() => markNotificationRead(n.id)))} className="underline">
                            Snooze 10 min
                          </button>
                          <button type="button" onClick={() => act(() => snoozeReminder(n.reminder_id!, 60).then(() => markNotificationRead(n.id)))} className="underline">
                            1 h
                          </button>
                        </>
                      )}
                      {!n.read_at && (
                        <button type="button" onClick={() => act(() => markNotificationRead(n.id))} className="underline">
                          Mark read
                        </button>
                      )}
                    </div>
                  </li>
                ))
              )}
            </ul>
            <Link href="/reminders" onClick={() => setOpen(false)} className="border-t border-zinc-200 px-3 py-2 text-center text-xs underline dark:border-zinc-800">
              Manage reminders
            </Link>
          </div>
        </>
      )}
    </div>
  );
}
