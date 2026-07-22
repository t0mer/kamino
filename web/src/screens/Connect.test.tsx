import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Connect } from "./Connect";
import { renderWithProviders } from "../test/harness";
import * as client from "../api/client";
import { ApiError } from "../api/client";

describe("Connect", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("shows what Test connection found", async () => {
    vi.spyOn(client, "apiFetch").mockResolvedValue(
      { ok: true, name: "homelab", categories: 4, profiles: 3, sha: "abc123" } as never
    );
    renderWithProviders(<Connect />);

    await userEvent.type(screen.getByLabelText(/repo url/i), "https://github.com/t0mer/cfg");
    await userEvent.click(screen.getByRole("button", { name: /test connection/i }));

    expect(await screen.findByText(/homelab/)).toBeInTheDocument();
    expect(screen.getByText(/4/)).toBeInTheDocument();
    expect(screen.getByText(/abc123/)).toBeInTheDocument();
  });

  it("renders a validation error's details inline", async () => {
    vi.spyOn(client, "apiFetch").mockRejectedValue(
      new ApiError(422, "config repo is invalid", ["categories/dev.yaml: item \"go\": source missing for arch arm64"])
    );
    renderWithProviders(<Connect />);

    await userEvent.type(screen.getByLabelText(/repo url/i), "https://github.com/t0mer/bad");
    await userEvent.click(screen.getByRole("button", { name: /test connection/i }));

    expect(await screen.findByText(/source missing for arch arm64/)).toBeInTheDocument();
  });
});
