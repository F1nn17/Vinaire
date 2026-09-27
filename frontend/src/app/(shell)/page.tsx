"use client";

import { useDashboard } from "@/lib/hooks/useDashboard";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { StatCard } from "@/components/ui/StatCard";
import { EventRow } from "@/components/ui/EventRow";

export default function DashboardPage() {
  const { data, isLoading, error } = useDashboard();

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load dashboard: {friendlyErrorMessage(error)}
      </div>
    );
  }

  if (isLoading || !data) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  const cardCount = data.statCards.length + (data.driftedFromGit ? 1 : 0);

  return (
    <div className="flex flex-col gap-[22px] px-8 py-7">
      <div
        className={`grid gap-4 ${cardCount >= 4 ? "grid-cols-4" : "grid-cols-3"}`}
      >
        {data.statCards.map((card) => (
          <StatCard key={card.title} {...card} />
        ))}
        {data.driftedFromGit && (
          <StatCard
            title="Drifted from Git"
            dotColor={data.driftedFromGit.dotColor}
            value={data.driftedFromGit.count}
            caption={data.driftedFromGit.caption}
          />
        )}
      </div>

      <div className="grid grid-cols-[1.6fr_1fr] gap-4">
        <div className="flex flex-col rounded-[10px] border border-border bg-surface p-5">
          <div className="mb-3.5 text-[13px] font-semibold">Recent events</div>
          <div className="flex flex-col gap-0.5 overflow-auto">
            {data.events.length === 0 ? (
              <div className="text-xs text-text-faint">No recent events.</div>
            ) : (
              data.events.map((event, i) => <EventRow key={i} {...event} />)
            )}
          </div>
        </div>

        <div className="flex flex-col gap-4">
          <div className="rounded-[10px] border border-border bg-surface p-[18px]">
            <div className="mb-2.5 flex items-center justify-between">
              <span className="text-[13px] font-semibold">Cluster CPU</span>
              {data.cpu && (
                <span className="font-mono text-xs text-text-muted">
                  {data.cpu.used} / {data.cpu.total}
                </span>
              )}
            </div>
            {data.cpu ? (
              <div className="h-2 overflow-hidden rounded bg-surface-2">
                <div
                  className="h-full bg-accent"
                  style={{ width: `${data.cpu.pct}%` }}
                />
              </div>
            ) : (
              <UnavailablePlaceholder text="metrics-server isn't installed or reachable." />
            )}
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-[18px]">
            <div className="mb-2.5 flex items-center justify-between">
              <span className="text-[13px] font-semibold">Cluster memory</span>
              {data.memory && (
                <span className="font-mono text-xs text-text-muted">
                  {data.memory.used} / {data.memory.total}
                </span>
              )}
            </div>
            {data.memory ? (
              <div className="h-2 overflow-hidden rounded bg-surface-2">
                <div
                  className="h-full bg-warn"
                  style={{ width: `${data.memory.pct}%` }}
                />
              </div>
            ) : (
              <UnavailablePlaceholder text="metrics-server isn't installed or reachable." />
            )}
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-[18px]">
            <span className="text-[13px] font-semibold">Namespaces</span>
            <div className="mt-3 flex flex-col gap-2.5">
              {data.namespaces.length === 0 ? (
                <div className="text-xs text-text-faint">No pods found.</div>
              ) : (
                data.namespaces.map((ns) => (
                  <div
                    key={ns.name}
                    className="flex items-center justify-between"
                  >
                    <span className="font-mono text-xs">{ns.name}</span>
                    <span className="font-mono text-xs text-text-muted">
                      {ns.pods} pods
                    </span>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// Roadmap Phase 18: an explicit "we don't have this data yet" placeholder,
// visually distinct from a drawn (fabricated) flat line at zero.
function UnavailablePlaceholder({ text }: { text: string }) {
  return (
    <div className="flex h-10 items-center justify-center text-xs text-text-faint">
      {text}
    </div>
  );
}
