import { stringify } from "yaml";
import { KEYCLOAK_THEME_FILES } from "./keycloakThemeFiles.ts";

// Keycloak's sign-in page in Vinaire's look (Roadmap Phase 85b): a theme named "vinaire", handed out here as
// a zip (for any Keycloak: unpack it into its `themes` folder) or as a ConfigMap mounted into the Keycloak
// that Vinaire installs itself. The files come from keycloak/theme/vinaire (scripts/gen-keycloak-theme.mjs).
// It restyles Keycloak's own pages: the forms, passkeys, 2-step codes and password reset stay Keycloak's.

export const THEME_NAME = "vinaire";
export const THEME_CONFIGMAP = "keycloak-vinaire-theme";

// a ConfigMap key cannot hold a slash, so each file gets a flat key and is mounted back at its path
function flatKey(path: string): string {
  return path.replace(/\//g, "__");
}

export function themeConfigMapYaml(namespace: string): string {
  const data: Record<string, string> = {};
  for (const f of KEYCLOAK_THEME_FILES) data[flatKey(f.path)] = f.content;
  return stringify({
    apiVersion: "v1",
    kind: "ConfigMap",
    metadata: {
      name: THEME_CONFIGMAP,
      namespace,
      labels: {
        "app.kubernetes.io/name": "keycloak",
        "app.kubernetes.io/component": "login-theme",
        "app.kubernetes.io/managed-by": "vinaire",
      },
    },
    data,
  });
}

// What a Keycloak StatefulSet's pod needs to see the theme at /opt/keycloak/themes/vinaire.
export function themeVolume(): {
  volume: Record<string, unknown>;
  mount: Record<string, unknown>;
} {
  return {
    volume: {
      name: "vinaire-theme",
      configMap: {
        name: THEME_CONFIGMAP,
        items: KEYCLOAK_THEME_FILES.map((f) => ({
          key: flatKey(f.path),
          path: f.path,
        })),
      },
    },
    mount: {
      name: "vinaire-theme",
      mountPath: `/opt/keycloak/themes/${THEME_NAME}`,
      readOnly: true,
    },
  };
}

// ---- a zip, made in the browser (stored, not compressed: the files are tiny) -----------------------

const crcTable = (() => {
  const t = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    t[n] = c >>> 0;
  }
  return t;
})();

export function crc32(bytes: Uint8Array): number {
  let c = 0xffffffff;
  for (const b of bytes) c = crcTable[(c ^ b) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

// the zip holds `vinaire/<path>`, so unpacking it into Keycloak's themes folder gives themes/vinaire/…
export function themeZip(): Uint8Array {
  const enc = new TextEncoder();
  const parts: Uint8Array[] = [];
  const central: Uint8Array[] = [];
  let offset = 0;
  const u16 = (v: number) => [v & 0xff, (v >>> 8) & 0xff];
  const u32 = (v: number) => [
    v & 0xff,
    (v >>> 8) & 0xff,
    (v >>> 16) & 0xff,
    (v >>> 24) & 0xff,
  ];
  for (const f of KEYCLOAK_THEME_FILES) {
    const name = enc.encode(`${THEME_NAME}/${f.path}`);
    const data = enc.encode(f.content);
    const crc = crc32(data);
    // 0x0800: the names are UTF-8; date/time are fixed (1980-01-01) so the zip is the same every time
    const local = new Uint8Array([
      ...u32(0x04034b50),
      ...u16(20),
      ...u16(0x0800),
      ...u16(0),
      ...u16(0),
      ...u16(0x21),
      ...u32(crc),
      ...u32(data.length),
      ...u32(data.length),
      ...u16(name.length),
      ...u16(0),
      ...name,
    ]);
    parts.push(local, data);
    central.push(
      new Uint8Array([
        ...u32(0x02014b50),
        ...u16(20),
        ...u16(20),
        ...u16(0x0800),
        ...u16(0),
        ...u16(0),
        ...u16(0x21),
        ...u32(crc),
        ...u32(data.length),
        ...u32(data.length),
        ...u16(name.length),
        ...u16(0),
        ...u16(0),
        ...u16(0),
        ...u16(0),
        ...u32(0),
        ...u32(offset),
        ...name,
      ]),
    );
    offset += local.length + data.length;
  }
  const centralSize = central.reduce((n, c) => n + c.length, 0);
  const end = new Uint8Array([
    ...u32(0x06054b50),
    ...u16(0),
    ...u16(0),
    ...u16(central.length),
    ...u16(central.length),
    ...u32(centralSize),
    ...u32(offset),
    ...u16(0),
  ]);
  const all = [...parts, ...central, end];
  const out = new Uint8Array(all.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of all) {
    out.set(p, at);
    at += p.length;
  }
  return out;
}
