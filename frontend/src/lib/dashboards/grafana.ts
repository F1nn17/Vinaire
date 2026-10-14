import type { Dashboard, PanelUnit } from "./model";

// A Grafana dashboard (schema 39) with the same panels. No datasource is named, so every panel uses
// the Grafana's default one — put Prometheus there. `{label}` in a legend becomes Grafana's
// `{{label}}`; `$name` variables stay, as Grafana variables of the same name.

const units: Record<PanelUnit, string> = {
  short: "short",
  percent: "percent",
  percentunit: "percentunit",
  bytes: "bytes",
  bytesps: "Bps",
  seconds: "s",
  ops: "ops",
};

export function toGrafana(d: Dashboard): Record<string, unknown> {
  let y = 0;
  return {
    uid: `vinaire-${d.id}`.slice(0, 40),
    title: d.title,
    description: d.description ?? "",
    tags: ["vinaire"],
    schemaVersion: 39,
    version: 1,
    editable: true,
    refresh: "30s",
    time: { from: "now-1h", to: "now" },
    templating: {
      list: (d.variables ?? []).map((v) => ({
        name: v.name,
        label: v.label,
        type: "query",
        query: { query: `label_values(${v.metric}, ${v.by})`, refId: "vars" },
        refresh: 2,
        includeAll: true,
        allValue: ".*",
        multi: false,
        current: { text: "All", value: "$__all" },
      })),
    },
    panels: d.panels.map((p, i) => {
      const stat = p.kind === "stat";
      const w = stat ? 6 : 12;
      const perRow = stat ? 4 : 2;
      const x = (i % perRow) * w;
      if (i > 0 && x === 0) y += stat ? 4 : 8;
      return {
        id: i + 1,
        type: stat ? "stat" : "timeseries",
        title: p.title,
        description: p.help ?? "",
        gridPos: { x, y, w, h: stat ? 4 : 8 },
        fieldConfig: { defaults: { unit: units[p.unit] }, overrides: [] },
        targets: p.queries.map((q, j) => ({
          refId: String.fromCharCode(65 + j),
          expr: q.expr,
          legendFormat: (q.legend ?? "").replace(/\{(\w+)\}/g, "{{$1}}"),
        })),
      };
    }),
  };
}
