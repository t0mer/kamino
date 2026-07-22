import { createElement } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { makeQueryClient } from "../api/queryClient";
import { ApiTokenProvider } from "./useApi";
import { useRunStream } from "./useRunStream";
import * as sse from "../api/sse";
import type { StreamHandlers } from "../api/sse";

// captureStream lets the test drive events into the hook by hand. It tracks
// every connection's handlers (in order) so a test can target a specific
// connection, e.g. to replay a log line on the reconnect after a drop.
function captureStream() {
  let handlers: StreamHandlers | null = null;
  const allHandlers: StreamHandlers[] = [];
  const cancel = vi.fn();
  vi.spyOn(sse, "streamRun").mockImplementation((_id, _tok, h) => {
    handlers = h;
    allHandlers.push(h);
    return cancel;
  });
  return {
    emit: (e: object) => act(() => handlers!.onEvent(e as never)),
    drop: () => act(() => handlers!.onError(new Error("drop"))),
    close: () => act(() => handlers!.onClose()),
    // emitOn drives an event into a specific connection by index (0 = first
    // connect, 1 = first reconnect, ...), regardless of which is "current".
    emitOn: (i: number, e: object) => act(() => allHandlers[i].onEvent(e as never)),
    cancel,
  };
}

// This file is plain .ts (no JSX), so the provider stack is built with
// createElement instead of JSX syntax.
function wrapper(token = "test-token") {
  const qc = makeQueryClient();
  return ({ children }: { children: React.ReactNode }) =>
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(ApiTokenProvider, { token, children })
    );
}

describe("useRunStream", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("folds log events into a growing line list, verbatim", async () => {
    const s = captureStream();
    const { result } = renderHook(() => useRunStream("r"), { wrapper: wrapper() });

    s.emit({ type: "log", run_id: "r", step_id: "tools/jq", line: "installing", ts: "t" });
    s.emit({ type: "log", run_id: "r", step_id: "tools/jq", line: "token ***", ts: "t" });

    await waitFor(() => expect(result.current.logs).toHaveLength(2));
    expect(result.current.logs[1].line).toBe("token ***");
  });

  it("reconnects on a non-terminal drop", async () => {
    const s = captureStream();
    renderHook(() => useRunStream("r"), { wrapper: wrapper() });

    s.drop();

    // The hook's reconnect delay is 1000ms, the same as waitFor's default
    // timeout — give it headroom so this isn't a race against that default.
    await waitFor(() => expect(sse.streamRun).toHaveBeenCalledTimes(2), { timeout: 2000 });
  });

  it("discards prior logs on reconnect so a replayed line is not duplicated", async () => {
    const s = captureStream();
    const { result } = renderHook(() => useRunStream("r"), { wrapper: wrapper() });

    const line = { type: "log", run_id: "r", step_id: "tools/jq", line: "installing", ts: "t" };
    s.emitOn(0, line);
    await waitFor(() => expect(result.current.logs).toHaveLength(1));

    s.drop();
    await waitFor(() => expect(sse.streamRun).toHaveBeenCalledTimes(2), { timeout: 2000 });

    // The server replays the full history on the new connection, so the
    // same log line arrives again.
    s.emitOn(1, line);
    await waitFor(() => expect(result.current.logs).toHaveLength(1));
    expect(result.current.logs[0].line).toBe("installing");
  });

  it("stops on a terminal close and does not reconnect", async () => {
    const s = captureStream();
    renderHook(() => useRunStream("r"), { wrapper: wrapper() });

    s.close();
    await new Promise((r) => setTimeout(r, 50));

    expect(sse.streamRun).toHaveBeenCalledTimes(1);
  });
});
