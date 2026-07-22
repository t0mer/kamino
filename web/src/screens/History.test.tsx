import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import { History } from "./History";
import { renderWithProviders } from "../test/harness";
import * as client from "../api/client";

describe("History", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("lists past runs with their status", async () => {
    vi.spyOn(client, "apiFetch").mockResolvedValue([
      { id: "r1", profile: "dev", config_sha: "abc", status: "success", started_at: "2026-07-22T10:00:00Z", finished_at: "2026-07-22T10:00:12Z" },
      { id: "r2", profile: "production", config_sha: "def", status: "failed", started_at: "2026-07-22T09:00:00Z", finished_at: "2026-07-22T09:01:00Z" },
    ] as never);
    renderWithProviders(<History />);

    expect(await screen.findByText("dev")).toBeInTheDocument();
    expect(screen.getByText("production")).toBeInTheDocument();
    expect(screen.getByText(/success/i)).toBeInTheDocument();
    expect(screen.getByText(/failed/i)).toBeInTheDocument();
  });

  it("shows an empty state when there are no runs", async () => {
    vi.spyOn(client, "apiFetch").mockResolvedValue([] as never);
    renderWithProviders(<History />);

    expect(await screen.findByText(/no runs yet/i)).toBeInTheDocument();
  });
});
