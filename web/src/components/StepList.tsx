import type { ComponentType } from "react";
import { Ban, CheckCircle2, Circle, Loader2, SkipForward, XCircle } from "lucide-react";
import { cn } from "../lib/utils";
import type { RunStatus, RunStep } from "../api/types";

const ICONS: Record<RunStatus, ComponentType<{ className?: string }>> = {
  pending: Circle,
  running: Loader2,
  success: CheckCircle2,
  failed: XCircle,
  skipped: SkipForward,
  blocked: Ban,
  cancelled: Ban,
};

const ICON_CLASS: Record<RunStatus, string> = {
  pending: "text-muted-foreground",
  running: "animate-spin text-blue-500",
  success: "text-green-600 dark:text-green-500",
  failed: "text-red-600 dark:text-red-500",
  skipped: "text-muted-foreground",
  blocked: "text-amber-600 dark:text-amber-500",
  cancelled: "text-muted-foreground",
};

// formatDuration renders a millisecond span as a short human string ("3s",
// "1m 12s"). Callers only invoke it once both endpoints are known.
export function formatDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.round(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return minutes > 0 ? `${minutes}m ${seconds}s` : `${seconds}s`;
}

function stepDuration(step: RunStep): string | null {
  if (!step.started_at || !step.finished_at) return null;
  const ms = new Date(step.finished_at).getTime() - new Date(step.started_at).getTime();
  if (Number.isNaN(ms)) return null;
  return formatDuration(ms);
}

// StepList renders one row per plan step: a status icon, the name, and a
// duration computed from started_at/finished_at when the run has reported
// both. It is purely presentational — Run.tsx merges the live SSE status
// onto each step before handing the array down.
export function StepList({ steps }: { steps: RunStep[] }) {
  return (
    <ul className="divide-y rounded-md border">
      {steps.map((step) => {
        const Icon = ICONS[step.status];
        const duration = stepDuration(step);
        return (
          <li key={step.item_ref} className="flex items-center gap-3 px-3 py-2 text-sm">
            <Icon className={cn("h-4 w-4 shrink-0", ICON_CLASS[step.status])} aria-hidden="true" />
            <span className="flex-1 font-medium">{step.name}</span>
            <span className="text-xs text-muted-foreground">{step.status}</span>
            {duration && <span className="text-xs text-muted-foreground">{duration}</span>}
          </li>
        );
      })}
    </ul>
  );
}
