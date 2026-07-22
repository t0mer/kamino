// ApiError is the single error type every request rejects with. It carries the
// HTTP status (which drives UI behaviour: 401 → re-gate, 409 → active-run
// toast, 422 → inline) and the server's `details` list, which for a config
// validation failure names the exact file and field that is wrong.
export class ApiError extends Error {
  readonly status: number;
  readonly details: string[];
  constructor(status: number, message: string, details: string[] = []) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.details = details;
  }
}

export interface ApiOptions {
  method?: string;
  body?: unknown;
  token: string;
  signal?: AbortSignal;
}

// isRecord narrows an unknown value to a plain object so its fields can be
// read without resorting to `any`.
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

// apiFetch performs one request against the Kamino API, attaching the token as
// X-API-Token and normalising both success and failure. It is deliberately
// pure — the token is passed in, not read from storage — so it is trivially
// testable and has no hidden global state.
export async function apiFetch<T>(path: string, opts: ApiOptions): Promise<T> {
  const headers: Record<string, string> = { "X-API-Token": opts.token };
  const init: RequestInit = { method: opts.method ?? "GET", headers, signal: opts.signal };

  if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(opts.body);
  }

  const res = await fetch(path, init);

  if (!res.ok) {
    let message = `request failed with status ${res.status}`;
    let details: string[] = [];
    try {
      const parsed: unknown = await res.json();
      if (isRecord(parsed) && typeof parsed.error === "string") {
        message = parsed.error;
      }
      if (isRecord(parsed) && Array.isArray(parsed.details)) {
        details = parsed.details.filter((d): d is string => typeof d === "string");
      }
    } catch {
      // A non-JSON error body (a proxy 502, say). Keep the generic message
      // rather than letting the parse failure mask the real status.
    }
    throw new ApiError(res.status, message, details);
  }

  // 202 (start run) and 204 have a body or not; guard the empty case.
  const text = await res.text();
  return (text ? (JSON.parse(text) as T) : ({} as T));
}
