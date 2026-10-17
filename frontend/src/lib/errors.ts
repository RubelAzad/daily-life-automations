import { ApiError } from "@/lib/api";

/** Human-readable message for an API or network error, including field errors. */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    const fields = err.fields ? Object.entries(err.fields).map(([k, v]) => `${k} ${v}`) : [];
    return fields.length ? fields.join("; ") : err.message;
  }
  return "Could not reach the API. Is the backend running?";
}
