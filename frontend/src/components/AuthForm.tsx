"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { ApiError, deviceTimeZone, login, register } from "@/lib/api";

type Mode = "login" | "register";

export default function AuthForm({ mode }: { mode: Mode }) {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const email = String(form.get("email") ?? "");
    const password = String(form.get("password") ?? "");

    setSubmitting(true);
    setError(null);
    setFields({});
    try {
      if (mode === "register") {
        await register({ name: String(form.get("name") ?? ""), email, password, timezone: deviceTimeZone() });
      } else {
        await login({ email, password });
      }
      router.replace("/today");
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
        setFields(err.fields ?? {});
      } else {
        setError("Could not reach the API. Is the backend running?");
      }
    } finally {
      setSubmitting(false);
    }
  }

  const isRegister = mode === "register";

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-sm flex-col justify-center gap-6 px-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{isRegister ? "Create your account" : "Sign in"}</h1>
        <p className="text-sm text-zinc-600 dark:text-zinc-400">LifeOS AI</p>
      </div>

      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        {isRegister && <Field name="name" label="Name" autoComplete="name" error={fields.name} />}
        <Field name="email" label="Email" type="email" autoComplete="email" error={fields.email} />
        <Field
          name="password"
          label="Password"
          type="password"
          autoComplete={isRegister ? "new-password" : "current-password"}
          error={fields.password}
          hint={isRegister ? "At least 8 characters" : undefined}
        />

        {error && !Object.keys(fields).length && <p className="text-sm text-red-600">{error}</p>}

        <button
          type="submit"
          disabled={submitting}
          className="h-10 rounded-md bg-zinc-900 font-medium text-white hover:bg-zinc-700 disabled:opacity-60 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
        >
          {submitting ? "Please wait…" : isRegister ? "Create account" : "Sign in"}
        </button>
      </form>

      <p className="text-sm text-zinc-600 dark:text-zinc-400">
        {isRegister ? "Already have an account? " : "New to LifeOS? "}
        <Link href={isRegister ? "/login" : "/register"} className="font-medium text-zinc-900 underline dark:text-zinc-100">
          {isRegister ? "Sign in" : "Create an account"}
        </Link>
      </p>
    </main>
  );
}

function Field(props: { name: string; label: string; type?: string; autoComplete?: string; error?: string; hint?: string }) {
  const { name, label, type = "text", autoComplete, error, hint } = props;
  return (
    <label className="flex flex-col gap-1 text-sm">
      <span className="font-medium">{label}</span>
      <input
        name={name}
        type={type}
        autoComplete={autoComplete}
        aria-invalid={Boolean(error)}
        className="h-10 rounded-md border border-zinc-300 bg-transparent px-3 outline-none focus:border-zinc-900 aria-invalid:border-red-500 dark:border-zinc-700 dark:focus:border-zinc-100"
      />
      {error ? (
        <span className="text-red-600">
          {label} {error}
        </span>
      ) : (
        hint && <span className="text-zinc-500">{hint}</span>
      )}
    </label>
  );
}
