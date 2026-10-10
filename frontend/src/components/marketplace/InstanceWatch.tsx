"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { t } from "@/lib/i18n/translate";
import { useWorkloads } from "@/lib/hooks/useWorkloads";
import { StatusDot } from "@/components/ui/StatusDot";

// Statuses that don't fix themselves: say what they usually mean instead of leaving the user to guess.
const PROBLEMS: Record<string, string> = {
  ImagePullBackOff:
    "The image can't be pulled: check the name and the tag (or the registry's rate limit).",
  ErrImagePull:
    "The image can't be pulled: check the name and the tag (or the registry's rate limit).",
  CrashLoopBackOff:
    "The program starts and stops again and again: open the pod's Logs tab to see why.",
  CreateContainerConfigError:
    "The container can't be configured — usually a Secret it refers to is missing or has no such key.",
  Pending:
    "Waiting to be placed: the cluster may lack CPU or memory, or the volume can't be created.",
};

// What happens after "Install" in the image wizard: the pod of the new StatefulSet, live, until it
// is ready — with the usual reason when it isn't. (The objects exist by then; this watches them
// come up. Reads only, as the signed-in user.)
export function InstanceWatch({
  namespace,
  name,
}: {
  namespace: string;
  name: string;
}) {
  const { data } = useWorkloads("pods", namespace);
  const [started] = useState(() => Date.now());
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const seconds = Math.max(0, Math.round((now - started) / 1000));

  const pods = (data ?? []).filter((p) =>
    new RegExp(`^${name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}-[0-9]+$`).test(
      p.name,
    ),
  );
  const ready =
    pods.length > 0 &&
    pods.every((p) => {
      const [a, b] = p.ready.split("/");
      return a === b && a !== "0" && p.status === "Running";
    });
  const problem = pods.map((p) => PROBLEMS[p.status]).find(Boolean);

  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface-2 p-3 text-xs">
      <div className="flex items-center justify-between gap-3">
        <span className="font-semibold">
          {ready
            ? t("Ready — the pod is running.")
            : t("Waiting for the pod to start…")}
        </span>
        <span className="font-mono text-text-muted">{seconds} s</span>
      </div>
      {pods.length === 0 ? (
        <span className="text-text-muted">
          {t("The pod hasn't appeared yet.")}
        </span>
      ) : (
        pods.map((p) => (
          <div key={p.name} className="flex items-center gap-3">
            <StatusDot color={p.statusColor} />
            <Link href={p.href} className="font-mono text-accent-text">
              {p.name}
            </Link>
            <span className="font-mono text-text-muted">{p.ready}</span>
            <span>{p.status}</span>
            {p.restarts !== "0" && (
              <span className="text-text-muted">
                {t("restarts")}: {p.restarts}
              </span>
            )}
          </div>
        ))
      )}
      {!ready && problem && seconds > 15 && (
        <span className="text-warn">{t(problem)}</span>
      )}
      {!ready && !problem && seconds > 90 && (
        <span className="text-text-muted">
          {t(
            "Still starting. The first start pulls the image and prepares the data, which can take a few minutes.",
          )}
        </span>
      )}
    </div>
  );
}
