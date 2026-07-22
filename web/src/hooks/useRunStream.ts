import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { streamRun } from "../api/sse";
import { useApiToken } from "./useApi";
import type { ApiEvent, RunStatus } from "../api/types";

export interface LogLine { stepId?: string; stream?: string; line: string; }
export interface StreamState {
  steps: Record<string, RunStatus>;
  logs: LogLine[];
  runStatus: RunStatus | null;
  reconnecting: boolean;
}

// useRunStream owns one run's live SSE lifecycle. It reconnects on a plain drop
// (the server replays from sqlite, so a re-fetch catches up with no gap and no
// duplicate) and stops on a terminal run event, refreshing the cached run so
// History and the final state reflect reality.
export function useRunStream(runId: string): StreamState {
  const token = useApiToken();
  const qc = useQueryClient();
  const [state, setState] = useState<StreamState>({ steps: {}, logs: [], runStatus: null, reconnecting: false });
  const stopped = useRef(false);

  useEffect(() => {
    stopped.current = false;
    let cancel = () => {};

    const onEvent = (e: ApiEvent) => setState((s) => fold(s, e));

    const connect = () => {
      cancel = streamRun(runId, token, {
        onEvent,
        onClose: () => {
          stopped.current = true;
          setState((s) => ({ ...s, reconnecting: false }));
          qc.invalidateQueries({ queryKey: ["run", runId] });
          qc.invalidateQueries({ queryKey: ["runs"] });
        },
        onError: () => {
          if (stopped.current) return;
          setState((s) => ({ ...s, reconnecting: true }));
          // Replay makes reconnect a plain re-open.
          setTimeout(() => { if (!stopped.current) connect(); }, 1000);
        },
      });
    };
    connect();

    return () => { stopped.current = true; cancel(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runId, token]);

  return state;
}

function fold(s: StreamState, e: ApiEvent): StreamState {
  if (e.type === "step" && e.step_id && e.status) {
    return { ...s, reconnecting: false, steps: { ...s.steps, [e.step_id]: e.status as RunStatus } };
  }
  if (e.type === "log" && e.line !== undefined) {
    return { ...s, reconnecting: false, logs: [...s.logs, { stepId: e.step_id, stream: e.stream, line: e.line }] };
  }
  if (e.type === "run" && e.status) {
    return { ...s, runStatus: e.status as RunStatus };
  }
  return s;
}
