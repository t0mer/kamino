import { render, screen } from "@testing-library/react";
import { describe, it, expect, beforeEach } from "vitest";
import App from "./App";

describe("App", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("shows the token gate when no token is stored", async () => {
    render(<App />);
    expect(await screen.findByLabelText(/api token/i)).toBeInTheDocument();
  });
});
