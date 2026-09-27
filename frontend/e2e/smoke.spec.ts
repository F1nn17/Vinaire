import { test, expect } from "@playwright/test";

// Roadmap Phase 23 — token login, then a walk through the sidebar's main
// sections confirming each one actually renders real cluster data (not an
// error state, not a permanently-empty loading spinner). Requires
// E2E_TOKEN: a real Kubernetes ServiceAccount token for a cluster the
// backend under test (AUTH_MODE=token) is actually pointed at — see
// .github/workflows/ci.yml for how CI provisions one against a throwaway
// k3d cluster. Skips itself entirely when unset, so a plain
// `npx playwright test` locally with no token configured doesn't fail for
// an unrelated reason.
const TOKEN = process.env.E2E_TOKEN ?? "";

test.describe("Vinaire smoke test", () => {
  test.skip(!TOKEN, "E2E_TOKEN not set — requires a real cluster + token");

  test("logs in and navigates through the sidebar's main sections", async ({
    page,
  }) => {
    await page.goto("/login");
    await page.getByLabel("Kubernetes ServiceAccount token").fill(TOKEN);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page).toHaveURL("/");

    // Overview — real stat cards, not an error/loading state.
    await expect(page.getByText("HEALTHY PODS")).toBeVisible();
    await expect(page.getByText("Failed to load dashboard")).toHaveCount(0);

    // Topology
    await page.getByRole("link", { name: "Topology" }).click();
    await expect(page).toHaveURL(/\/topology/);
    await expect(page.getByText("Service graph")).toBeVisible();

    // Nodes
    await page.getByRole("link", { name: "Nodes" }).click();
    await expect(page).toHaveURL(/\/nodes/);
    await expect(page.getByText("Failed to load nodes")).toHaveCount(0);

    // Workloads (Deployments & pods)
    await page.getByRole("link", { name: "Deployments & pods" }).click();
    await expect(page).toHaveURL(/\/workloads/);
    await expect(page.getByRole("button", { name: "Pods" })).toBeVisible();

    // ConfigMaps
    await page.getByRole("link", { name: "ConfigMaps" }).click();
    await expect(page).toHaveURL(/\/configmaps/);

    // Secrets
    await page.getByRole("link", { name: "Secrets" }).click();
    await expect(page).toHaveURL(/\/secrets/);

    // Storage
    await page.getByRole("link", { name: "PVCs & volumes" }).click();
    await expect(page).toHaveURL(/\/storage/);

    // Network
    await page.getByRole("link", { name: "Services & Ingress" }).click();
    await expect(page).toHaveURL(/\/network/);

    // Access control
    await page.getByRole("link", { name: "RBAC" }).click();
    await expect(page).toHaveURL(/\/access-control/);

    // Settings
    await page.getByRole("link", { name: "Settings" }).click();
    await expect(page).toHaveURL(/\/settings/);
    await expect(page.getByText("Add a cluster")).toBeVisible();
  });
});
