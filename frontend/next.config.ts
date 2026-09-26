import path from "node:path";
import type { NextConfig } from "next";

// Roadmap Phase 10: Next.js is used purely as a build tool, not a runtime
// server. `output: "export"` makes `next build` render every page once,
// at build time, into static HTML/JS/CSS under /frontend/out — the exact
// files the single combined image's Go backend serves directly via
// go:embed/http.FileServer, on the same port as the API. Nothing here ever
// runs a Next.js server at runtime, in dev-via-Docker or especially in
// prod; all dynamic content loads client-side via fetch/WebSocket to the
// Go backend. See AGENTS.md's "Rendering model" section.
//
// `next dev` ignores `output: "export"` (it always runs a normal dev
// server with hot reload) — this only changes what `next build` produces.
// Next.js also disables custom routing (`rewrites`/`redirects`/`headers`)
// entirely once `output: "export"` is set, even in dev — confirmed by
// actually running `next dev` with both configured: it starts, warns that
// rewrites won't work, and then doesn't apply them. So dev and the
// production static export both talk to the backend the same way, over
// plain cross-origin fetch — see lib/api/config.ts's API_BASE and
// internal/authn's cookie settings on the backend for how that cookie
// still works cross-origin locally.
const nextConfig: NextConfig = {
  output: "export",

  // The repo root also has a package-lock.json (for the `npm run dev`
  // orchestrator script), which makes Next.js misdetect the workspace root.
  outputFileTracingRoot: path.join(__dirname),
};

export default nextConfig;
