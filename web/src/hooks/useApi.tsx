import { createContext, useContext } from "react";

const ApiTokenContext = createContext<string | null>(null);

// ApiTokenProvider supplies the current API token to every hook, so a component
// calls useConfig() without passing the token by hand.
export function ApiTokenProvider({ token, children }: { token: string; children: React.ReactNode }) {
  return <ApiTokenContext.Provider value={token}>{children}</ApiTokenContext.Provider>;
}

export function useApiToken(): string {
  const t = useContext(ApiTokenContext);
  if (t === null) throw new Error("useApiToken used outside an ApiTokenProvider");
  return t;
}
