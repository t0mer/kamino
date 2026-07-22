import type { ReactElement } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { render } from "@testing-library/react";
import { makeQueryClient } from "../api/queryClient";
import { ApiTokenProvider } from "../hooks/useApi";

// renderWithProviders wraps a screen in the same provider stack the real app
// uses (query client, API token, router) so screen tests exercise hooks and
// navigation without hand-rolling the stack per test file. Shared by every
// screen test from Task 9 onward.
export function renderWithProviders(ui: ReactElement, initialEntries: string[] = ["/"]) {
  const qc = makeQueryClient();
  return render(
    <QueryClientProvider client={qc}>
      <ApiTokenProvider token="test-token">
        <MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>
      </ApiTokenProvider>
    </QueryClientProvider>
  );
}
