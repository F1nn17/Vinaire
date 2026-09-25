import path from "node:path";
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // The repo root also has a package-lock.json (for the `npm run dev`
  // orchestrator script), which makes Next.js misdetect the workspace root.
  outputFileTracingRoot: path.join(__dirname),
};

export default nextConfig;
