import { QueryClient } from "@tanstack/react-query";
import { ApiError } from "./client";
import { authFailed } from "../auth/useToken";

// makeQueryClient builds a QueryClient whose global error handling routes a 401
// to the auth gate. Centralising it here means no individual hook or screen has
// to remember the "401 → re-gate" rule.
export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: (count, err) => {
          if (err instanceof ApiError && err.status === 401) return false;
          return count < 1;
        },
      },
    },
  });
}

// handleError inspects an error thrown by any query or mutation and fires the
// auth signal on a 401. Hooks call this in their onError.
export function handleError(err: unknown) {
  if (err instanceof ApiError && err.status === 401) authFailed();
}

export const queryClient = makeQueryClient();
