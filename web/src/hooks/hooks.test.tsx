import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { makeQueryClient } from "../api/queryClient";
import { ApiTokenProvider } from "./useApi";
import { useConfig } from "./index";
import * as client from "../api/client";
import { ApiError } from "../api/client";
import * as auth from "../auth/useToken";

function wrapper(token: string) {
  const qc = makeQueryClient();
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={qc}>
      <ApiTokenProvider token={token}>{children}</ApiTokenProvider>
    </QueryClientProvider>
  );
}

describe("useConfig", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("fetches the config with the context token", async () => {
    const spy = vi.spyOn(client, "apiFetch").mockResolvedValue({ name: "cfg", categories: [], profiles: [] } as never);
    const { result } = renderHook(() => useConfig(), { wrapper: wrapper("tok") });

    await waitFor(() => expect(result.current.data?.name).toBe("cfg"));
    expect(spy).toHaveBeenCalledWith("/api/v1/config", { token: "tok" });
  });

  it("signals authFailed on a 401", async () => {
    const failed = vi.spyOn(auth, "authFailed");
    vi.spyOn(client, "apiFetch").mockRejectedValue(new ApiError(401, "invalid or missing API token"));

    const { result } = renderHook(() => useConfig(), { wrapper: wrapper("bad") });
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(failed).toHaveBeenCalled();
  });
});
