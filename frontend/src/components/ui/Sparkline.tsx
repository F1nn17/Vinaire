export function Sparkline({
  points,
  color,
  height = 40,
}: {
  points: number[];
  color: string;
  height?: number;
}) {
  const step = 220 / (points.length - 1);
  const coords = points.map((y, i) => `${Math.round(i * step)},${y}`).join(" ");

  return (
    <svg
      aria-hidden="true"
      width="100%"
      height={height}
      viewBox={`0 0 220 ${height}`}
      preserveAspectRatio="none"
    >
      <polyline
        points={coords}
        fill="none"
        style={{ stroke: color }}
        strokeWidth={2}
      />
    </svg>
  );
}
