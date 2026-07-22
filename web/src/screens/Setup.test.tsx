import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Setup } from "./Setup";
import { renderWithProviders } from "../test/harness";
import * as client from "../api/client";

function stubApi(map: Record<string, unknown>) {
  vi.spyOn(client, "apiFetch").mockImplementation(async (path: string) => {
    for (const key of Object.keys(map)) if (path.startsWith(key)) return map[key] as never;
    throw new Error("unstubbed " + path);
  });
}

describe("Setup secret gating", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("blocks Install until every declared secret is filled", async () => {
    stubApi({
      "/api/v1/system": { arch: "amd64", hostname: "h", os: "linux", distro: "ubuntu", version_id: "24.04", root: true },
      "/api/v1/settings": { configured: true, repo_url: "x", ref: "main", has_repo_token: false },
      "/api/v1/config": { name: "cfg", sha: "s", stale: false, categories: [], profiles: [{ id: "production", name: "Production" }] },
      "/api/v1/plan": { profile: "production", arch: "amd64", config_sha: "s", stale: false, steps: [{ ref: "n/cf", name: "cf", type: "deb" }], secrets: ["CF_TUNNEL_TOKEN"] },
    });
    renderWithProviders(<Setup />);

    await userEvent.selectOptions(await screen.findByLabelText(/profile/i), "production");
    await userEvent.click(await screen.findByRole("button", { name: /review|plan/i }));

    const install = await screen.findByRole("button", { name: /install/i });
    expect(install).toBeDisabled();

    // A whitespace-only value must not count as filled — Install stays disabled.
    await userEvent.type(screen.getByLabelText("CF_TUNNEL_TOKEN"), "   ");
    expect(install).toBeDisabled();

    await userEvent.clear(screen.getByLabelText("CF_TUNNEL_TOKEN"));
    await userEvent.type(screen.getByLabelText("CF_TUNNEL_TOKEN"), "abc");
    expect(install).toBeEnabled();
  });
});
