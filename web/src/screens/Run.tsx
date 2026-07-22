import { useMemo } from "react";
import { useParams } from "react-router-dom";
import { Loader2 } from "lucide-react";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "../components/ui/card";
import { Progress } from "../components/ui/progress";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { StepList } from "../components/StepList";
import { LogPane } from "../components/LogPane";
import { ErrorNotice } from "../components/ErrorNotice";
import { useCancelRun, useRun } from "../hooks";
import { useRunStream } from "../hooks/useRunStream";
import type { RunStatus, RunStep } from "../api/types";

// DONE holds every terminal-for-a-step status: a run's progress bar counts a
// step as "done" once it can no longer change, whether or not it succeeded.
const DONE: ReadonlySet<RunStatus> = new Set(["success", "failed", "skipped", "blocked", "cancelled"]);
const CANCELLABLE: ReadonlySet<RunStatus> = new Set(["pending", "running"]);

// Run is the live install view. It seeds from useRun's snapshot — so a
// completed run drilled into from History renders its final state with no
// live stream needed — and layers useRunStream's SSE-derived state on top
// for a run that is still in progress.
export function Run() {
  const { id } = useParams<{ id: string }>();
  const runId = id ?? "";
  const { data: run } = useRun(runId);
  const stream = useRunStream(runId);
  const cancelRun = useCancelRun();

  const steps: RunStep[] = useMemo(() => {
    const base = run?.steps ?? [];
    return base.map((step) => ({
      ...step,
      status: stream.steps[step.item_ref] ?? step.status,
    }));
  }, [run?.steps, stream.steps]);

  const total = steps.length;
  const done = steps.filter((s) => DONE.has(s.status)).length;
  const percent = total > 0 ? Math.round((done / total) * 100) : 0;

  const runStatus = stream.runStatus ?? run?.status;
  const current = steps.find((s) => s.status === "running");
  const failedSteps = steps.filter((s) => s.status === "failed");

  if (!run) {
    return <p className="text-muted-foreground">Loading…</p>;
  }

  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between gap-2">
            <span>Run {run.profile}</span>
            <div className="flex items-center gap-2">
              {stream.reconnecting && <Badge variant="secondary">reconnecting…</Badge>}
              {runStatus && <Badge>{runStatus}</Badge>}
            </div>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <Progress value={percent} />
          <p className="text-sm text-muted-foreground">
            {done} / {total} steps
          </p>
          {current && (
            <p className="flex items-center gap-2 text-sm font-medium">
              <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
              Installing {current.name}…
            </p>
          )}
        </CardContent>
        <CardFooter>
          <Button
            type="button"
            variant="destructive"
            onClick={() => cancelRun.mutate(runId)}
            disabled={cancelRun.isPending || !runStatus || !CANCELLABLE.has(runStatus)}
          >
            Cancel
          </Button>
        </CardFooter>
      </Card>

      {cancelRun.isError && <ErrorNotice error={cancelRun.error} />}

      <StepList steps={steps} />

      {failedSteps.map((step) => (
        <div
          key={step.item_ref}
          className="rounded-md border border-red-500/40 bg-red-500/10 p-3 text-sm"
        >
          <p className="font-medium text-red-600 dark:text-red-400">{step.name} failed</p>
          <div className="mt-1 space-y-0.5 font-mono text-xs">
            {stream.logs
              .filter((l) => l.stepId === step.item_ref)
              .slice(-10)
              .map((l, i) => (
                <div key={i} className="whitespace-pre-wrap break-all">{l.line}</div>
              ))}
          </div>
        </div>
      ))}

      <LogPane lines={stream.logs} />
    </div>
  );
}
