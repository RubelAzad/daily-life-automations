"use client";

import { useEffect, useState } from "react";
import { getHealth, type Health } from "@/lib/api";

export default function HealthStatus() {
  const [health, setHealth] = useState<Health | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getHealth()
      .then(setHealth)
      .catch((err: Error) => setError(err.message));
  }, []);

  if (error) return <p className="text-red-600">API unreachable: {error}</p>;
  if (!health) return <p className="text-zinc-500">Checking API…</p>;

  const ok = health.status === "ok";
  return (
    <p className={ok ? "text-green-600" : "text-red-600"}>
      API: {health.status} · Database: {health.database}
    </p>
  );
}
