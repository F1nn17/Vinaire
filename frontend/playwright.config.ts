import { defineConfig, devices } from "@playwright/test";

// Roadmap Phase 23 — basic e2e smoke test against a real backend/cluster
// (see backend/integration/ for the real-cluster integration test suite
// this pairs with, and .github/workflows/ci.yml for how both get wired
// up). Never run against mocked data: E2E_BASE_URL should point at a
// `next dev` (or the static export) instance whose backend is pointed at
// a real, disposable k3d cluster with AUTH_MODE=token.
export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:3100",
    trace: "on-first-retry",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
