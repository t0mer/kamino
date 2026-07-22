import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
  },
  server: {
    // During `npm run dev`, proxy the API to a locally running `kamino serve`
    // so the dev UI talks to a real backend.
    proxy: {
      "/api": "http://127.0.0.1:8844",
      "/healthz": "http://127.0.0.1:8844",
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
  },
} as any);
