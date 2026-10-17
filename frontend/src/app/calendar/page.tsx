import type { Metadata } from "next";
import CalendarView from "@/components/calendar/CalendarView";

export const metadata: Metadata = { title: "Calendar · LifeOS AI" };

export default function CalendarPage() {
  return <CalendarView />;
}
