import type { Metadata } from "next";
import ApprovalsView from "@/components/approvals/ApprovalsView";

export const metadata: Metadata = { title: "Approvals · LifeOS AI" };

export default function ApprovalsPage() {
  return <ApprovalsView />;
}
