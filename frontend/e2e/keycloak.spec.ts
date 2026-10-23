import { test, expect, type Browser, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";

// Roadmap Phase 83 — the whole Keycloak sign-in, in a real browser, against a real Keycloak and a real
// cluster (see .github/workflows/ci.yml, job "keycloak-e2e", for how CI stands them up):
//
//   an administrator pastes a token -> sets Keycloak up from Settings -> Sign-in -> a person signs in
//   through Keycloak (a temporary password has to be changed on the way) -> lands back on THIS Vinaire
//   (not the development address) -> the cluster knows them as a ServiceAccount of their own with no rights
//   -> an administrator binds a role -> they can read, but not delete -> signing in again finds the same
//   account and the binding still works.
//
// It is the test that would have caught the redirect to localhost:3100 and an http Keycloak offered
// API-server flags Kubernetes refuses. Skips itself without the environment below, so a plain
// `npx playwright test` (and the smoke job) is unaffected.
//
//   E2E_VINAIRE_URL    where Vinaire is reached (its origin is what Keycloak is told to send people back to)
//   E2E_KEYCLOAK_URL   a Keycloak on a PRIVATE address that both this browser and the Vinaire pod can open
//   E2E_ADMIN_TOKEN    a ServiceAccount token that may do everything (to sign in as the administrator)
//   E2E_KC_ADMIN_USER / E2E_KC_ADMIN_PASSWORD   Keycloak's bootstrap administrator
//   kubectl            on the PATH, pointed at the same cluster
const BASE = process.env.E2E_VINAIRE_URL ?? "";
const KEYCLOAK = process.env.E2E_KEYCLOAK_URL ?? "";
const ADMIN_TOKEN = process.env.E2E_ADMIN_TOKEN ?? "";
const KC_ADMIN = process.env.E2E_KC_ADMIN_USER ?? "admin";
const KC_ADMIN_PASSWORD = process.env.E2E_KC_ADMIN_PASSWORD ?? "";

const PERSON = "e2e-shiraku";
const TEMPORARY_PASSWORD = "Temporary-pw-1";
const REAL_PASSWORD = "Chosen-by-the-person-2";

test.skip(
  !BASE || !KEYCLOAK || !ADMIN_TOKEN || !KC_ADMIN_PASSWORD,
  "E2E_VINAIRE_URL, E2E_KEYCLOAK_URL, E2E_ADMIN_TOKEN and E2E_KC_ADMIN_PASSWORD are needed",
);
test.use({ baseURL: BASE });

// A call to Vinaire's API made by the page itself (the browser's cookies and origin, like the UI's own).
async function api(
  page: Page,
  method: string,
  path: string,
): Promise<{ status: number; body: unknown }> {
  return page.evaluate(
    async ([m, p]) => {
      const res = await fetch(p, { method: m, credentials: "include" });
      let body: unknown = null;
      try {
        body = await res.json();
      } catch {
        /* not JSON */
      }
      return { status: res.status, body };
    },
    [method, path],
  );
}

function kubectl(...args: string[]): string {
  return execFileSync("kubectl", args, { encoding: "utf8" });
}

// Walks Keycloak's pages until the browser is back on Vinaire: the sign-in form, then whatever it asks
// for on the way (changing the temporary password, completing the profile).
async function keycloakSignIn(page: Page, user: string, password: string) {
  await page.waitForURL((u) => u.href.startsWith(KEYCLOAK), {
    timeout: 30_000,
  });
  await page.locator("#username").fill(user);
  await page.locator("#password").fill(password);
  await page.locator("#kc-login").click();

  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    if (page.url().startsWith(BASE)) return;
    if (await page.locator("#password-new").count()) {
      await page.locator("#password-new").fill(REAL_PASSWORD);
      await page.locator("#password-confirm").fill(REAL_PASSWORD);
      await page
        .locator("input[type=submit], button[type=submit]")
        .first()
        .click();
    } else if (await page.locator("#firstName").count()) {
      for (const [id, value] of [
        ["#firstName", "E2E"],
        ["#lastName", "Shiraku"],
      ] as const) {
        const field = page.locator(id);
        if (!(await field.inputValue())) await field.fill(value);
      }
      await page
        .locator("input[type=submit], button[type=submit]")
        .first()
        .click();
    }
    await page.waitForTimeout(800);
  }
  throw new Error(`still on ${page.url()} after the sign-in steps`);
}

// A fresh browser profile: no cookie of Vinaire's, none of Keycloak's.
async function newPerson(browser: Browser) {
  const context = await browser.newContext({ baseURL: BASE });
  return { context, page: await context.newPage() };
}

