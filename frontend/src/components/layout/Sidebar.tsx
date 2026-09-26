"use client";

import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { Suspense, type ReactNode } from "react";
import {
  ConfigMapIcon,
  GridIcon,
  GrapeLogo,
  LayersIcon,
  NetworkIcon,
  PuzzleIcon,
  SecretIcon,
  SettingsIcon,
  ShieldIcon,
  StorageIcon,
  TopologyIcon,
} from "@/components/icons";
import { useCRDs } from "@/lib/hooks/useCRDs";

type NavItem = {
  href: string;
  label: string;
  icon: ReactNode;
  isActive: (pathname: string) => boolean;
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
        href: "/topology",
        label: "Topology",
        icon: <TopologyIcon />,
        isActive: (p) => p.startsWith("/topology"),
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

// A dynamic sidebar section built from whatever CRDs are actually installed
// on the connected cluster (Roadmap Phase 15) — no hardcoded per-CRD nav
// entries. Split out from Sidebar so only this part needs the Suspense
// boundary useSearchParams requires.
function CustomResourcesNav({ pathname }: { pathname: string }) {
  const searchParams = useSearchParams();
  const { data: crds } = useCRDs();
  const activeCRD = searchParams.get("crd");

  if (!crds || crds.length === 0) {
    return null;
  }

  return (
    <div className="flex flex-col gap-0.5">
      <span className="px-2.5 pb-1 pt-0.5 text-[10.5px] uppercase tracking-wider text-text-faint">
        Custom Resources
      </span>
      {crds.map((crd) => {
        const href = `/custom-resources?crd=${encodeURIComponent(crd.name)}`;
        const active =
          pathname.startsWith("/custom-resources") && activeCRD === crd.name;
        return (
          <NavLink
            key={crd.name}
            item={{
              href,
              label: crd.kind,
              icon: <PuzzleIcon />,
              isActive: () => active,
            }}
            active={active}
          />
        );
      })}
    </div>
  );
}

export function Sidebar() {
  const pathname = usePathname();

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
          {group.items.map((item) => (
            <NavLink
              key={item.href}
              item={item}
              active={item.isActive(pathname)}
            />
          ))}
        </div>
      ))}

      <Suspense fallback={null}>
        <CustomResourcesNav pathname={pathname} />
      </Suspense>

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
