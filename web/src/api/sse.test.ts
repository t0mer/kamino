import { describe, it, expect, vi, beforeEach } from "vitest";
import { streamRun } from "./sse";
import * as auth from "../auth/useToken";
import type { ApiEvent } from "./types";

// makeStream turns an array of string chunks into a fetch Response whose body
// is a ReadableStream, so we control exactly how frames are split.
function fetchWithChunks(chunks: string[]) {
  return vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    body: new ReadableStream({
      start(controller) {
        const enc = new TextEncoder();
        for (const c of chunks) controller.enqueue(enc.encode(c));
        controller.close();
      },
    }),
  });
}

function collect() {
  const events: ApiEvent[] = [];
  let closed = false;
  let error: unknown = null;
  return {
    events,
    handlers: {
      onEvent: (e: ApiEvent) => events.push(e),
      onClose: () => (closed = true),
      onError: (e: unknown) => (error = e),
    },
    get closed() { return closed; },
    get error() { return error; },
  };
}

const frame = (o: object) => `data: ${JSON.stringify(o)}\n\n`;

describe("streamRun", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("parses one frame into an event", async () => {
    vi.stubGlobal("fetch", fetchWithChunks([frame({ type: "log", run_id: "r", line: "hi", ts: "t" })]));
    const c = collect();

    streamRun("r", "tok", c.handlers);
    await vi.waitFor(() => expect(c.events).toHaveLength(1));

    expect(c.events[0].line).toBe("hi");
  });

  it("attaches the token as a header", async () => {
    const f = fetchWithChunks([]);
    vi.stubGlobal("fetch", f);

    streamRun("r", "tok", collect().handlers);
    await vi.waitFor(() => expect(f).toHaveBeenCalled());

    const [, init] = f.mock.calls[0];
    expect(init.headers["X-API-Token"]).toBe("tok");
  });

  it("reassembles a frame split across two chunks", async () => {
    const whole = frame({ type: "log", run_id: "r", line: "split", ts: "t" });
    const mid = Math.floor(whole.length / 2);
    vi.stubGlobal("fetch", fetchWithChunks([whole.slice(0, mid), whole.slice(mid)]));
    const c = collect();

    streamRun("r", "tok", c.handlers);
    await vi.waitFor(() => expect(c.events).toHaveLength(1));

    expect(c.events[0].line).toBe("split");
  });

  it("ignores heartbeat comment lines", async () => {
    vi.stubGlobal("fetch", fetchWithChunks([
      ": heartbeat\n\n",
      frame({ type: "log", run_id: "r", line: "real", ts: "t" }),
    ]));
    const c = collect();

    streamRun("r", "tok", c.handlers);
    await vi.waitFor(() => expect(c.events).toHaveLength(1));

    expect(c.events[0].line).toBe("real");
  });

  it("calls onClose on a terminal run event, not on a plain drop", async () => {
    vi.stubGlobal("fetch", fetchWithChunks([
      frame({ type: "run", run_id: "r", status: "success", ts: "t" }),
    ]));
    const c = collect();

    streamRun("r", "tok", c.handlers);
    await vi.waitFor(() => expect(c.closed).toBe(true));

    expect(c.error).toBeNull();
  });

  it("calls onError when the stream ends without a terminal event", async () => {
    vi.stubGlobal("fetch", fetchWithChunks([
      frame({ type: "log", run_id: "r", line: "then silence", ts: "t" }),
    ]));
    const c = collect();

    streamRun("r", "tok", c.handlers);
    await vi.waitFor(() => expect(c.error).not.toBeNull());

    expect(c.closed).toBe(false);
  });

  it("calls onError on a non-200 response", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 404, body: null }));
    const c = collect();

    streamRun("r", "tok", c.handlers);
    await vi.waitFor(() => expect(c.error).not.toBeNull());
  });

  it("re-gates via authFailed on a 401 and does not call onError", async () => {
    const authFailedSpy = vi.spyOn(auth, "authFailed").mockImplementation(() => {});
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 401, body: null }));
    const c = collect();

    streamRun("r", "tok", c.handlers);
    await vi.waitFor(() => expect(authFailedSpy).toHaveBeenCalledTimes(1));

    expect(c.error).toBeNull();
    expect(c.closed).toBe(false);
  });

  it("preserves a masked secret verbatim", async () => {
    vi.stubGlobal("fetch", fetchWithChunks([
      frame({ type: "log", run_id: "r", line: "token is ***", ts: "t" }),
    ]));
    const c = collect();

    streamRun("r", "tok", c.handlers);
    await vi.waitFor(() => expect(c.events).toHaveLength(1));

    expect(c.events[0].line).toBe("token is ***");
  });
});
