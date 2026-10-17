"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { getMe, logout, type User } from "@/lib/api";

type State = { status: "loading" } | { status: "signed-out" } | { status: "signed-in"; user: User } | { status: "error" };

export default function UserPanel() {
  const [state, setState] = useState<State>({ status: "loading" });

  useEffect(() => {
    getMe()
      .then((user) => setState(user ? { status: "signed-in", user } : { status: "signed-out" }))
      .catch(() => setState({ status: "error" }));
  }, []);

  async function onLogout() {
    await logout();
    setState({ status: "signed-out" });
  }

  switch (state.status) {
    case "loading":
      return <p className="text-zinc-500">Loading…</p>;
    case "error":
      return null; // HealthStatus already reports an unreachable API
    case "signed-out":
      return (
        <div className="flex gap-3">
          <Link href="/login" className="rounded-md bg-zinc-900 px-4 py-2 font-medium text-white dark:bg-zinc-100 dark:text-zinc-900">
            Sign in
          </Link>
          <Link href="/register" className="rounded-md border border-zinc-300 px-4 py-2 font-medium dark:border-zinc-700">
            Create account
          </Link>
        </div>
      );
    case "signed-in":
      return (
        <div className="flex items-center gap-4">
          <p>
            Signed in as <span className="font-medium">{state.user.name}</span>{" "}
            <span className="text-zinc-500">({state.user.email})</span>
          </p>
          <button type="button" onClick={onLogout} className="text-sm underline">
            Sign out
          </button>
        </div>
      );
  }
}
