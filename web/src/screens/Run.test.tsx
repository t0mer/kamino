import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, act } from "@testing-library/react";
import { Routes, Route } from "react-router-dom";
import { Run } from "./Run";
import { renderWithProviders } from "../test/harness";
import * as client from "../api/client";
import * as sse from "../api/sse";
import type { StreamHandlers } from "../api/sse";

function stubApi(map: Record<string, unknown>) {
  vi.spyOn(client, "apiFetch").mockImplementation(async (path: string) => {
    for (const key of Object.keys(map)) if (path.startsWith(key)) return map[key] as never;
    throw new Error("unstubbed " + path);
  });
}

// captureStream mirrors the useRunStream test's harness so this screen test
// can drive SSE events by hand without a real connection.
function captureStream() {
  let handlers: StreamHandlers | null = null;
  const cancel = vi.fn();
  vi.spyOn(sse, "streamRun").mockImplementation((_id, _tok, h) => {
    handlers = h;
    return cancel;
  });
  return {
    emit: (e: object) => act(() => handlers!.onEvent(e as never)),
  };
}

describe("Run screen", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("shows the Installing… label for the running step and renders a *** log line verbatim", async () => {
    stubApi({
      "/api/v1/runs/r1": {
        id: "r1",
        profile: "production",
        config_sha: "sha1",
        status: "running",
        started_at: "t0",
        steps: [
          { id: "s1", item_ref: "tools/jq", name: "jq", status: "running", exit_code: 0, started_at: "t0" },
        ],
      },
    });
    const stream = captureStream();

    renderWithProviders(
      <Routes>
        <Route path="/run/:id" element={<Run />} />
      </Routes>,
      ["/run/r1"]
    );

    expect(await screen.findByText(/installing jq…/i)).toBeInTheDocument();

    stream.emit({ type: "log", run_id: "r1", step_id: "tools/jq", line: "token ***", ts: "t" });

    expect(await screen.findByText("token ***")).toBeInTheDocument();
  });
});
