import {
  getProvisionJob,
  provisionStreamUrl,
  type ProvisionEvent,
} from "@/lib/api/provision";

// Follows a provisioning run (Roadmap Phase 30/34): a WebSocket replays and then
// streams its log; if the socket drops before the run ends, fall back to
// polling the job (the run itself keeps going on the server either way).
// `onEvents` may see an event more than once — callers merge by `seq`.
// Returns a function that stops watching.
export function watchProvisionJob(
  jobId: string,
  onEvents: (events: ProvisionEvent[]) => void,
): () => void {
  let finished = false;
  let stopped = false;
  let lastSeq = -1;
  let timer: ReturnType<typeof setInterval> | undefined;

  const deliver = (evs: ProvisionEvent[]) => {
    for (const e of evs) lastSeq = Math.max(lastSeq, e.seq);
    if (evs.some((e) => e.kind === "status")) finished = true;
    if (!stopped) onEvents(evs);
  };

  const ws = new WebSocket(provisionStreamUrl(jobId, -1));
  ws.onmessage = (m) => deliver([JSON.parse(m.data) as ProvisionEvent]);
  ws.onclose = () => {
    if (finished || stopped) return;
    timer = setInterval(async () => {
      try {
        const snap = await getProvisionJob(jobId);
        deliver(snap.events.filter((e) => e.seq > lastSeq));
        if (snap.status !== "running") clearInterval(timer);
      } catch {
        clearInterval(timer);
      }
    }, 3000);
  };

  return () => {
    stopped = true;
    ws.close();
    if (timer) clearInterval(timer);
  };
}
