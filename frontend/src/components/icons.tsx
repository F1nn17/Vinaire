type IconProps = {
  size?: number;
  className?: string;
};

const base = {
  fill: "none",
  stroke: "currentColor",
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

export function GrapeLogo({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.6}
      className={className}
    >
      <path d="M12 2v3.2" />
      <circle cx="12" cy="7.3" r="2" />
      <circle cx="9" cy="10.6" r="2" />
      <circle cx="15" cy="10.6" r="2" />
      <circle cx="6.3" cy="14" r="2" />
      <circle cx="12" cy="14" r="2" />
      <circle cx="17.7" cy="14" r="2" />
      <circle cx="9" cy="17.4" r="2" />
      <circle cx="15" cy="17.4" r="2" />
      <circle cx="12" cy="20.6" r="2" />
    </svg>
  );
}

export function GridIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <rect x="3" y="3" width="7" height="7" rx="1.5" />
      <rect x="14" y="3" width="7" height="7" rx="1.5" />
      <rect x="3" y="14" width="7" height="7" rx="1.5" />
      <rect x="14" y="14" width="7" height="7" rx="1.5" />
    </svg>
  );
}

export function TopologyIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <circle cx="5" cy="6" r="2.4" />
      <circle cx="19" cy="6" r="2.4" />
      <circle cx="12" cy="18" r="2.4" />
      <path d="M6.9 7.6L10.2 16.4M17.1 7.6L13.8 16.4M7.4 6H16.6" />
    </svg>
  );
}

export function LayersIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M12 3l9 5-9 5-9-5z" />
      <path d="M3 13l9 5 9-5" />
    </svg>
  );
}

export function ConfigMapIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M7 3h7l4 4v14H7z" />
      <path d="M14 3v4h4M10 12h6M10 16h6" />
    </svg>
  );
}

export function SecretIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <rect x="5" y="11" width="14" height="9" rx="1.5" />
      <path d="M8 11V7a4 4 0 018 0v4" />
    </svg>
  );
}

export function NetworkIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <circle cx="12" cy="12" r="8" />
      <path d="M4 12h16M12 4c2.5 2.6 2.5 13.4 0 16M12 4c-2.5 2.6-2.5 13.4 0 16" />
    </svg>
  );
}

export function SettingsIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <circle cx="12" cy="12" r="3.2" />
      <path d="M19.4 13.5a1.7 1.7 0 00.3 1.9l.1.1a2 2 0 11-2.9 2.9l-.1-.1a1.7 1.7 0 00-1.9-.3 1.7 1.7 0 00-1 1.6V20a2 2 0 11-4 0v-.2a1.7 1.7 0 00-1-1.6 1.7 1.7 0 00-1.9.3l-.1.1a2 2 0 11-2.9-2.9l.1-.1a1.7 1.7 0 00.3-1.9 1.7 1.7 0 00-1.6-1H4a2 2 0 110-4h.2a1.7 1.7 0 001.6-1 1.7 1.7 0 00-.3-1.9l-.1-.1a2 2 0 112.9-2.9l.1.1a1.7 1.7 0 001.9.3H10a1.7 1.7 0 001-1.6V4a2 2 0 114 0v.2a1.7 1.7 0 001 1.6 1.7 1.7 0 001.9-.3l.1-.1a2 2 0 112.9 2.9l-.1.1a1.7 1.7 0 00-.3 1.9V10a1.7 1.7 0 001.6 1H20a2 2 0 110 4h-.2a1.7 1.7 0 00-1.6 1z" />
    </svg>
  );
}

export function SearchIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <circle cx="11" cy="11" r="7" />
      <path d="M21 21l-4.3-4.3" />
    </svg>
  );
}

export function ChevronDownIcon({ size = 14, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={2}
      className={className}
    >
      <path d="M6 9l6 6 6-6" />
    </svg>
  );
}

export function BellIcon({ size = 19, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M18 8a6 6 0 10-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
      <path d="M13.7 21a2 2 0 01-3.4 0" />
    </svg>
  );
}

export function PlusIcon({ size = 14, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={2.2}
      className={className}
    >
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}

export function ServiceIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M12 3l7.8 4.5v9L12 21l-7.8-4.5v-9z" />
    </svg>
  );
}

export function StorageIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <ellipse cx="12" cy="5.5" rx="8" ry="3" />
      <path d="M4 5.5v6.5c0 1.7 3.6 3 8 3s8-1.3 8-3V5.5" />
      <path d="M4 12v6.5c0 1.7 3.6 3 8 3s8-1.3 8-3V12" />
    </svg>
  );
}

