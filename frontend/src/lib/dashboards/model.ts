// Dashboards (Roadmap Phase 68): a dashboard is a list of panels, each one a PromQL query. They are
// drawn by Vinaire from the cluster's Prometheus — the same read-as-the-caller proxy the Metrics tab
// uses — so no Grafana is needed. The built-in ones live in catalog.ts; a person's own are stored as a
// ConfigMap (store.ts) and can be exported as a Grafana dashboard (grafana.ts).

export type PanelUnit =
  | "short"
  | "percent" // 0–100
  | "percentunit" // 0–1
  | "bytes"
  | "bytesps"
  | "seconds"
  | "ops";

export type PanelKind = "line" | "stat";

export type Panel = {
  title: string;
  kind: PanelKind;
  unit: PanelUnit;
  // One or more queries drawn together; `legend` names the series of a query that returns one,
  // `{label}` in it is replaced by that label's value for a query that returns many.
  queries: { expr: string; legend?: string }[];
  help?: string;
};

// A choice the person makes once for the whole dashboard (a node, a namespace): the options are the
// values of `label` on `metric`, and `$name` in a query is replaced by the choice.
export type DashboardVariable = {
  name: string; // a-z, used as $name in queries
  label: string; // what it is called on the page
  metric: string; // a metric that carries the label
  by: string; // the label whose values are the options
};

export type Dashboard = {
  id: string;
  title: string;
  description?: string;
  // The built-in group it is listed under; custom dashboards are all in "Custom".
  group: string;
  // A metric that has to exist for the dashboard to make sense; without it the page says what is
  // missing instead of drawing empty charts.
  requires?: { metric: string; hint: string };
  variables?: DashboardVariable[];
  panels: Panel[];
};

export const MAX_PANELS = 24;
export const MAX_QUERIES = 6;
export const MAX_EXPR = 2000;

export const variableNameRe = /^[a-z][a-z0-9_]{0,23}$/;
export const labelRe = /^[a-zA-Z_][a-zA-Z0-9_]{0,63}$/;
export const metricRe = /^[a-zA-Z_:][a-zA-Z0-9_:]{0,127}$/;
export const dashboardIdRe = /^[a-z0-9]([-a-z0-9]{0,40}[a-z0-9])?$/;
// what a variable's value may be: enough for a node address, a namespace or a pod name
export const variableValueRe = /^[A-Za-z0-9_.:/-]{1,200}$/;

// A PromQL string literal for the regex matcher: the value is validated first, and its dots (the only
// regex character it can hold) are escaped for the string and the regex both.
export function regexLiteral(value: string): string {
  return value.replace(/\./g, "\\\\.");
}

// Puts the person's choices into a query: `$instance` becomes the escaped value, or `.*` for "all".
// A variable the dashboard doesn't declare is left alone, so a `$` in a custom query survives.
export function substitute(
  expr: string,
  declared: string[],
  chosen: Record<string, string>,
): string {
  return expr.replace(/\$([a-z][a-z0-9_]*)/g, (whole, name: string) => {
    if (!declared.includes(name)) return whole;
    const v = chosen[name];
    return v && variableValueRe.test(v) ? regexLiteral(v) : ".*";
  });
}

// What is wrong with a dashboard someone wrote or loaded, or "" when it is fine.
export function validateDashboard(d: Dashboard): string {
  if (!dashboardIdRe.test(d.id)) return "The name must be lower-case letters, digits and dashes.";
  if (!d.title.trim() || d.title.length > 80) return "A dashboard needs a title (up to 80 characters).";
  if (d.panels.length === 0) return "Add at least one panel.";
  if (d.panels.length > MAX_PANELS) return "Too many panels.";
  const vars = d.variables ?? [];
  for (const v of vars) {
    if (!variableNameRe.test(v.name) || !metricRe.test(v.metric) || !labelRe.test(v.by)) {
      return "A variable has an invalid name, metric or label.";
    }
  }
  for (const p of d.panels) {
    if (!p.title.trim() || p.title.length > 80) return "Every panel needs a title.";
    if (p.queries.length === 0 || p.queries.length > MAX_QUERIES) return "A panel needs 1–6 queries.";
    for (const q of p.queries) {
      if (!q.expr.trim() || q.expr.length > MAX_EXPR) return "Every query needs an expression (up to 2000 characters).";
    }
  }
  return "";
}
