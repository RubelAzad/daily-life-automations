import type { Metadata } from "next";
import AIView from "@/components/ai/AIView";

export const metadata: Metadata = { title: "AI assistant · LifeOS AI" };

export default function AIPage() {
  return <AIView />;
}
