"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { useGitOps } from "@/lib/hooks/useGitOps";
import {
  ClustersIcon,
  ConfigMapIcon,
  GitOpsIcon,
  GaugeIcon,
  GridIcon,
  GrapeLogo,
  LayersIcon,
  NamespaceIcon,
  NetworkIcon,
  SecretIcon,
  ServerIcon,
  SettingsIcon,
  ShieldIcon,
  StorageIcon,
  StoreIcon,
  TopologyIcon,
} from "@/components/icons";

type NavItem = {
  href: string;
  label: string;
  icon: ReactNode;
  isActive: (pathname: string) => boolean;
  // shown only when the current cluster's GitOps mode isn't "off"
  gitops?: boolean;
};

type NavGroup = {
  label: string;
  items: NavItem[];
};

const groups: NavGroup[] = [
  {
    label: "Cluster",
    items: [
      {
        href: "/",
        label: "Overview",
        icon: <GridIcon />,
        isActive: (p) => p === "/",
      },
      {
        href: "/clusters",
        label: "Clusters",
        icon: <ClustersIcon />,
        isActive: (p) => p.startsWith("/clusters"),
      },
      {
        href: "/topology",
        label: "Topology",
        icon: <TopologyIcon />,
        isActive: (p) => p.startsWith("/topology"),
      },
      {
        href: "/namespaces",
        label: "Namespaces",
        icon: <NamespaceIcon />,
        isActive: (p) => p.startsWith("/namespaces"),
      },
      {
        href: "/nodes",
        label: "Nodes",
        icon: <ServerIcon />,
        isActive: (p) => p.startsWith("/nodes"),
      },
      {
        href: "/requests-limits",
        label: "Requests & Limits",
        icon: <GaugeIcon />,
        isActive: (p) => p.startsWith("/requests-limits"),
      },
    ],
  },
  {
    label: "Workloads",
    items: [
      {
        href: "/workloads",
        label: "Deployments & pods",
        icon: <LayersIcon />,
        isActive: (p) => p.startsWith("/workloads"),
      },
    ],
  },
  {
    label: "Apps",
    items: [
      {
        href: "/marketplace",
        label: "Marketplace",
        icon: <StoreIcon />,
        isActive: (p) => p.startsWith("/marketplace"),
      },
      {
        href: "/gitops",
        label: "GitOps",
        icon: <GitOpsIcon />,
        isActive: (p) => p.startsWith("/gitops"),
        gitops: true,
      },
    ],
  },
  {
    label: "Configuration",
    items: [
      {
        href: "/configmaps",
        label: "ConfigMaps",
        icon: <ConfigMapIcon />,
        isActive: (p) => p.startsWith("/configmaps"),
      },
      {
        href: "/secrets",
        label: "Secrets",
        icon: <SecretIcon />,
        isActive: (p) => p.startsWith("/secrets"),
      },
    ],
  },
  {
    label: "Storage",
    items: [
      {
        href: "/storage",
        label: "PVCs & volumes",
        icon: <StorageIcon />,
        isActive: (p) => p.startsWith("/storage"),
      },
    ],
  },
  {
    label: "Network",
    items: [
      {
        href: "/network",
        label: "Services & Ingress",
        icon: <NetworkIcon />,
        isActive: (p) => p.startsWith("/network"),
      },
    ],
  },
  {
    label: "Access control",
    items: [
      {
        href: "/access-control",
        label: "RBAC",
        icon: <ShieldIcon />,
        isActive: (p) => p.startsWith("/access-control"),
      },
    ],
  },
];

function NavLink({ item, active }: { item: NavItem; active: boolean }) {
  return (
    <Link
      href={item.href}
      aria-current={active ? "page" : undefined}
      className={`flex items-center gap-2.5 rounded-[7px] px-2.5 py-[9px] text-[13px] ${
        active ? "bg-surface-2 text-accent-text" : "text-text-muted"
      }`}
    >
      <span
        className="flex shrink-0"
        style={{ color: active ? "var(--accent)" : undefined }}
      >
        {item.icon}
      </span>
      <span>{item.label}</span>
    </Link>
  );
}

export function Sidebar() {
  const pathname = usePathname();
  // Roadmap Phase 29: with GitOps off for this cluster the section isn't shown.
  // (It is switched on from the Clusters page, or by opening /gitops.)
  const { data: gitops } = useGitOps();
  const showGitOps = !!gitops && gitops.mode !== "off";

  return (
    <nav
      aria-label="Primary"
      className="flex w-[232px] shrink-0 flex-col gap-5 overflow-auto border-r border-border bg-surface px-3.5 py-5"
    >
      <div className="flex items-center gap-2.5 px-2">
        <div className="flex h-7 w-7 items-center justify-center rounded-[7px] bg-accent text-white">
          <GrapeLogo />
        </div>
        <span className="font-mono text-sm font-semibold">Vinaire</span>
      </div>

      {groups.map((group) => (
        <div key={group.label} className="flex flex-col gap-0.5">
          <span className="px-2.5 pb-1 pt-0.5 text-[10.5px] uppercase tracking-wider text-text-faint">
            {group.label}
          </span>
          {group.items
            .filter((item) => !item.gitops || showGitOps)
            .map((item) => (
              <NavLink
                key={item.href}
                item={item}
                active={item.isActive(pathname)}
              />
            ))}
        </div>
      ))}

      <div className="mt-auto flex flex-col gap-0.5">
        <NavLink
          item={{
            href: "/settings",
            label: "Settings",
            icon: <SettingsIcon />,
            isActive: (p) => p.startsWith("/settings"),
          }}
          active={pathname.startsWith("/settings")}
        />
      </div>
    </nav>
  );
}
