import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TokenGate } from "./TokenGate";
import * as client from "../api/client";
import { ApiError } from "../api/client";
import { authFailed } from "./useToken";

describe("TokenGate", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("shows the paste form when no token is stored", async () => {
    render(<TokenGate><div>secret app</div></TokenGate>);
    expect(await screen.findByLabelText(/api token/i)).toBeInTheDocument();
    expect(screen.queryByText("secret app")).not.toBeInTheDocument();
  });

  it("renders children when a stored token authenticates", async () => {
    localStorage.setItem("kamino_api_token", "good");
    vi.spyOn(client, "apiFetch").mockResolvedValue({ arch: "amd64" } as never);

    render(<TokenGate><div>secret app</div></TokenGate>);

    expect(await screen.findByText("secret app")).toBeInTheDocument();
  });

  it("accepts a pasted token that authenticates", async () => {
    vi.spyOn(client, "apiFetch").mockResolvedValue({ arch: "amd64" } as never);
    render(<TokenGate><div>secret app</div></TokenGate>);

    await userEvent.type(await screen.findByLabelText(/api token/i), "good");
    await userEvent.click(screen.getByRole("button", { name: /connect/i }));

    expect(await screen.findByText("secret app")).toBeInTheDocument();
    expect(localStorage.getItem("kamino_api_token")).toBe("good");
  });

  it("rejects a pasted token that fails", async () => {
    vi.spyOn(client, "apiFetch").mockRejectedValue(new ApiError(401, "invalid or missing API token"));
    render(<TokenGate><div>secret app</div></TokenGate>);

    await userEvent.type(await screen.findByLabelText(/api token/i), "bad");
    await userEvent.click(screen.getByRole("button", { name: /connect/i }));

    expect(await screen.findByText(/rejected/i)).toBeInTheDocument();
    expect(screen.queryByText("secret app")).not.toBeInTheDocument();
    expect(localStorage.getItem("kamino_api_token")).toBeNull();
  });

  it("re-gates when authFailed fires after being authenticated", async () => {
    localStorage.setItem("kamino_api_token", "good");
    vi.spyOn(client, "apiFetch").mockResolvedValue({ arch: "amd64" } as never);
    render(<TokenGate><div>secret app</div></TokenGate>);
    await screen.findByText("secret app");

    authFailed();

    expect(await screen.findByLabelText(/api token/i)).toBeInTheDocument();
    expect(screen.queryByText("secret app")).not.toBeInTheDocument();
  });
});

  it("distinguishes an unreachable server from a rejected token", async () => {
    // A network failure (not an ApiError 401) must not be reported as a bad
    // token — that sends the operator chasing the wrong problem.
    vi.spyOn(client, "apiFetch").mockRejectedValue(new Error("Failed to fetch"));
    render(<TokenGate><div>secret app</div></TokenGate>);

    await userEvent.type(await screen.findByLabelText(/api token/i), "whatever");
    await userEvent.click(screen.getByRole("button", { name: /connect/i }));

    expect(await screen.findByText(/could not reach the server/i)).toBeInTheDocument();
    expect(screen.queryByText(/rejected/i)).not.toBeInTheDocument();
  });
