import assert from "node:assert/strict";
import test from "node:test";
import { pickKeycloak } from "./keycloakPick.ts";

// Roadmap Phase 106: the theme button changes one Keycloak, never all that were found.

const dev = { namespace: "dev", name: "keycloak", kind: "StatefulSet" };
const prod = { namespace: "prod", name: "keycloak", kind: "StatefulSet" };

test("an in-cluster issuer picks the matching instance only", () => {
  const p = pickKeycloak(
    [dev, prod],
    "http://keycloak.dev.svc.cluster.local:8080/realms/vinaire",
  );
  assert.deepEqual(p.target, dev);
  assert.equal(p.reason, "issuer");
});

test("two Keycloaks and a public issuer: nothing is chosen for the person", () => {
  const p = pickKeycloak([dev, prod], "https://sso.example.com/realms/vinaire");
  assert.equal(p.target, null);
  assert.deepEqual(p.choices, [dev, prod]);
});

test("a single Keycloak that the issuer does not point at is not assumed", () => {
  const p = pickKeycloak([dev], "https://sso.example.com/realms/vinaire");
  assert.equal(p.target, null);
  assert.deepEqual(p.choices, [dev]);
  assert.equal(p.reason, "choose");
  assert.equal(pickKeycloak([dev], undefined).target, null);
});

test("no Keycloak, no target; a bad issuer matches nothing", () => {
  assert.equal(pickKeycloak([], "x").reason, "none");
  assert.equal(pickKeycloak([dev, prod], "not a url").target, null);
});

test("a host that merely contains the name does not match", () => {
  const p = pickKeycloak(
    [dev, prod],
    "https://keycloak.dev.evil.example/realms/x",
  );
  assert.equal(p.target, null);
});