test("a person signs in through Keycloak and gets exactly the rights they are given", async ({
  browser,
  page,
}) => {
  test.setTimeout(300_000);

  // ---- the administrator sets Keycloak up from the UI
  await page.goto("/login");
  await page.getByLabel("Kubernetes ServiceAccount token").fill(ADMIN_TOKEN);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL("/");

  await page.goto("/settings");
  await page.getByRole("button", { name: "Sign-in", exact: true }).click();
  await page.getByLabel("Keycloak URL").fill(KEYCLOAK);
  await page.getByLabel("Administrator", { exact: true }).fill(KC_ADMIN);
  await page.getByLabel("Administrator password").fill(KC_ADMIN_PASSWORD);
  await page.getByLabel("User name").fill(PERSON);
  await page.getByLabel("E-mail").fill(`${PERSON}@example.com`);
  await page.getByLabel("User password").fill(TEMPORARY_PASSWORD);
  await page.getByRole("button", { name: "Set up Keycloak sign-in" }).click();
  await expect(page.getByText("Keycloak sign-in is on.")).toBeVisible({
    timeout: 120_000,
  });

  // an http Keycloak on a private network must come out in the ServiceAccount mode — with no API-server
  // flags Kubernetes would refuse — and the chart's role prefix must have reached Vinaire
  const status = (await api(page, "GET", "/api/v1/auth/keycloak")).body as {
    active: boolean;
    identityMode: string;
    userNamespace: string;
    rolePrefix: string;
  };
  expect(status.active).toBe(true);
  expect(status.identityMode).toBe("serviceaccount");
  expect(status.userNamespace).toBeTruthy();
  expect(status.rolePrefix).toBeTruthy();
  await expect(page.getByText("--oidc-issuer-url")).toHaveCount(0);

  // ---- the person signs in, with a temporary password to replace on the way
  const first = await newPerson(browser);
  await first.page.goto("/login");
  await first.page.getByRole("link", { name: "Sign in with Keycloak" }).click();
  await keycloakSignIn(first.page, PERSON, TEMPORARY_PASSWORD);

  // back on THIS Vinaire — the development address http://localhost:3100 is what an install without a
  // FRONTEND_URL used to send people to
  expect(first.page.url().startsWith(BASE)).toBe(true);
  expect(first.page.url()).not.toContain(":3100");
  await expect(first.page).toHaveURL(/\/$/);

  const me = (await api(first.page, "GET", "/api/v1/auth/me")).body as {
    username: string;
    kubernetesUser: string;
  };
  expect(me.username).toBe(PERSON);
  expect(me.kubernetesUser).toMatch(
    new RegExp(`^system:serviceaccount:${status.userNamespace}:oidc-`),
  );
  const account = me.kubernetesUser.split(":").pop() as string;

  // a new account has no rights at all
  expect((await api(first.page, "GET", "/api/v1/pods")).status).toBe(403);

  // ---- an administrator gives it the read-only role of the chart (and only that)
  kubectl(
    "create",
    "clusterrolebinding",
    "e2e-keycloak-reader",
    `--clusterrole=${status.rolePrefix}-reader`,
    `--serviceaccount=${status.userNamespace}:${account}`,
  );
  await expect
    .poll(async () => (await api(first.page, "GET", "/api/v1/pods")).status, {
      timeout: 30_000,
    })
    .toBe(200);

  // it reads, it cannot change anything — and the read-only role leaves out Secrets, which `-viewer` reads
  const pods = (await api(first.page, "GET", "/api/v1/pods")).body as {
    namespace: string;
    name: string;
  }[];
  expect(pods.length).toBeGreaterThan(0);
  const target = pods[0];
  expect(
    (
      await api(
        first.page,
        "DELETE",
        `/api/v1/pods/${target.namespace}/${target.name}`,
      )
    ).status,
  ).toBe(403);
  expect((await api(first.page, "GET", "/api/v1/secrets")).status).toBe(403);
  await first.context.close();

  // ---- signing in again finds the same account, and its binding
  const second = await newPerson(browser);
  await second.page.goto("/login");
  await second.page
    .getByRole("link", { name: "Sign in with Keycloak" })
    .click();
  await keycloakSignIn(second.page, PERSON, REAL_PASSWORD);
  const again = (await api(second.page, "GET", "/api/v1/auth/me")).body as {
    kubernetesUser: string;
  };
  expect(again.kubernetesUser).toBe(me.kubernetesUser);
  expect((await api(second.page, "GET", "/api/v1/pods")).status).toBe(200);
  await second.context.close();

  // one person, one account
  const accounts = kubectl(
    "-n",
    status.userNamespace,
    "get",
    "serviceaccount",
    "-o",
    "name",
  )
    .split("\n")
    .filter((n) => n.includes("/oidc-"));
  expect(accounts).toHaveLength(1);

  // clean up what this test bound
  kubectl("delete", "clusterrolebinding", "e2e-keycloak-reader");
});
