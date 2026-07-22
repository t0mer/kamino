import { describe, it, expect, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useToken } from "./useToken";

describe("useToken", () => {
  beforeEach(() => localStorage.clear());

  it("starts null when nothing is stored", () => {
    const { result } = renderHook(() => useToken());
    expect(result.current.token).toBeNull();
  });

  it("persists a saved token across a remount", () => {
    const { result } = renderHook(() => useToken());
    act(() => result.current.save("s3cret"));
    expect(result.current.token).toBe("s3cret");

    const second = renderHook(() => useToken());
    expect(second.result.current.token).toBe("s3cret");
  });

  it("clears a stored token", () => {
    const { result } = renderHook(() => useToken());
    act(() => result.current.save("s3cret"));
    act(() => result.current.clear());
    expect(result.current.token).toBeNull();
    expect(localStorage.getItem("kamino_api_token")).toBeNull();
  });
});
