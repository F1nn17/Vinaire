import type { SecretDetail } from "@/lib/types";

// Values below are base64-shaped placeholders for the mockup, not real secrets.
export const secretsMock: SecretDetail[] = [
  {
    id: "sec1",
    name: "keycloak-db-creds",
    namespace: "auth",
    type: "Opaque",
    age: "14d",
    keys: [
      { id: "username", label: "username", value: "a2V5Y2xvYWs=" },
      { id: "password", label: "password", value: "UDRzc3cwcmQhMjAyNg==" },
    ],
  },
  {
    id: "sec2",
    name: "argocd-repo-creds",
    namespace: "gitops",
    type: "Opaque",
    age: "22d",
    keys: [
      {
        id: "url",
        label: "url",
        value: "aHR0cHM6Ly9naXRodWIuY29tL3lvdS9yZXBv",
      },
      {
        id: "sshKey",
        label: "sshPrivateKey",
        value: "LS0tLS1CRUdJTi1SU0EtS0VZLS0tLS0=",
      },
    ],
  },
  {
    id: "sec3",
    name: "tls-wildcard-cert",
    namespace: "network",
    type: "kubernetes.io/tls",
    age: "44d",
    keys: [
      {
        id: "crt",
        label: "tls.crt",
        value: "LS0tLS1CRUdJTi1DRVJUSUZJQ0FURS0tLS0t",
      },
      {
        id: "key",
        label: "tls.key",
        value: "LS0tLS1CRUdJTi1QUklWQVRFLUtFWS0tLS0t",
      },
    ],
  },
  {
    id: "sec4",
    name: "registry-pull-secret",
    namespace: "default",
    type: "kubernetes.io/dockerconfigjson",
    age: "30d",
    keys: [
      {
        id: "dockercfg",
        label: ".dockerconfigjson",
        value: "eyJhdXRocyI6eyJnaGNyLmlvIjp7fX19",
      },
    ],
  },
];
