import assert from "node:assert/strict";
import test from "node:test";
import {
  backendLabel,
  describeIngress,
  hostHasTLS,
  routeUrl,
} from "./ingress.ts";

// Roadmap Phase 89: what the Ingress page shows is read from the object's YAML, which is cluster data and so untrusted.

const keycloak = `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: keycloak
  namespace: keycloak-vinaire
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: '{"huge":"json"}'
    traefik.ingress.kubernetes.io/router.entrypoints: websecure
spec:
  ingressClassName: traefik
  tls:
    - hosts: [kc-vinaire.home]
      secretName: kc-tls
  rules:
    - host: kc-vinaire.home
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service: { name: keycloak, port: { number: 8080 } }
          - path: /admin
            pathType: Exact
            backend:
              service: { name: keycloak-admin, port: { name: http } }
    - host: plain.home
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              resource: { apiGroup: example.com, kind: Bucket, name: files }
  defaultBackend:
    service: { name: fallback, port: { number: 80 } }
status:
  loadBalancer:
    ingress:
      - ip: 192.168.88.10
      - hostname: lb.example.net
`;

test("the rules, the TLS block, the class and the addresses are read out", () => {
  const i = describeIngress(keycloak);
  assert.ok(i);
  assert.equal(i.className, "traefik");
  assert.deepEqual(i.addresses, ["192.168.88.10", "lb.example.net"]);
  assert.equal(i.routes.length, 3);
  assert.deepEqual(i.routes[0], {
    host: "kc-vinaire.home",
    path: "/",
    pathType: "Prefix",
    backend: { service: { name: "keycloak", port: "8080" } },
  });
  assert.equal(i.routes[1].backend.service?.port, "http"); // a named port
  assert.equal(i.routes[2].backend.resource, "example.com/Bucket/files");
  assert.deepEqual(i.tls, [
    { hosts: ["kc-vinaire.home"], secretName: "kc-tls" },
  ]);
  assert.equal(i.defaultBackend?.service?.name, "fallback");
  assert.ok(
    !("kubectl.kubernetes.io/last-applied-configuration" in i.annotations),
  );
  assert.equal(
    i.annotations["traefik.ingress.kubernetes.io/router.entrypoints"],
    "websecure",
  );
});

test("an address is https only for a host in a TLS block, and has no address without a host", () => {
  const i = describeIngress(keycloak)!;
  assert.equal(routeUrl(i.routes[0], i.tls), "https://kc-vinaire.home");
  assert.equal(routeUrl(i.routes[1], i.tls), "https://kc-vinaire.home/admin");
  assert.equal(routeUrl(i.routes[2], i.tls), "http://plain.home");
  assert.equal(
    routeUrl({ host: "", path: "/", pathType: "Prefix", backend: {} }, []),
    "",
  );
  assert.equal(
    routeUrl(
      { host: "*.home", path: "/", pathType: "Prefix", backend: {} },
      [],
    ),
    "",
  );
  // a TLS entry without hosts covers every host
  assert.ok(hostHasTLS("anything.home", [{ hosts: [], secretName: "s" }]));
  assert.ok(!hostHasTLS("anything.home", []));
});

test("the legacy class annotation counts when spec.ingressClassName is absent", () => {
  const i = describeIngress(`
kind: Ingress
metadata:
  annotations: { kubernetes.io/ingress.class: nginx }
spec: {}
`);
  assert.equal(i?.className, "nginx");
  assert.deepEqual(i?.routes, []);
  assert.deepEqual(i?.tls, []);
  assert.equal(i?.defaultBackend, undefined);
});

test("what is not an Ingress, or not YAML, is refused", () => {
  assert.equal(describeIngress("kind: Service\nspec: {}"), null);
  assert.equal(describeIngress("{ not: [valid"), null);
  assert.equal(describeIngress(""), null);
});

test("hostile or odd values never throw and are bounded", () => {
  const rules = Array.from(
    { length: 500 },
    (_, n) => `
    - host: h${n}.example.com
      http: { paths: [ { path: /, pathType: Prefix, backend: { service: { name: s, port: { number: 80 } } } } ] }`,
  ).join("");
  const big = describeIngress(`kind: Ingress\nspec:\n  rules:${rules}\n`);
  assert.equal(big?.routes.length, 200);
  const odd = describeIngress(`
kind: Ingress
metadata: { annotations: { a: [1, 2], b: { c: 1 } } }
spec:
  rules: "nope"
  tls: [ 7, null, { hosts: "x", secretName: [1] } ]
  defaultBackend: 5
status: { loadBalancer: { ingress: [ 1, { ip: 5 } ] } }
`);
  assert.ok(odd);
  assert.deepEqual(odd.routes, []);
  assert.equal(odd.tls.length, 3);
  assert.equal(odd.defaultBackend, undefined);
  assert.equal(odd.annotations.a, "");
  // a value that looks like markup is just text here — it is rendered as text by the page
  const xss = describeIngress(
    `kind: Ingress\nspec:\n  rules:\n    - host: "<img src=x onerror=alert(1)>"\n`,
  );
  assert.deepEqual(xss?.routes, []);
});

test("a backend is labelled name:port", () => {
  assert.equal(
    backendLabel({ service: { name: "web", port: "8080" } }),
    "web:8080",
  );
  assert.equal(backendLabel({ service: { name: "web", port: "" } }), "web");
  assert.equal(backendLabel({ resource: "g/K/n" }), "g/K/n");
  assert.equal(backendLabel({}), "");
});
