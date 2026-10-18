import type { Metadata } from "next";
import { Suspense } from "react";
import FinanceView from "@/components/finance/FinanceView";

export const metadata: Metadata = { title: "Finance · LifeOS AI" };

// FinanceView starts on the current month (new Date()), which needs a
// Suspense boundary to keep the rest of the page prerenderable.
export default function FinancePage() {
  return (
    <Suspense fallback={<p className="p-6 text-zinc-500">Loading…</p>}>
      <FinanceView />
    </Suspense>
  );
}
