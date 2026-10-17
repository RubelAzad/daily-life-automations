import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import ProjectDetail from "@/components/projects/ProjectDetail";

export const metadata: Metadata = { title: "Project · LifeOS AI" };

// Not async: awaiting the dynamic param inside <Suspense> keeps the static
// shell prerenderable (required with cacheComponents).
export default function ProjectPage({ params }: PageProps<"/projects/[id]">) {
  return (
    <Suspense fallback={<p className="p-6 text-zinc-500">Loading…</p>}>
      <ProjectFromParams params={params} />
    </Suspense>
  );
}

async function ProjectFromParams({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const projectId = Number(id);
  if (!Number.isInteger(projectId) || projectId <= 0) notFound();
  return <ProjectDetail id={projectId} />;
}
