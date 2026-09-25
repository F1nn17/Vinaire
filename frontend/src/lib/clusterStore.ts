"use client";

import { useSyncExternalStore } from "react";

// Selected-cluster state (Roadmap Phase 11) lives here as a plain module
// variable, not React state — lib/api/*.ts's apiFetch is a plain function
// (not a hook) and needs to read the current selection synchronously on
// every call. useCurrentCluster() below is the reactive view for
// components (e.g. the Topbar's cluster picker); the two stay in sync via
// the listener set.
const storageKey = "vinaire.cluster";
let current = "";
const listeners = new Set<() => void>();

function notify() {
  for (const listener of listeners) listener();
}

// initCurrentCluster restores the last-selected cluster from localStorage.
// Call once on the client before anything reads getCurrentCluster() —
// see ClusterInit in components/providers/ClusterInit.tsx.
export function initCurrentCluster() {
  try {
    const saved = localStorage.getItem(storageKey);
    if (saved) {
      current = saved;
      notify();
    }
  } catch {
    // Storage unavailable (private browsing, etc.) — fall back to "" (the
    // backend's default cluster), same as a first-ever visit.
  }
}

export function getCurrentCluster(): string {
  return current;
}

export function setCurrentCluster(name: string) {
  current = name;
  try {
    localStorage.setItem(storageKey, name);
  } catch {
    // Non-fatal — the selection just won't survive a page reload.
  }
  notify();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

// useCurrentCluster gives components a reactive view of the same value
// apiFetch reads synchronously, so e.g. SWR keys can include it and
// refetch when the Topbar's dropdown changes the selection.
export function useCurrentCluster(): string {
  return useSyncExternalStore(subscribe, getCurrentCluster, () => "");
}
