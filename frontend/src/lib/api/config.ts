// Defaults to the dev backend's own origin — `next.config.ts` can't proxy
// this away (Next.js disables custom routing entirely once
// `output: "export"` is set, even in `next dev` — see its comment), so dev
// and the static export both call the backend cross-origin. Set to ""
// (baked in at `next build` time, e.g. in the single-image Dockerfile) to
// make it relative instead, for a deployment where the Go backend serves
// both the API and this static export from one origin (Roadmap Phase 10)
// — same-origin needs no CORS and can use a stricter session cookie.
export const API_BASE =
  process.env.NEXT_PUBLIC_API_BASE ?? "http://localhost:8080";

// Simulated network latency so loading states are visible on mock data.
// Remove once these functions call the real backend below.
export function simulatedDelay(ms = 260): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
