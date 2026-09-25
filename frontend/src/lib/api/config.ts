export const API_BASE =
  process.env.NEXT_PUBLIC_API_BASE ?? "http://localhost:8080";

// Simulated network latency so loading states are visible on mock data.
// Remove once these functions call the real backend below.
export function simulatedDelay(ms = 260): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
