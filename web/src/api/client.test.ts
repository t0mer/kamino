import { describe, it, expect, vi, beforeEach } from "vitest";
import { apiFetch, ApiError } from "./client";

function mockFetch(status: number, body: unknown) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(JSON.stringify(body)),
  });
}

describe("apiFetch", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("attaches the token as X-API-Token", async () => {
    const fetchMock = mockFetch(200, { arch: "amd64" });
    vi.stubGlobal("fetch", fetchMock);

    await apiFetch("/api/v1/system", { token: "s3cret" });

    const [, init] = fetchMock.mock.calls[0];
    expect(init.headers["X-API-Token"]).toBe("s3cret");
  });

  it("returns the parsed body on success", async () => {
    vi.stubGlobal("fetch", mockFetch(200, { arch: "amd64" }));
    const got = await apiFetch<{ arch: string }>("/api/v1/system", { token: "t" });
    expect(got.arch).toBe("amd64");
  });

  it("throws ApiError carrying status and details on failure", async () => {
    vi.stubGlobal("fetch", mockFetch(422, { error: "config repo is invalid", details: ["dev/go: bad"] }));

    await expect(apiFetch("/api/v1/config", { token: "t" })).rejects.toMatchObject({
      status: 422,
      message: "config repo is invalid",
      details: ["dev/go: bad"],
    });
  });

  it("wraps a non-JSON error body without throwing a parse error", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.reject(new Error("not json")),
      text: () => Promise.resolve("internal error"),
    });
    vi.stubGlobal("fetch", fetchMock);

    const err = (await apiFetch("/api/v1/system", { token: "t" }).catch((e: unknown) => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(500);
  });

  it("sends a JSON body and content-type for a mutation", async () => {
    const fetchMock = mockFetch(200, {});
    vi.stubGlobal("fetch", fetchMock);

    await apiFetch("/api/v1/settings", { method: "PUT", body: { repo_url: "x" }, token: "t" });

    const [, init] = fetchMock.mock.calls[0];
    expect(init.method).toBe("PUT");
    expect(init.headers["Content-Type"]).toBe("application/json");
    expect(JSON.parse(init.body)).toEqual({ repo_url: "x" });
  });
});
