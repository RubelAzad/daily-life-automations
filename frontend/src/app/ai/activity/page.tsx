import type { Metadata } from "next";
import AIView from "@/components/ai/AIView";

export const metadata: Metadata = { title: "AI activity · LifeOS AI" };

export default function AIActivityPage() {
  return <AIView />;
}
