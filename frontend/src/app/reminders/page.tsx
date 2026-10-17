import type { Metadata } from "next";
import RemindersView from "@/components/RemindersView";

export const metadata: Metadata = { title: "Reminders · LifeOS AI" };

export default function RemindersPage() {
  return <RemindersView />;
}
