"use client";

import { useDashboard } from "@/lib/hooks/useDashboard";
import { StatCard } from "@/components/ui/StatCard";
import { EventRow } from "@/components/ui/EventRow";
import { Sparkline } from "@/components/ui/Sparkline";

export default function DashboardPage() {
  const { data, isLoading } = useDashboard();

  if (isLoading || !data) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  return (
    <div className="flex flex-col gap-[22px] px-8 py-7">
      <div className="grid grid-cols-4 gap-4">
        {data.statCards.map((card) => (
          <StatCard key={card.title} {...card} />
        ))}
      </div>

      <div className="grid grid-cols-[1.6fr_1fr] gap-4">
        <div className="flex flex-col rounded-[10px] border border-border bg-surface p-5">
          <div className="mb-3.5 text-[13px] font-semibold">Recent events</div>
          <div className="flex flex-col gap-0.5 overflow-auto">
            {data.events.map((event, i) => (
              <EventRow key={i} {...event} />
            ))}
          </div>
        </div>

        <div className="flex flex-col gap-4">
          <div className="rounded-[10px] border border-border bg-surface p-[18px]">
            <div className="mb-2.5 flex items-center justify-between">
              <span className="text-[13px] font-semibold">Cluster CPU</span>
              <span className="font-mono text-xs text-text-muted">
                {data.cpu.used} / {data.cpu.total}
              </span>
            </div>
            <Sparkline points={data.cpu.points} color="var(--accent)" />
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-[18px]">
            <div className="mb-2.5 flex items-center justify-between">
              <span className="text-[13px] font-semibold">Cluster memory</span>
              <span className="font-mono text-xs text-text-muted">
                {data.memory.used} / {data.memory.total}
              </span>
            </div>
            <Sparkline points={data.memory.points} color="var(--ok)" />
          </div>

          <div className="flex-1 rounded-[10px] border border-border bg-surface p-[18px]">
            <span className="text-[13px] font-semibold">Namespaces</span>
            <div className="mt-3 flex flex-col gap-2.5">
              {data.namespaces.map((ns) => (
                <div
                  key={ns.name}
                  className="flex items-center justify-between"
                >
                  <span className="font-mono text-xs">{ns.name}</span>
                  <span className="font-mono text-xs text-text-muted">
                    {ns.pods} pods
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
