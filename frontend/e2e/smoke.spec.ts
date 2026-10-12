import { test, expect, type Page } from "@playwright/test";

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

// Click a sidebar link and wait for the page. A click that lands while the previous page is still
// hydrating can be swallowed, so the click is repeated until the URL changes (clicking the link of the
// page you are on does nothing).
async function open(page: Page, name: string, url: RegExp) {
  await expect(async () => {
    await page.getByRole("link", { name, exact: true }).click();
    await expect(page).toHaveURL(url, { timeout: 4000 });
  }).toPass({ timeout: 30_000 });
}

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
    await open(page, "Topology", /\/topology/);
    await expect(page.getByText("Service graph")).toBeVisible();

    // Nodes
    await open(page, "Nodes", /\/nodes/);
    await expect(page.getByText("Failed to load nodes")).toHaveCount(0);

    // Workloads (Deployments & pods)
    await open(page, "Deployments & pods", /\/workloads/);
    await expect(page.getByRole("button", { name: "Pods" })).toBeVisible();

    // ConfigMaps
    await open(page, "ConfigMaps", /\/configmaps/);

    // Secrets
    await open(page, "Secrets", /\/secrets/);

    // Storage
    await open(page, "PVCs & volumes", /\/storage/);

    // Network
    await open(page, "Routing", /\/network/);

    // Access control
    await open(page, "RBAC", /\/access-control/);

    // Settings
    await open(page, "Settings", /\/settings/);
    await expect(page.getByText("Add a cluster")).toBeVisible();
  });
});
