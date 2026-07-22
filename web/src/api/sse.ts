import type { ApiEvent } from "./types";

export interface StreamHandlers {
  onEvent: (e: ApiEvent) => void;
  // onClose fires only on a terminal run event — the stream is genuinely over.
  onClose: () => void;
  // onError fires on a connection failure or a stream that ended without a
  // terminal event. The caller decides whether to reconnect (the server
  // replays, so reconnect is a plain re-invocation of streamRun).
  onError: (err: unknown) => void;
}

const TERMINAL = new Set(["success", "failed", "cancelled"]);

// streamRun opens a run's SSE stream over fetch, because EventSource cannot
// send the X-API-Token header. It returns a cancel function that aborts the
// request; the returned function is safe to call more than once.
export function streamRun(runId: string, token: string, h: StreamHandlers): () => void {
  const ac = new AbortController();

  (async () => {
    let res: Response;
    try {
      res = await fetch(`/api/v1/runs/${encodeURIComponent(runId)}/events`, {
        headers: { "X-API-Token": token },
        signal: ac.signal,
      });
    } catch (err) {
      if (!ac.signal.aborted) h.onError(err);
      return;
    }

    if (!res.ok || !res.body) {
      h.onError(new Error(`event stream failed with status ${res.status}`));
      return;
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let terminal = false;

    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        // Frames are separated by a blank line. Process every complete frame,
        // leaving any partial frame in the buffer for the next chunk.
        let sep: number;
        while ((sep = buffer.indexOf("\n\n")) !== -1) {
          const frame = buffer.slice(0, sep);
          buffer = buffer.slice(sep + 2);
          const data = dataOf(frame);
          if (data === null) continue; // heartbeat / comment
          let ev: ApiEvent;
          try {
            ev = JSON.parse(data);
          } catch {
            continue; // ignore a malformed frame rather than tearing down
          }
          h.onEvent(ev);
          if (ev.type === "run" && ev.status && TERMINAL.has(ev.status)) {
            terminal = true;
          }
        }
      }
    } catch (err) {
      if (!ac.signal.aborted) h.onError(err);
      return;
    } finally {
      // Release the reader on every exit — natural close, error, or abort — so
      // a run stream never leaves a locked reader behind. abort() already tears
      // down the body; this is hygiene, hence the swallow if it is already gone.
      try {
        reader.releaseLock();
      } catch {
        /* already released, or a read was still pending after abort */
      }
    }

    if (ac.signal.aborted) return;
    if (terminal) h.onClose();
    else h.onError(new Error("event stream ended without a terminal event"));
  })();

  let cancelled = false;
  return () => {
    if (cancelled) return;
    cancelled = true;
    ac.abort();
  };
}

// dataOf extracts the JSON payload of an SSE frame, or null for a comment
// (heartbeat) frame. A frame may in principle carry multiple `data:` lines;
// concatenate them per the SSE spec.
function dataOf(frame: string): string | null {
  const lines = frame.split("\n");
  const parts: string[] = [];
  for (const line of lines) {
    if (line.startsWith(":")) continue; // comment / heartbeat
    if (line.startsWith("data:")) parts.push(line.slice(5).replace(/^ /, ""));
  }
  return parts.length ? parts.join("\n") : null;
}
