import { stringify } from "yaml";
import { newForm, presets, type Form } from "./images/presets";

// Keycloak for Vinaire's own sign-in, in one click: a PostgreSQL and a Keycloak from the "Install
// from an image" presets, wired together, plus an Ingress to reach it. This file only *describes*
// the objects; they are created through the generic YAML create (Phase 25) as the signed-in user —
// no write path of its own. Passwords are generated in the browser and written only into the
// instances' Secrets.

export const KC_NAME = "keycloak";
export const DB_NAME = "keycloak-db";

export type KeycloakInstallInput = {
  namespace: string;
  host: string;
  ingressClass: string;
  // "none": plain http; "default": https with the ingress controller's own certificate; "secret":
  // https with the certificate in `tlsSecret` (a kubernetes.io/tls Secret in the namespace)
  tls: "none" | "default" | "secret";
  tlsSecret: string;
  tag: string;
  dbSize: string;
};

export const nsRe = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/;
// a DNS name with at least two labels, as typed in a browser's address bar
export const hostRe = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?(\.[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?)+$/;
export const classNameRe = /^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$/;
export const sizeRe = /^[1-9][0-9]{0,5}(Mi|Gi|Ti)$/;
export const tagRe = /^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$/;

// What is wrong with the input, by field — empty when it can be applied.
export function validateInstall(i: KeycloakInstallInput): Record<string, string> {
  const e: Record<string, string> = {};
  if (!nsRe.test(i.namespace)) e.namespace = "A valid namespace name.";
  if (!hostRe.test(i.host)) e.host = "A DNS name such as keycloak.example.com.";
  if (i.ingressClass && !classNameRe.test(i.ingressClass))
    e.ingressClass = "A valid class name.";
  if (i.tls === "secret" && !classNameRe.test(i.tlsSecret))
    e.tlsSecret = "A valid Secret name.";
  if (!tagRe.test(i.tag)) e.tag = "A version, e.g. 26.2.5.";
  if (!sizeRe.test(i.dbSize)) e.dbSize = "A size like 5Gi.";
  return e;
}

function preset(id: string) {
  const p = presets.find((x) => x.id === id);
  if (!p) throw new Error(`no preset ${id}`);
  return p;
}

// the database: its own PostgreSQL, one volume
export function databaseForm(i: KeycloakInstallInput): Form {
  return {
    ...newForm(preset("postgres")),
    name: DB_NAME,
    namespace: i.namespace,
    values: { user: "keycloak", database: "keycloak" },
    size: i.dbSize,
  };
}

// Keycloak itself, pointed at that database through its Secret (key "password")
export function keycloakForm(i: KeycloakInstallInput): Form {
  return {
    ...newForm(preset("keycloak")),
    name: KC_NAME,
    namespace: i.namespace,
    tag: i.tag,
    values: {
      dbHost: DB_NAME,
      dbPort: "5432",
      dbName: "keycloak",
      dbUser: "keycloak",
      dbSecret: DB_NAME,
      dbSecretKey: "password",
    },
  };
}

// The Ingress that publishes Keycloak's Service on `host`; it belongs to the Keycloak StatefulSet,
// so deleting that removes it.
export function ingressYaml(i: KeycloakInstallInput, ownerUid: string): string {
  return stringify({
    apiVersion: "networking.k8s.io/v1",
    kind: "Ingress",
    metadata: {
      name: KC_NAME,
      namespace: i.namespace,
      labels: {
        "app.kubernetes.io/name": "keycloak",
        "app.kubernetes.io/instance": KC_NAME,
        "app.kubernetes.io/managed-by": "vinaire",
      },
      ownerReferences: [
        { apiVersion: "apps/v1", kind: "StatefulSet", name: KC_NAME, uid: ownerUid },
      ],
    },
    spec: {
      ...(i.ingressClass ? { ingressClassName: i.ingressClass } : {}),
      ...(i.tls !== "none"
        ? {
            tls: [
              {
                hosts: [i.host],
                ...(i.tls === "secret" ? { secretName: i.tlsSecret } : {}),
              },
            ],
          }
        : {}),
      rules: [
        {
          host: i.host,
          http: {
            paths: [
              {
                path: "/",
                pathType: "Prefix",
                backend: { service: { name: KC_NAME, port: { number: 8080 } } },
              },
            ],
          },
        },
      ],
    },
  });
}

// where Vinaire and the browser reach it
export function publicUrl(i: KeycloakInstallInput): string {
  return `${i.tls !== "none" ? "https" : "http"}://${i.host}`;
}
