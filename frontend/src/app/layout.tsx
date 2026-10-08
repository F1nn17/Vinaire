import type { Metadata } from "next";
import localFont from "next/font/local";
import { AppInit } from "@/components/providers/AppInit";
import { I18nProvider } from "@/components/providers/I18nProvider";
import "./globals.css";

// Self-hosted (latin subset, static woff2 files checked into the repo)
// rather than next/font/google — that loader fetches font files from
// Google's servers at build time, which made the Docker build fail
// whenever that network hop was unreachable/flaky. Re-fetch instructions:
// see frontend/src/app/fonts/README.md.
const plexSans = localFont({
  variable: "--font-sans",
  src: [
    { path: "./fonts/IBMPlexSans-400.woff2", weight: "400", style: "normal" },
    { path: "./fonts/IBMPlexSans-500.woff2", weight: "500", style: "normal" },
    { path: "./fonts/IBMPlexSans-600.woff2", weight: "600", style: "normal" },
  ],
});

const jetBrainsMono = localFont({
  variable: "--font-mono",
  src: [
    { path: "./fonts/JetBrainsMono-500.woff2", weight: "500", style: "normal" },
    { path: "./fonts/JetBrainsMono-600.woff2", weight: "600", style: "normal" },
    { path: "./fonts/JetBrainsMono-700.woff2", weight: "700", style: "normal" },
  ],
});

export const metadata: Metadata = {
  title: "Vinaire",
  description: "A friendlier control plane UI for Kubernetes clusters.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="en"
      className={`${plexSans.variable} ${jetBrainsMono.variable} h-full antialiased`}
      // public/theme-init.js sets data-theme before React hydrates
      suppressHydrationWarning
    >
      <head>
        {/* sets <html data-theme> before first paint, so there is no flash of the wrong theme */}
        {/* eslint-disable-next-line @next/next/no-sync-scripts -- deliberately blocking: a tiny same-origin file that must run before first paint */}
        <script src="/theme-init.js" />
      </head>
      <body className="min-h-full bg-bg text-text font-sans text-[14px]">
        <AppInit>
          <I18nProvider>{children}</I18nProvider>
        </AppInit>
      </body>
    </html>
  );
}
