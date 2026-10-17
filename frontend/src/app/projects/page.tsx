import type { Metadata } from "next";
import ProjectsView from "@/components/projects/ProjectsView";

export const metadata: Metadata = { title: "Projects · LifeOS AI" };

export default function ProjectsPage() {
  return <ProjectsView />;
}
