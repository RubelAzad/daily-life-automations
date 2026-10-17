"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import RemindersPanel from "@/components/RemindersPanel";
import {
  createNote,
  deleteFile,
  deleteNote,
  fileUrl,
  getNote,
  NOTE_KINDS,
  updateNote,
  uploadFile,
  type Note,
  type NoteKind,
  type Project,
} from "@/lib/api";
import { formatBytes } from "@/lib/format";

type Props = {
  noteId: number | null; // null = new note
  projects: Project[];
  onSaved: (note: Note) => void;
  onDeleted: () => void;
  onChanged: () => void; // attachments changed (list counts)
  onError: (err: unknown) => void;
};

const control = "rounded-md border border-zinc-300 bg-transparent px-2 py-1.5 text-sm dark:border-zinc-700";
const LABEL: Record<NoteKind, string> = { note: "Note", idea: "Idea", link: "Link", document: "Document", receipt: "Receipt" };

export default function NoteEditor(props: Props) {
  const { noteId, onError } = props;
  const [note, setNote] = useState<Note | null>(null);
  const [version, setVersion] = useState(0);

  useEffect(() => {
    if (noteId === null) return;
    let cancelled = false;
    getNote(noteId)
      .then((n) => {
        if (!cancelled) setNote(n);
      })
      .catch(onError);
    return () => {
      cancelled = true;
    };
  }, [noteId, version, onError]);

  if (noteId !== null && !note) return <p className="text-zinc-500">Loading note…</p>;
  // Remounts with fresh server values after each save (updated_at changes);
  // attachment changes refetch without resetting what's being typed.
  return <NoteForm key={note?.updated_at ?? "new"} {...props} note={note} refresh={() => setVersion((v) => v + 1)} />;
}

