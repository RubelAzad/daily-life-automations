"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { getMe, type User } from "@/lib/api";
import { errorMessage } from "@/lib/errors";

/** Loads the signed-in user, redirecting to /login when there is none. */
export function useRequireUser() {
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getMe()
      .then((u) => (u ? setUser(u) : router.replace("/login")))
      .catch((err) => setError(errorMessage(err)));
  }, [router]);

  return { user, error };
}
