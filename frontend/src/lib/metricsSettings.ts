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
  volumeSize: string; // e.g. "20Gi"; empty = leave the volume alone
};

const sizeRe = /^([1-9][0-9]{0,4})(Mi|Gi|Ti)$/;

// Mebibytes of a size like "10Gi", or null if it isn't one of the shapes the
// backend accepts.
export function toMiB(size: string): number | null {
  const m = size.match(sizeRe);
  if (!m) return null;
  const n = Number(m[1]);
  return m[2] === "Mi" ? n : m[2] === "Gi" ? n * 1024 : n * 1024 * 1024;
}

// A claim can only grow: shrinking one would lose data, and Kubernetes refuses it.
export function volumeChangeError(
  current: string,
  next: string,
): string | null {
  if (next === "" || next === current) return null;
  const a = toMiB(current);
  const b = toMiB(next);
  if (b === null) return "The volume size must look like 20Gi.";
  if (a !== null && b < a)
    return "A volume can only grow — shrinking it would lose data.";
  return null;
}

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
    volumeSize: String(
      doc.getIn(
        profile === "full"
          ? [
              "prometheus",
              "prometheusSpec",
              "storageSpec",
              "volumeClaimTemplate",
              "spec",
              "resources",
              "requests",
              "storage",
            ]
          : ["server", "persistentVolume", "size"],
      ) ?? "",
    ),
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
  if (s.volumeSize !== "") {
    const path =
      profile === "full"
        ? [
            "prometheus",
            "prometheusSpec",
            "storageSpec",
            "volumeClaimTemplate",
            "spec",
            "resources",
            "requests",
            "storage",
          ]
        : ["server", "persistentVolume", "size"];
    if (doc.hasIn(path)) doc.setIn(path, s.volumeSize);
  }
  const sel = (path: (string | number)[]) => {
    if (s.nodeSelector === "") {
      if (doc.hasIn(path)) doc.deleteIn(path);
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
      if (doc.hasIn(["alertmanager", "nodeSelector"]))
        doc.deleteIn(["alertmanager", "nodeSelector"]);
    }
  }
  return doc.toString();
}
