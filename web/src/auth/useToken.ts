import { useCallback, useState } from "react";

const KEY = "kamino_api_token";

// The API token is a session credential for reaching this server, not a
// config-repo secret. It is stored in localStorage so a reload does not force a
// re-paste; it is sent only as the X-API-Token header, never in a URL.
export function useToken() {
  const [token, setToken] = useState<string | null>(() => localStorage.getItem(KEY));

  const save = useCallback((t: string) => {
    localStorage.setItem(KEY, t);
    setToken(t);
  }, []);

  const clear = useCallback(() => {
    localStorage.removeItem(KEY);
    setToken(null);
  }, []);

  return { token, save, clear };
}

// A tiny module-level signal so any request that sees a 401 can tell the gate
// the token went stale, without threading a callback through every screen.
type Cb = () => void;
const subscribers = new Set<Cb>();

export function authFailed() {
  for (const cb of subscribers) cb();
}

export function onAuthFailed(cb: Cb): () => void {
  subscribers.add(cb);
  return () => subscribers.delete(cb);
}
