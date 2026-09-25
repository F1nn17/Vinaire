import type { StatusColor } from "@/lib/types";
import { statusColorVar } from "@/lib/statusColor";

export function StatusDot({
  color,
  size = 6,
}: {
  color: StatusColor;
  size?: number;
}) {
  return (
    <span
      aria-hidden="true"
      className="inline-block shrink-0 rounded-full"
      style={{
        width: size,
        height: size,
        background: statusColorVar(color),
      }}
    />
  );
}
