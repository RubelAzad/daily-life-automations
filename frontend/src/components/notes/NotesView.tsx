"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import AppHeader, { ErrorBanner } from "@/components/AppHeader";
import { listNotes, listProjects, listTags, NOTE_KINDS, type Note, type NoteKind, type Project, type Tag } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatDate, isPastDate } from "@/lib/format";
import { useRequireUser } from "@/lib/useRequireUser";
import NoteEditor from "./NoteEditor";

export const KIND_LABEL: Record<NoteKind, string> = { note: "Note", idea: "Idea", link: "Link", document: "Document", receipt: "Receipt" };
const EXPIRING_SOON_DAYS = 30;

export default function NotesView() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { user, error: authError } = useRequireUser();
  const [notes, setNotes] = useState<Note[] | null>(null);
  const [total, setTotal] = useState(0);
  const [tags, setTags] = useState<Tag[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState<NoteKind | "">("");
  const [tagId, setTagId] = useState<number | null>(null);
  const [expiringSoon, setExpiringSoon] = useState(false);
  const [archived, setArchived] = useState(false);
  const selectedParam = searchParams.get("id");
  const selected: number | "new" | null = selectedParam === "new" ? "new" : selectedParam ? Number(selectedParam) : null;
  const [error, setError] = useState<string | null>(null);
  const [version, setVersion] = useState(0);

  const showError = useCallback((err: unknown) => setError(errorMessage(err)), []);
  const reload = useCallback(() => setVersion((v) => v + 1), []);
  const select = useCallback((id: number | "new" | null) => router.replace(id === null ? "/notes" : `/notes?id=${id}`, { scroll: false }), [router]);

  useEffect(() => {
    const id = setTimeout(() => setQuery(search.trim()), 300);
    return () => clearTimeout(id);
  }, [search]);

  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    listNotes({
      q: query,
      kind: kind || undefined,
      tag_id: tagId ?? undefined,
      archived: archived || undefined,
      expiring_within_days: expiringSoon ? EXPIRING_SOON_DAYS : undefined,
      per_page: 100,
    })
      .then((r) => {
        if (cancelled) return;
        setNotes(r.data);
        setTotal(r.meta.total);
      })
      .catch(showError);
    return () => {
      cancelled = true;
    };
  }, [user, query, kind, tagId, archived, expiringSoon, version, showError]);

  useEffect(() => {
    if (!user) return;
    listTags().then(setTags).catch(showError);
  }, [user, version, showError]);

  useEffect(() => {
    if (!user) return;
    listProjects({ per_page: 100 })
      .then((r) => setProjects(r.data))
      .catch(showError);
  }, [user, showError]);

  if (!user) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  const control = "rounded-md border border-zinc-300 bg-transparent px-2 py-1.5 text-sm dark:border-zinc-700";
  const chip = "rounded-full border px-2.5 py-0.5 text-xs";

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">Notes & documents</h1>
        <button type="button" onClick={() => select("new")} className="rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white dark:bg-zinc-100 dark:text-zinc-900">
          New note
        </button>
      </div>

      <div className="grid gap-6 md:grid-cols-[320px_1fr]">
        <aside className="flex min-w-0 flex-col gap-3">
          <input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search notes, tags, file names…"
            aria-label="Search notes"
            className={control}
          />
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <select value={kind} onChange={(e) => setKind(e.target.value as NoteKind | "")} aria-label="Kind" className={control}>
              <option value="">All kinds</option>
              {NOTE_KINDS.map((k) => (
                <option key={k} value={k}>
                  {KIND_LABEL[k]}s
                </option>
              ))}
            </select>
            <label className="flex items-center gap-1.5">
              <input type="checkbox" checked={expiringSoon} onChange={(e) => setExpiringSoon(e.target.checked)} />
              Expiring soon
            </label>
            <label className="flex items-center gap-1.5">
              <input type="checkbox" checked={archived} onChange={(e) => setArchived(e.target.checked)} />
              Archive
            </label>
          </div>
          {tags.length > 0 && (
            <div className="flex flex-wrap gap-1.5" aria-label="Filter by tag">
              {tags.map((t) => (
                <button
                  key={t.id}
                  type="button"
                  aria-pressed={tagId === t.id}
                  onClick={() => setTagId(tagId === t.id ? null : t.id)}
                  className={`${chip} border-zinc-300 aria-pressed:border-zinc-900 aria-pressed:bg-zinc-900 aria-pressed:text-white dark:border-zinc-700 dark:aria-pressed:bg-zinc-100 dark:aria-pressed:text-zinc-900`}
                  style={t.color && tagId !== t.id ? { borderColor: t.color } : undefined}
                >
                  #{t.name} <span className="text-zinc-500">{t.note_count}</span>
                </button>
              ))}
            </div>
          )}

          {notes === null ? (
            <p className="text-sm text-zinc-500">Loading notes…</p>
          ) : notes.length === 0 ? (
            <p className="rounded-md border border-dashed border-zinc-300 px-3 py-6 text-center text-sm text-zinc-500 dark:border-zinc-700">
              {query || kind || tagId || expiringSoon ? "No notes match." : archived ? "The archive is empty." : "No notes yet. Save ideas, links, receipts and documents here."}
            </p>
          ) : (
            <ul className="flex flex-col gap-1">
              {notes.map((n) => {
                const expired = n.expires_on && isPastDate(n.expires_on);
                return (
                  <li key={n.id}>
                    <button
                      type="button"
                      onClick={() => select(n.id)}
                      aria-current={selected === n.id}
                      className="flex w-full flex-col gap-1 rounded-md px-3 py-2 text-left hover:bg-zinc-100 aria-[current=true]:bg-zinc-100 dark:hover:bg-zinc-900 dark:aria-[current=true]:bg-zinc-900"
                    >
                      <span className="flex items-center gap-2">
                        {n.pinned && <span aria-label="Pinned">★</span>}
                        <span className="min-w-0 flex-1 truncate font-medium">{n.title}</span>
                        <span className="shrink-0 text-xs text-zinc-500">{KIND_LABEL[n.kind]}</span>
                      </span>
                      {(n.body || n.url) && <span className="line-clamp-2 text-xs text-zinc-500">{n.body ?? n.url}</span>}
                      <span className="flex flex-wrap items-center gap-2 text-xs text-zinc-500">
                        {n.expires_on && (
                          <span className={expired ? "font-medium text-red-600" : ""}>
                            {expired ? "Expired" : "Expires"} {formatDate(n.expires_on)}
                          </span>
                        )}
                        {n.file_count > 0 && <span>📎 {n.file_count}</span>}
                        {n.tags.map((t) => (
                          <span key={t.id}>#{t.name}</span>
                        ))}
                      </span>
                    </button>
                  </li>
                );
              })}
              {total > notes.length && <li className="px-3 text-xs text-zinc-500">Showing {notes.length} of {total}. Refine your search to see more.</li>}
            </ul>
          )}
        </aside>

        <main className="min-w-0">
          {selected === null ? (
            <p className="rounded-md border border-dashed border-zinc-300 px-4 py-12 text-center text-zinc-500 dark:border-zinc-700">Select a note, or create a new one.</p>
          ) : (
            <NoteEditor
              key={selected}
              noteId={selected === "new" ? null : selected}
              projects={projects}
              onSaved={(note) => {
                reload();
                select(note.id);
              }}
              onDeleted={() => {
                reload();
                select(null);
              }}
              onChanged={reload}
              onError={showError}
            />
          )}
        </main>
      </div>
    </div>
  );
}
