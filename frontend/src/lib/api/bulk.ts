import { ApiError } from "./http";

// Roadmap Phase 58: the same action on several objects, one request per object — the
// endpoints that already exist (restart / delete a Deployment, delete a Pod), so each call is
// audited on its own and decided by the caller's own RBAC. Nothing new is allowed to write.

export type BulkItem = { namespace: string; name: string };
export type BulkResult = { key: string; ok: boolean; error?: string };

export const bulkKey = (i: BulkItem) => `${i.namespace}/${i.name}`;

// The most objects one run takes: one request each, throttled by the write limiter.
export const BULK_MAX = 50;

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// Runs `action` over the items one after another. The write limiter (5 at once, then one a
// second) answers 429 when the burst is spent — that is waited out, not worked around, and
// the run goes on. A failure of one object (a 403, a 404 because it is already gone) is
// recorded and the rest still run. `signal` stops the run between two objects; an object
// that was not attempted is not in the results.
export async function runBulk(
  items: BulkItem[],
  action: (item: BulkItem) => Promise<void>,
  onProgress: (results: BulkResult[]) => void,
  signal?: AbortSignal,
): Promise<BulkResult[]> {
  const results: BulkResult[] = [];
  for (const item of items) {
    if (signal?.aborted) break;
    const key = bulkKey(item);
    let result: BulkResult | null = null;
    for (let attempt = 0; result === null; attempt++) {
      try {
        await action(item);
        result = { key, ok: true };
      } catch (e) {
        if (e instanceof ApiError && e.status === 429 && attempt < 8) {
          await sleep(1200);
          if (signal?.aborted) break;
          continue;
        }
        result = {
          key,
          ok: false,
          error: e instanceof Error ? e.message : String(e),
        };
      }
    }
    if (result === null) break; // stopped while waiting to retry
    results.push(result);
    onProgress([...results]);
  }
  return results;
}
