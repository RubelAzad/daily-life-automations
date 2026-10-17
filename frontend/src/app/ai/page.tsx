import type { Metadata } from "next";
import { Suspense } from "react";
import ChatView from "@/components/ai/ChatView";

export const metadata: Metadata = { title: "AI assistant · LifeOS AI" };

// ChatView reads ?c= (useSearchParams), which needs a Suspense boundary.
export default function AIPage() {
  return (
    <Suspense fallback={<p className="p-6 text-zinc-500">Loading…</p>}>
      <ChatView />
    </Suspense>
  );
}
