import type { PanelUnit } from "./model";

function bytes(v: number): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  let n = Math.abs(v);
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${v < 0 ? "-" : ""}${n >= 100 || i === 0 ? n.toFixed(0) : n.toFixed(1)} ${units[i]}`;
}

function plain(v: number): string {
  const a = Math.abs(v);
  if (a >= 1e9) return `${(v / 1e9).toFixed(1)}G`;
  if (a >= 1e6) return `${(v / 1e6).toFixed(1)}M`;
  if (a >= 1e4) return `${(v / 1e3).toFixed(1)}k`;
  if (a >= 100 || Number.isInteger(v)) return v.toFixed(0);
  if (a >= 1) return v.toFixed(2);
  if (a === 0) return "0";
  return a >= 0.01 ? v.toFixed(3) : v.toExponential(1);
}

export function formatPanelValue(v: number, unit: PanelUnit): string {
  if (!Number.isFinite(v)) return "—";
  switch (unit) {
    case "percent":
      return `${v.toFixed(v >= 10 ? 0 : 1)} %`;
    case "percentunit":
      return `${(v * 100).toFixed(v * 100 >= 10 ? 0 : 1)} %`;
    case "bytes":
      return bytes(v);
    case "bytesps":
      return `${bytes(v)}/s`;
    case "seconds":
      return Math.abs(v) >= 1 ? `${v.toFixed(2)} s` : `${(v * 1000).toFixed(0)} ms`;
    case "ops":
      return `${plain(v)}/s`;
    default:
      return plain(v);
  }
}
