/// <reference types="vitest" />
import { defineConfig } from "vitest/config";
import path from "path";

// v0.6 carbon — minimal vitest config. Pure-logic tests only for now;
// component tests deferred to v0.7 when we add @testing-library/react +
// a Redux store render utility.
export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    environment: "happy-dom",
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
    globals: false,
  },
});