export function ShieldIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M12 3l7 3v6c0 4.5-3 7.5-7 9-4-1.5-7-4.5-7-9V6z" />
      <path d="M9 12l2 2 4-4" />
    </svg>
  );
}

export function PuzzleIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M9 4h4v2.2a1.8 1.8 0 003.6 0V4H20v4.4h-2.2a1.8 1.8 0 000 3.6H20V16h-3.4v-2.2a1.8 1.8 0 00-3.6 0V16H9v-4.4H6.8a1.8 1.8 0 000-3.6H9z" />
      <path d="M9 16H4v-4.4" />
    </svg>
  );
}

export function PodIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M12 3l8 4.5v9L12 21l-8-4.5v-9z" />
      <path d="M4 7.5L12 12l8-4.5M12 12v9" />
    </svg>
  );
}

export function ServerIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <rect x="3" y="4" width="18" height="6" rx="1.5" />
      <rect x="3" y="14" width="18" height="6" rx="1.5" />
      <path d="M7 7h.01M7 17h.01" />
    </svg>
  );
}

// Roadmap Phase 33's consolidated resource action bar.
export function RestartIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M3 12a9 9 0 1 1 3 6.7" />
      <path d="M3 21v-5h5" />
    </svg>
  );
}

export function ScaleIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M12 3v18M8 7l4-4 4 4M8 17l4 4 4-4" />
    </svg>
  );
}

export function PauseIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <rect x="6" y="4" width="4" height="16" rx="1" />
      <rect x="14" y="4" width="4" height="16" rx="1" />
    </svg>
  );
}

export function PlayIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M6 4l14 8-14 8z" />
    </svg>
  );
}

export function HistoryIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M3 12a9 9 0 1 0 3-6.7" />
      <path d="M3 4v5h5" />
      <path d="M12 8v5l3 2" />
    </svg>
  );
}

export function TrashIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M4 7h16" />
      <path d="M9 7V4h6v3" />
      <path d="M6 7l1 13h10l1-13" />
      <path d="M10 11v6M14 11v6" />
    </svg>
  );
}

export function NamespaceIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <rect x="3" y="3" width="18" height="18" rx="3" strokeDasharray="3 2.5" />
      <rect x="8" y="8" width="8" height="8" rx="1.5" />
    </svg>
  );
}

export function PortForwardIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M4 12h12M12 7l5 5-5 5" />
      <path d="M20 5v14" />
    </svg>
  );
}

export function ClustersIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <circle cx="12" cy="5" r="2.5" />
      <circle cx="5" cy="18" r="2.5" />
      <circle cx="19" cy="18" r="2.5" />
      <path d="M11 7.3 6.2 15.8M13 7.3l4.8 8.5M7.5 18h9" />
    </svg>
  );
}

export function GitOpsIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <circle cx="6" cy="5.5" r="2.2" />
      <circle cx="6" cy="18.5" r="2.2" />
      <circle cx="18" cy="9" r="2.2" />
      <path d="M6 7.7v8.6" />
      <path d="M18 11.2c0 3.4-3 3.4-6 4.2-2.4.6-6 .8-6 .8" />
    </svg>
  );
}

export function StoreIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M4 9l1.5-5h13L20 9" />
      <path d="M4 9a2.7 2.7 0 005.3 0 2.7 2.7 0 005.4 0A2.7 2.7 0 0020 9" />
      <path d="M5 12v8h14v-8" />
      <path d="M10 20v-4.5h4V20" />
    </svg>
  );
}

export function GaugeIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M4 17a8 8 0 1116 0" />
      <path d="M12 17l4-6" />
      <circle cx="12" cy="17" r="1" />
    </svg>
  );
}

export function ListIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M8 6h13M8 12h13M8 18h13" />
      <path d="M3.5 6h.01M3.5 12h.01M3.5 18h.01" />
    </svg>
  );
}

export function DrainIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M4 5h16l-2 9H6z" />
      <path d="M12 14v6" />
      <path d="M9 17l3 3 3-3" />
    </svg>
  );
}

export function TagIcon({ size = 16, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      {...base}
      strokeWidth={1.8}
      className={className}
    >
      <path d="M3 12V4h8l10 10-8 8z" />
      <circle cx="7.5" cy="8.5" r="1" />
    </svg>
  );
}