function NoteForm({ note, projects, onSaved, onDeleted, onChanged, onError, refresh }: Props & { note: Note | null; refresh: () => void }) {
  const [title, setTitle] = useState(note?.title ?? "");
  const [kind, setKind] = useState<NoteKind>(note?.kind ?? "note");
  const [url, setUrl] = useState(note?.url ?? "");
  const [body, setBody] = useState(note?.body ?? "");
  const [tags, setTags] = useState(note?.tags.map((t) => t.name).join(", ") ?? "");
  const [expiresOn, setExpiresOn] = useState(note?.expires_on ?? "");
  const [projectId, setProjectId] = useState(note?.project_id ? String(note.project_id) : "");
  const [pinned, setPinned] = useState(note?.pinned ?? false);
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const input = {
      title,
      kind,
      url: url.trim() || null,
      body: body.trim() ? body : null,
      tags: tags.split(",").map((t) => t.trim()).filter(Boolean),
      expires_on: expiresOn || null,
      project_id: projectId ? Number(projectId) : null,
      pinned,
    };
    setSaving(true);
    try {
      const saved = note ? await updateNote(note.id, input) : await createNote(input);
      refresh();
      onSaved(saved);
    } catch (err) {
      onError(err);
      setSaving(false);
    }
  }

  async function run(action: () => Promise<unknown>, after: () => void) {
    try {
      await action();
      after();
    } catch (err) {
      onError(err);
    }
  }

  async function onUpload(files: FileList | null) {
    if (!note || !files?.length) return;
    setUploading(true);
    try {
      for (const f of Array.from(files)) await uploadFile(note.id, f);
    } catch (err) {
      onError(err);
    } finally {
      setUploading(false);
      if (fileInput.current) fileInput.current.value = "";
      filesChanged();
    }
  }

  function filesChanged() {
    refresh();
    onChanged();
  }

  const showUrl = kind === "link" || url !== "";
  const showExpiry = kind === "document" || kind === "receipt" || expiresOn !== "";

  return (
    <div className="flex flex-col gap-4">
      <form onSubmit={onSubmit} className="flex flex-col gap-3 rounded-lg border border-zinc-200 p-4 dark:border-zinc-800">
        <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Title" maxLength={255} aria-label="Title" className={`${control} text-lg`} autoFocus={!note} />
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <select value={kind} onChange={(e) => setKind(e.target.value as NoteKind)} aria-label="Kind" className={control}>
            {NOTE_KINDS.map((k) => (
              <option key={k} value={k}>
                {LABEL[k]}
              </option>
            ))}
          </select>
          <select value={projectId} onChange={(e) => setProjectId(e.target.value)} aria-label="Project" className={control}>
            <option value="">No project</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <label className="flex items-center gap-1.5">
            <input type="checkbox" checked={pinned} onChange={(e) => setPinned(e.target.checked)} />
            Pinned
          </label>
          {!showExpiry && (
            <button type="button" onClick={() => setExpiresOn(new Date().toISOString().slice(0, 10))} className="text-zinc-500 underline">
              + Expiry date
            </button>
          )}
        </div>
        {showUrl && <input type="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://…" aria-label="URL" className={control} />}
        {showExpiry && (
          <label className="flex flex-wrap items-center gap-2 text-sm">
            <span className="text-zinc-500">Expires on</span>
            <input type="date" value={expiresOn} onChange={(e) => setExpiresOn(e.target.value)} aria-label="Expiry date" className={control} />
            <span className="text-xs text-zinc-500">For passports, memberships, warranties, insurance…</span>
          </label>
        )}
        <textarea value={body} onChange={(e) => setBody(e.target.value)} rows={10} placeholder="Write anything: ideas, details, numbers…" aria-label="Note text" className={control} />
        <input value={tags} onChange={(e) => setTags(e.target.value)} placeholder="Tags, comma separated (e.g. home, receipts)" aria-label="Tags" className={control} />
        <div className="flex flex-wrap items-center justify-between gap-2">
          {note ? (
            <div className="flex gap-3 text-sm">
              <button type="button" onClick={() => run(() => updateNote(note.id, { archived: !note.archived }).then(onSaved), refresh)} className="underline">
                {note.archived ? "Restore from archive" : "Archive"}
              </button>
              <button
                type="button"
                onClick={() =>
                  window.confirm(`Delete "${note.title}"${note.file_count ? ` and its ${note.file_count} file(s)` : ""}? This can't be undone.`) &&
                  run(() => deleteNote(note.id), onDeleted)
                }
                className="text-red-600 underline"
              >
                Delete
              </button>
            </div>
          ) : (
            <span />
          )}
          <button type="submit" disabled={saving || !title.trim()} className="rounded-md bg-zinc-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900">
            {saving ? "Saving…" : note ? "Save" : "Create note"}
          </button>
        </div>
      </form>

      {note && (
        <section className="flex flex-col gap-2 rounded-lg border border-zinc-200 p-4 text-sm dark:border-zinc-800">
          <div className="flex items-center justify-between">
            <h2 className="font-medium">Attachments</h2>
            <label className={`cursor-pointer rounded-md border border-zinc-300 px-3 py-1 dark:border-zinc-700 ${uploading ? "opacity-50" : ""}`}>
              {uploading ? "Uploading…" : "Upload files"}
              <input
                ref={fileInput}
                type="file"
                multiple
                disabled={uploading}
                onChange={(e) => onUpload(e.target.files)}
                accept=".pdf,.png,.jpg,.jpeg,.gif,.webp,.txt,.md,.csv,.docx,.xlsx,.pptx,.doc,.xls,.zip"
                className="sr-only"
              />
            </label>
          </div>
          {note.files?.length ? (
            <ul className="flex flex-col divide-y divide-zinc-100 dark:divide-zinc-900">
              {note.files.map((f) => (
                <li key={f.id} className="flex items-center gap-3 py-1.5">
                  <a href={fileUrl(f.id)} target="_blank" rel="noopener noreferrer" className="min-w-0 flex-1 truncate underline">
                    {f.name}
                  </a>
                  <span className="shrink-0 text-xs text-zinc-500">{formatBytes(f.size_bytes)}</span>
                  <a href={fileUrl(f.id, true)} className="shrink-0 text-xs underline">
                    Download
                  </a>
                  <button
                    type="button"
                    onClick={() => window.confirm(`Delete ${f.name}?`) && run(() => deleteFile(f.id), filesChanged)}
                    aria-label={`Delete ${f.name}`}
                    className="shrink-0 text-zinc-400 hover:text-red-600"
                  >
                    ×
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-zinc-500">Receipts, scans, manuals, PDFs and images — up to 20 MB each.</p>
          )}
        </section>
      )}

      {note?.expires_on && (
        <section className="rounded-lg border border-zinc-200 p-4 dark:border-zinc-800">
          <RemindersPanel noteId={note.id} onError={onError} />
        </section>
      )}
    </div>
  );
}
