import { useCallback, useEffect, useState } from "react";
import { apiFetch, ApiError } from "../api/client";
import type { SystemInfo } from "../api/types";
import { onAuthFailed, useToken } from "./useToken";

type Status = "checking" | "authed" | "needsToken" | "rejected" | "unreachable";

// TokenGate stands in front of the whole app. This API installs software as
// root; nothing renders until a valid token is held. The token is printed once
// in the `kamino serve` output, and the operator pastes it here.
export function TokenGate({ children }: { children: React.ReactNode }) {
  const { token, save, clear } = useToken();
  const [status, setStatus] = useState<Status>(token ? "checking" : "needsToken");
  const [input, setInput] = useState("");

  const probe = useCallback(async (t: string) => {
    try {
      await apiFetch<SystemInfo>("/api/v1/system", { token: t });
      save(t);
      setStatus("authed");
    } catch (err) {
      clear();
      // Distinguish "the server rejected this token" (a real 401) from "the
      // server could not be reached at all" (a network error, or a proxy
      // returning a 5xx). Showing "token rejected" for an unreachable server
      // sends the operator chasing a credential problem that isn't there.
      const rejected = err instanceof ApiError && err.status === 401;
      setStatus(rejected ? "rejected" : "unreachable");
    }
  }, [save, clear]);

  // Probe a stored token on mount.
  useEffect(() => {
    if (token) probe(token);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Any request elsewhere that hits a 401 flips us back to the gate.
  useEffect(() => onAuthFailed(() => setStatus("needsToken")), []);

  if (status === "authed") return <>{children}</>;

  return (
    <div className="min-h-screen flex items-center justify-center p-6">
      <form
        className="w-full max-w-md space-y-4 rounded-lg border p-6"
        onSubmit={(e) => {
          e.preventDefault();
          if (input) probe(input);
        }}
      >
        <h1 className="text-xl font-semibold">Kamino</h1>
        <p className="text-sm text-muted-foreground">
          Paste the API token printed when you ran <code>kamino serve</code>.
        </p>
        <label className="block text-sm font-medium" htmlFor="token">API token</label>
        <input
          id="token"
          type="password"
          className="w-full rounded border px-3 py-2"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          autoFocus
        />
        {status === "rejected" && (
          <p className="text-sm text-red-600">That token was rejected. Check the serve output and try again.</p>
        )}
        {status === "unreachable" && (
          <p className="text-sm text-red-600">Could not reach the server. Is <code>kamino serve</code> still running?</p>
        )}
        <button type="submit" className="w-full rounded bg-primary px-3 py-2 text-primary-foreground">
          Connect
        </button>
      </form>
    </div>
  );
}
