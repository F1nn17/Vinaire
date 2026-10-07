import { parseDocument } from "yaml";

// Changing the settings of a metrics stack Vinaire installed (Roadmap Phase 37) is a
// Helm upgrade of that release with its own values edited in place — so anything the
// user changed by hand, and the volume the data lives on, are kept. These are the
// places the two charts read each setting from.

export type StackProfile = "standard" | "full";

export type StackSettings = {
  retentionDays: number;
  nodeSelector: string; // "key=value", empty = anywhere
  alertmanager: boolean; // standard only; the full profile always has it
};

export function readSettings(
  valuesYaml: string,
  profile: StackProfile,
): StackSettings {
  const doc = parseDocument(valuesYaml || "{}");
  const ret = String(
    doc.getIn(
      profile === "full"
        ? ["prometheus", "prometheusSpec", "retention"]
        : ["server", "retention"],
    ) ?? "",
  );
  const m = ret.match(/^(\d+)d$/);
  const sel = doc.getIn(
    profile === "full"
      ? ["prometheus", "prometheusSpec", "nodeSelector"]
      : ["server", "nodeSelector"],
  );
  const selObj =
    sel && typeof (sel as { toJSON?: () => unknown }).toJSON === "function"
      ? ((sel as { toJSON: () => Record<string, string> }).toJSON() ?? {})
      : {};
  const k = Object.keys(selObj)[0];
  return {
    retentionDays: m ? Number(m[1]) : 7,
    nodeSelector: k ? `${k}=${selObj[k]}` : "",
    alertmanager:
      profile === "full" || doc.getIn(["alertmanager", "enabled"]) === true,
  };
}

// The same strict shapes the backend accepts (it re-validates; this only keeps the
// form honest before a request is made).
const keyRe =
  /^([a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?\/)?[A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$/;
const valRe = /^([A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?)?$/;

export function validSelector(sel: string): boolean {
  if (sel === "") return true;
  const i = sel.indexOf("=");
  return i > 0 && keyRe.test(sel.slice(0, i)) && valRe.test(sel.slice(i + 1));
}

export function applySettings(
  valuesYaml: string,
  profile: StackProfile,
  s: StackSettings,
): string {
  if (
    !Number.isInteger(s.retentionDays) ||
    s.retentionDays < 1 ||
    s.retentionDays > 365
  )
    throw new Error("Retention must be 1–365 days.");
  if (!validSelector(s.nodeSelector))
    throw new Error("The node selector must look like key=value.");
  const doc = parseDocument(valuesYaml || "{}");
  const sel = (path: (string | number)[]) => {
    if (s.nodeSelector === "") {
      doc.deleteIn(path);
    } else {
      const i = s.nodeSelector.indexOf("=");
      doc.setIn(path, {
        [s.nodeSelector.slice(0, i)]: s.nodeSelector.slice(i + 1),
      });
    }
  };
  if (profile === "full") {
    doc.setIn(
      ["prometheus", "prometheusSpec", "retention"],
      `${s.retentionDays}d`,
    );
    sel(["prometheus", "prometheusSpec", "nodeSelector"]);
    sel(["alertmanager", "alertmanagerSpec", "nodeSelector"]);
  } else {
    doc.setIn(["server", "retention"], `${s.retentionDays}d`);
    sel(["server", "nodeSelector"]);
    sel(["kube-state-metrics", "nodeSelector"]);
    if (s.alertmanager) {
      doc.setIn(["alertmanager", "enabled"], true);
      if (!doc.hasIn(["alertmanager", "persistence"]))
        doc.setIn(["alertmanager", "persistence", "enabled"], false);
      sel(["alertmanager", "nodeSelector"]);
    } else {
      doc.setIn(["alertmanager", "enabled"], false);
      doc.deleteIn(["alertmanager", "nodeSelector"]);
    }
  }
  return doc.toString();
}
