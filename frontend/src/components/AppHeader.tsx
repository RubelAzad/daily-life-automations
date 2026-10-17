"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { logout, type User } from "@/lib/api";
import NotificationBell from "./NotificationBell";

const NAV = [
  { href: "/tasks", label: "Tasks" },
  { href: "/calendar", label: "Calendar" },
  { href: "/projects", label: "Projects" },
  { href: "/reminders", label: "Reminders" },
];

export default function AppHeader({ user }: { user: User }) {
  const router = useRouter();
  const pathname = usePathname();

  async function onLogout() {
    await logout();
    router.replace("/login");
  }

  return (
    <header className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex items-center gap-5">
        <Link href="/" className="text-lg font-semibold tracking-tight">
          LifeOS AI
        </Link>
        <nav className="flex gap-1 text-sm" aria-label="Main">
          {NAV.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              aria-current={pathname.startsWith(item.href) ? "page" : undefined}
              className="rounded-md px-2.5 py-1.5 text-zinc-600 hover:text-zinc-900 aria-[current=page]:bg-zinc-100 aria-[current=page]:font-medium aria-[current=page]:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-100 dark:aria-[current=page]:bg-zinc-800 dark:aria-[current=page]:text-zinc-100"
            >
              {item.label}
            </Link>
          ))}
        </nav>
      </div>
      <div className="flex items-center gap-3 text-sm">
        <NotificationBell />
        <span className="text-zinc-600 dark:text-zinc-400">{user.name}</span>
        <button type="button" onClick={onLogout} className="underline">
          Sign out
        </button>
      </div>
    </header>
  );
}

export function ErrorBanner({ error, onDismiss }: { error: string | null; onDismiss: () => void }) {
  if (!error) return null;
  return (
    <div
      role="alert"
      className="flex items-start justify-between gap-3 rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200"
    >
      <span>{error}</span>
      <button type="button" onClick={onDismiss} aria-label="Dismiss error">
        ×
      </button>
    </div>
  );
}

export function ProgressBar({ percent, color, label }: { percent: number; color?: string | null; label: string }) {
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuenow={percent}
      aria-valuemin={0}
      aria-valuemax={100}
      className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-200 dark:bg-zinc-800"
    >
      <div className="h-full rounded-full bg-zinc-900 dark:bg-zinc-100" style={{ width: `${percent}%`, backgroundColor: color ?? undefined }} />
    </div>
  );
}
