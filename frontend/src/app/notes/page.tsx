import type { Metadata } from "next";
import { Suspense } from "react";
import NotesView from "@/components/notes/NotesView";

export const metadata: Metadata = { title: "Notes · LifeOS AI" };

// NotesView reads ?id= (useSearchParams), which needs a Suspense boundary to
// keep the rest of the page prerenderable.
export default function NotesPage() {
  return (
    <Suspense fallback={<p className="p-6 text-zinc-500">Loading…</p>}>
      <NotesView />
    </Suspense>
  );
}
