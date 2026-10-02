import { expect, test, type Page } from "@playwright/test";

// Roadmap Phase 36 — the response headers, the Content-Security-Policy and the
// CSRF guard, checked in a real browser against the combined image (the Go
// backend serving the static export, so the UI and the API share an origin).
// Skipped otherwise: the dev setup serves the UI from another origin, where a
// same-origin CSP can't hold.
//
// Every request is made by the browser (fetch / navigation), not by Node's HTTP
// client, so the test behaves the same on a developer machine whose Node traffic
// is intercepted (see AGENTS.md) as in CI.
test.describe("headers, CSP and CSRF guard (combined image)", () => {
  test.skip(
    process.env.E2E_SAME_ORIGIN !== "1",
    "needs the combined image — set E2E_SAME_ORIGIN=1 and E2E_BASE_URL",
  );

  // headersOf fetches a same-origin URL from inside the page and returns the
  // response's status and headers (a same-origin fetch may read all of them).
  async function headersOf(page: Page, path: string) {
    return page.evaluate(async (p) => {
      const r = await fetch(p);
      return {
        status: r.status,
        headers: Object.fromEntries(r.headers.entries()),
      };
    }, path);
  }

  test("a page carries the security headers and a hash-based CSP", async ({
    page,
  }) => {
    await page.goto("/login");
    const { status, headers: h } = await headersOf(page, "/login");
    expect(status).toBe(200);
    expect(h["x-content-type-options"]).toBe("nosniff");
    expect(h["x-frame-options"]).toBe("DENY");
    expect(h["referrer-policy"]).toBe("no-referrer");

    const csp = h["content-security-policy"] ?? "";
    expect(csp).toContain("frame-ancestors 'none'");
    expect(csp).toContain("object-src 'none'");
    const scriptSrc = csp
      .split(";")
      .map((d) => d.trim())
      .find((d) => d.startsWith("script-src"));
    expect(scriptSrc).toBeDefined();
    expect(scriptSrc).not.toContain("unsafe-inline");
    expect(scriptSrc).not.toContain("unsafe-eval");
    expect(scriptSrc).toContain("'sha256-"); // the page's own inline scripts, by hash
  });

  test("the API answers with an allow-nothing CSP", async ({ page }) => {
    await page.goto("/login");
    const { headers } = await headersOf(page, "/api/v1/auth/config");
    expect(headers["content-security-policy"]).toContain("default-src 'none'");
  });

  test("a static asset is NOT given the allow-nothing CSP (workers need that)", async ({
    page,
  }) => {
    await page.goto("/login");
    const script = await page.evaluate(
      () =>
        Array.from(document.scripts)
          .map((s) => s.src)
          .find((s) => s.includes("/_next/")) ?? "",
    );
    expect(script).not.toBe("");
    const { headers } = await headersOf(page, new URL(script).pathname);
    expect(headers["content-security-policy"]).toBeUndefined();
  });

  test("the page renders without a CSP violation, and injected script is blocked", async ({
    page,
  }) => {
    const violations: string[] = [];
    page.on("console", (m) => {
      if (/content security policy|refused to/i.test(m.text()))
        violations.push(m.text());
    });
    await page.goto("/login");
    await expect(page.locator("body")).not.toBeEmpty();
    expect(violations, violations.join("\n")).toHaveLength(0);

    // Anything that gets into the DOM can't run: the policy refuses inline code.
    const ran = await page.evaluate(
      () =>
        new Promise<boolean>((resolve) => {
          const w = window as unknown as { __x: boolean };
          w.__x = false;
          const s = document.createElement("script");
          s.textContent = "window.__x = true";
          document.body.appendChild(s);
          const img = document.createElement("img");
          img.setAttribute("onerror", "window.__x = true");
          img.src = "x:invalid";
          document.body.appendChild(img);
          setTimeout(() => resolve(w.__x), 500);
        }),
    );
    expect(ran).toBe(false);
  });

  // A browser can't read the status of a response its own CORS check blocked, so
  // the cross-site case is made with Node's HTTP client, sending the header a
  // browser would. (On a machine whose Node traffic is intercepted — AGENTS.md —
  // it can't connect and is skipped; the guard is also covered by Go unit tests.)
  test("a cross-site state-changing request is refused before any handler", async ({
    request,
    baseURL,
  }) => {
    const target = `${baseURL}/api/v1/auth/login`;
    let evil;
    try {
      evil = await request.post(target, {
        headers: {
          Origin: "https://evil.example",
          "Content-Type": "text/plain",
        },
        data: '{"token":"x"}',
      });
    } catch (e) {
      test.skip(
        /socket hang up|ECONN/i.test(String(e)),
        "Node HTTP can't reach localhost on this machine",
      );
      throw e;
    }
    expect(evil.status()).toBe(403);

    // the same request from this site is let through to the handler (which then
    // rejects the bad token, but not as a cross-site request)
    const own = await request.post(target, {
      headers: { Origin: new URL(baseURL ?? target).origin },
      data: { token: "x" },
    });
    expect(own.status()).not.toBe(403);
  });
});
