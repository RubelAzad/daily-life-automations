import HealthStatus from "@/components/HealthStatus";
import UserPanel from "@/components/UserPanel";

export default function Home() {
  return (
    <main className="mx-auto flex min-h-screen w-full max-w-3xl flex-col justify-center gap-4 px-4">
      <h1 className="text-3xl font-semibold tracking-tight">LifeOS AI</h1>
      <p className="text-zinc-600 dark:text-zinc-400">Daily-life task and workflow automation.</p>
      <UserPanel />
      <HealthStatus />
    </main>
  );
}
