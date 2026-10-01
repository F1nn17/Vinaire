import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { apiFetch } from "./http";
import { buildStreamURL } from "./wsUrl";

// Roadmap Phase 30 — adding worker nodes over SSH. The backend's rules (see
// internal/api/provision.go): credentials go in a POST body only (never a URL),
// over HTTPS, and the SSH host key fingerprint must be fetched, confirmed by
// the user and echoed back before anything is sent.

export type HostKey = {
  fingerprint: string;
  keyType: string;
  address: string;
};

export type SshAuth =
  | { type: "key"; privateKey: string; passphrase?: string }
  | { type: "password"; password: string };

export type SshTarget = {
  host: string;
  port: number;
  username: string;
  auth: SshAuth;
  hostKeyFingerprint: string;
};

export type ProvisionRequest = {
  provider: "k3s";
  node: SshTarget;
  controlPlane?: SshTarget;
  serverUrl?: string;
  token?: string;
  version?: string;
  nodeName?: string;
};

export type ProvisionEvent = {
  seq: number;
  time: string;
  kind: "log" | "step" | "status";
  text: string;
  status?: "running" | "succeeded" | "failed";
};

export type ProvisionSnapshot = {
  id: string;
  provider: string;
  host: string;
  status: "running" | "succeeded" | "failed";
  error?: string;
  nodeName?: string;
  events: ProvisionEvent[];
};

export type ProvisionDefaults = {
  serverUrl: string;
  k3sVersion: string;
};

export function useProvisionDefaults() {
  const cluster = useCurrentCluster();
  return useSWR(["provision-defaults", cluster], () =>
    apiFetch<ProvisionDefaults>("/api/v1/nodes/provision/defaults"),
  );
}

export function fetchHostKey(host: string, port: number): Promise<HostKey> {
  return apiFetch<HostKey>("/api/v1/nodes/provision/hostkey", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ host, port }),
  });
}

export function startProvision(
  req: ProvisionRequest,
): Promise<{ jobId: string }> {
  return apiFetch<{ jobId: string }>("/api/v1/nodes/provision", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
}

export function getProvisionJob(id: string): Promise<ProvisionSnapshot> {
  return apiFetch<ProvisionSnapshot>(
    `/api/v1/nodes/provision/${encodeURIComponent(id)}`,
  );
}

export function provisionStreamUrl(id: string, afterSeq: number): string {
  return buildStreamURL(
    `/api/v1/nodes/provision/${encodeURIComponent(id)}/stream?after=${afterSeq}`,
  );
}
