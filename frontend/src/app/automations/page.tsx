import type { Metadata } from "next";
import AutomationsView from "@/components/automations/AutomationsView";

export const metadata: Metadata = { title: "Automations · LifeOS AI" };

export default function AutomationsPage() {
  return <AutomationsView />;
}
