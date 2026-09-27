import type { SearchResult } from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoint (Roadmap Phase 20).
export function search(query: string): Promise<SearchResult[]> {
  return apiFetch<SearchResult[]>(
    `/api/v1/search?q=${encodeURIComponent(query)}`,
  );
}
