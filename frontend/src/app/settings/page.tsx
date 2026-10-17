import type { Metadata } from "next";
import SettingsView from "@/components/SettingsView";

export const metadata: Metadata = { title: "Settings · LifeOS AI" };

export default function SettingsPage() {
  return <SettingsView />;
}
