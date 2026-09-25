import type { StatusColor } from "@/lib/types";

export function statusColorVar(color: StatusColor): string {
  return `var(--${color})`;
}
