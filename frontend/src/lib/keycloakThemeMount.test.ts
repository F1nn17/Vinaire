import assert from "node:assert/strict";
import test from "node:test";
import { parse } from "yaml";
import { themeMounted, withThemeMounted } from "./keycloakThemeMount.ts";

// Roadmap Phase 91: a Keycloak installed before the theme existed gets the theme mounted by editing its workload.

const sts = `
apiVersion: apps/v1
kind: StatefulSet
metadata: { name: keycloak, namespace: keycloak }
spec:
  template:
    spec:
      containers:
        - name: sidecar
          image: busybox
        - name: app
          image: quay.io/keycloak/keycloak:26.4.0
          volumeMounts:
            - { name: data, mountPath: /opt/keycloak/data }
      volumes:
        - { name: data, emptyDir: {} }
`;

test("the theme volume and mount go into the Keycloak container, next to what is already there", () => {
  const r = withThemeMounted(sts);
  assert.ok(r.ok && r.changed);
  const doc = parse(r.yaml);
  const pod = doc.spec.template.spec;
  assert.equal(pod.volumes.length, 2);
  assert.equal(pod.volumes[1].configMap.name, "keycloak-vinaire-theme");
  assert.ok(pod.volumes[1].configMap.items.length >= 2);
  const kc = pod.containers[1];
  assert.equal(kc.volumeMounts.length, 2);
  assert.equal(kc.volumeMounts[1].mountPath, "/opt/keycloak/themes/vinaire");
  assert.equal(kc.volumeMounts[1].readOnly, true);
  assert.equal(pod.containers[0].volumeMounts, undefined); // the other container is left alone
});

test("a workload that already has the theme is left exactly as it is", () => {
  const once = withThemeMounted(sts);
  assert.ok(once.ok);
  assert.equal(themeMounted(once.yaml), true);
  const twice = withThemeMounted(once.yaml);
  assert.ok(twice.ok && !twice.changed);
  assert.equal(twice.yaml, once.yaml);
  assert.equal(themeMounted(sts), false);
});

test("a Deployment works, a single container is the Keycloak one, and anything else is refused with a reason", () => {
  const dep = withThemeMounted(
    "kind: Deployment\nspec:\n  template:\n    spec:\n      containers:\n        - { name: only, image: my/idp:1 }\n",
  );
  assert.ok(dep.ok && dep.changed);
  for (const bad of [
    "kind: Pod\nspec: {}",
    "kind: StatefulSet\nspec:\n  template:\n    spec:\n      containers: []\n",
    "kind: StatefulSet\nspec:\n  template:\n    spec:\n      containers:\n        - { name: a, image: x }\n        - { name: b, image: y }\n",
    "{ not: [yaml",
    "",
  ]) {
    const r = withThemeMounted(bad);
    assert.equal(r.ok, false, bad);
    assert.ok(!r.ok && r.problem.length > 0);
  }
});
