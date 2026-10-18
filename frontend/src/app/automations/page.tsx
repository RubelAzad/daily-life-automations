import type { Metadata } from "next";
import { Suspense } from "react";
import AutomationsView from "@/components/automations/AutomationsView";

export const metadata: Metadata = { title: "Automations · LifeOS AI" };

// AutomationsView reads ?run= (useSearchParams), which needs a Suspense
// boundary to keep the rest of the page prerenderable.
export default function AutomationsPage() {
  return (
    <Suspense fallback={<p className="p-6 text-zinc-500">Loading…</p>}>
      <AutomationsView />
    </Suspense>
  );
}
